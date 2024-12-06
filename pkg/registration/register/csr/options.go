package csr

import (
	"crypto/x509/pkix"
	"fmt"
	"strings"

	"github.com/openshift/library-go/pkg/controller/factory"
	certificates "k8s.io/api/certificates/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	certificatesinformers "k8s.io/client-go/informers/certificates"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"
	"k8s.io/klog/v2"

	addonv1alpha1 "open-cluster-management.io/api/addon/v1alpha1"
	clusterv1 "open-cluster-management.io/api/cluster/v1"

	"open-cluster-management.io/ocm/pkg/registration/hub/user"
	"open-cluster-management.io/ocm/pkg/registration/register"
)

const (
	indexByCluster = "indexByCluster"

	// TODO(qiujian16) expose it if necessary in the future.
	clusterCSRThreshold = 10
)

// CSROption includes options that is used to create and monitor csrs
type CSROption struct {
	// ObjectMeta is the ObjectMeta shared by all created csrs. It should use GenerateName instead of Name
	// to generate random csr names
	ObjectMeta metav1.ObjectMeta
	// Subject represents the subject of the client certificate used to create csrs
	Subject *pkix.Name
	// DNSNames represents DNS names used to create the client certificate
	DNSNames []string
	// SignerName is the name of the signer specified in the created csrs
	SignerName string

	// ExpirationSeconds is the requested duration of validity of the issued
	// certificate.
	// Certificate signers may not honor this field for various reasons:
	//
	//   1. Old signer that is unaware of the field (such as the in-tree
	//      implementations prior to v1.22)
	//   2. Signer whose configured maximum is shorter than the requested duration
	//   3. Signer whose configured minimum is longer than the requested duration
	//
	// The minimum valid value for expirationSeconds is 3600, i.e. 1 hour.
	ExpirationSeconds *int32

	// EventFilterFunc matches csrs created with above options
	EventFilterFunc factory.EventFilterFunc

	CSRControl CSRControl

	// HaltCSRCreation halt the csr creation
	HaltCSRCreation func() bool
}

func NewCSROption(
	logger klog.Logger,
	secretOption register.SecretOption,
	csrExpirationSeconds int32,
	hubCSRInformer certificatesinformers.Interface,
	hubKubeClient kubernetes.Interface) (*CSROption, error) {
	csrControl, err := NewCSRControl(logger, hubCSRInformer, hubKubeClient)
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
	return &CSROption{
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
