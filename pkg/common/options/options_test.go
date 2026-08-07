package options

import (
	"context"
	"os"
	"path"
	"strings"
	"testing"
	"time"

	"github.com/openshift/library-go/pkg/controller/controllercmd"
	"github.com/spf13/cobra"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	kubefake "k8s.io/client-go/kubernetes/fake"
	clocktesting "k8s.io/utils/clock/testing"

	tlslib "open-cluster-management.io/sdk-go/pkg/tls"

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
				context.TODO(), kubeClient.CoreV1(), componentNamespace, "hub-kubeconfig-secret",
				options.HubKubeconfigDir)
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

func TestApplyTLSToCommand(t *testing.T) {
	cases := []struct {
		name            string
		tlsMinVersion   string
		tlsCipherSuites string
		configPreSet    bool
		wantConfigSet   bool
		wantError       bool
		wantMinVersion  string
		wantCiphers     []string
	}{
		{
			name:          "no TLS flags set — no-op",
			wantConfigSet: false,
		},
		{
			name:           "only tls-min-version set",
			tlsMinVersion:  "VersionTLS13",
			wantConfigSet:  true,
			wantMinVersion: "VersionTLS13",
		},
		{
			name:            "only tls-cipher-suites set",
			tlsCipherSuites: "TLS_AES_256_GCM_SHA384",
			wantConfigSet:   true,
			wantCiphers:     []string{"TLS_AES_256_GCM_SHA384"},
		},
		{
			name:            "both tls-min-version and tls-cipher-suites set",
			tlsMinVersion:   "VersionTLS13",
			tlsCipherSuites: "TLS_AES_256_GCM_SHA384,TLS_CHACHA20_POLY1305_SHA256",
			wantConfigSet:   true,
			wantMinVersion:  "VersionTLS13",
			wantCiphers:     []string{"TLS_AES_256_GCM_SHA384", "TLS_CHACHA20_POLY1305_SHA256"},
		},
		{
			name:          "user already provided --config — skip injection",
			tlsMinVersion: "VersionTLS13",
			configPreSet:  true,
			wantConfigSet: false,
		},
		{
			name:          "invalid tls-min-version — error before file creation",
			tlsMinVersion: "BadVersion",
			wantError:     true,
		},
		{
			name:            "invalid tls-cipher-suites — error before file creation",
			tlsCipherSuites: "NOT_A_REAL_CIPHER",
			wantError:       true,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			opts := NewOptions()
			opts.TLSMinVersion = c.tlsMinVersion
			opts.TLSCipherSuites = c.tlsCipherSuites

			// Simulate library-go's --config flag.
			cmd := &cobra.Command{Use: "test"}
			var configFile string
			cmd.Flags().StringVar(&configFile, "config", "", "")

			if c.configPreSet {
				if err := cmd.Flags().Set("config", "/existing/config.yaml"); err != nil {
					t.Fatal(err)
				}
			}

			opts.ApplyTLSToCommand(cmd)

			// PersistentPreRunE is set by ApplyTLSToCommand; invoke it directly.
			err := cmd.PersistentPreRunE(cmd, nil)
			if c.wantError {
				if err == nil {
					t.Fatal("expected an error but got none")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if c.configPreSet {
				// --config should remain as the user set it, not overwritten.
				if configFile != "/existing/config.yaml" {
					t.Errorf("expected --config to remain %q, got %q", "/existing/config.yaml", configFile)
				}
				return
			}

			if !c.wantConfigSet {
				if configFile != "" {
					t.Errorf("expected --config to be empty, got %q", configFile)
				}
				return
			}

			if configFile == "" {
				t.Fatal("expected --config to be set, but it is empty")
			}
			defer os.Remove(configFile)

			content, err := os.ReadFile(configFile)
			if err != nil {
				t.Fatalf("failed to read generated config file: %v", err)
			}
			s := string(content)

			if c.wantMinVersion != "" && !strings.Contains(s, "minTLSVersion: "+c.wantMinVersion) {
				t.Errorf("expected config to contain minTLSVersion %q, got:\n%s", c.wantMinVersion, s)
			}
			if c.wantMinVersion == "" && strings.Contains(s, "minTLSVersion") {
				t.Errorf("expected config to NOT contain minTLSVersion, got:\n%s", s)
			}
			for _, cipher := range c.wantCiphers {
				if !strings.Contains(s, cipher) {
					t.Errorf("expected config to contain cipher %q, got:\n%s", cipher, s)
				}
			}
		})
	}
}

func TestApplyTLSFromConfigMap(t *testing.T) {
	cases := []struct {
		name           string
		configMap      *corev1.ConfigMap
		wantConfigSet  bool
		wantMinVersion string
		wantCiphers    []string
	}{
		{
			name:          "ConfigMap not found -- no-op",
			configMap:     nil,
			wantConfigSet: false,
		},
		{
			name: "ConfigMap with VersionTLS13",
			configMap: &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      tlslib.ConfigMapName,
					Namespace: "test-ns",
				},
				Data: map[string]string{
					tlslib.ConfigMapKeyMinVersion: "VersionTLS13",
				},
			},
			wantConfigSet:  true,
			wantMinVersion: "VersionTLS13",
		},
		{
			name: "ConfigMap with VersionTLS12 and cipher suites",
			configMap: &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      tlslib.ConfigMapName,
					Namespace: "test-ns",
				},
				Data: map[string]string{
					tlslib.ConfigMapKeyMinVersion:   "VersionTLS12",
					tlslib.ConfigMapKeyCipherSuites: "TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256,TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384",
				},
			},
			wantConfigSet:  true,
			wantMinVersion: "VersionTLS12",
			wantCiphers:    []string{"TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256", "TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384"},
		},
		{
			name: "empty ConfigMap data -- uses default VersionTLS12",
			configMap: &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      tlslib.ConfigMapName,
					Namespace: "test-ns",
				},
				Data: map[string]string{},
			},
			wantConfigSet:  true,
			wantMinVersion: "VersionTLS12",
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var objects []runtime.Object
			if c.configMap != nil {
				objects = append(objects, c.configMap)
			}
			kubeClient := kubefake.NewSimpleClientset(objects...)

			cmd := &cobra.Command{Use: "test"}
			var configFile string
			cmd.Flags().StringVar(&configFile, "config", "", "")

			err := applyTLSFromConfigMap(context.Background(), kubeClient, "test-ns", cmd)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if !c.wantConfigSet {
				if configFile != "" {
					t.Errorf("expected --config to be empty, got %q", configFile)
				}
				return
			}

			if configFile == "" {
				t.Fatal("expected --config to be set, but it is empty")
			}
			defer os.Remove(configFile)

			content, err := os.ReadFile(configFile)
			if err != nil {
				t.Fatalf("failed to read generated config file: %v", err)
			}
			s := string(content)

			if c.wantMinVersion != "" && !strings.Contains(s, "minTLSVersion: "+c.wantMinVersion) {
				t.Errorf("expected config to contain minTLSVersion %q, got:\n%s", c.wantMinVersion, s)
			}
			for _, cipher := range c.wantCiphers {
				if !strings.Contains(s, cipher) {
					t.Errorf("expected config to contain cipher %q, got:\n%s", cipher, s)
				}
			}
		})
	}
}

func TestApplyTLSFromConfigMapPersistentPreRunE(t *testing.T) {
	// Verify that ApplyTLSFromConfigMapToCommand sets PersistentPreRunE.
	opts := NewOptions()
	cmd := &cobra.Command{Use: "test"}
	cmd.Flags().String("config", "", "")
	cmd.Flags().String("namespace", "", "")

	if cmd.PersistentPreRunE != nil {
		t.Fatal("expected PersistentPreRunE to be nil before ApplyTLSFromConfigMapToCommand")
	}
	opts.ApplyTLSFromConfigMapToCommand(cmd)
	if cmd.PersistentPreRunE == nil {
		t.Error("expected PersistentPreRunE to be set by ApplyTLSFromConfigMapToCommand")
	}
}

func TestApplyTLSFromConfigMapSkipsWhenConfigAlreadySet(t *testing.T) {
	// When --config is already set, the hook should be a no-op even if the
	// ConfigMap exists.  This guards against overwriting user-supplied config.
	// We test the hook directly: without in-cluster credentials the hook
	// exits early (no namespace) -- so set the namespace flag explicitly and
	// run the hook with --config pre-set.  The hook should return nil without
	// creating a new temp file.
	opts := NewOptions()
	cmd := &cobra.Command{Use: "test"}
	var configFile string
	cmd.Flags().StringVar(&configFile, "config", "/existing/config.yaml", "")
	cmd.Flags().String("namespace", "test-ns", "")
	if err := cmd.Flags().Set("config", "/existing/config.yaml"); err != nil {
		t.Fatal(err)
	}

	opts.ApplyTLSFromConfigMapToCommand(cmd)

	// Run the hook -- it should detect --config is already set and return immediately.
	if err := cmd.PersistentPreRunE(cmd, nil); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// --config must remain unchanged.
	if configFile != "/existing/config.yaml" {
		t.Errorf("expected --config to remain %q, got %q", "/existing/config.yaml", configFile)
	}
}
