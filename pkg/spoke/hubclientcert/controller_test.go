package hubclientcert

import (
	"context"
	"fmt"
	"testing"
	"time"

	certificates "k8s.io/api/certificates/v1beta1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/informers"
	kubefake "k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/clientcmd"
)

func TestSyncCSR(t *testing.T) {
	secretNamespace := "default"
	secretName := "secret"
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: secretNamespace,
			Name:      secretName,
		},
	}

	commonName := "cluster0"
	key, cert, err := newCertKey(commonName, 10*time.Second)
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

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go csrInformer.Informer().Run(ctx.Done())
	if ok := cache.WaitForCacheSync(ctx.Done(), csrInformer.Informer().HasSynced); !ok {
		t.Error("failed to wait for kubernetes caches to sync")
	}

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
	}
	changed, err := controller.syncCSR(secret)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if !changed {
		t.Error("secret should be changed")
	}

	if !hasValidKubeconfig(secret) {
		t.Error("kubeconfig is valid")
	}
}

// test bootstrap
func TestSyncWithoutKubeconfig(t *testing.T) {
	secretNamespace := "default"
	secretName := "secret"
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: secretNamespace,
			Name:      secretName,
		},
	}

	clusterName := "cluster0"
	agentName := "agent0"

	fakeKubeClient := kubefake.NewSimpleClientset(secret)
	controller := &ClientCertForHubController{
		clusterName:               clusterName,
		agentName:                 agentName,
		kubeconfigSecretNamespace: secretNamespace,
		kubeconfigSecretName:      secretName,
		spokeCoreClient:           fakeKubeClient.CoreV1(),
		hubCSRClient:              fakeKubeClient.CertificatesV1beta1().CertificateSigningRequests(),
	}
	err := controller.sync(nil, nil)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	csrs, err := fakeKubeClient.CertificatesV1beta1().CertificateSigningRequests().List(context.Background(), metav1.ListOptions{})
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	if len(csrs.Items) != 1 {
		t.Errorf("expect 1 csr created, but got: %d", len(csrs.Items))
	}
}

func TestSyncWithKubeconfig(t *testing.T) {
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
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: secretNamespace,
			Name:      secretName,
		},
		Data: map[string][]byte{
			KubeconfigFile: kubeconfigData,
			TLSCertFile:    cert,
			TLSKeyFile:     key,
		},
	}

	fakeKubeClient := kubefake.NewSimpleClientset(secret)
	controller := &ClientCertForHubController{
		clusterName:               clusterName,
		agentName:                 agentName,
		kubeconfigSecretNamespace: secretNamespace,
		kubeconfigSecretName:      secretName,
		spokeCoreClient:           fakeKubeClient.CoreV1(),
		hubCSRClient:              fakeKubeClient.CertificatesV1beta1().CertificateSigningRequests(),
	}
	err = controller.sync(nil, nil)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	csrs, err := fakeKubeClient.CertificatesV1beta1().CertificateSigningRequests().List(context.Background(), metav1.ListOptions{})
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	if len(csrs.Items) != 0 {
		t.Errorf("expect 0 csr created, but got: %d", len(csrs.Items))
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

	key, cert, err := newCertKey(fmt.Sprintf("%s%s:%s", subjectPrefix, clusterName, agentName), 1*time.Second)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	secretNamespace := "default"
	secretName := "secret"
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: secretNamespace,
			Name:      secretName,
		},
		Data: map[string][]byte{
			KubeconfigFile: kubeconfigData,
			TLSCertFile:    cert,
			TLSKeyFile:     key,
		},
	}

	fakeKubeClient := kubefake.NewSimpleClientset(secret)
	controller := &ClientCertForHubController{
		clusterName:               clusterName,
		agentName:                 agentName,
		kubeconfigSecretNamespace: secretNamespace,
		kubeconfigSecretName:      secretName,
		spokeCoreClient:           fakeKubeClient.CoreV1(),
		hubCSRClient:              fakeKubeClient.CertificatesV1beta1().CertificateSigningRequests(),
	}

	time.Sleep(3 * time.Second)
	err = controller.sync(nil, nil)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	csrs, err := fakeKubeClient.CertificatesV1beta1().CertificateSigningRequests().List(context.Background(), metav1.ListOptions{})
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	if len(csrs.Items) != 1 {
		t.Errorf("expect 1 csr created, but got: %d", len(csrs.Items))
	}
}
