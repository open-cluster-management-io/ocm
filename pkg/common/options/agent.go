package options

import (
	"fmt"
	"os"
	"path"
	"strings"

	"github.com/spf13/pflag"
	apimachineryvalidation "k8s.io/apimachinery/pkg/api/validation"
	utilrand "k8s.io/apimachinery/pkg/util/rand"
	"k8s.io/apimachinery/pkg/util/uuid"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/klog/v2"

	"open-cluster-management.io/ocm/pkg/registration/clientcert"
	"open-cluster-management.io/ocm/pkg/registration/spoke/registration"
)

const (
	// spokeAgentNameLength is the length of the spoke agent name which is generated automatically
	spokeAgentNameLength = 5
	// defaultSpokeComponentNamespace is the default namespace in which the spoke agent is deployed
	defaultSpokeComponentNamespace = "open-cluster-management-agent"
)

// AgentOptions is the common agent options
type AgentOptions struct {
	CommoOpts           *Options
	ComponentNamespace  string
	SpokeKubeconfigFile string
	SpokeClusterName    string
	HubKubeconfigDir    string
	HubKubeconfigFile   string
	AgentID             string
}

// NewAgentOptions returns the flags with default value set
func NewAgentOptions() *AgentOptions {
	opts := &AgentOptions{
		HubKubeconfigDir:   "/spoke/hub-kubeconfig",
		ComponentNamespace: defaultSpokeComponentNamespace,
		CommoOpts:          NewOptions(),
	}
	// get component namespace of spoke agent
	nsBytes, err := os.ReadFile("/var/run/secrets/kubernetes.io/serviceaccount/namespace")
	if err == nil {
		opts.ComponentNamespace = string(nsBytes)
	}
	return opts
}

func (o *AgentOptions) AddFlags(flags *pflag.FlagSet) {
	o.CommoOpts.AddFlags(flags)
	flags.StringVar(&o.SpokeKubeconfigFile, "spoke-kubeconfig", o.SpokeKubeconfigFile,
		"Location of kubeconfig file to connect to spoke cluster. If this is not set, will use '--kubeconfig' to build client to connect to the managed cluster.")
	flags.StringVar(&o.SpokeClusterName, "spoke-cluster-name", o.SpokeClusterName, "Name of the spoke cluster.")
	_ = flags.MarkDeprecated("cluster-name", "use spoke-cluster-name flag")
	flags.StringVar(&o.SpokeClusterName, "cluster-name", o.SpokeClusterName,
		"Name of the spoke cluster.")
	flags.StringVar(&o.HubKubeconfigDir, "hub-kubeconfig-dir", o.HubKubeconfigDir,
		"The mount path of hub-kubeconfig-secret in the container.")
	flags.StringVar(&o.HubKubeconfigFile, "hub-kubeconfig", o.HubKubeconfigFile, "Location of kubeconfig file to connect to hub cluster.")
	flags.StringVar(&o.AgentID, "agent-id", o.AgentID, "ID of the agent")
}

// SpokeKubeConfig builds kubeconfig for the spoke/managed cluster
func (o *AgentOptions) SpokeKubeConfig(managedRestConfig *rest.Config) (*rest.Config, error) {
	if o.SpokeKubeconfigFile == "" {
		managedRestConfig.QPS = o.CommoOpts.QPS
		managedRestConfig.Burst = o.CommoOpts.Burst
		return managedRestConfig, nil
	}

	spokeRestConfig, err := clientcmd.BuildConfigFromFlags("" /* leave masterurl as empty */, o.SpokeKubeconfigFile)
	if err != nil {
		return nil, fmt.Errorf("unable to load spoke kubeconfig from file %q: %w", o.SpokeKubeconfigFile, err)
	}
	spokeRestConfig.QPS = o.CommoOpts.QPS
	spokeRestConfig.Burst = o.CommoOpts.Burst
	return spokeRestConfig, nil
}

func (o *AgentOptions) Validate() error {
	if o.SpokeClusterName == "" {
		return fmt.Errorf("cluster name is empty")
	}
	if errMsgs := apimachineryvalidation.ValidateNamespaceName(o.SpokeClusterName, false); len(errMsgs) > 0 {
		return fmt.Errorf("metadata.name format is not correct: %s", strings.Join(errMsgs, ","))
	}

	return nil
}

// Complete fills in missing values.
func (o *AgentOptions) Complete() error {
	if len(o.HubKubeconfigFile) == 0 {
		o.HubKubeconfigFile = path.Join(o.HubKubeconfigDir, clientcert.KubeconfigFile)
	}

	// load or generate cluster/agent names
	o.SpokeClusterName, o.AgentID = o.getOrGenerateClusterAgentID()

	return nil
}

// getOrGenerateClusterAgentID returns cluster name and agent id.
// Rules for picking up cluster name:
//   1. Use cluster name from input arguments if 'spoke-cluster-name' is specified;
//   2. Parse cluster name from the common name of the certification subject if the certification exists;
//   3. Fallback to cluster name in the mounted secret if it exists;
//   4. TODO: Read cluster name from openshift struct if the agent is running in an openshift cluster;
//   5. Generate a random cluster name then;

// Rules for picking up agent id:
//  1. Read from the flag "agent-id" at first.
//  2. Parse agent name from the common name of the certification subject if the certification exists;
//  3. Fallback to agent name in the mounted secret if it exists;
//  4. Generate a random agent name then;
func (o *AgentOptions) getOrGenerateClusterAgentID() (string, string) {
	if len(o.SpokeClusterName) > 0 && len(o.AgentID) > 0 {
		return o.SpokeClusterName, o.AgentID
	}
	// try to load cluster/agent name from tls certification
	var clusterNameInCert, agentNameInCert string
	certPath := path.Join(o.HubKubeconfigDir, clientcert.TLSCertFile)
	certData, certErr := os.ReadFile(path.Clean(certPath))
	if certErr == nil {
		clusterNameInCert, agentNameInCert, _ = registration.GetClusterAgentNamesFromCertificate(certData)
	}

	clusterName := o.SpokeClusterName
	// if cluster name is not specified with input argument, try to load it from file
	if clusterName == "" {
		// TODO, read cluster name from openshift struct if the spoke agent is running in an openshift cluster

		// and then load the cluster name from the mounted secret
		clusterNameFilePath := path.Join(o.HubKubeconfigDir, clientcert.ClusterNameFile)
		clusterNameBytes, err := os.ReadFile(path.Clean(clusterNameFilePath))
		switch {
		case len(clusterNameInCert) > 0:
			// use cluster name loaded from the tls certification
			clusterName = clusterNameInCert
			if clusterNameInCert != string(clusterNameBytes) {
				klog.Warningf("Use cluster name %q in certification instead of %q in the mounted secret", clusterNameInCert, string(clusterNameBytes))
			}
		case err == nil:
			// use cluster name load from the mounted secret
			clusterName = string(clusterNameBytes)
		default:
			// generate random cluster name
			clusterName = generateClusterName()
		}
	}

	agentID := o.AgentID
	// try to load agent name from the mounted secret
	if len(agentID) == 0 {
		agentIDFilePath := path.Join(o.HubKubeconfigDir, clientcert.AgentNameFile)
		agentIDBytes, err := os.ReadFile(path.Clean(agentIDFilePath))
		switch {
		case len(agentNameInCert) > 0:
			// use agent name loaded from the tls certification
			agentID = agentNameInCert
			if agentNameInCert != agentID {
				klog.Warningf(
					"Use agent name %q in certification instead of %q in the mounted secret",
					agentNameInCert, agentID)
			}
		case err == nil:
			// use agent name loaded from the mounted secret
			agentID = string(agentIDBytes)
		default:
			// generate random agent name
			agentID = generateAgentName()
		}
	}

	return clusterName, agentID
}

// generateClusterName generates a name for spoke cluster
func generateClusterName() string {
	return string(uuid.NewUUID())
}

// generateAgentName generates a random name for spoke cluster agent
func generateAgentName() string {
	return utilrand.String(spokeAgentNameLength)
}
