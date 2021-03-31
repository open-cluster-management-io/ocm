package addon

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"testing"
)

func TestGetRegistrationConfigs(t *testing.T) {
	addOnName := "addon1"
	addOnNamespace := "ns1"
	config1 := newRegistrationConfig(addOnName, addOnNamespace, "kubernetes.io/kube-apiserver-client", "", nil)
	config2 := newRegistrationConfig(addOnName, addOnNamespace, "mysigner", "", nil)

	cases := []struct {
		name        string
		annotations map[string]string
		configs     []registrationConfig
	}{
		{
			name: "no annotation",
		},
		{
			name: "no registration",
			annotations: map[string]string{
				"addon.open-cluster-management.io/installNamespace": addOnNamespace,
			},
		},
		{
			name: "with default signer",
			annotations: map[string]string{
				"addon.open-cluster-management.io/installNamespace": addOnNamespace,
				"addon.open-cluster-management.io/registrations":    `[{}]`,
			},
			configs: []registrationConfig{
				config1,
			},
		},
		{
			name: "with custom signer",
			annotations: map[string]string{
				"addon.open-cluster-management.io/installNamespace": addOnNamespace,
				"addon.open-cluster-management.io/registrations":    `[{"signerName":"mysigner"}]`,
			},
			configs: []registrationConfig{
				config2,
			},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			configs, err := getRegistrationConfigs(addOnName, c.annotations)
			if err != nil {
				t.Errorf("unexpected error: %v", err)
			}
			if len(configs) != len(c.configs) {
				t.Errorf("expected %d configs, but got %d", len(c.configs), len(configs))
			}

			for _, config := range c.configs {
				if _, ok := configs[config.hash]; !ok {
					t.Errorf("unexpected registrationConfig: %v", config)
				}
			}
		})
	}
}

func newRegistrationConfig(addOnName, addOnNamespace, signerName, commonName string, organization []string) registrationConfig {
	config := registrationConfig{
		AddOnName:             addOnName,
		InstallationNamespace: addOnNamespace,
		SignerName:            signerName,
		Subject: certSubject{
			CommonName:   commonName,
			Organization: organization,
		},
	}

	data, _ := json.Marshal(config)
	h := sha256.New()
	h.Write(data)
	config.hash = fmt.Sprintf("%x", h.Sum(nil))

	return config
}
