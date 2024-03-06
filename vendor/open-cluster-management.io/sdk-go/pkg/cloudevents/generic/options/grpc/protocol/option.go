package protocol

import (
	"fmt"
)

// Option is the function signature
type Option func(*Protocol) error

// PublishOption
type PublishOption struct {
	Topic string
}

// SubscribeOption
type SubscribeOption struct {
	Topics []string
}

// WithPublishOption sets the Publish configuration for the client. This option is required if you want to send messages.
func WithPublishOption(publishOpt *PublishOption) Option {
	return func(p *Protocol) error {
		if publishOpt == nil {
			return fmt.Errorf("the publish option must not be nil")
		}
		p.publishOption = publishOpt
		return nil
	}
}

// WithSubscribeOption sets the Subscribe configuration for the client. This option is required if you want to receive messages.
func WithSubscribeOption(subscribeOpt *SubscribeOption) Option {
	return func(p *Protocol) error {
		if subscribeOpt == nil {
			return fmt.Errorf("the subscribe option must not be nil")
		}
		p.subscribeOption = subscribeOpt
		return nil
	}
}
