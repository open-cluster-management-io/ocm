package hubclientcert

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io/ioutil"
	"path"
	"strings"
	"time"

	"github.com/openshift/library-go/pkg/controller/factory"
	"github.com/openshift/library-go/pkg/operator/events"
	kerrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes"
	corev1client "k8s.io/client-go/kubernetes/typed/core/v1"
	"k8s.io/client-go/rest"
	restclient "k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/clientcmd"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"
	"k8s.io/client-go/util/keyutil"
	"k8s.io/klog"
)

const (
	kubeconfigFile    = "kubeconfig"
	hubClientKeyFile  = "spoke-agent.key"
	hubClientCertFile = "spoke-agent.crt"
	tmpAgentNameFile  = "agent-name.tmp"
	tmpPrivateKeyFile = "spoke-agent.key.tmp"
	subjectPrefix     = "system:open-cluster-management:"
)

// ClientCertForHubController maintains the client cert and kubeconfig for hub
type ClientCertForHubController struct {
	kubeconfigSecret    string
	bootstrapSecret     string
	clusterNameOverride string
	kubeconfigDir       string
	spokeCoreClient     corev1client.CoreV1Interface

	kubeconfigSecretStore SecretStore
	bootstrapSecretReader SecretReader
	initialKubeconfigDir  string
	clusterName           string
	agentName             string
	bootstrapped          bool

	timer                *time.Timer
	initialHubKubeClient *kubernetes.Clientset
}

// NewClientCertForHubController return a ClientCertForHubController
func NewClientCertForHubController(hubKubeconfigSecret, bootstrapKubeconfigSecret, hubKubeconfigDir,
	clusterNameOverride string, spokeCoreClient corev1client.CoreV1Interface) (*ClientCertForHubController, error) {

	c := &ClientCertForHubController{
		kubeconfigSecret:    hubKubeconfigSecret,
		bootstrapSecret:     bootstrapKubeconfigSecret,
		clusterNameOverride: clusterNameOverride,
		kubeconfigDir:       hubKubeconfigDir,
		spokeCoreClient:     spokeCoreClient,
	}

	err := c.recove()
	if err != nil {
		return nil, err
	}

	return c, nil
}

// ToController convert ClientCertForHubController to a real contoller
func (c *ClientCertForHubController) ToController(csrInformer cache.SharedIndexInformer, recorder events.Recorder) factory.Controller {
	return factory.New().
		WithInformersQueueKeyFunc(func(obj runtime.Object) string {
			accessor, _ := meta.Accessor(obj)
			return accessor.GetName()
		}, csrInformer).
		WithSync(c.sync).
		//WithSyncDegradedOnError(operatorClient).
		ResyncEvery(30*time.Second).
		ToController("ClientCertForHubController", recorder)
}

// recove recoves the state of ClientCertForHubController according to data in kubeconfigSecret
func (c *ClientCertForHubController) recove() error {
	var err error
	c.kubeconfigSecretStore, err = NewSecretStore(c.kubeconfigSecret, c.spokeCoreClient)
	if err != nil {
		return err
	}

	c.bootstrapSecretReader, err = NewSecretReader(c.bootstrapSecret, c.spokeCoreClient)
	if err != nil {
		return err
	}

	c.agentName, c.bootstrapped, err = recoverAgentState(c.kubeconfigSecretStore, c.clusterNameOverride)
	if err != nil {
		return err
	}

	// parse cluster name from agent name
	names := strings.Split(c.agentName, ":")
	if len(names) != 2 {
		return fmt.Errorf("invalid agent name %q", c.agentName)
	}
	c.clusterName = names[0]

	// build initial kube client for hub
	var kubeconfigPath string
	if c.bootstrapped {
		kubeconfigPath = path.Join(c.kubeconfigDir, kubeconfigFile)
	} else {
		err = c.createInitialKubeconfigFile()
		if err != nil {
			return err
		}
		kubeconfigPath = path.Join(c.initialKubeconfigDir, kubeconfigFile)
	}

	clientConfig, err := loadHubClientConfig(kubeconfigPath)
	if err != nil {
		return err
	}

	c.initialHubKubeClient, err = kubernetes.NewForConfig(clientConfig)
	return err
}

// createInitialKubeconfigFile create an inital kubeconfig file, which is used to
// build kube client for controller informer. If spoke agent is bootstrapped, use
// the current kubeconfig for hub; otherwise, use bootstrap kubeconfig.
func (c *ClientCertForHubController) createInitialKubeconfigFile() error {
	kubeconfigData, exists, err := c.bootstrapSecretReader.Get(kubeconfigFile)
	if err != nil {
		return err
	}

	if !exists {
		return errors.New("no bootstrap kubeconfig found for hub")
	}

	clientConfig, err := clientcmd.RESTConfigFromKubeConfig(kubeconfigData)
	if err != nil {
		return err
	}

	// create a dir for initial kubeconfig file
	dir, err := ioutil.TempDir("", "prefix")
	if err != nil {
		return err
	}
	c.initialKubeconfigDir = dir
	klog.V(4).Infof("Initial kubeconfig directory created: %s", dir)

	// write cert/key data as files on disk
	certPath := path.Join(dir, hubClientCertFile)
	err = ioutil.WriteFile(certPath, clientConfig.TLSClientConfig.CertData, 0644)
	if err != nil {
		return err
	}
	keyPath := path.Join(dir, hubClientKeyFile)
	err = ioutil.WriteFile(keyPath, clientConfig.TLSClientConfig.KeyData, 0644)
	if err != nil {
		return err
	}

	// write the client config into file on disk
	kubeconfigPath := path.Join(dir, kubeconfigFile)
	kubeconfig := buildKubeconfig(clientConfig, hubClientCertFile, hubClientKeyFile)
	return clientcmd.WriteToFile(kubeconfig, kubeconfigPath)
}

// GetInitialHubKubeClient returns inital kube client for hub
func (c *ClientCertForHubController) GetInitialHubKubeClient() *kubernetes.Clientset {
	return c.initialHubKubeClient
}

// GetClientConfig returns client config for hub. If the client config is not available
// it will block.
func (c *ClientCertForHubController) GetClientConfig() (*rest.Config, error) {
	if !c.bootstrapped {
		wait.PollImmediateInfinite(1*time.Second, func() (bool, error) {
			return c.bootstrapped, nil
		})
	}

	kubeconfigPath := path.Join(c.kubeconfigDir, kubeconfigFile)
	return loadHubClientConfig(kubeconfigPath)
}

func (c *ClientCertForHubController) sync(ctx context.Context, syncCtx factory.SyncContext) error {
	name := syncCtx.QueueKey()
	if name == factory.DefaultQueueKey {
		return c.resync(ctx, syncCtx)
	}

	csr, err := c.initialHubKubeClient.CertificatesV1beta1().CertificateSigningRequests().Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		// ignore csr deleted
		if kerrors.IsNotFound(err) {
			return nil
		}
		return err
	}

	// ignore unapproved csr
	if !isCSRApproved(csr) {
		return nil
	}

	// ignore csr without certificate
	if csr.Status.Certificate == nil {
		return nil
	}

	// ignore csr with cert not issued for this agent
	agentName, err := getAgentNameFromCertificates(csr.Status.Certificate)
	if err != nil {
		return err
	}
	if agentName != c.agentName {
		return nil
	}

	// ignore csr with invalid cert
	valid, err := isClientCertificateStillValid(csr.Status.Certificate, "")
	if err != nil {
		return err
	}

	if !valid {
		return nil
	}

	// ignore csr if this is no temporary private key exists
	keyData, exists, err := c.kubeconfigSecretStore.Get(tmpPrivateKeyFile)
	if err != nil {
		return err
	}
	if !exists {
		return nil
	}

	// ignore csr if cert in csr status does not match the temporary private key
	_, err = tls.X509KeyPair(csr.Status.Certificate, keyData)
	if err != nil {
		return nil
	}

	// ignore the csr whose cert expires before the current client cert
	currentCert, exists, err := c.kubeconfigSecretStore.Get(hubClientCertFile)
	if err != nil {
		return err
	}

	if exists {
		commonName := subjectPrefix + c.agentName
		currentCertLeaf, err := getCertLeaf(currentCert, commonName)
		if err != nil {
			return err
		}

		csrCertLeaf, err := getCertLeaf(csr.Status.Certificate, commonName)
		if err != nil {
			return err
		}

		if currentCertLeaf.NotAfter.After(csrCertLeaf.NotAfter) || currentCertLeaf.NotAfter == csrCertLeaf.NotAfter {
			return nil
		}
	}

	// and then update the cert/key in secret and initial kubeconfig dir
	klog.Infof("Sync csr %q", name)
	err = c.updateClientCert(csr.Status.Certificate)
	if err != nil {
		return err
	}
	if !c.bootstrapped {
		c.bootstrapped = true
		klog.Info("Client certificate is available.")
	} else {
		klog.Info("Client certificate rotated.")
	}
	return nil
}

func (c *ClientCertForHubController) resync(ctx context.Context, syncCtx factory.SyncContext) error {
	if c.bootstrapped {
		if c.timer != nil {
			return nil
		}

		certData, exists, err := c.kubeconfigSecretStore.Get(hubClientCertFile)
		if err != nil {
			return err
		}
		if !exists {
			return errors.New("no cert data found")
		}

		leaf, err := getCertLeaf(certData, subjectPrefix+c.agentName)
		if err != nil {
			return err
		}

		deadline := nextRotationDeadline(leaf)
		if sleepInterval := time.Until(deadline); sleepInterval > 0 {
			go func() {
				klog.Infof("Wait %v for next certificate rotation", sleepInterval.Round(time.Second))
				c.timer = time.NewTimer(sleepInterval)
				defer func() {
					c.timer.Stop()
					c.timer = nil
				}()

				// unblock when deadline expires
				<-c.timer.C

				backoff := wait.Backoff{
					Duration: 2 * time.Second,
					Factor:   2,
					Jitter:   0.1,
					Steps:    5,
				}
				if err := wait.ExponentialBackoff(backoff, c.rotateCerts); err != nil {
					utilruntime.HandleError(fmt.Errorf("Reached backoff limit, still unable to rotate certs: %v", err))
					wait.PollInfinite(32*time.Second, c.rotateCerts)
				}

			}()
		}

	} else {
		err := c.bootstrap()
		if err != nil {
			return err
		}
	}

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

	// create csr for cert renewal
	err = renewClusterCertificate(c.initialHubKubeClient.CertificatesV1beta1().CertificateSigningRequests(), keyData, c.clusterName, c.agentName)
	if err != nil {
		utilruntime.HandleError(err)
		return false, nil
	}

	return true, nil
}

func (c *ClientCertForHubController) bootstrap() error {
	// reuse temporary private key if exists
	keyData, exists, err := c.kubeconfigSecretStore.Get(tmpPrivateKeyFile)
	if err != nil {
		return err
	}

	// otherwise create a new one
	if !exists {
		keyData, err = keyutil.MakeEllipticPrivateKeyPEM()
		if err != nil {
			return err
		}

		// save the new private key in secret so that it can be reused on next startup
		// if CSR request fails.
		_, err = c.kubeconfigSecretStore.Set(tmpPrivateKeyFile, keyData)
		if err != nil {
			return err
		}
	}

	// create bootstrap csr
	return requestClusterCertificate(c.initialHubKubeClient.CertificatesV1beta1().CertificateSigningRequests(), keyData, c.clusterName, c.agentName)
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
	delete(data, tmpAgentNameFile)
	delete(data, tmpPrivateKeyFile)

	// create a kubeconfig which refers to the key/cert files if it dose not exists.
	// The secret could be shared with other component in separated deployments. So they
	// are able to access the kubeconfig for hub as well.
	if _, ok := data[kubeconfigFile]; !ok {
		bootstrapKubeconfigData, exists, err := c.bootstrapSecretReader.Get(kubeconfigFile)
		if err != nil {
			return err
		}

		if !exists {
			return errors.New("no bootstrap kubeconfig found")
		}

		bootstrapClientConfig, err := clientcmd.RESTConfigFromKubeConfig(bootstrapKubeconfigData)
		if err != nil {
			return err
		}

		kubeconfig := buildKubeconfig(bootstrapClientConfig, hubClientCertFile, hubClientKeyFile)
		kubeconfigData, err := clientcmd.Write(kubeconfig)
		if err != nil {
			return err
		}
		data[kubeconfigFile] = kubeconfigData
	}

	// save the change into secret
	_, err = c.kubeconfigSecretStore.SetData(data)
	if err != nil {
		return err
	}

	// save the cert/key into initial kubeconfig dir either so the inital kube client
	// is able to reload the new client certificates
	if c.initialKubeconfigDir != "" {
		certPath := path.Join(c.initialKubeconfigDir, hubClientCertFile)
		err = ioutil.WriteFile(certPath, certData, 0644)
		if err != nil {
			return err
		}
		keyPath := path.Join(c.initialKubeconfigDir, hubClientKeyFile)
		err = ioutil.WriteFile(keyPath, keyData, 0644)
		if err != nil {
			return err
		}
	}

	return nil
}

// recoverAgentState recovers the current state of the cluster agent
func recoverAgentState(kubeconfigSecretStore SecretStore, clusterNameOverride string) (string, bool, error) {
	certData, exists, err := kubeconfigSecretStore.Get(hubClientCertFile)
	if err != nil {
		return "", false, err
	}

	if exists {
		valid, err := isClientCertificateStillValid(certData, clusterNameOverride)
		if err != nil {
			return "", false, nil
		}

		if valid {
			klog.Info("Client certificate for hub exists and is valid, skipping bootstrap")
			agentName, err := getAgentNameFromCertificates(certData)
			if err != nil {
				return "", false, err
			}

			klog.V(4).Infof("Agent %q is recoved from client certifiate for hub", agentName)
			return agentName, true, nil
		}

		klog.Info("Client certificate for hub is no long valid, bootstrap is required")
	} else {
		klog.Info("No client certificate for hub is found, bootstrap is required")
	}

	// reuse the agent name saved in secrec if it does not conflict with clusterNameOverride
	agentName, exists, err := kubeconfigSecretStore.GetString(tmpAgentNameFile)
	if exists {
		if clusterNameOverride == "" || strings.HasPrefix(agentName, clusterNameOverride) {
			klog.V(4).Infof("Reuse agent name %q", agentName)
			return agentName, false, nil
		}
	}

	// generate random cluster/agent name if agent name does not exists. Use the overrided
	// cluster name if it is specified
	agentName, err = generateAgentName(clusterNameOverride)
	if err != nil {
		return "", false, fmt.Errorf("unable to generate agent name: %v", err)
	}

	klog.V(4).Infof("Agene name generated: %s", agentName)

	// save the generated agent name in secret, and it can be reused once spoke agent is restared
	_, err = kubeconfigSecretStore.SetString(tmpAgentNameFile, agentName)
	if err != nil {
		return "", false, err
	}

	return agentName, false, nil
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

// generateClusterName generates a random name for cluster or return cluster UID if it's an openshift cluster
func generateClusterName() (string, error) {
	// TODO add logic to generate random cluster name
	return "cluster0", nil
}

// generateAgentName generates a random name for cluster agent
func generateAgentName(clusterName string) (string, error) {
	// TODO add logic to generate random agent name
	if clusterName == "" {
		var err error
		clusterName, err = generateClusterName()
		if err != nil {
			return "", err
		}
	}
	return clusterName + ":agent0", nil
}
