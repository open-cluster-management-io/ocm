package grpc

import (
	"testing"

	"github.com/spf13/pflag"
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

func TestNewOptions(t *testing.T) {
	opts := NewOptions()

	if opts == nil {
		t.Fatal("NewOptions() returned nil")
	}

	if opts.BootstrapConfigFile != "" {
		t.Errorf("Expected BootstrapConfigFile to be empty by default, got %q", opts.BootstrapConfigFile)
	}

	if opts.ConfigFile != "" {
		t.Errorf("Expected ConfigFile to be empty by default, got %q", opts.ConfigFile)
	}
}

func TestAddFlags(t *testing.T) {
	opts := NewOptions()
	fs := pflag.NewFlagSet("test", pflag.ContinueOnError)

	opts.AddFlags(fs)

	// Test that grpc-bootstrap-config flag is added
	bootstrapFlag := fs.Lookup("grpc-bootstrap-config")
	if bootstrapFlag == nil {
		t.Error("Expected grpc-bootstrap-config flag to be added")
	}

	// Test that grpc-config flag is added
	configFlag := fs.Lookup("grpc-config")
	if configFlag == nil {
		t.Error("Expected grpc-config flag to be added")
	}

	// Test setting bootstrap config flag
	err := fs.Set("grpc-bootstrap-config", "test-bootstrap.yaml")
	if err != nil {
		t.Errorf("Failed to set grpc-bootstrap-config flag: %v", err)
	}

	if opts.BootstrapConfigFile != "test-bootstrap.yaml" {
		t.Errorf("Expected BootstrapConfigFile to be 'test-bootstrap.yaml', got %q", opts.BootstrapConfigFile)
	}

	// Test setting config flag
	err = fs.Set("grpc-config", "test-config.yaml")
	if err != nil {
		t.Errorf("Failed to set grpc-config flag: %v", err)
	}

	if opts.ConfigFile != "test-config.yaml" {
		t.Errorf("Expected ConfigFile to be 'test-config.yaml', got %q", opts.ConfigFile)
	}
}

func TestValidateEdgeCases(t *testing.T) {
	tests := []struct {
		name            string
		bootstrapConfig string
		config          string
		expectErr       bool
	}{
		{
			name:            "both configs set",
			bootstrapConfig: "bootstrap.yaml",
			config:          "config.yaml",
			expectErr:       false,
		},
		{
			name:            "empty strings",
			bootstrapConfig: "",
			config:          "",
			expectErr:       true,
		},
		{
			name:            "only bootstrap set",
			bootstrapConfig: "bootstrap.yaml",
			config:          "",
			expectErr:       false,
		},
		{
			name:            "only config set",
			bootstrapConfig: "",
			config:          "config.yaml",
			expectErr:       false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			opts := &Option{
				BootstrapConfigFile: tt.bootstrapConfig,
				ConfigFile:          tt.config,
			}

			err := opts.Validate()
			if tt.expectErr && err == nil {
				t.Error("Expected error but got nil")
			}
			if !tt.expectErr && err != nil {
				t.Errorf("Unexpected error: %v", err)
			}
		})
	}
}

func TestOptionStruct(t *testing.T) {
	opts := &Option{
		BootstrapConfigFile: "bootstrap.yaml",
		ConfigFile:          "config.yaml",
	}

	if opts.BootstrapConfigFile != "bootstrap.yaml" {
		t.Error("BootstrapConfigFile field not set correctly")
	}

	if opts.ConfigFile != "config.yaml" {
		t.Error("ConfigFile field not set correctly")
	}
}
