package hubclientcert

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/openshift/library-go/pkg/controller/factory"
	"github.com/openshift/library-go/pkg/operator/events"
	certificates "k8s.io/api/certificates/v1beta1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/informers"
	kubefake "k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/util/keyutil"
)

func newSecret(namespace, name, resourceVersion string, data map[string][]byte) *corev1.Secret {
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Namespace:       namespace,
			Name:            name,
			ResourceVersion: resourceVersion,
		},
		Data: data,
	}

	return secret
}

func TestSyncCSR(t *testing.T) {
	secretNamespace := "default"
	secretName := "secret"
	secret := newSecret(secretNamespace, secretName, "", nil)

	key, cert, err := newCertKey("cluster0", 10*time.Second)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	csrName := "csr1"
	csr := &certificates.CertificateSigningRequest{
		ObjectMeta: metav1.ObjectMeta{
			Name: csrName,
		},
		Spec: certificates.CertificateSigningRequestSpec{},
		Status: certificates.CertificateSigningRequestStatus{
			Conditions: []certificates.CertificateSigningRequestCondition{
				{
					Type: certificates.CertificateApproved,
				},
			},
			Certificate: cert,
		},
	}

	fakeKubeClient := kubefake.NewSimpleClientset(csr, secret)
	csrInformer := informers.NewSharedInformerFactory(fakeKubeClient,
		3*time.Minute).Certificates().V1beta1().CertificateSigningRequests()

	// create csr informer/lister
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go csrInformer.Informer().Run(ctx.Done())
	if ok := cache.WaitForCacheSync(ctx.Done(), csrInformer.Informer().HasSynced); !ok {
		t.Error("failed to wait for kubernetes caches to sync")
	}

	// create a fake client config as template
	kubeconfigData, err := clientcmd.Write(newKubeconfig(key, cert))
	clientConfig, err := clientcmd.RESTConfigFromKubeConfig(kubeconfigData)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	controller := &ClientCertForHubController{
		csrName:         csrName,
		keyData:         key,
		hubCSRLister:    csrInformer.Lister(),
		hubClientConfig: clientConfig,
		hubCSRClient:    fakeKubeClient.CertificatesV1beta1().CertificateSigningRequests(),
	}
	newSecretConfig, err := controller.syncCSR(secret)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if newSecretConfig == nil {
		t.Error("secret should be changed")
	}

	// check if csrName/keyData are cleared
	if controller.csrName != "" {
		t.Error("controller.csrName should be empty")
	}
	if controller.keyData != nil {
		t.Error("controller.keyData should be nil")
	}

	// validate the kubeconfig in secret
	secret.Data = newSecretConfig
	if !hasValidKubeconfig(secret) {
		t.Error("kubeconfig should be valid")
	}
}

// test bootstrap
func TestSyncWithoutHubKubeconfigSecret(t *testing.T) {
	secretNamespace := "default"
	secretName := "secret"

	clusterName := "cluster0"
	agentName := "agent0"
	fakeKubeClient := kubefake.NewSimpleClientset()

	// create secret informer/lister
	secretInformer := informers.NewSharedInformerFactory(fakeKubeClient,
		3*time.Minute).Core().V1().Secrets()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go secretInformer.Informer().Run(ctx.Done())
	if ok := cache.WaitForCacheSync(ctx.Done(), secretInformer.Informer().HasSynced); !ok {
		t.Error("failed to wait for kubernetes caches to sync")
	}

	controller := &ClientCertForHubController{
		clusterName:                  clusterName,
		agentName:                    agentName,
		hubKubeconfigSecretNamespace: secretNamespace,
		hubKubeconfigSecretName:      secretName,
		spokeCoreClient:              fakeKubeClient.CoreV1(),
		hubCSRClient:                 fakeKubeClient.CertificatesV1beta1().CertificateSigningRequests(),
		spokeSecretLister:            secretInformer.Lister(),
	}

	eventRecorder := events.NewInMemoryRecorder("")
	syncContext := factory.NewSyncContext("", eventRecorder)
	err := controller.sync(nil, syncContext)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	secret, err := fakeKubeClient.CoreV1().Secrets(secretNamespace).Get(context.Background(), secretName, metav1.GetOptions{})
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	// check if cluster/agent name are stored into secret
	if len(secret.Data) == 0 {
		t.Errorf("secret should have data stored")
	}
	if string(secret.Data[ClusterNameFile]) != clusterName {
		t.Errorf("expected cluster name %q but got: %s", clusterName, string(secret.Data[ClusterNameFile]))
	}
	if string(secret.Data[AgentNameFile]) != agentName {
		t.Errorf("expected agent name %q but got: %s", agentName, string(secret.Data[AgentNameFile]))
	}

	// checkc if there is new csr created
	csrs, err := fakeKubeClient.CertificatesV1beta1().CertificateSigningRequests().List(context.Background(), metav1.ListOptions{})
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	if len(csrs.Items) != 1 {
		t.Errorf("expect 1 csr created, but got: %d", len(csrs.Items))
	}

	// check if csrName/keyData are set. Since GenerateName is not working for fake clent, check keyData only
	if controller.keyData == nil {
		t.Error("controller.keyData should be set")
	}
}

func TestSyncWithValidKubeconfig(t *testing.T) {
	kubeconfigData, err := clientcmd.Write(newKubeconfig(nil, nil))
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	clusterName := "cluster0"
	agentName := "agent0"
	key, cert, err := newCertKey(fmt.Sprintf("%s%s:%s", subjectPrefix, clusterName, agentName), 100*time.Second)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	secretNamespace := "default"
	secretName := "secret"
	secret := newSecret(secretNamespace, secretName, "1", map[string][]byte{
		KubeconfigFile: kubeconfigData,
		TLSCertFile:    cert,
		TLSKeyFile:     key,
	})

	fakeKubeClient := kubefake.NewSimpleClientset(secret)

	// create secret informer/lister
	secretInformer := informers.NewSharedInformerFactory(fakeKubeClient,
		3*time.Minute).Core().V1().Secrets()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go secretInformer.Informer().Run(ctx.Done())
	if ok := cache.WaitForCacheSync(ctx.Done(), secretInformer.Informer().HasSynced); !ok {
		t.Error("failed to wait for kubernetes caches to sync")
	}

	controller := &ClientCertForHubController{
		clusterName:                  clusterName,
		agentName:                    agentName,
		hubKubeconfigSecretNamespace: secretNamespace,
		hubKubeconfigSecretName:      secretName,
		spokeCoreClient:              fakeKubeClient.CoreV1(),
		hubCSRClient:                 fakeKubeClient.CertificatesV1beta1().CertificateSigningRequests(),
		spokeSecretLister:            secretInformer.Lister(),
	}

	eventRecorder := events.NewInMemoryRecorder("")
	syncContext := factory.NewSyncContext("", eventRecorder)
	err = controller.sync(nil, syncContext)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	// check if there is any csr created
	csrs, err := fakeKubeClient.CertificatesV1beta1().CertificateSigningRequests().List(context.Background(), metav1.ListOptions{})
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	if len(csrs.Items) != 0 {
		t.Errorf("expect 0 csr created, but got: %d", len(csrs.Items))
	}

	// check if csrName/keyData are unset
	if controller.csrName != "" {
		t.Error("controller.csrName should be empty")
	}
	if controller.keyData != nil {
		t.Error("controller.keyData should be nil")
	}
}

// test cert rotation
func TestSyncWithExpiringCert(t *testing.T) {
	kubeconfigData, err := clientcmd.Write(newKubeconfig(nil, nil))
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	clusterName := "cluster0"
	agentName := "agent0"

	key, cert, err := newCertKey(fmt.Sprintf("%s%s:%s", subjectPrefix, clusterName, agentName), -3*time.Second)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	secretNamespace := "default"
	secretName := "secret"
	secret := newSecret(secretNamespace, secretName, "1", map[string][]byte{
		KubeconfigFile: kubeconfigData,
		TLSCertFile:    cert,
		TLSKeyFile:     key,
	})

	fakeKubeClient := kubefake.NewSimpleClientset(secret)

	// create secret informer/lister
	secretInformer := informers.NewSharedInformerFactory(fakeKubeClient,
		3*time.Minute).Core().V1().Secrets()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go secretInformer.Informer().Run(ctx.Done())
	if ok := cache.WaitForCacheSync(ctx.Done(), secretInformer.Informer().HasSynced); !ok {
		t.Error("failed to wait for kubernetes caches to sync")
	}

	controller := &ClientCertForHubController{
		clusterName:                  clusterName,
		agentName:                    agentName,
		hubKubeconfigSecretNamespace: secretNamespace,
		hubKubeconfigSecretName:      secretName,
		spokeCoreClient:              fakeKubeClient.CoreV1(),
		hubCSRClient:                 fakeKubeClient.CertificatesV1beta1().CertificateSigningRequests(),
		spokeSecretLister:            secretInformer.Lister(),
	}

	eventRecorder := events.NewInMemoryRecorder("")
	syncContext := factory.NewSyncContext("", eventRecorder)
	err = controller.sync(nil, syncContext)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	// check if there is any csr created
	csrs, err := fakeKubeClient.CertificatesV1beta1().CertificateSigningRequests().List(context.Background(), metav1.ListOptions{})
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	if len(csrs.Items) != 1 {
		t.Errorf("expect 1 csr created, but got: %d", len(csrs.Items))
	}

	// check if csrName/keyData are set. Since GenerateName is not working for fake clent, check keyData only
	if controller.keyData == nil {
		t.Error("controller.keyData should be set")
	}
}

func TestCreateCSR(t *testing.T) {
	fakeKubeClient := kubefake.NewSimpleClientset()

	keyData, err := keyutil.MakeEllipticPrivateKeyPEM()
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	controller := &ClientCertForHubController{
		clusterName:  "cluster0",
		agentName:    "agent0",
		keyData:      keyData,
		hubCSRClient: fakeKubeClient.CertificatesV1beta1().CertificateSigningRequests(),
	}

	csrName, err := controller.createCSR()
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	_, err = fakeKubeClient.CertificatesV1beta1().CertificateSigningRequests().Get(context.Background(), csrName, metav1.GetOptions{})
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestSaveHubKubeconfigSecret(t *testing.T) {
	secretNamespace := "default"
	secretName := "secret"

	fakeKubeClient := kubefake.NewSimpleClientset()

	controller := &ClientCertForHubController{
		hubKubeconfigSecretNamespace: secretNamespace,
		hubKubeconfigSecretName:      secretName,
		spokeCoreClient:              fakeKubeClient.CoreV1(),
	}

	key, value := "key", "value"
	secret := newSecret(secretNamespace, secretName, "", map[string][]byte{
		key: []byte(value),
	})
	err := controller.saveHubKubeconfigSecret(secret)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	secret, err = fakeKubeClient.CoreV1().Secrets(secretNamespace).Get(context.Background(), secretName, metav1.GetOptions{})
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if len(secret.Data) == 0 {
		t.Error("secret should have data stored")
	}
	if string(secret.Data[key]) != value {
		t.Errorf("expected %q but get: %s", value, string(secret.Data[key]))
	}
}

func TestSaveHubKubeconfigSecretWithExistingSecret(t *testing.T) {
	secretNamespace := "default"
	secretName := "secret"
	secret := newSecret(secretNamespace, secretName, "1", nil)

	fakeKubeClient := kubefake.NewSimpleClientset(secret)
	controller := &ClientCertForHubController{
		hubKubeconfigSecretNamespace: secretNamespace,
		hubKubeconfigSecretName:      secretName,
		spokeCoreClient:              fakeKubeClient.CoreV1(),
	}

	key, value := "key", "value"
	secret.Data = map[string][]byte{
		key: []byte(value),
	}

	err := controller.saveHubKubeconfigSecret(secret)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	secret, err = fakeKubeClient.CoreV1().Secrets(secretNamespace).Get(context.Background(), secretName, metav1.GetOptions{})
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if len(secret.Data) == 0 {
		t.Error("secret should have data stored")
	}
	if string(secret.Data[key]) != value {
		t.Errorf("expected %q but get: %s", value, string(secret.Data[key]))
	}
}
