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
			name:        "bootstrap config file is set",
			opt:         &Option{BootstrapConfigFile: "bootstrap-config.yaml"},
			expectedErr: false,
		},
		{
			name:        "config file is set",
			opt:         &Option{ConfigFile: "config.yaml"},
			expectedErr: false,
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
