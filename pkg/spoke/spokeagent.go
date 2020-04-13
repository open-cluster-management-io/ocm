package spoke

import (
	"context"
	"errors"
	"fmt"
	"io/ioutil"
	"os"
	"path"
	"time"

	"github.com/open-cluster-management/registration/pkg/spoke/hubclientcert"
	"github.com/openshift/library-go/pkg/controller/controllercmd"
	"github.com/openshift/library-go/pkg/controller/factory"
	"github.com/openshift/library-go/pkg/operator/events"
	"github.com/spf13/pflag"
	corev1 "k8s.io/api/core/v1"
	kerrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	utilrand "k8s.io/apimachinery/pkg/util/rand"
	"k8s.io/apimachinery/pkg/util/uuid"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	corev1client "k8s.io/client-go/kubernetes/typed/core/v1"
	restclient "k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"
	"k8s.io/klog"
)

const (
	spokeAgentNameLength           = 5
	defaultSpokeComponentNamespace = "open-cluster-management"
	clusterNameSecretDataKey       = "cluster-name"
	agentNameSecretDataKey         = "agent-name"
)

// SpokeAgentOptions holds configuration for spoke cluster agent
type SpokeAgentOptions struct {
	ComponentNamespace  string
	ClusterName         string
	BootstrapKubeconfig string
	HubKubeconfigSecret string
	HubKubeconfigDir    string
}

// NewSpokeAgentOptions returns a SpokeAgentOptions
func NewSpokeAgentOptions() *SpokeAgentOptions {
	return &SpokeAgentOptions{
		HubKubeconfigSecret: "hub-kubeconfig-secret",
		HubKubeconfigDir:    "/spoke/hub-kubeconfig",
	}
}

// RunSpokeAgent starts the controllers on spoke agent to register to hub.
// It handles the following scenarios:
//   #1. Bootstrap kubeconfig is valid and there is no valid hub kubeconfig in secret
//   #2. Both bootstrap kubeconfig and hub kubeconfig are valid
//   #3. Bootstrap kubeconfig is invalid (e.g. certificte expired) and hub kubeconfig is valid
//   #4. Neither bootstrap kubeconfig nor hub kubeconfig is valid
// A temporary ClientCertForHubController with bootstrap kubeconfig is created and started if
// hub kubeconfig does not exists or is invalid. It will be stopped once client config for hub
// is ready. And then run other controllers, including another ClientCertForHubController which
// is created for client certificate rotation.
func (o *SpokeAgentOptions) RunSpokeAgent(ctx context.Context, controllerContext *controllercmd.ControllerContext) error {
	if err := o.Validate(); err != nil {
		klog.Fatal(err)
	}

	if err := o.Complete(); err != nil {
		klog.Fatal(err)
	}

	// create kube client for spoke cluster
	spokeKubeClient, err := kubernetes.NewForConfig(controllerContext.KubeConfig)
	if err != nil {
		return err
	}

	// resolve cluster name and agent name. Cluster name will be used by multiple controllers
	clusterName, agentName, err := getOrGenerateClusterAgentNames(o.ClusterName, o.ComponentNamespace, o.HubKubeconfigSecret, spokeKubeClient.CoreV1())
	if err != nil {
		return fmt.Errorf("unable to get or generate cluster/agent names: %v", err)
	}
	klog.Infof("Cluster name is %q", clusterName)
	klog.V(4).Infof("Agent name is %q", agentName)

	// check if there already exists a valid client config for hub
	ok, err := o.hasValidHubClientConfig()
	if err != nil {
		return err
	}

	// create and start a ClientCertForHubController for spoke agent bootstrap to deal with scenario #1 and #4.
	// Running the bootsrap ClientCertForHubController is optional. If always run it no matter if there already
	// exists a valid client config for hub or not, the controller will be started and then stopped immediately
	// in scenario #2 and #3, which results in an error message in log: 'Observed a panic: timeout waiting for
	// informer cache'
	if !ok {
		// load bootstrap clent config
		bootstrapClientConfig, err := hubclientcert.LoadClientConfig(o.BootstrapKubeconfig)
		if err != nil {
			return fmt.Errorf("unable to load bootstrap kubeconfig from file %q: %v", o.BootstrapKubeconfig, err)
		}

		// create and start a ClientCertForHubController for spoke agent bootstrap
		hubCSRInformer, clientCertForHubController, err := o.buildClientCertForHubController(clusterName, agentName,
			bootstrapClientConfig, spokeKubeClient.CoreV1(), controllerContext.EventRecorder, "BoostrapClientCertForHubController")
		if err != nil {
			return err
		}
		bootstrapCtx, stopBootstrap := context.WithCancel(ctx)
		go hubCSRInformer.Run(bootstrapCtx.Done())
		go clientCertForHubController.Run(bootstrapCtx, 1)

		// wait for the client config for hub is ready
		err = wait.PollImmediateInfinite(1*time.Second, o.hasValidHubClientConfig)
		if err != nil {
			return err
		}

		// stop the ClientCertForHubController for bootstrap once the client config for hub is ready
		stopBootstrap()
	}

	kubeconfigPath := path.Join(o.HubKubeconfigDir, hubclientcert.KubeconfigFile)
	hubClientConfig, err := hubclientcert.LoadClientConfig(kubeconfigPath)
	if err != nil {
		return err
	}

	klog.Info("Client config for hub is ready.")
	// build and start another ClientCertForHubController for client certificate rotation
	hubCSRInformer, clientCertForHubController, err := o.buildClientCertForHubController(clusterName, agentName,
		hubClientConfig, spokeKubeClient.CoreV1(), controllerContext.EventRecorder, "")
	if err != nil {
		return err
	}

	go hubCSRInformer.Run(ctx.Done())
	go clientCertForHubController.Run(ctx, 1)

	<-ctx.Done()
	return nil
}

// AddFlags registers flags for Agent
func (o *SpokeAgentOptions) AddFlags(fs *pflag.FlagSet) {
	fs.StringVar(&o.ClusterName, "cluster-name", o.ClusterName,
		"If non-empty, will use as cluster name instead of generated random name.")
	fs.StringVar(&o.BootstrapKubeconfig, "bootstrap-kubeconfig", o.BootstrapKubeconfig,
		"The path of the kubeconfig file for spoke agent bootstrap.")
	fs.StringVar(&o.HubKubeconfigSecret, "hub-kubeconfig-secret", o.HubKubeconfigSecret,
		"The name of secret in component namespace storing kubeconfig for hub.")
	fs.StringVar(&o.HubKubeconfigDir, "hub-kubeconfig-dir", o.HubKubeconfigDir,
		"The mount path of hub-kubeconfig-secret in the container.")
}

// Validate verifies the inputs.
func (o *SpokeAgentOptions) Validate() error {
	if o.BootstrapKubeconfig == "" {
		return errors.New("bootstrap-kubeconfig is required")
	}
	return nil
}

// Complete fills in missing values.
func (o *SpokeAgentOptions) Complete() error {
	// get component namespace of spoke agent
	nsBytes, err := ioutil.ReadFile("/var/run/secrets/kubernetes.io/serviceaccount/namespace")
	if err != nil {
		o.ComponentNamespace = defaultSpokeComponentNamespace
	} else {
		o.ComponentNamespace = string(nsBytes)
	}

	return nil
}

// generateClusterName generates a name for spoke cluster
func generateClusterName() string {
	// TODO return cluster ID if the spoke agent is running in an ocp cluster
	return string(uuid.NewUUID())
}

// generateAgentName generates a random name for spoke cluster agent
func generateAgentName() string {
	return utilrand.String(spokeAgentNameLength)
}

// buildClientCertForHubController creates and returns a csr informer and a ClientCertForHubController
func (o *SpokeAgentOptions) buildClientCertForHubController(clusterName, agentName string,
	initialHubClientConfig *restclient.Config, spokeCoreClient corev1client.CoreV1Interface,
	recorder events.Recorder, controllerNameOverride string) (cache.SharedIndexInformer, factory.Controller, error) {
	initialHubKubeClient, err := kubernetes.NewForConfig(initialHubClientConfig)
	if err != nil {
		return nil, nil, err
	}

	hubCSRInformer := informers.NewSharedInformerFactory(initialHubKubeClient,
		10*time.Minute).Certificates().V1beta1().CertificateSigningRequests()
	clientCertForHubController, err := hubclientcert.NewClientCertForHubController(clusterName, agentName,
		o.ComponentNamespace, o.HubKubeconfigSecret, initialHubClientConfig, spokeCoreClient,
		initialHubKubeClient.CertificatesV1beta1().CertificateSigningRequests(), hubCSRInformer, recorder, controllerNameOverride)
	if err != nil {
		return nil, nil, err
	}
	return hubCSRInformer.Informer(), clientCertForHubController, nil
}

// hasValidHubClientConfig returns ture if the conditions below are met:
//   1. KubeconfigFile exists
//   2. TLSKeyFile exists
//   3. TLSCertFile exists and the certificate is not expired
func (o *SpokeAgentOptions) hasValidHubClientConfig() (bool, error) {
	kubeconfigPath := path.Join(o.HubKubeconfigDir, hubclientcert.KubeconfigFile)
	if _, err := os.Stat(kubeconfigPath); os.IsNotExist(err) {
		klog.V(4).Infof("Kubeconfig file %q not found", kubeconfigPath)
		return false, nil
	}

	keyPath := path.Join(o.HubKubeconfigDir, hubclientcert.TLSKeyFile)
	if _, err := os.Stat(keyPath); os.IsNotExist(err) {
		klog.V(4).Infof("TLS key file %q not found", keyPath)
		return false, nil
	}

	certPath := path.Join(o.HubKubeconfigDir, hubclientcert.TLSCertFile)
	certData, err := ioutil.ReadFile(certPath)
	if err != nil {
		klog.V(4).Infof("Unable to load TLS cert file %q", certPath)
		return false, nil
	}

	return hubclientcert.IsCertificatetValid(certData)
}

// getOrGenerateClusterAgentNames returns cluster name and agent name.
// Rules for picking up cluster/agent names:
//
//   1. take clusterName from input arguments if it is not empty
//   2. take cluster/agent names in secret if they exist
//   3. generate random cluster/agent names then
//
// the final cluster/agent names will be saved into secret
func getOrGenerateClusterAgentNames(clusterName, secretNamespace, secretName string, coreClient corev1client.CoreV1Interface) (string, string, error) {
	var agentName string

	// load cluster/agent names from secret
	exists := true
	secret, err := coreClient.Secrets(secretNamespace).Get(context.Background(), secretName, metav1.GetOptions{})
	if err != nil {
		if kerrors.IsNotFound(err) {
			exists = false
			secret = &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: secretNamespace,
					Name:      secretName,
				},
			}
		} else {
			return "", "", fmt.Errorf("unable to get secret %q: %v", secretNamespace+"/"+secretName, err)
		}
	} else {
		if clusterName == "" {
			clusterName = string(secret.Data[clusterNameSecretDataKey])
		}
		agentName = string(secret.Data[agentNameSecretDataKey])
	}

	// generate cluster/agent names if necessary
	if clusterName == "" {
		clusterName = generateClusterName()
	}
	if agentName == "" {
		agentName = generateAgentName()
	}

	// save cluster/agent names into secret
	if secret.Data == nil {
		secret.Data = map[string][]byte{}
	}
	secret.Data[clusterNameSecretDataKey] = []byte(clusterName)
	secret.Data[agentNameSecretDataKey] = []byte(agentName)

	if exists {
		_, err = coreClient.Secrets(secretNamespace).Update(context.Background(), secret, metav1.UpdateOptions{})
	} else {
		_, err = coreClient.Secrets(secretNamespace).Create(context.Background(), secret, metav1.CreateOptions{})
	}
	if err != nil {
		return "", "", err
	}

	return clusterName, agentName, nil
}
