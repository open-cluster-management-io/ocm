package options

import (
	"context"
	"os"
	"path"
	"testing"
	"time"

	"github.com/openshift/library-go/pkg/controller/controllercmd"
	"github.com/openshift/library-go/pkg/operator/events/eventstesting"
	"github.com/spf13/cobra"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	kubefake "k8s.io/client-go/kubernetes/fake"
	clocktesting "k8s.io/utils/clock/testing"

	testinghelpers "open-cluster-management.io/ocm/pkg/registration/helpers/testing"
	"open-cluster-management.io/ocm/pkg/registration/register"
	"open-cluster-management.io/ocm/pkg/registration/spoke/registration"
	"open-cluster-management.io/ocm/pkg/version"
)

func TestComplete(t *testing.T) {
	// get component namespace
	var componentNamespace string
	nsBytes, err := os.ReadFile("/var/run/secrets/kubernetes.io/serviceaccount/namespace")
	if err != nil {
		componentNamespace = defaultSpokeComponentNamespace
	} else {
		componentNamespace = string(nsBytes)
	}

	cases := []struct {
		name                string
		clusterName         string
		secret              *corev1.Secret
		expectedClusterName string
		expectedAgentName   string
	}{
		{
			name: "generate random cluster/agent name",
		},
		{
			name:                "specify cluster name",
			clusterName:         "cluster1",
			expectedClusterName: "cluster1",
		},
		{
			name:        "override cluster name in secret with specified value",
			clusterName: "cluster1",
			secret: testinghelpers.NewHubKubeconfigSecret(componentNamespace, "hub-kubeconfig-secret", "", nil, map[string][]byte{
				"cluster-name": []byte("cluster2"),
				"agent-name":   []byte("agent2"),
			}),
			expectedClusterName: "cluster1",
			expectedAgentName:   "agent2",
		},
		{
			name: "take cluster/agent name from secret",
			secret: testinghelpers.NewHubKubeconfigSecret(
				componentNamespace, "hub-kubeconfig-secret", "", nil, map[string][]byte{
					"cluster-name": []byte("cluster1"),
					"agent-name":   []byte("agent1"),
				}),
			expectedClusterName: "cluster1",
			expectedAgentName:   "agent1",
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			// setup kube client
			var objects []runtime.Object
			if c.secret != nil {
				objects = append(objects, c.secret)
			}
			kubeClient := kubefake.NewSimpleClientset(objects...)

			// create a tmp dir to dump hub kubeconfig
			dir, err := os.MkdirTemp("", "hub-kubeconfig")
			if err != nil {
				t.Error("unable to create a tmp dir")
			}
			defer os.RemoveAll(dir)

			options := NewAgentOptions()
			options.SpokeClusterName = c.clusterName
			options.HubKubeconfigDir = dir

			err = registration.DumpSecret(
				kubeClient.CoreV1(), componentNamespace, "hub-kubeconfig-secret",
				options.HubKubeconfigDir, context.TODO(), eventstesting.NewTestingEventRecorder(t))
			if err != nil {
				t.Error(err)
			}

			if err := options.Complete(); err != nil {
				t.Errorf("unexpected error: %v", err)
			}
			if options.ComponentNamespace == "" {
				t.Error("component namespace should not be empty")
			}
			if options.SpokeClusterName == "" {
				t.Error("cluster name should not be empty")
			}
			if options.AgentID == "" {
				t.Error("agent name should not be empty")
			}
			if len(c.expectedClusterName) > 0 && options.SpokeClusterName != c.expectedClusterName {
				t.Errorf("expect cluster name %q but got %q", c.expectedClusterName, options.SpokeClusterName)
			}
			if len(c.expectedAgentName) > 0 && options.AgentID != c.expectedAgentName {
				t.Errorf("expect agent name %q but got %q", c.expectedAgentName, options.AgentID)
			}
		})
	}
}

func TestValidate(t *testing.T) {
	cases := []struct {
		name        string
		clusterName string
		expectedErr bool
	}{
		{
			name:        "empty cluster name",
			expectedErr: true,
		},
		{
			name:        "invalid cluster name format",
			clusterName: "test.cluster",
			expectedErr: true,
		},
		{
			name:        "valid passed",
			clusterName: "cluster-1",
			expectedErr: false,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			options := NewAgentOptions()
			options.SpokeClusterName = c.clusterName
			err := options.Validate()
			if err == nil && c.expectedErr {
				t.Errorf("expect to get err")
			}
			if err != nil && !c.expectedErr {
				t.Errorf("expect not error but got %v", err)
			}
		})
	}
}

func TestGetOrGenerateClusterAgentNames(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "testgetorgenerateclusteragentnames")
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	defer os.RemoveAll(tempDir)

	cases := []struct {
		name                string
		options             *AgentOptions
		expectedClusterName string
		expectedAgentName   string
	}{
		{
			name:                "cluster name is specified",
			options:             &AgentOptions{SpokeClusterName: "cluster0"},
			expectedClusterName: "cluster0",
		},
		{
			name:                "cluster name and agent name are in file",
			options:             &AgentOptions{HubKubeconfigDir: tempDir},
			expectedClusterName: "cluster1",
			expectedAgentName:   "agent1",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if c.options.HubKubeconfigDir != "" {
				testinghelpers.WriteFile(path.Join(tempDir, register.ClusterNameFile), []byte(c.expectedClusterName))
				testinghelpers.WriteFile(path.Join(tempDir, register.AgentNameFile), []byte(c.expectedAgentName))
			}
			clusterName, agentName := c.options.getOrGenerateClusterAgentID()
			if clusterName != c.expectedClusterName {
				t.Errorf("expect cluster name %q but got %q", c.expectedClusterName, clusterName)
			}

			// agent name cannot be empty, it is either generated or from file
			if agentName == "" {
				t.Error("agent name should not be empty")
			}

			if c.expectedAgentName != "" && c.expectedAgentName != agentName {
				t.Errorf("expect agent name %q but got %q", c.expectedAgentName, agentName)
			}
		})
	}
}

func TestNewOptions(t *testing.T) {
	opts := NewOptions()
	cmd := &cobra.Command{
		Use:   "test",
		Short: "test Controller",
		Run: func(cmd *cobra.Command, args []string) {
			_ = cmd.Help()
			os.Exit(1)
		},
	}

	opts.NewControllerCommandConfig("test", version.Get(), func(ctx context.Context, controllerCtx *controllercmd.ControllerContext) error {
		return nil
	}, clocktesting.NewFakeClock(time.Now()))

	opts.AddFlags(cmd.Flags())
	if err := cmd.Flags().Set("kube-api-qps", "10"); err != nil {
		t.Fatal(err)
	}
	if err := cmd.Flags().Set("kube-api-burst", "20"); err != nil {
		t.Fatal(err)

	}
	if err := cmd.Flags().Set("disable-leader-election", "true"); err != nil {
		t.Fatal(err)
	}
	if err := cmd.Flags().Set("unsupported-flag", "true"); err == nil {
		t.Errorf("Should return err")
	}
}
