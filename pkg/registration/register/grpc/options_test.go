package grpc

import (
	"testing"
)

func TestValidate(t *testing.T) {
	cases := []struct {
		name        string
		opt         *Option
		expectedErr bool
	}{
		{
			name:        "no config file",
			opt:         NewOptions(),
			expectedErr: true,
		},
		{
			name:        "invalid bootstrap config file",
			opt:         &Option{BootstrapConfigFile: "bootstrap-config.yaml"},
			expectedErr: true,
		},
		{
			name:        "invalid config file",
			opt:         &Option{ConfigFile: "config.yaml"},
			expectedErr: true,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := c.opt.Validate()
			if c.expectedErr {
				if err == nil {
					t.Errorf("expected an error, but failed")
				}
				return
			}

			if err != nil {
				t.Errorf("unexpected error: %v", err)
			}
		})
	}
}
