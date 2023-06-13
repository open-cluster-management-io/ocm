package options

import (
	"fmt"
	"strings"

	"github.com/spf13/pflag"
	apimachineryvalidation "k8s.io/apimachinery/pkg/api/validation"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

// AgentOptions is the common agent options
type AgentOptions struct {
	SpokeKubeconfigFile string
	SpokeClusterName    string
	Burst               int
	QPS                 float32
}

// NewWorkloadAgentOptions returns the flags with default value set
func NewAgentOptions() *AgentOptions {
	return &AgentOptions{
		QPS:   50,
		Burst: 100,
	}
}

func (o *AgentOptions) AddFlags(flags *pflag.FlagSet) {
	flags.StringVar(&o.SpokeKubeconfigFile, "spoke-kubeconfig", o.SpokeKubeconfigFile,
		"Location of kubeconfig file to connect to spoke cluster. If this is not set, will use '--kubeconfig' to build client to connect to the managed cluster.")
	flags.StringVar(&o.SpokeClusterName, "spoke-cluster-name", o.SpokeClusterName, "Name of the spoke cluster.")
	_ = flags.MarkDeprecated("cluster-name", "use spoke-cluster-name flag")
	flags.StringVar(&o.SpokeClusterName, "cluster-name", o.SpokeClusterName,
		"Name of the spoke cluster.")
	flags.Float32Var(&o.QPS, "spoke-kube-api-qps", o.QPS, "QPS to use while talking with apiserver on spoke cluster.")
	flags.IntVar(&o.Burst, "spoke-kube-api-burst", o.Burst, "Burst to use while talking with apiserver on spoke cluster.")
}

// spokeKubeConfig builds kubeconfig for the spoke/managed cluster
func (o *AgentOptions) SpokeKubeConfig(managedRestConfig *rest.Config) (*rest.Config, error) {
	if o.SpokeKubeconfigFile == "" {
		return managedRestConfig, nil
	}

	spokeRestConfig, err := clientcmd.BuildConfigFromFlags("" /* leave masterurl as empty */, o.SpokeKubeconfigFile)
	if err != nil {
		return nil, fmt.Errorf("unable to load spoke kubeconfig from file %q: %w", o.SpokeKubeconfigFile, err)
	}
	spokeRestConfig.QPS = o.QPS
	spokeRestConfig.Burst = o.Burst
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
