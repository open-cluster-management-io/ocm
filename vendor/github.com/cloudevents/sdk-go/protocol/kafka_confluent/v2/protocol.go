/*
 Copyright 2023 The CloudEvents Authors
 SPDX-License-Identifier: Apache-2.0
*/

package kafka_confluent

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sync"

	"github.com/cloudevents/sdk-go/v2/binding"
	"github.com/cloudevents/sdk-go/v2/protocol"
	"github.com/confluentinc/confluent-kafka-go/v2/kafka"

	cecontext "github.com/cloudevents/sdk-go/v2/context"
)

var (
	_ protocol.Sender   = (*Protocol)(nil)
	_ protocol.Opener   = (*Protocol)(nil)
	_ protocol.Receiver = (*Protocol)(nil)
	_ protocol.Closer   = (*Protocol)(nil)
)

type Protocol struct {
	kafkaConfigMap *kafka.ConfigMap

	consumer             *kafka.Consumer
	consumerTopics       []string
	consumerRebalanceCb  kafka.RebalanceCb                          // optional
	consumerPollTimeout  int                                        // optional
	consumerErrorHandler func(ctx context.Context, err kafka.Error) // optional
	consumerMux          sync.Mutex
	consumerIncoming     chan *kafka.Message
	consumerCtx          context.Context
	consumerCancel       context.CancelFunc

	producer             *kafka.Producer
	producerDefaultTopic string // optional

	closerMux sync.Mutex
}

func New(opts ...Option) (*Protocol, error) {
	p := &Protocol{
		consumerPollTimeout: 100,
		consumerIncoming:    make(chan *kafka.Message),
	}
	if err := p.applyOptions(opts...); err != nil {
		return nil, err
	}

	if p.kafkaConfigMap != nil {
		if p.consumerTopics != nil && p.consumer == nil {
			consumer, err := kafka.NewConsumer(p.kafkaConfigMap)
			if err != nil {
				return nil, err
			}
			p.consumer = consumer
		}
		if p.producerDefaultTopic != "" && p.producer == nil {
			producer, err := kafka.NewProducer(p.kafkaConfigMap)
			if err != nil {
				return nil, err
			}
			p.producer = producer
		}
		if p.producer == nil && p.consumer == nil {
			return nil, errors.New("at least receiver or sender topic must be set")
		}
	}
	if p.producerDefaultTopic != "" && p.producer == nil {
		return nil, fmt.Errorf("at least configmap or producer must be set for the sender topic: %s", p.producerDefaultTopic)
	}

	if len(p.consumerTopics) > 0 && p.consumer == nil {
		return nil, fmt.Errorf("at least configmap or consumer must be set for the receiver topics: %s", p.consumerTopics)
	}

	if p.kafkaConfigMap == nil && p.producer == nil && p.consumer == nil {
		return nil, errors.New("at least one of the following to initialize the protocol must be set: config, producer, or consumer")
	}
	return p, nil
}

// Events returns the events channel used by Confluent Kafka to deliver the result from a produce, i.e., send, operation.
// When using this SDK to produce (send) messages, this channel must be monitored to avoid resource leaks and this channel becoming full. See Confluent SDK for Go for details on the implementation.
func (p *Protocol) Events() (chan kafka.Event, error) {
	if p.producer == nil {
		return nil, errors.New("producer not set")
	}
	return p.producer.Events(), nil
}

func (p *Protocol) applyOptions(opts ...Option) error {
	for _, fn := range opts {
		if err := fn(p); err != nil {
			return err
		}
	}
	return nil
}

// Send message by kafka.Producer. You must monitor the Events() channel when using this function.
func (p *Protocol) Send(ctx context.Context, in binding.Message, transformers ...binding.Transformer) (err error) {
	if p.producer == nil {
		return errors.New("producer client must be set")
	}

	p.closerMux.Lock()
	defer p.closerMux.Unlock()
	if p.producer.IsClosed() {
		return errors.New("producer is closed")
	}

	defer in.Finish(err)

	kafkaMsg := &kafka.Message{
		TopicPartition: kafka.TopicPartition{
			Topic:     &p.producerDefaultTopic,
			Partition: kafka.PartitionAny,
		},
	}

	if topic := cecontext.TopicFrom(ctx); topic != "" {
		kafkaMsg.TopicPartition.Topic = &topic
	}

	if messageKey := MessageKeyFrom(ctx); messageKey != "" {
		kafkaMsg.Key = []byte(messageKey)
	}

	if err = WriteProducerMessage(ctx, in, kafkaMsg, transformers...); err != nil {
		return fmt.Errorf("create producer message: %w", err)
	}

	if err = p.producer.Produce(kafkaMsg, nil); err != nil {
		return fmt.Errorf("produce message: %w", err)
	}
	return nil
}

func (p *Protocol) OpenInbound(ctx context.Context) error {
	if p.consumer == nil {
		return errors.New("the consumer client must be set")
	}
	if p.consumerTopics == nil {
		return errors.New("the consumer topics must be set")
	}

	p.consumerMux.Lock()
	defer p.consumerMux.Unlock()
	logger := cecontext.LoggerFrom(ctx)

	// Query committed offsets for each partition
	if positions := TopicPartitionOffsetsFrom(ctx); positions != nil {
		if err := p.consumer.Assign(positions); err != nil {
			return err
		}
	}

	logger.Infof("Subscribing to topics: %v", p.consumerTopics)
	err := p.consumer.SubscribeTopics(p.consumerTopics, p.consumerRebalanceCb)
	if err != nil {
		return err
	}

	p.closerMux.Lock()
	p.consumerCtx, p.consumerCancel = context.WithCancel(ctx)
	defer p.consumerCancel()
	p.closerMux.Unlock()

	defer func() {
		if !p.consumer.IsClosed() {
			logger.Infof("Closing consumer %v", p.consumerTopics)
			if err = p.consumer.Close(); err != nil {
				logger.Errorf("failed to close the consumer: %v", err)
			}
		}
		close(p.consumerIncoming)
	}()

	for {
		select {
		case <-p.consumerCtx.Done():
			return p.consumerCtx.Err()
		default:
			ev := p.consumer.Poll(p.consumerPollTimeout)
			if ev == nil {
				continue
			}
			switch e := ev.(type) {
			case *kafka.Message:
				p.consumerIncoming <- e
			case kafka.Error:
				// Errors should generally be considered informational, the client will try to automatically recover.
				// But in here, we choose to terminate the application if all brokers are down.
				logger.Infof("Error %v: %v", e.Code(), e)
				if p.consumerErrorHandler != nil {
					p.consumerErrorHandler(ctx, e)
				}
				if e.Code() == kafka.ErrAllBrokersDown {
					logger.Error("All broker connections are down")
					return e
				}
			}
		}
	}
}

// Receive implements Receiver.Receive
func (p *Protocol) Receive(ctx context.Context) (binding.Message, error) {
	select {
	case m, ok := <-p.consumerIncoming:
		if !ok {
			return nil, io.EOF
		}
		msg := NewMessage(m)
		return msg, nil
	case <-ctx.Done():
		return nil, io.EOF
	}
}

// Close cleans up resources after use. Must be called to properly close underlying Kafka resources and avoid resource leaks
func (p *Protocol) Close(ctx context.Context) error {
	p.closerMux.Lock()
	defer p.closerMux.Unlock()
	logger := cecontext.LoggerFrom(ctx)

	if p.consumerCancel != nil {
		p.consumerCancel()
	}

	if p.producer != nil && !p.producer.IsClosed() {
		// Flush and close the producer with a 10 seconds timeout (closes Events channel)
		for p.producer.Flush(10000) > 0 {
			logger.Info("Flushing outstanding messages")
		}
		p.producer.Close()
	}
	return nil
}
