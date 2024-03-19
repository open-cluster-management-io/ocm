package protocol

import (
	"fmt"
)

// Option is the function signature
type Option func(*Protocol) error

// SubscribeOption
type SubscribeOption struct {
	Source string
}

// WithSubscribeOption sets the Subscribe configuration for the client.
func WithSubscribeOption(subscribeOpt *SubscribeOption) Option {
	return func(p *Protocol) error {
		if subscribeOpt == nil {
			return fmt.Errorf("the subscribe option must not be nil")
		}
		p.subscribeOption = subscribeOpt
		return nil
	}
}
