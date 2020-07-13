package hubclientcert

import (
	"context"
	"crypto/tls"
	"crypto/x509/pkix"
	"fmt"
	"reflect"
	"time"

	"github.com/openshift/library-go/pkg/controller/factory"
	"github.com/openshift/library-go/pkg/operator/events"

	certificates "k8s.io/api/certificates/v1beta1"
	corev1 "k8s.io/api/core/v1"
	kerrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	certificatesinformers "k8s.io/client-go/informers/certificates/v1beta1"
	corev1informers "k8s.io/client-go/informers/core/v1"
	csrclient "k8s.io/client-go/kubernetes/typed/certificates/v1beta1"
	corev1client "k8s.io/client-go/kubernetes/typed/core/v1"
	certificateslisters "k8s.io/client-go/listers/certificates/v1beta1"
	corev1lister "k8s.io/client-go/listers/core/v1"
	restclient "k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	certutil "k8s.io/client-go/util/cert"
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
	ClusterNameFile       = "cluster-name"
	AgentNameFile         = "agent-name"
)

// ControllerSyncInterval is exposed so that integration tests can crank up the constroller sync speed.
var ControllerSyncInterval = 5 * time.Minute

// ClientCertForHubController maintains the client cert and kubeconfig for hub
type ClientCertForHubController struct {
	clusterName string
	agentName   string
	// hubKubeconfigSecretNamespace is the namespace of the hubKubeconfigSecret.
	// The secret may contain the keys below:
	//   1. kubeconfig: kubeconfig file for hub with references to tls key/cert files in the same directory
	//   2. tls.key: tls key file
	//   3. tls.crt: tls cert file
	//   4. cluster-name: cluster name
	//   5. agent-name: agent name
	hubKubeconfigSecretNamespace string
	hubKubeconfigSecretName      string
	// hubClientConfig is an anonymous client config used as template to create the real client config for hub.
	// It should not be mutated by controller.
	hubClientConfig   *restclient.Config
	hubCSRLister      certificateslisters.CertificateSigningRequestLister
	hubCSRClient      csrclient.CertificateSigningRequestInterface
	spokeSecretLister corev1lister.SecretLister
	spokeCoreClient   corev1client.CoreV1Interface
	// csrName is the name of csr created by controller and waiting for approval.
	csrName string
	// keyData is the private key data used to created a csr
	// csrName and keyData store the internal state of the controller. They are set after controller creates a new csr
	// and cleared once the csr is approved and processed by controller. There are 4 combination of their values:
	//   1. csrName empty, keyData empty: means we aren't trying to create a new client cert, our current one is valid
	//   2. csrName set, keyData empty: there was bug
	//   3. csrName set, keyData set: we are waiting for a new cert to be signed.
	//   4. csrName empty, keydata set: the CSR failed to create, this shouldn't happen, it's a bug.
	keyData []byte
}

// NewClientCertForHubController return a ClientCertForHubController
func NewClientCertForHubController(
	clusterName, agentName, hubKubeconfigSecretNamespace, kubeconfigSecretName string,
	hubClientConfig *restclient.Config,
	spokeCoreClient corev1client.CoreV1Interface,
	hubCSRClient csrclient.CertificateSigningRequestInterface,
	hubCSRInformer certificatesinformers.CertificateSigningRequestInformer,
	spokeSecretInformer corev1informers.SecretInformer,
	recorder events.Recorder, controllerName string) factory.Controller {
	c := &ClientCertForHubController{
		clusterName:                  clusterName,
		agentName:                    agentName,
		hubKubeconfigSecretNamespace: hubKubeconfigSecretNamespace,
		hubKubeconfigSecretName:      kubeconfigSecretName,
		hubClientConfig:              hubClientConfig,
		hubCSRLister:                 hubCSRInformer.Lister(),
		hubCSRClient:                 hubCSRClient,
		spokeSecretLister:            spokeSecretInformer.Lister(),
		spokeCoreClient:              spokeCoreClient,
	}

	return factory.New().
		WithInformers(hubCSRInformer.Informer(), spokeSecretInformer.Informer()).
		WithSync(c.sync).
		ResyncEvery(ControllerSyncInterval).
		ToController(controllerName, recorder)
}

func (c *ClientCertForHubController) sync(ctx context.Context, syncCtx factory.SyncContext) error {
	// get hubKubeconfigSecret
	secret, err := c.spokeSecretLister.Secrets(c.hubKubeconfigSecretNamespace).Get(c.hubKubeconfigSecretName)
	switch {
	case kerrors.IsNotFound(err):
		secret = &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: c.hubKubeconfigSecretNamespace,
				Name:      c.hubKubeconfigSecretName,
			},
		}
	case err != nil:
		return fmt.Errorf("unable to get secret %q: %w", c.hubKubeconfigSecretNamespace+"/"+c.hubKubeconfigSecretName, err)
	}

	// reconcile pending csr if exists
	if c.csrName != "" {
		newSecretConfig, err := c.syncCSR(secret)
		if err != nil {
			c.reset()
			return err
		}
		if len(newSecretConfig) == 0 {
			return nil
		}

		newSecretConfig[ClusterNameFile] = []byte(c.clusterName)
		newSecretConfig[AgentNameFile] = []byte(c.agentName)
		secret.Data = newSecretConfig

		// save the changes into secret
		if err := c.saveHubKubeconfigSecret(secret); err != nil {
			return err
		}
		syncCtx.Recorder().Event("ClientCertificateCreated", "A new client certificate is available")
		return nil
	}

	// save the cluster name and agent name into secret if they are not saved yet
	newSecretConfig := map[string][]byte{}
	for k, v := range secret.Data {
		newSecretConfig[k] = v
	}
	newSecretConfig[ClusterNameFile] = []byte(c.clusterName)
	newSecretConfig[AgentNameFile] = []byte(c.agentName)
	if !reflect.DeepEqual(newSecretConfig, secret.Data) {
		secret.Data = newSecretConfig
		if err := c.saveHubKubeconfigSecret(secret); err != nil {
			return err
		}
	}

	// create a csr to request new client certificate if
	// a. there is no client certificate
	// b. client certificate exists and has less than 20% of its life remaining
	if hasValidKubeconfig(secret) {
		notBefore, notAfter, err := getCertValidityPeriod(secret)
		if err != nil {
			return err
		}

		total := notAfter.Sub(*notBefore)
		remaining := notAfter.Sub(time.Now())
		if remaining.Seconds()/total.Seconds() > 0.2 {
			// Do nothing if the client certificate is valid and has more than 20% of its life remaining
			klog.V(4).Info("Client certificate is valid and has more than 20% of its life remaining")
			return nil
		}
		syncCtx.Recorder().Eventf("CertificateRotationStarted", "The current client certificate for hub expires in %v. Start certificate rotation", remaining.Round(time.Second))
	} else {
		syncCtx.Recorder().Event("NoValidCertificateFound", "No valid client certificate for hub is found. Bootstrap is required")
	}

	// create a csr
	c.keyData, err = keyutil.MakeEllipticPrivateKeyPEM()
	if err != nil {
		return err
	}

	c.csrName, err = c.createCSR()
	if err != nil {
		c.reset()
		return err
	}
	syncCtx.Recorder().Eventf("CSRCreated", "A csr %q is created", c.csrName)
	return nil
}

func (c *ClientCertForHubController) syncCSR(secret *corev1.Secret) (map[string][]byte, error) {
	// skip if there is no ongoing csr
	if c.csrName == "" {
		c.reset()
		return nil, nil
	}

	// skip if csr no longer exists
	csr, err := c.hubCSRLister.Get(c.csrName)
	if kerrors.IsNotFound(err) {
		// fallback to fetching csr from hub apiserver in case it is not cached by informer yet
		csr, err = c.hubCSRClient.Get(context.Background(), c.csrName, metav1.GetOptions{})
		if kerrors.IsNotFound(err) {
			klog.V(4).Infof("Unable to get csr %q. It might have already been deleted.", c.csrName)
			c.reset()
			return nil, nil
		}
	}
	if err != nil {
		return nil, err
	}

	// skip if csr is not approved yet
	if !isCSRApproved(csr) {
		return nil, nil
	}

	// skip if csr has no certificate in its status yet
	if csr.Status.Certificate == nil {
		return nil, nil
	}

	klog.V(4).Infof("Sync csr %v", c.csrName)
	// check if cert in csr status matches with the corresponding private key
	if c.keyData == nil {
		c.reset()
		return nil, fmt.Errorf("No private key found for certificate in csr: %s", c.csrName)
	}
	_, err = tls.X509KeyPair(csr.Status.Certificate, c.keyData)
	if err != nil {
		c.reset()
		return nil, fmt.Errorf("Private key does not match with the certificate in csr: %s", c.csrName)
	}

	data := map[string][]byte{
		TLSCertFile: csr.Status.Certificate,
		TLSKeyFile:  c.keyData,
	}

	// create a kubeconfig with references to the key/cert files in kubeconfigSecret if it dose not exists.
	// So other components deployed in separated deployments are able to access this kubeconfig for hub as
	// well by sharing the secret
	kubeconfigData, ok := secret.Data[KubeconfigFile]
	if !ok {
		kubeconfig := buildKubeconfig(restclient.CopyConfig(c.hubClientConfig), TLSCertFile, TLSKeyFile)
		kubeconfigData, err = clientcmd.Write(kubeconfig)
		if err != nil {
			return nil, err
		}
	}
	data[KubeconfigFile] = kubeconfigData

	// clear the csr name and private key
	c.reset()
	return data, nil
}

func (c *ClientCertForHubController) createCSR() (string, error) {
	subject := &pkix.Name{
		Organization: []string{fmt.Sprintf("%s%s", subjectPrefix, c.clusterName)},
		CommonName:   fmt.Sprintf("%s%s:%s", subjectPrefix, c.clusterName, c.agentName),
	}

	privateKey, err := keyutil.ParsePrivateKeyPEM(c.keyData)
	if err != nil {
		return "", fmt.Errorf("invalid private key for certificate request: %w", err)
	}
	csrData, err := certutil.MakeCSR(privateKey, subject, nil, nil)
	if err != nil {
		return "", fmt.Errorf("unable to generate certificate request: %w", err)
	}

	signerName := certificates.KubeAPIServerClientSignerName

	csr := &certificates.CertificateSigningRequest{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: fmt.Sprintf("%s-", c.clusterName),
			Labels: map[string]string{
				// the label is only an hint for cluster name. Anyone could set/modify it.
				clusterNameAnnotation: c.clusterName,
			},
		},
		Spec: certificates.CertificateSigningRequestSpec{
			Request: csrData,
			Usages: []certificates.KeyUsage{
				certificates.UsageDigitalSignature,
				certificates.UsageKeyEncipherment,
				certificates.UsageClientAuth,
			},
			SignerName: &signerName,
		},
	}

	req, err := c.hubCSRClient.Create(context.TODO(), csr, metav1.CreateOptions{})
	if err != nil {
		return "", err
	}
	return req.Name, nil
}

func (c *ClientCertForHubController) saveHubKubeconfigSecret(secret *corev1.Secret) error {
	var err error
	if secret.ResourceVersion == "" {
		_, err = c.spokeCoreClient.Secrets(c.hubKubeconfigSecretNamespace).Create(context.Background(), secret, metav1.CreateOptions{})
		return err
	}
	_, err = c.spokeCoreClient.Secrets(c.hubKubeconfigSecretNamespace).Update(context.Background(), secret, metav1.UpdateOptions{})
	return err
}

func (c *ClientCertForHubController) reset() {
	c.csrName = ""
	c.keyData = nil
}
