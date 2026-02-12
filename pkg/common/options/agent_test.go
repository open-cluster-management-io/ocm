package options

import (
	"os"
	"path"
	"testing"

	"github.com/spf13/pflag"
)

func TestNewAgentOptions(t *testing.T) {
	opts := NewAgentOptions()
	if opts.HubKubeconfigDir != "/spoke/hub-kubeconfig" {
		t.Errorf("unexpected HubKubeconfigDir: %s", opts.HubKubeconfigDir)
	}
	// ComponentNamespace should either be the default or read from the service account namespace file
	if opts.ComponentNamespace == "" {
		t.Errorf("ComponentNamespace should not be empty")
	}
	// In test environments, it may be read from /var/run/secrets/kubernetes.io/serviceaccount/namespace
	// Otherwise, it should be the default value
	if _, err := os.ReadFile("/var/run/secrets/kubernetes.io/serviceaccount/namespace"); err != nil {
		// No service account namespace file, should be default
		if opts.ComponentNamespace != "open-cluster-management-agent" {
			t.Errorf("unexpected ComponentNamespace: %s", opts.ComponentNamespace)
		}
	}
}

func TestAgentOptions_AddFlags(t *testing.T) {
	opts := NewAgentOptions()
	flags := pflag.NewFlagSet("test", pflag.ContinueOnError)
	opts.AddFlags(flags)

	err := flags.Parse([]string{
		"--spoke-kubeconfig=test-spoke-kubeconfig",
		"--spoke-cluster-name=test-cluster",
		"--hub-kubeconfig-dir=/test/hub-kubeconfig-dir",
		"--hub-kubeconfig=test-hub-kubeconfig",
		"--agent-id=test-agent-id",
		"--hub-kube-api-qps=100.0",
		"--hub-kube-api-burst=200",
	})
	if err != nil {
		t.Fatal(err)
	}

	if opts.SpokeKubeconfigFile != "test-spoke-kubeconfig" {
		t.Errorf("unexpected SpokeKubeconfigFile: %s", opts.SpokeKubeconfigFile)
	}
	if opts.SpokeClusterName != "test-cluster" {
		t.Errorf("unexpected SpokeClusterName: %s", opts.SpokeClusterName)
	}
	if opts.HubKubeconfigDir != "/test/hub-kubeconfig-dir" {
		t.Errorf("unexpected HubKubeconfigDir: %s", opts.HubKubeconfigDir)
	}
	if opts.HubKubeconfigFile != "test-hub-kubeconfig" {
		t.Errorf("unexpected HubKubeconfigFile: %s", opts.HubKubeconfigFile)
	}
	if opts.AgentID != "test-agent-id" {
		t.Errorf("unexpected AgentID: %s", opts.AgentID)
	}
	if opts.HubQPS != 100.0 {
		t.Errorf("unexpected HubQPS: %f", opts.HubQPS)
	}
	if opts.HubBurst != 200 {
		t.Errorf("unexpected HubBurst: %d", opts.HubBurst)
	}
}

func TestAgentOptions_Validate(t *testing.T) {
	cases := []struct {
		name        string
		options     *AgentOptions
		expectedErr bool
	}{
		{
			name: "valid options",
			options: &AgentOptions{
				SpokeClusterName: "test-cluster",
			},
			expectedErr: false,
		},
		{
			name:        "empty cluster name",
			options:     &AgentOptions{},
			expectedErr: true,
		},
		{
			name: "invalid cluster name",
			options: &AgentOptions{
				SpokeClusterName: "test-cluster-!@#",
			},
			expectedErr: true,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := c.options.Validate()
			if (err != nil) != c.expectedErr {
				t.Errorf("unexpected error: %v", err)
			}
		})
	}
}

func TestAgentOptions_Complete(t *testing.T) {
	dir, err := os.MkdirTemp("", "test-complete")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)

	opts := &AgentOptions{
		HubKubeconfigDir: dir,
	}

	err = opts.Complete()
	if err != nil {
		t.Fatal(err)
	}

	if opts.HubKubeconfigFile != path.Join(dir, "kubeconfig") {
		t.Errorf("unexpected HubKubeconfigFile: %s", opts.HubKubeconfigFile)
	}
	if len(opts.SpokeClusterName) == 0 {
		t.Error("SpokeClusterName should be generated")
	}
	if len(opts.AgentID) == 0 {
		t.Error("AgentID should be generated")
	}
}

func TestAgentOptions_getOrGenerateClusterAgentID(t *testing.T) {
	dir, err := os.MkdirTemp("", "test-get-or-generate")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)

	clusterNameFile := path.Join(dir, "cluster-name")
	agentNameFile := path.Join(dir, "agent-name")

	err = os.WriteFile(clusterNameFile, []byte("cluster-from-file"), 0600)
	if err != nil {
		t.Fatal(err)
	}
	err = os.WriteFile(agentNameFile, []byte("agent-from-file"), 0600)
	if err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		name                string
		options             *AgentOptions
		expectedClusterName string
		expectedAgentName   string
	}{
		{
			name: "from options",
			options: &AgentOptions{
				SpokeClusterName: "cluster-from-options",
				AgentID:          "agent-from-options",
				HubKubeconfigDir: dir,
			},
			expectedClusterName: "cluster-from-options",
			expectedAgentName:   "agent-from-options",
		},
		{
			name: "from file",
			options: &AgentOptions{
				HubKubeconfigDir: dir,
			},
			expectedClusterName: "cluster-from-file",
			expectedAgentName:   "agent-from-file",
		},
		{
			name: "generate",
			options: &AgentOptions{
				HubKubeconfigDir: "/non-exist-dir",
			},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			clusterName, agentName := c.options.getOrGenerateClusterAgentID()
			if len(c.expectedClusterName) > 0 && clusterName != c.expectedClusterName {
				t.Errorf("unexpected cluster name: %s", clusterName)
			}
			if len(c.expectedAgentName) > 0 && agentName != c.expectedAgentName {
				t.Errorf("unexpected agent name: %s", agentName)
			}
			if len(c.expectedClusterName) == 0 && len(clusterName) == 0 {
				t.Error("cluster name should be generated")
			}
			if len(c.expectedAgentName) == 0 && len(agentName) == 0 {
				t.Error("agent name should be generated")
			}
		})
	}
}
