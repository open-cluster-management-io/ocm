package hubclientcert

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"path"
	"time"

	"github.com/openshift/library-go/pkg/controller/factory"
	"github.com/openshift/library-go/pkg/operator/events"
	kerrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	certificatesinformersv1beta1 "k8s.io/client-go/informers/certificates/v1beta1"
	"k8s.io/client-go/kubernetes"
	corev1client "k8s.io/client-go/kubernetes/typed/core/v1"
	certificateslistersv1beta1 "k8s.io/client-go/listers/certificates/v1beta1"
	"k8s.io/client-go/rest"
	restclient "k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"
	"k8s.io/client-go/util/keyutil"
	"k8s.io/klog"
)

const (
	kubeconfigFile    = "kubeconfig"
	hubClientKeyFile  = "spoke-agent.key"
	hubClientCertFile = "spoke-agent.crt"
	clusterNameFile   = "cluster-name.txt"
	agentNameFile     = "agent-name.txt"
	tmpPrivateKeyFile = "spoke-agent.key.tmp"
	subjectPrefix     = "system:open-cluster-management:"
)

// ClientCertForHubController maintains the client cert and kubeconfig for hub
type ClientCertForHubController struct {
	kubeconfigDir         string
	kubeconfigSecretStore *SecretStore
	clusterName           string
	agentName             string
	bootstrapped          bool
	bootstrapClientConfig *restclient.Config
	initialClientConfig   *restclient.Config
	csrLister             certificateslistersv1beta1.CertificateSigningRequestLister
	csrName               string
}

// NewClientCertForHubController return a ClientCertForHubController
func NewClientCertForHubController(hubKubeconfigSecretNamespace, hubKubeconfigSecret, hubKubeconfigDir,
	clusterName, agentName string, bootstrapClientConfig *restclient.Config, bootstrapped bool,
	spokeCoreClient corev1client.CoreV1Interface) (*ClientCertForHubController, error) {
	kubeconfigSecretStore := NewSecretStore(hubKubeconfigSecretNamespace, hubKubeconfigSecret, spokeCoreClient)

	// build initial hub client config for building csr informer and creating csrs
	var initialClientConfig *restclient.Config
	var err error
	if bootstrapped {
		kubeconfigPath := path.Join(hubKubeconfigDir, kubeconfigFile)
		initialClientConfig, err = loadHubClientConfig(kubeconfigPath)
		if err != nil {
			return nil, fmt.Errorf("unable to load hub kubeconfig: %q: %v", kubeconfigPath, err)
		}
	} else {
		initialClientConfig = restclient.CopyConfig(bootstrapClientConfig)
	}

	return &ClientCertForHubController{
		bootstrapClientConfig: bootstrapClientConfig,
		kubeconfigSecretStore: kubeconfigSecretStore,
		kubeconfigDir:         hubKubeconfigDir,
		clusterName:           clusterName,
		agentName:             agentName,
		bootstrapped:          bootstrapped,
		initialClientConfig:   initialClientConfig,
	}, nil
}

// ToController convert ClientCertForHubController to a real contoller
func (c *ClientCertForHubController) ToController(csrInformer certificatesinformersv1beta1.CertificateSigningRequestInformer, recorder events.Recorder) factory.Controller {
	c.csrLister = csrInformer.Lister()
	return factory.New().
		WithInformers(csrInformer.Informer()).
		WithSync(c.sync).
		ResyncEvery(5*time.Minute).
		ToController("ClientCertForHubController", recorder)
}

// GetInitialHubClientConfig returns an inital hub client config for building csr
// informer and creating csrs
func (c *ClientCertForHubController) GetInitialHubClientConfig() *restclient.Config {
	return c.initialClientConfig
}

// GetHubClientConfig returns client config for hub. It will block until
// the client config is available
func (c *ClientCertForHubController) GetHubClientConfig() (*rest.Config, error) {
	if !c.bootstrapped {
		wait.PollImmediateInfinite(1*time.Second, func() (bool, error) {
			return c.bootstrapped, nil
		})
	}

	kubeconfigPath := path.Join(c.kubeconfigDir, kubeconfigFile)
	return loadHubClientConfig(kubeconfigPath)
}

func (c *ClientCertForHubController) sync(ctx context.Context, syncCtx factory.SyncContext) error {
	if c.csrName != "" {
		return c.syncCSR()
	}

	backoff := wait.Backoff{
		Duration: 2 * time.Second,
		Factor:   2,
		Jitter:   0.1,
		Steps:    5,
	}
	if c.bootstrapped {
		// check the expiry time fo client certificate and create renewal csr if necessary
		certData, exists, err := c.kubeconfigSecretStore.Get(hubClientCertFile)
		if err != nil {
			return err
		}
		if !exists {
			return errors.New("no cert data found")
		}

		commonName := fmt.Sprintf("%s%s:%s", subjectPrefix, c.clusterName, c.agentName)
		leaf, err := getCertLeaf(certData, commonName)
		if err != nil {
			return err
		}

		// create renewal csr if the curent client certificate has less than 20% of its life remaining
		total := leaf.NotAfter.Sub(leaf.NotBefore)
		remaining := leaf.NotAfter.Sub(time.Now())
		if remaining.Seconds()/total.Seconds() < 0.2 {
			klog.Infof("Client certificate will expire in %v. Start certificate rotation", remaining.Round(time.Second))
			if err := wait.ExponentialBackoff(backoff, c.rotateCerts); err != nil {
				utilruntime.HandleError(fmt.Errorf("Reached backoff limit, still unable to rotate certs: %v", err))
				wait.PollInfinite(32*time.Second, c.rotateCerts)
			}
		}
	} else {
		// create bootstrap csr
		if err := wait.ExponentialBackoff(backoff, c.bootstrap); err != nil {
			utilruntime.HandleError(fmt.Errorf("Reached backoff limit, still unable to bootstrap agent: %v", err))
		}
	}
	return nil
}

func (c *ClientCertForHubController) syncCSR() error {
	// skip if there is no ongoing csr
	if c.csrName == "" {
		return nil
	}

	// skip if csr no longer exists
	csr, err := c.csrLister.Get(c.csrName)
	if err != nil {
		if kerrors.IsNotFound(err) {
			c.csrName = ""
			return nil
		}
		return err
	}

	// skip if csr is not approved yet
	if !isCSRApproved(csr) {
		return nil
	}

	// skip if csr has no certificate in its status yet
	if csr.Status.Certificate == nil {
		return nil
	}

	klog.V(4).Infof("Sync csr: %s", c.csrName)
	// check if cert in csr status matches with the corresponding private key
	keyData, exists, err := c.kubeconfigSecretStore.Get(tmpPrivateKeyFile)
	if err != nil {
		return err
	}
	if !exists {
		klog.Warningf("Private key is not found for certificate in csr: %s", c.csrName)
		c.csrName = ""
		return nil
	}
	_, err = tls.X509KeyPair(csr.Status.Certificate, keyData)
	if err != nil {
		klog.Warningf("Private key does not match with the certificate in csr: %s", c.csrName)
		c.csrName = ""
		return nil
	}

	// save the cert/key in secret
	err = c.updateClientCert(csr.Status.Certificate)
	if err != nil {
		return err
	}

	if c.bootstrapped {
		klog.Info("Client certificate rotated.")
	} else {
		// update the initial cient config to refer to the latest cert/key
		c.initialClientConfig.TLSClientConfig.CertData = nil
		c.initialClientConfig.TLSClientConfig.CertFile = path.Join(c.kubeconfigDir, hubClientCertFile)
		c.initialClientConfig.TLSClientConfig.KeyData = nil
		c.initialClientConfig.TLSClientConfig.KeyFile = path.Join(c.kubeconfigDir, hubClientKeyFile)

		c.bootstrapped = true
		klog.Info("Bootstrap is done and client certificate is available.")
	}

	// clear the csr name
	c.csrName = ""
	return nil
}

// rotateCerts creates renewal csr to rotate client certificates
func (c *ClientCertForHubController) rotateCerts() (bool, error) {
	// reuse temporary private key if exists
	keyData, exists, err := c.kubeconfigSecretStore.Get(tmpPrivateKeyFile)
	if err != nil {
		utilruntime.HandleError(err)
		return false, nil
	}

	// otherwise create a new one
	if !exists {
		keyData, err = keyutil.MakeEllipticPrivateKeyPEM()
		if err != nil {
			utilruntime.HandleError(fmt.Errorf("Unable to generate private key: %v", err))
			return false, nil
		}

		// save the new private key in secret so that it can be reused on next startup
		// if CSR request fails.
		_, err = c.kubeconfigSecretStore.Set(tmpPrivateKeyFile, keyData)
		if err != nil {
			utilruntime.HandleError(err)
			return false, nil
		}
	}

	// create a csr for cert renewal
	initialHubKubeClient, err := kubernetes.NewForConfig(c.initialClientConfig)
	if err != nil {
		utilruntime.HandleError(err)
		return false, nil
	}

	c.csrName, err = requestClusterCertificate(initialHubKubeClient.CertificatesV1beta1().CertificateSigningRequests(),
		keyData, c.clusterName, c.agentName, true)
	if err != nil {
		utilruntime.HandleError(err)
		return false, nil
	}

	return true, nil
}

func (c *ClientCertForHubController) bootstrap() (bool, error) {
	// reuse temporary private key if exists
	keyData, exists, err := c.kubeconfigSecretStore.Get(tmpPrivateKeyFile)
	if err != nil {
		utilruntime.HandleError(err)
		return false, nil
	}

	// otherwise create a new one
	if !exists {
		keyData, err = keyutil.MakeEllipticPrivateKeyPEM()
		if err != nil {
			utilruntime.HandleError(err)
			return false, nil
		}

		// save the new private key in secret so that it can be reused on next startup
		// if CSR request fails.
		_, err = c.kubeconfigSecretStore.Set(tmpPrivateKeyFile, keyData)
		if err != nil {
			utilruntime.HandleError(err)
			return false, nil
		}
	}

	// create a csr on hub for bootstrap
	initialHubKubeClient, err := kubernetes.NewForConfig(c.initialClientConfig)
	if err != nil {
		utilruntime.HandleError(err)
		return false, nil
	}
	c.csrName, err = requestClusterCertificate(initialHubKubeClient.CertificatesV1beta1().CertificateSigningRequests(),
		keyData, c.clusterName, c.agentName, false)
	if err != nil {
		utilruntime.HandleError(err)
		return false, nil
	}

	return true, nil
}

// updateClientCert saves cert and its coressponding key into secret. if inital
// kubeconfig dir exists, save the cert/key in that dir as well
func (c *ClientCertForHubController) updateClientCert(certData []byte) error {
	data, err := c.kubeconfigSecretStore.GetData()
	if err != nil {
		return err
	}

	keyData, ok := data[tmpPrivateKeyFile]
	if !ok {
		return errors.New("private key is not found for cert")
	}

	// put cert/key into secret data and delete temporary data
	data[hubClientKeyFile] = keyData
	data[hubClientCertFile] = certData
	delete(data, tmpPrivateKeyFile)

	// create a kubeconfig which refers to the key/cert files if it dose not exists.
	// The secret could be shared with other component in separated deployments. So they
	// are able to access the kubeconfig for hub as well.
	if _, ok := data[kubeconfigFile]; !ok {
		clientConfigTemplate := restclient.CopyConfig(c.bootstrapClientConfig)
		kubeconfig := buildKubeconfig(clientConfigTemplate, hubClientCertFile, hubClientKeyFile)
		kubeconfigData, err := clientcmd.Write(kubeconfig)
		if err != nil {
			return err
		}
		data[kubeconfigFile] = kubeconfigData
	}

	// save the change into secret
	_, err = c.kubeconfigSecretStore.SetData(data)
	return err
}

func loadHubClientConfig(kubeconfigPath string) (*restclient.Config, error) {
	// Load structured kubeconfig data from the given path.
	loader := &clientcmd.ClientConfigLoadingRules{ExplicitPath: kubeconfigPath}
	loadedConfig, err := loader.Load()
	if err != nil {
		return nil, err
	}

	// Flatten the loaded data to a particular restclient.Config based on the current context.
	return clientcmd.NewNonInteractiveClientConfig(
		*loadedConfig,
		loadedConfig.CurrentContext,
		&clientcmd.ConfigOverrides{},
		loader,
	).ClientConfig()

	/*
		kubeconfigData, err := ioutil.ReadFile(kubeconfigPath)
		if err != nil {
			return nil, err
		}
		kubeConfig, err := clientcmd.NewClientConfigFromBytes(kubeconfigData)
		if err != nil {
			return nil, err
		}
		return kubeConfig.ClientConfig()
	*/
}

// buildKubeconfig builds kubeconfig based on template rest config and a cert/key pair
func buildKubeconfig(clientConfig *restclient.Config, certPath, keyPath string) clientcmdapi.Config {
	// Get the CA data from the bootstrap client config.
	caFile, caData := clientConfig.CAFile, []byte{}
	if len(caFile) == 0 {
		caData = clientConfig.CAData
	}

	// Build kubeconfig.
	kubeconfig := clientcmdapi.Config{
		// Define a cluster stanza based on the bootstrap kubeconfig.
		Clusters: map[string]*clientcmdapi.Cluster{"default-cluster": {
			Server:                   clientConfig.Host,
			InsecureSkipTLSVerify:    clientConfig.Insecure,
			CertificateAuthority:     caFile,
			CertificateAuthorityData: caData,
		}},
		// Define auth based on the obtained client cert.
		AuthInfos: map[string]*clientcmdapi.AuthInfo{"default-auth": {
			ClientCertificate: certPath,
			ClientKey:         keyPath,
		}},
		// Define a context that connects the auth info and cluster, and set it as the default
		Contexts: map[string]*clientcmdapi.Context{"default-context": {
			Cluster:   "default-cluster",
			AuthInfo:  "default-auth",
			Namespace: "default",
		}},
		CurrentContext: "default-context",
	}

	return kubeconfig
}

// LoadClientConfigFromSecret returns a client config stored in a secret
func LoadClientConfigFromSecret(namespace, name string, spokeCoreClient corev1client.CoreV1Interface) (*restclient.Config, error) {
	secret, err := spokeCoreClient.Secrets(namespace).Get(context.Background(), name, metav1.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("unable to load client config from secret %q: %v", namespace+"/"+name, err)
	}

	if secret.Data == nil {
		secret.Data = map[string][]byte{}
	}

	kubeconfigData, ok := secret.Data[kubeconfigFile]
	if !ok {
		return nil, fmt.Errorf("no kubeconfig found in secret %q", namespace+"/"+name)
	}

	clientConfig, err := clientcmd.RESTConfigFromKubeConfig(kubeconfigData)
	if err != nil {
		return nil, fmt.Errorf("unable to parse kubeconfig from secret %q: %v", namespace+"/"+name, err)
	}

	return clientConfig, nil
}
