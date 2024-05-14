/*
 Copyright 2023 The CloudEvents Authors
 SPDX-License-Identifier: Apache-2.0
*/

package kafka_confluent

import (
	"context"
	"errors"

	"github.com/confluentinc/confluent-kafka-go/v2/kafka"
)

// Option is the function signature required to be considered an kafka_confluent.Option.
type Option func(*Protocol) error

// WithConfigMap sets the configMap to init the kafka client.
func WithConfigMap(config *kafka.ConfigMap) Option {
	return func(p *Protocol) error {
		if config == nil {
			return errors.New("the kafka.ConfigMap option must not be nil")
		}
		p.kafkaConfigMap = config
		return nil
	}
}

// WithSenderTopic sets the defaultTopic for the kafka.Producer.
func WithSenderTopic(defaultTopic string) Option {
	return func(p *Protocol) error {
		if defaultTopic == "" {
			return errors.New("the producer topic option must not be nil")
		}
		p.producerDefaultTopic = defaultTopic
		return nil
	}
}

// WithReceiverTopics sets the topics for the kafka.Consumer.
func WithReceiverTopics(topics []string) Option {
	return func(p *Protocol) error {
		if topics == nil {
			return errors.New("the consumer topics option must not be nil")
		}
		p.consumerTopics = topics
		return nil
	}
}

// WithRebalanceCallBack sets the callback for rebalancing of the consumer group.
func WithRebalanceCallBack(rebalanceCb kafka.RebalanceCb) Option {
	return func(p *Protocol) error {
		if rebalanceCb == nil {
			return errors.New("the consumer group rebalance callback must not be nil")
		}
		p.consumerRebalanceCb = rebalanceCb
		return nil
	}
}

// WithPollTimeout sets timeout of the consumer polling for message or events, return nil on timeout.
func WithPollTimeout(timeoutMs int) Option {
	return func(p *Protocol) error {
		p.consumerPollTimeout = timeoutMs
		return nil
	}
}

// WithSender set a kafka.Producer instance to init the client directly.
func WithSender(producer *kafka.Producer) Option {
	return func(p *Protocol) error {
		if producer == nil {
			return errors.New("the producer option must not be nil")
		}
		p.producer = producer
		return nil
	}
}

// WithErrorHandler provide a func on how to handle the kafka.Error which the kafka.Consumer has polled.
func WithErrorHandler(handler func(ctx context.Context, err kafka.Error)) Option {
	return func(p *Protocol) error {
		p.consumerErrorHandler = handler
		return nil
	}
}

// WithReceiver set a kafka.Consumer instance to init the client directly.
func WithReceiver(consumer *kafka.Consumer) Option {
	return func(p *Protocol) error {
		if consumer == nil {
			return errors.New("the consumer option must not be nil")
		}
		p.consumer = consumer
		return nil
	}
}

// Opaque key type used to store topicPartitionOffsets: assign them from ctx.
type topicPartitionOffsetsType struct{}

var offsetKey = topicPartitionOffsetsType{}

// WithTopicPartitionOffsets will set the positions where the consumer starts consuming from.
func WithTopicPartitionOffsets(ctx context.Context, topicPartitionOffsets []kafka.TopicPartition) context.Context {
	if len(topicPartitionOffsets) == 0 {
		panic("the topicPartitionOffsets cannot be empty")
	}
	for _, offset := range topicPartitionOffsets {
		if offset.Topic == nil || *(offset.Topic) == "" {
			panic("the kafka topic cannot be nil or empty")
		}
		if offset.Partition < 0 || offset.Offset < 0 {
			panic("the kafka partition/offset must be non-negative")
		}
	}
	return context.WithValue(ctx, offsetKey, topicPartitionOffsets)
}

// TopicPartitionOffsetsFrom looks in the given context and returns []kafka.TopicPartition or nil if not set
func TopicPartitionOffsetsFrom(ctx context.Context) []kafka.TopicPartition {
	c := ctx.Value(offsetKey)
	if c != nil {
		if s, ok := c.([]kafka.TopicPartition); ok {
			return s
		}
	}
	return nil
}

// Opaque key type used to store message key
type messageKeyType struct{}

var keyForMessageKey = messageKeyType{}

// WithMessageKey returns back a new context with the given messageKey.
func WithMessageKey(ctx context.Context, messageKey string) context.Context {
	return context.WithValue(ctx, keyForMessageKey, messageKey)
}

// MessageKeyFrom looks in the given context and returns `messageKey` as a string if found and valid, otherwise "".
func MessageKeyFrom(ctx context.Context) string {
	c := ctx.Value(keyForMessageKey)
	if c != nil {
		if s, ok := c.(string); ok {
			return s
		}
	}
	return ""
}
