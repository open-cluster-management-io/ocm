package registration

import (
	"crypto/x509/pkix"
	"fmt"
	"strings"

	"golang.org/x/net/context"
	certificates "k8s.io/api/certificates/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	certificatesinformers "k8s.io/client-go/informers/certificates"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"
	"k8s.io/klog/v2"

	addonv1alpha1 "open-cluster-management.io/api/addon/v1alpha1"
	hubclusterclientset "open-cluster-management.io/api/client/cluster/clientset/versioned"
	managedclusterinformers "open-cluster-management.io/api/client/cluster/informers/externalversions/cluster"
	clusterv1listers "open-cluster-management.io/api/client/cluster/listers/cluster/v1"
	clusterv1 "open-cluster-management.io/api/cluster/v1"
	"open-cluster-management.io/sdk-go/pkg/patcher"

	"open-cluster-management.io/ocm/pkg/registration/hub/user"
	"open-cluster-management.io/ocm/pkg/registration/register"
	awsIrsa "open-cluster-management.io/ocm/pkg/registration/register/aws_irsa"
	"open-cluster-management.io/ocm/pkg/registration/register/csr"
)

const (
	indexByCluster = "indexByCluster"

	// TODO(qiujian16) expose it if necessary in the future.
	clusterCSRThreshold = 10
)

func NewCSROption(
	logger klog.Logger,
	secretOption register.SecretOption,
	csrExpirationSeconds int32,
	hubCSRInformer certificatesinformers.Interface,
	hubKubeClient kubernetes.Interface) (*csr.CSROption, error) {
	csrControl, err := csr.NewCSRControl(logger, hubCSRInformer, hubKubeClient)
	if err != nil {
		return nil, fmt.Errorf("failed to create CSR control: %w", err)
	}
	var csrExpirationSecondsInCSROption *int32
	if csrExpirationSeconds != 0 {
		csrExpirationSecondsInCSROption = &csrExpirationSeconds
	}
	err = csrControl.Informer().AddIndexers(cache.Indexers{
		indexByCluster: indexByClusterFunc,
	})
	if err != nil {
		return nil, err
	}
	return &csr.CSROption{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: fmt.Sprintf("%s-", secretOption.ClusterName),
			Labels: map[string]string{
				// the label is only an hint for cluster name. Anyone could set/modify it.
				clusterv1.ClusterNameLabelKey: secretOption.ClusterName,
			},
		},
		Subject: &pkix.Name{
			Organization: []string{
				fmt.Sprintf("%s%s", user.SubjectPrefix, secretOption.ClusterName),
				user.ManagedClustersGroup,
			},
			CommonName: fmt.Sprintf("%s%s:%s", user.SubjectPrefix, secretOption.ClusterName, secretOption.AgentName),
		},
		SignerName: certificates.KubeAPIServerClientSignerName,
		EventFilterFunc: func(obj interface{}) bool {
			accessor, err := meta.Accessor(obj)
			if err != nil {
				return false
			}
			labels := accessor.GetLabels()
			// only enqueue csr from a specific managed cluster
			if labels[clusterv1.ClusterNameLabelKey] != secretOption.ClusterName {
				return false
			}

			// should not contain addon key
			_, ok := labels[addonv1alpha1.AddonLabelKey]
			if ok {
				return false
			}

			// only enqueue csr whose name starts with the cluster name
			return strings.HasPrefix(accessor.GetName(), fmt.Sprintf("%s-", secretOption.ClusterName))
		},
		HaltCSRCreation:   haltCSRCreationFunc(csrControl.Informer().GetIndexer(), secretOption.ClusterName),
		ExpirationSeconds: csrExpirationSecondsInCSROption,
		CSRControl:        csrControl,
	}, nil
}

func NewAWSOption(
	secretOption register.SecretOption,
	hubManagedClusterInformer managedclusterinformers.Interface,
	hubClusterClientSet hubclusterclientset.Interface) (*awsIrsa.AWSOption, error) {
	awsIrsaControl, err := awsIrsa.NewAWSIRSAControl(hubManagedClusterInformer, hubClusterClientSet)
	if err != nil {
		return nil, fmt.Errorf("failed to create AWS IRSA control: %w", err)
	}
	err = awsIrsaControl.Informer().AddIndexers(cache.Indexers{
		indexByCluster: indexByClusterFunc,
	})
	if err != nil {
		return nil, err
	}
	return &awsIrsa.AWSOption{
		EventFilterFunc: func(obj interface{}) bool {
			// TODO: implement EventFilterFunc and update below
			accessor, err := meta.Accessor(obj)
			if err != nil {
				return false
			}
			labels := accessor.GetLabels()
			// only enqueue csr from a specific managed cluster
			if labels[clusterv1.ClusterNameLabelKey] != secretOption.ClusterName {
				return false
			}

			// should not contain addon key
			_, ok := labels[addonv1alpha1.AddonLabelKey]
			if ok {
				return false
			}

			// only enqueue csr whose name starts with the cluster name
			return strings.HasPrefix(accessor.GetName(), fmt.Sprintf("%s-", secretOption.ClusterName))
		},
		AWSIRSAControl: awsIrsaControl,
	}, nil
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

func GenerateBootstrapStatusUpdater() register.StatusUpdateFunc {
	return func(ctx context.Context, cond metav1.Condition) error {
		return nil
	}
}

// GenerateStatusUpdater generates status update func for the certificate management
func GenerateStatusUpdater(hubClusterClient hubclusterclientset.Interface,
	hubClusterLister clusterv1listers.ManagedClusterLister, clusterName string) register.StatusUpdateFunc {
	return func(ctx context.Context, cond metav1.Condition) error {
		cluster, err := hubClusterLister.Get(clusterName)
		if errors.IsNotFound(err) {
			return nil
		}
		if err != nil {
			return err
		}
		newCluster := cluster.DeepCopy()
		meta.SetStatusCondition(&newCluster.Status.Conditions, cond)
		patcher := patcher.NewPatcher[
			*clusterv1.ManagedCluster, clusterv1.ManagedClusterSpec, clusterv1.ManagedClusterStatus](
			hubClusterClient.ClusterV1().ManagedClusters())
		_, err = patcher.PatchStatus(ctx, newCluster, newCluster.Status, cluster.Status)
		return err
	}
}

func indexByClusterFunc(obj interface{}) ([]string, error) {
	accessor, err := meta.Accessor(obj)
	if err != nil {
		return nil, err
	}

	cluster, ok := accessor.GetLabels()[clusterv1.ClusterNameLabelKey]
	if !ok {
		return []string{}, nil
	}

	// should not contain addon key
	if _, ok := accessor.GetLabels()[addonv1alpha1.AddonLabelKey]; ok {
		return []string{}, nil
	}

	return []string{cluster}, nil
}
