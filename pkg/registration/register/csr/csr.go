package csr

import (
	"context"
	"crypto/x509/pkix"
	"fmt"
	"os"
	"path"

	"github.com/openshift/library-go/pkg/controller/factory"
	"github.com/openshift/library-go/pkg/operator/events"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"
	"k8s.io/klog/v2"

	"open-cluster-management.io/ocm/pkg/registration/register"
)

const (
	// TLSKeyFile is the name of tls key file in kubeconfigSecret
	TLSKeyFile = "tls.key"
	// TLSCertFile is the name of the tls cert file in kubeconfigSecret
	TLSCertFile = "tls.crt"

	// ClusterCertificateRotatedCondition is a condition type that client certificate is rotated
	ClusterCertificateRotatedCondition = "ClusterCertificateRotated"
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

type CSRDriver struct{}

func (c *CSRDriver) Start(
	ctx context.Context,
	name string,
	updater register.StatusUpdateFunc,
	recorder events.Recorder,
	secretOption register.SecretOption,
	option any,
	addtionalData map[string][]byte) {
	logger := klog.FromContext(ctx)
	csrOption, ok := option.(*CSROption)
	if !ok {
		logger.Error(fmt.Errorf("option type is not correct"), "option type is not correct")
		return
	}
	ctrl := NewClientCertificateController(
		secretOption.SecretNamespace, secretOption.SecretName, addtionalData, *csrOption, updater,
		secretOption.ManagementSecretInformer, secretOption.ManagementCoreClient, recorder, name)
	ctrl.Run(ctx, 1)
}

func (c *CSRDriver) BuildKubeConfigFromBootstrap(bootstrapConfig *clientcmdapi.Config) (*clientcmdapi.Config, error) {
	kubeConfig, err := register.BaseKubeConfigFromBootStrap(bootstrapConfig)
	if err != nil {
		return nil, err
	}

	kubeConfig.AuthInfos = map[string]*clientcmdapi.AuthInfo{register.DefaultKubeConfigAuth: {
		ClientCertificate: TLSCertFile,
		ClientKey:         TLSKeyFile,
	}}

	return kubeConfig, nil
}

func (c *CSRDriver) IsHubKubeConfigValid(ctx context.Context, secretOption register.SecretOption) (bool, error) {
	logger := klog.FromContext(ctx)
	keyPath := path.Join(secretOption.HubKubeconfigDir, TLSKeyFile)
	if _, err := os.Stat(keyPath); os.IsNotExist(err) {
		logger.V(4).Info("TLS key file not found", "keyPath", keyPath)
		return false, nil
	}

	certPath := path.Join(secretOption.HubKubeconfigDir, TLSCertFile)
	certData, err := os.ReadFile(path.Clean(certPath))
	if err != nil {
		logger.V(4).Info("Unable to load TLS cert file", "certPath", certPath)
		return false, nil
	}

	// only set when clustername/agentname are set
	if len(secretOption.ClusterName) > 0 && len(secretOption.AgentName) > 0 {
		// check if the tls certificate is issued for the current cluster/agent
		clusterNameInCert, agentNameInCert, err := GetClusterAgentNamesFromCertificate(certData)
		if err != nil {
			return false, nil
		}
		if secretOption.ClusterName != clusterNameInCert || secretOption.AgentName != agentNameInCert {
			logger.V(4).Info("Certificate in file is issued for different agent",
				"certPath", certPath,
				"issuedFor", fmt.Sprintf("%s:%s", secretOption.ClusterName, secretOption.AgentName),
				"expectedFor", fmt.Sprintf("%s:%s", secretOption.ClusterName, secretOption.AgentName))

			return false, nil
		}
	}

	return isCertificateValid(logger, certData, nil)
}

func NewCSRDriver() register.RegisterDriver {
	return &CSRDriver{}
}
