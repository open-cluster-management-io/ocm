package hubclientcert

import (
	"context"
	"crypto/tls"
	"fmt"
	"time"

	"github.com/openshift/library-go/pkg/controller/factory"
	"github.com/openshift/library-go/pkg/operator/events"
	corev1 "k8s.io/api/core/v1"
	kerrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	certificatesinformers "k8s.io/client-go/informers/certificates/v1beta1"
	csrclient "k8s.io/client-go/kubernetes/typed/certificates/v1beta1"
	corev1client "k8s.io/client-go/kubernetes/typed/core/v1"
	certificateslisters "k8s.io/client-go/listers/certificates/v1beta1"
	restclient "k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/util/keyutil"
	"k8s.io/klog"
)

const (
	// KubeconfigFile is the name of the kubeconfig file in kubeconfigSecret
	KubeconfigFile = "kubeconfig"
	// TLSKeyFile is the name of tls key file in kubeconfigSecret
	TLSKeyFile = "tls.key"
	// TLSCertFile is the name of the tls cert file in kubeconfigSecret
	TLSCertFile = "tls.crt"

	subjectPrefix         = "system:open-cluster-management:"
	clusterNameAnnotation = "open-cluster-management.io/cluster-name"
)

// ClientCertForHubController maintains the client cert and kubeconfig for hub
type ClientCertForHubController struct {
	clusterName               string
	agentName                 string
	kubeconfigSecretNamespace string
	kubeconfigSecretName      string
	hubClientConfig           *restclient.Config
	csrName                   string
	hubCSRLister              certificateslisters.CertificateSigningRequestLister
	hubCSRClient              csrclient.CertificateSigningRequestInterface
	spokeCoreClient           corev1client.CoreV1Interface
	keyData                   []byte
}

// NewClientCertForHubController return a ClientCertForHubController
func NewClientCertForHubController(clusterName, agentName, hubKubeconfigSecretNamespace,
	kubeconfigSecretName string, hubClientConfig *restclient.Config, spokeCoreClient corev1client.CoreV1Interface,
	hubCSRClient csrclient.CertificateSigningRequestInterface, csrInformer certificatesinformers.CertificateSigningRequestInformer,
	recorder events.Recorder, controllerNameOverride string) (factory.Controller, error) {
	c := &ClientCertForHubController{
		clusterName:               clusterName,
		agentName:                 agentName,
		kubeconfigSecretNamespace: hubKubeconfigSecretNamespace,
		kubeconfigSecretName:      kubeconfigSecretName,
		hubClientConfig:           hubClientConfig,
		hubCSRLister:              csrInformer.Lister(),
		hubCSRClient:              hubCSRClient,
		spokeCoreClient:           spokeCoreClient,
	}

	controllerName := "ClientCertForHubController"
	if controllerNameOverride != "" {
		controllerName = controllerNameOverride
	}

	return factory.New().
		WithInformers(csrInformer.Informer()).
		WithSync(c.sync).
		ResyncEvery(5*time.Minute).
		ToController(controllerName, recorder), nil
}

func (c *ClientCertForHubController) sync(ctx context.Context, syncCtx factory.SyncContext) error {
	// get kubeconfigSecret
	exists := true
	secret, err := c.spokeCoreClient.Secrets(c.kubeconfigSecretNamespace).Get(context.Background(), c.kubeconfigSecretName, metav1.GetOptions{})
	if err != nil {
		if kerrors.IsNotFound(err) {
			exists = false
			secret = &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: c.kubeconfigSecretNamespace,
					Name:      c.kubeconfigSecretName,
				},
			}
		} else {
			return fmt.Errorf("unable to get secret %q: %v", c.kubeconfigSecretNamespace+"/"+c.kubeconfigSecretName, err)
		}
	}

	// reconcile pending csr if exists
	if c.csrName != "" {
		updated, err := c.syncCSR(secret)
		if err != nil {
			return err
		}
		if !updated {
			return nil
		}

		// write the new cert/key pair into the secret
		if exists {
			_, err = c.spokeCoreClient.Secrets(c.kubeconfigSecretNamespace).Update(context.Background(), secret, metav1.UpdateOptions{})
		} else {
			_, err = c.spokeCoreClient.Secrets(c.kubeconfigSecretNamespace).Create(context.Background(), secret, metav1.CreateOptions{})
		}
		if err != nil {
			return err
		}
		klog.Info("A new client certificate is available")
		return nil
	}

	// create a csr to request new client certificate if
	// a. there is no client certificate
	// b. client certificate exists and has less than 20% of its life remaining
	if hasValidKubeconfig(secret) {
		commonName := fmt.Sprintf("%s%s:%s", subjectPrefix, c.clusterName, c.agentName)
		leaf, err := getCertLeaf(secret, commonName)
		if err != nil {
			return err
		}

		total := leaf.NotAfter.Sub(leaf.NotBefore)
		remaining := leaf.NotAfter.Sub(time.Now())
		if remaining.Seconds()/total.Seconds() > 0.2 {
			return nil
		}
		klog.Infof("Ther current client certificate for hub expires in %v. Start certificate rotation", remaining.Round(time.Second))
	} else {
		klog.Info("No valid client certificate for hub is found. Bootstrap is required")
	}

	// create a csr
	c.keyData, err = keyutil.MakeEllipticPrivateKeyPEM()
	if err != nil {
		return err
	}

	c.csrName, err = createCSR(c.hubCSRClient, c.keyData, c.clusterName, c.agentName)
	if err != nil {
		return err
	}
	klog.Infof("A csr %q is created", c.csrName)
	return nil
}

func (c *ClientCertForHubController) syncCSR(secret *corev1.Secret) (bool, error) {
	// skip if there is no ongoing csr
	if c.csrName == "" {
		return false, nil
	}

	// skip if csr no longer exists
	csr, err := c.hubCSRLister.Get(c.csrName)
	if err != nil {
		if kerrors.IsNotFound(err) {
			c.csrName = ""
			return false, nil
		}
		return false, err
	}

	// skip if csr is not approved yet
	if !isCSRApproved(csr) {
		return false, nil
	}

	// skip if csr has no certificate in its status yet
	if csr.Status.Certificate == nil {
		return false, nil
	}

	klog.V(4).Infof("Sync csr %q", c.csrName)
	// check if cert in csr status matches with the corresponding private key
	if c.keyData == nil {
		klog.Warningf("No private key found for certificate in csr: %s", c.csrName)
		c.csrName = ""
		return false, nil
	}
	_, err = tls.X509KeyPair(csr.Status.Certificate, c.keyData)
	if err != nil {
		klog.Warningf("Private key does not match with the certificate in csr: %s", c.csrName)
		c.csrName = ""
		return false, nil
	}

	// save the cert/key in secret
	if secret.Data == nil {
		secret.Data = map[string][]byte{}
	}
	secret.Data[TLSCertFile] = csr.Status.Certificate
	secret.Data[TLSKeyFile] = c.keyData

	// create a kubeconfig with references to the key/cert files in kubeconfigSecret if it dose not exists.
	// So other components deployed in separated deployments are able to access this kubeconfig for hub as
	// well by sharing the secret
	if _, ok := secret.Data[KubeconfigFile]; !ok {
		kubeconfig := buildKubeconfig(restclient.CopyConfig(c.hubClientConfig), TLSCertFile, TLSKeyFile)
		kubeconfigData, err := clientcmd.Write(kubeconfig)
		if err != nil {
			return false, err
		}
		secret.Data[KubeconfigFile] = kubeconfigData
	}

	// clear the csr name and private key
	c.csrName = ""
	c.keyData = nil

	return true, nil
}
