package spoke

import (
	"os"
	"path"
	"testing"
	"time"

	testinghelpers "open-cluster-management.io/ocm/pkg/registration/helpers/testing"
	"open-cluster-management.io/ocm/pkg/registration/register"
	"open-cluster-management.io/ocm/pkg/registration/register/csr"
)

func TestHubKubeConfigHealthChecker(t *testing.T) {
	testDir, err := os.MkdirTemp("", "HubKubeConfigHealthChecker")
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	defer func() {
		err := os.RemoveAll(testDir)
		if err != nil {
			t.Fatal(err)
		}
	}()

	kubeconfig := testinghelpers.NewKubeconfig("c1", "https://127.0.0.1:6001", "", "", nil, nil, nil)
	testinghelpers.WriteFile(path.Join(testDir, "kubeconfig"), kubeconfig)

	validCert := testinghelpers.NewTestCert("system:open-cluster-management:cluster1:agent1", 10*time.Minute)

	expiredCert := testinghelpers.NewTestCert("system:open-cluster-management:cluster1:agent1", -1*time.Minute)

	cases := []struct {
		name      string
		tlsCert   []byte
		tlsKey    []byte
		unhealthy bool
	}{
		{
			name:    "valid client cert",
			tlsKey:  validCert.Key,
			tlsCert: validCert.Cert,
		},
		{
			name:      "expired client cert",
			tlsKey:    expiredCert.Key,
			tlsCert:   expiredCert.Cert,
			unhealthy: true,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if c.tlsKey != nil {
				testinghelpers.WriteFile(path.Join(testDir, "tls.key"), c.tlsKey)
			}
			if c.tlsCert != nil {
				testinghelpers.WriteFile(path.Join(testDir, "tls.crt"), c.tlsCert)
			}

			secretOption := register.SecretOption{
				ClusterName:             "cluster1",
				AgentName:               "agent1",
				BootStrapKubeConfigFile: path.Join(testDir, "kubeconfig"),
				HubKubeconfigDir:        testDir,
				HubKubeconfigFile:       path.Join(testDir, "kubeconfig"),
			}
			driver, err := csr.NewCSRDriver(csr.NewCSROption(), secretOption)
			if err != nil {
				t.Fatal(err)
			}

			hc := &hubKubeConfigHealthChecker{
				checkFunc:    register.IsHubKubeConfigValidFunc(driver, secretOption),
				bootstrapped: true,
			}

			err = hc.Check(nil)
			if c.unhealthy && err == nil {
				t.Errorf("expected error, but got nil")
			}
			if !c.unhealthy && err != nil {
				t.Errorf("unexpected error: %v", err)
			}
		})
	}
}
