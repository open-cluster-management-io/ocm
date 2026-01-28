package testing

import (
	"context"

	"open-cluster-management.io/ocm/pkg/registration/register"
)

// TestAddonAuthConfig is a simple implementation of AddonAuthConfig for testing
type TestAddonAuthConfig struct {
	KubeClientAuth string
	CSROption      register.CSRConfiguration
	TokenOption    register.TokenConfiguration
}

func (t *TestAddonAuthConfig) GetKubeClientAuth() string {
	return t.KubeClientAuth
}

func (t *TestAddonAuthConfig) GetCSRConfiguration() register.CSRConfiguration {
	return t.CSROption
}

func (t *TestAddonAuthConfig) GetTokenConfiguration() register.TokenConfiguration {
	return t.TokenOption
}

// MockTokenControl is a simple mock implementation of TokenControl for testing
type MockTokenControl struct{}

func (m *MockTokenControl) CreateToken(ctx context.Context, serviceAccountName, namespace string, expirationSeconds int64) (string, error) {
	return "mock-token", nil
}
