package managedcluster

import (
	"crypto/x509/pkix"
	"fmt"
	"strings"

	"github.com/openshift/library-go/pkg/controller/factory"
	"github.com/openshift/library-go/pkg/operator/events"
	"golang.org/x/net/context"
	certificates "k8s.io/api/certificates/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	corev1informers "k8s.io/client-go/informers/core/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"
	certutil "k8s.io/client-go/util/cert"
	clientset "open-cluster-management.io/api/client/cluster/clientset/versioned"

	"open-cluster-management.io/registration/pkg/clientcert"
	"open-cluster-management.io/registration/pkg/helpers"
	"open-cluster-management.io/registration/pkg/hub/user"
)

const (
	indexByCluster = "indexByCluster"

	// TODO(qiujian16) expose it if necessary in the future.
	clusterCSRThreshold = 10
)

// NewClientCertForHubController returns a controller to
// 1). Create a new client certificate and build a hub kubeconfig for the registration agent;
// 2). Or rotate the client certificate referenced by the hub kubeconfig before it become expired;
func NewClientCertForHubController(
	clusterName string,
	agentName string,
	clientCertSecretNamespace string,
	clientCertSecretName string,
	kubeconfigData []byte,
	spokeSecretInformer corev1informers.SecretInformer,
	csrControl clientcert.CSRControl,
	spokeKubeClient kubernetes.Interface,
	statusUpdater clientcert.StatusUpdateFunc,
	recorder events.Recorder,
	controllerName string,
) factory.Controller {
	err := csrControl.Informer().AddIndexers(cache.Indexers{
		indexByCluster: indexByClusterFunc,
	})
	if err != nil {
		utilruntime.HandleError(err)
	}
	clientCertOption := clientcert.ClientCertOption{
		SecretNamespace: clientCertSecretNamespace,
		SecretName:      clientCertSecretName,
		AdditionalSecretData: map[string][]byte{
			clientcert.ClusterNameFile: []byte(clusterName),
			clientcert.AgentNameFile:   []byte(agentName),
			clientcert.KubeconfigFile:  kubeconfigData,
		},
	}
	csrOption := clientcert.CSROption{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: fmt.Sprintf("%s-", clusterName),
			Labels: map[string]string{
				// the label is only an hint for cluster name. Anyone could set/modify it.
				clientcert.ClusterNameLabel: clusterName,
			},
		},
		Subject: &pkix.Name{
			Organization: []string{
				fmt.Sprintf("%s%s", user.SubjectPrefix, clusterName),
				user.ManagedClustersGroup,
			},
			CommonName: fmt.Sprintf("%s%s:%s", user.SubjectPrefix, clusterName, agentName),
		},
		SignerName: certificates.KubeAPIServerClientSignerName,
		EventFilterFunc: func(obj interface{}) bool {
			accessor, err := meta.Accessor(obj)
			if err != nil {
				return false
			}
			labels := accessor.GetLabels()
			// only enqueue csr from a specific managed cluster
			if labels[clientcert.ClusterNameLabel] != clusterName {
				return false
			}

			// should not contain addon key
			_, ok := labels[clientcert.AddonNameLabel]
			if ok {
				return false
			}

			// only enqueue csr whose name starts with the cluster name
			return strings.HasPrefix(accessor.GetName(), fmt.Sprintf("%s-", clusterName))
		},
		HaltCSRCreation: haltCSRCreationFunc(csrControl.Informer().GetIndexer(), clusterName),
	}

	return clientcert.NewClientCertificateController(
		clientCertOption,
		csrOption,
		csrControl,
		spokeSecretInformer,
		spokeKubeClient.CoreV1(),
		statusUpdater,
		recorder,
		controllerName,
	)
}

func haltCSRCreationFunc(indexer cache.Indexer, clusterName string) func() bool {
	return func() bool {
		items, err := indexer.ByIndex(indexByCluster, clusterName)
		if err != nil {
			return false
		}

		if len(items) >= clusterCSRThreshold {
			return true
		}
		return false
	}
}

func GenerateBootstrapStatusUpdater() clientcert.StatusUpdateFunc {
	return func(ctx context.Context, cond metav1.Condition) error {
		return nil
	}
}

// GenerateStatusUpdater generates status update func for the certificate management
func GenerateStatusUpdater(hubClusterClient clientset.Interface, clusterName string) clientcert.StatusUpdateFunc {
	return func(ctx context.Context, cond metav1.Condition) error {
		_, _, updatedErr := helpers.UpdateManagedClusterStatus(
			ctx, hubClusterClient, clusterName, helpers.UpdateManagedClusterConditionFn(cond),
		)

		return updatedErr
	}
}

// GetClusterAgentNamesFromCertificate returns the cluster name and agent name by parsing
// the common name of the certification
func GetClusterAgentNamesFromCertificate(certData []byte) (clusterName, agentName string, err error) {
	certs, err := certutil.ParseCertsPEM(certData)
	if err != nil {
		return "", "", fmt.Errorf("unable to parse certificate: %w", err)
	}

	for _, cert := range certs {
		if ok := strings.HasPrefix(cert.Subject.CommonName, user.SubjectPrefix); !ok {
			continue
		}
		names := strings.Split(strings.TrimPrefix(cert.Subject.CommonName, user.SubjectPrefix), ":")
		if len(names) != 2 {
			continue
		}
		return names[0], names[1], nil
	}

	return "", "", nil
}

func indexByClusterFunc(obj interface{}) ([]string, error) {
	accessor, err := meta.Accessor(obj)
	if err != nil {
		return nil, err
	}

	cluster, ok := accessor.GetLabels()[clientcert.ClusterNameLabel]
	if !ok {
		return []string{}, nil
	}

	// should not contain addon key
	if _, ok := accessor.GetLabels()[clientcert.AddonNameLabel]; ok {
		return []string{}, nil
	}

	return []string{cluster}, nil
}
