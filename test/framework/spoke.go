package framework

import (
	"k8s.io/client-go/rest"
	clientcmd "k8s.io/client-go/tools/clientcmd"
)

// Spoke represents a spoke cluster, it holds:
// * the clients to interact with the spoke cluster
// * the metadata of the spoke
// * the runtime data of the spoke
type Spoke struct {
	*OCMClients
	// Note: this is the namespace and name where the KlusterletOperator deployment is created, which
	// is different from the klusterlet namespace and name.
	KlusterletOperatorNamespace string
	KlusterletOperator          string

	RestConfig *rest.Config
}

func NewSpoke(kubeconfig string) (*Spoke, error) {
	clusterCfg, err := clientcmd.BuildConfigFromFlags("", kubeconfig)
	if err != nil {
		return nil, err
	}
	clients, err := NewOCMClients(clusterCfg)
	if err != nil {
		return nil, err
	}
	return &Spoke{
		OCMClients: clients,
		// the name of the KlusterletOperator object is constantly "klusterlet-operator" at the moment;
		// The same name as deploy/klusterlet/config/operator/operator.yaml
		KlusterletOperatorNamespace: "open-cluster-management",
		KlusterletOperator:          "klusterlet",
		RestConfig:                  clusterCfg,
	}, nil
}
