package webhook

import (
	"testing"

	"github.com/spf13/pflag"
)

func TestOptions_AddFlags(t *testing.T) {
	opts := NewOptions()
	flags := pflag.NewFlagSet("test", pflag.ContinueOnError)

	// Verify initial state
	expectedDefault := opts.ManifestLimit
	opts.AddFlags(flags)
	// Verify flag was registered
	flag := flags.Lookup("manifestLimit")
	if flag == nil {
		t.Error("manifestLimit flag not registered")
	}

	// Verify default value is preserved
	if opts.ManifestLimit != expectedDefault {
		t.Errorf("expected %d, but got %d", expectedDefault, opts.ManifestLimit)
	}
}
