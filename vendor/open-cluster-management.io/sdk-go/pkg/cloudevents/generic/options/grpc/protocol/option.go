package protocol

import (
	"fmt"
	"time"
)

// Option is the function signature
type Option func(*Protocol) error

// SubscribeOption
type SubscribeOption struct {
	Source      string
	ClusterName string
	DataType    string // data type for the client, eg. "io.open-cluster-management.works.v1alpha1.manifestbundles"
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

func WithReconnectErrorChan(errorChan chan error) Option {
	return func(p *Protocol) error {
		if errorChan == nil {
			return fmt.Errorf("the error channel must not be nil")
		}
		p.reconnectErrorChan = errorChan
		return nil
	}
}

func WithServerHealthinessTimeout(timeout *time.Duration) Option {
	return func(p *Protocol) error {
		if timeout != nil {
			if *timeout <= 0 {
				return fmt.Errorf("the server healthiness timeout must be greater than 0")
			}
			p.serverHealthinessTimeout = timeout
		}
		return nil
	}
}
