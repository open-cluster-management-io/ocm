package util

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"io/ioutil"
	"math/big"
	"net"
	"os"
	"path"
	"time"

	"github.com/onsi/ginkgo"

	clusterclientset "open-cluster-management.io/api/client/cluster/clientset/versioned"
	clusterv1 "open-cluster-management.io/api/cluster/v1"

	"github.com/openshift/library-go/pkg/operator/events"

	certificates "k8s.io/api/certificates/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"
	certutil "k8s.io/client-go/util/cert"
	"k8s.io/client-go/util/keyutil"
)

const (
	TestLeaseDurationSeconds = 1
	TestDir                  = "/tmp/registration-integration-test"
)

var (
	CertDir        = path.Join(TestDir, "server-certs")
	CAFile         = path.Join(CertDir, "ca.crt")
	CAKeyFile      = path.Join(CertDir, "ca.key")
	ServerCertFile = path.Join(CertDir, "apiserver.crt")
	ServerKeyFile  = path.Join(CertDir, "apiserver.key")
)

func CreateBootstrapKubeConfig(configFileName string, securePort int) error {
	caData, err := ioutil.ReadFile(CAFile)
	if err != nil {
		return err
	}

	config := clientcmdapi.NewConfig()
	config.Clusters["hub"] = &clientcmdapi.Cluster{
		Server:                   fmt.Sprintf("https://127.0.0.1:%d", securePort),
		CertificateAuthorityData: caData,
	}
	config.AuthInfos["bootstrap"] = &clientcmdapi.AuthInfo{
		ClientCertificate: ServerCertFile,
		ClientKey:         ServerKeyFile,
	}
	config.Contexts["bootstrap"] = &clientcmdapi.Context{
		Cluster:  "hub",
		AuthInfo: "bootstrap",
	}
	config.CurrentContext = "bootstrap"

	return clientcmd.WriteToFile(*config, configFileName)
}

func CreateBootstrapKubeConfigWithCertAge(configFileName string, securePort int, certAge time.Duration) error {
	caData, err := ioutil.ReadFile(CAFile)
	if err != nil {
		return err
	}

	certData, keyData, err := SignAPIServerCertKeyWithCA(certAge)
	if err != nil {
		return err
	}

	configDir := path.Dir(configFileName)
	if _, err := os.Stat(configDir); os.IsNotExist(err) {
		if err = os.MkdirAll(configDir, 0755); err != nil {
			return err
		}
	}

	if err := ioutil.WriteFile(path.Join(configDir, "bootstrap.crt"), certData, 0644); err != nil {
		return err
	}
	if err := ioutil.WriteFile(path.Join(configDir, "bootstrap.key"), keyData, 0644); err != nil {
		return err
	}

	config := clientcmdapi.NewConfig()
	config.Clusters["hub"] = &clientcmdapi.Cluster{
		Server:                   fmt.Sprintf("https://127.0.0.1:%d", securePort),
		CertificateAuthorityData: caData,
	}
	config.AuthInfos["bootstrap"] = &clientcmdapi.AuthInfo{
		ClientCertificate: path.Join(configDir, "bootstrap.crt"),
		ClientKey:         path.Join(configDir, "bootstrap.key"),
	}
	config.Contexts["bootstrap"] = &clientcmdapi.Context{
		Cluster:  "hub",
		AuthInfo: "bootstrap",
	}
	config.CurrentContext = "bootstrap"

	return clientcmd.WriteToFile(*config, configFileName)
}

func CreateSpokeKubeConfig(restConfig *rest.Config, securePort int) (*rest.Config, error) {
	spokeConfig := rest.CopyConfig(restConfig)
	spokeConfig.Host = fmt.Sprintf("127.0.0.1:%d", securePort)

	caData, err := ioutil.ReadFile(CAFile)
	if err != nil {
		return nil, err
	}
	spokeConfig.CAData = caData

	certData, err := ioutil.ReadFile(ServerCertFile)
	if err != nil {
		return nil, err
	}
	spokeConfig.CertData = certData

	keyData, err := ioutil.ReadFile(ServerKeyFile)
	if err != nil {
		return nil, err
	}
	spokeConfig.KeyData = keyData
	return spokeConfig, nil
}

func GenerateSelfSignedCertKey() error {
	if _, err := os.Stat(CertDir); os.IsNotExist(err) {
		if err = os.MkdirAll(CertDir, 0755); err != nil {
			return err
		}
	}

	now := time.Now()
	maxAge := time.Hour * 24

	// generate ca cert and key
	caKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return err
	}

	caTemplate := x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "127.0.0.1"},
		NotBefore:             now.UTC(),
		NotAfter:              now.Add(maxAge).UTC(),
		KeyUsage:              x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign,
		BasicConstraintsValid: true,
		IsCA:                  true,
	}
	caDERBytes, err := x509.CreateCertificate(rand.Reader, &caTemplate, &caTemplate, &caKey.PublicKey, caKey)
	if err != nil {
		return err
	}

	caCertBuffer := bytes.Buffer{}
	if err := pem.Encode(&caCertBuffer, &pem.Block{Type: certutil.CertificateBlockType, Bytes: caDERBytes}); err != nil {
		return err
	}
	if err := ioutil.WriteFile(CAFile, caCertBuffer.Bytes(), 0644); err != nil {
		return err
	}

	caKeyBuffer := bytes.Buffer{}
	if err := pem.Encode(
		&caKeyBuffer, &pem.Block{Type: keyutil.RSAPrivateKeyBlockType, Bytes: x509.MarshalPKCS1PrivateKey(caKey)}); err != nil {
		return err
	}
	if err := ioutil.WriteFile(CAKeyFile, caKeyBuffer.Bytes(), 0644); err != nil {
		return err
	}

	serverCert, serverKey, err := SignAPIServerCertKeyWithCA(maxAge)
	if err != nil {
		return err
	}
	if err := ioutil.WriteFile(ServerCertFile, serverCert, 0644); err != nil {
		return err
	}
	if err := ioutil.WriteFile(ServerKeyFile, serverKey, 0644); err != nil {
		return err
	}

	return nil
}

func SignAPIServerCertKeyWithCA(maxAge time.Duration) ([]byte, []byte, error) {
	now := time.Now()
	caData, err := ioutil.ReadFile(CAFile)
	if err != nil {
		return nil, nil, err
	}
	caBlock, _ := pem.Decode(caData)
	caCert, err := x509.ParseCertificate(caBlock.Bytes)
	if err != nil {
		return nil, nil, err
	}

	caKeyData, err := ioutil.ReadFile(CAKeyFile)
	if err != nil {
		return nil, nil, err
	}
	keyBlock, _ := pem.Decode(caKeyData)
	caKey, err := x509.ParsePKCS1PrivateKey(keyBlock.Bytes)
	if err != nil {
		return nil, nil, err
	}

	serverKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return nil, nil, err
	}

	serverDERBytes, err := x509.CreateCertificate(
		rand.Reader,
		&x509.Certificate{
			SerialNumber:          big.NewInt(1),
			Subject:               pkix.Name{Organization: []string{"registration.integration.test"}, CommonName: "127.0.0.1"},
			NotBefore:             now.UTC(),
			NotAfter:              now.Add(maxAge).UTC(),
			BasicConstraintsValid: false,
			IsCA:                  false,
			KeyUsage:              x509.KeyUsageKeyEncipherment | x509.KeyUsageDataEncipherment,
			ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth, x509.ExtKeyUsageServerAuth},
			IPAddresses:           []net.IP{net.ParseIP("127.0.0.1"), net.ParseIP("10.0.0.0")},
			DNSNames: []string{
				"kubernetes",
				"kubernetes.default",
				"kubernetes.default.svc",
				"kubernetes.default.svc.cluster",
				"kubernetes.default.svc.cluster.local",
			},
		},
		caCert,
		&serverKey.PublicKey,
		caKey,
	)
	if err != nil {
		return nil, nil, err
	}
	certBuffer := bytes.Buffer{}
	if err := pem.Encode(&certBuffer, &pem.Block{Type: certutil.CertificateBlockType, Bytes: serverDERBytes}); err != nil {
		return nil, nil, err
	}

	keyBuffer := bytes.Buffer{}
	if err := pem.Encode(
		&keyBuffer, &pem.Block{Type: keyutil.RSAPrivateKeyBlockType, Bytes: x509.MarshalPKCS1PrivateKey(serverKey)}); err != nil {
		return nil, nil, err
	}
	return certBuffer.Bytes(), keyBuffer.Bytes(), nil
}

func PrepareSpokeAgentNamespace(kubeClient kubernetes.Interface, namespace string) error {
	_, err := kubeClient.CoreV1().Namespaces().Get(context.TODO(), namespace, metav1.GetOptions{})
	if err == nil {
		return nil
	}
	if !errors.IsNotFound(err) {
		return err
	}
	ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: namespace}}
	_, err = kubeClient.CoreV1().Namespaces().Create(context.TODO(), ns, metav1.CreateOptions{})
	return err
}

func GetFilledHubKubeConfigSecret(kubeClient kubernetes.Interface, secretNamespace, secretName string) (*corev1.Secret, error) {
	secret, err := kubeClient.CoreV1().Secrets(secretNamespace).Get(context.TODO(), secretName, metav1.GetOptions{})
	if err != nil {
		return nil, err
	}
	if _, existed := secret.Data["cluster-name"]; !existed {
		return nil, fmt.Errorf("cluster-name is not found\n")
	}

	if _, existed := secret.Data["agent-name"]; !existed {
		return nil, fmt.Errorf("agent-name is not found\n")
	}

	if _, existed := secret.Data["kubeconfig"]; !existed {
		return nil, fmt.Errorf("kubeconfig is not found\n")
	}

	if _, existed := secret.Data["tls.crt"]; !existed {
		return nil, fmt.Errorf("tls.crt is not found\n")
	}

	if _, existed := secret.Data["tls.key"]; !existed {
		return nil, fmt.Errorf("tls.key is not found\n")
	}
	return secret, nil
}

func FindUnapprovedSpokeCSR(kubeClient kubernetes.Interface, spokeClusterName string) (*certificates.CertificateSigningRequest, error) {
	csrList, err := kubeClient.CertificatesV1().CertificateSigningRequests().List(context.TODO(), metav1.ListOptions{
		LabelSelector: fmt.Sprintf("open-cluster-management.io/cluster-name=%s", spokeClusterName),
	})
	if err != nil {
		return nil, err
	}

	var unapproved *certificates.CertificateSigningRequest
	for _, csr := range csrList.Items {
		if len(csr.Status.Conditions) == 0 {
			unapproved = csr.DeepCopy()
			break
		}
	}

	if unapproved == nil {
		return nil, fmt.Errorf("failed to find unapproved csr for spoke cluster %q\n", spokeClusterName)
	}

	return unapproved, nil
}

func FindAddOnCSRs(kubeClient kubernetes.Interface, spokeClusterName, addOnName string) ([]*certificates.CertificateSigningRequest, error) {
	csrList, err := kubeClient.CertificatesV1().CertificateSigningRequests().List(context.TODO(), metav1.ListOptions{
		LabelSelector: fmt.Sprintf("open-cluster-management.io/cluster-name=%s,open-cluster-management.io/addon-name=%s", spokeClusterName, addOnName),
	})
	if err != nil {
		return nil, err
	}

	csrs := []*certificates.CertificateSigningRequest{}
	for _, csr := range csrList.Items {
		csrs = append(csrs, &csr)
	}

	return csrs, nil
}

func FindUnapprovedAddOnCSR(kubeClient kubernetes.Interface, spokeClusterName, addOnName string) (*certificates.CertificateSigningRequest, error) {
	csrList, err := kubeClient.CertificatesV1().CertificateSigningRequests().List(context.TODO(), metav1.ListOptions{
		LabelSelector: fmt.Sprintf("open-cluster-management.io/cluster-name=%s,open-cluster-management.io/addon-name=%s", spokeClusterName, addOnName),
	})
	if err != nil {
		return nil, err
	}

	var unapproved *certificates.CertificateSigningRequest
	for _, csr := range csrList.Items {
		if len(csr.Status.Conditions) == 0 {
			unapproved = csr.DeepCopy()
			break
		}
	}

	if unapproved == nil {
		return nil, fmt.Errorf("failed to find unapproved csr for spoke cluster %q\n", spokeClusterName)
	}

	return unapproved, nil
}

func FindAutoApprovedSpokeCSR(kubeClient kubernetes.Interface, spokeClusterName string) (*certificates.CertificateSigningRequest, error) {
	csrList, err := kubeClient.CertificatesV1().CertificateSigningRequests().List(context.TODO(), metav1.ListOptions{
		LabelSelector: fmt.Sprintf("open-cluster-management.io/cluster-name=%s", spokeClusterName),
	})
	if err != nil {
		return nil, err
	}

	var autoApproved *certificates.CertificateSigningRequest
	for _, csr := range csrList.Items {
		if len(csr.Status.Conditions) == 0 {
			continue
		}
		cond := csr.Status.Conditions[0]
		if cond.Type == certificates.CertificateApproved &&
			cond.Reason == "AutoApprovedByHubCSRApprovingController" {
			autoApproved = csr.DeepCopy()
			break
		}
	}

	if autoApproved == nil {
		return nil, fmt.Errorf("failed to find autoapproved csr for spoke cluster %q\n", spokeClusterName)
	}

	return autoApproved, nil
}

func ApproveSpokeClusterCSRWithExpiredCert(kubeClient kubernetes.Interface, spokeClusterName string) error {
	now := time.Now()

	csr, err := FindUnapprovedSpokeCSR(kubeClient, spokeClusterName)
	if err != nil {
		return err
	}

	return ApproveCSR(kubeClient, csr, now.UTC(), now.Add(-1*time.Hour).UTC())
}

func ApproveSpokeClusterCSR(kubeClient kubernetes.Interface, spokeClusterName string, certAge time.Duration) error {
	now := time.Now()

	csr, err := FindUnapprovedSpokeCSR(kubeClient, spokeClusterName)
	if err != nil {
		return err
	}

	return ApproveCSR(kubeClient, csr, now.UTC(), now.Add(certAge).UTC())
}

func ApproveCSR(kubeClient kubernetes.Interface, csr *certificates.CertificateSigningRequest, notBefore, notAfter time.Time) error {
	block, _ := pem.Decode(csr.Spec.Request)
	cr, err := x509.ParseCertificateRequest(block.Bytes)
	if err != nil {
		return err
	}

	caData, err := ioutil.ReadFile(CAFile)
	if err != nil {
		return err
	}
	caBlock, _ := pem.Decode(caData)
	caCert, err := x509.ParseCertificate(caBlock.Bytes)
	if err != nil {
		return err
	}

	caKeyData, err := ioutil.ReadFile(CAKeyFile)
	if err != nil {
		return err
	}
	keyBlock, _ := pem.Decode(caKeyData)
	caKey, err := x509.ParsePKCS1PrivateKey(keyBlock.Bytes)
	if err != nil {
		return err
	}

	certDERBytes, err := x509.CreateCertificate(
		rand.Reader,
		&x509.Certificate{
			SerialNumber:          big.NewInt(1),
			Subject:               pkix.Name{Organization: cr.Subject.Organization, CommonName: cr.Subject.CommonName},
			NotBefore:             notBefore,
			NotAfter:              notAfter,
			BasicConstraintsValid: false,
			IsCA:                  false,
			KeyUsage:              x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature,
			ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
		},
		caCert,
		cr.PublicKey,
		caKey,
	)
	if err != nil {
		return err
	}

	certBuffer := bytes.Buffer{}
	if err := pem.Encode(&certBuffer, &pem.Block{Type: certutil.CertificateBlockType, Bytes: certDERBytes}); err != nil {
		return err
	}

	// set cert
	csr.Status = certificates.CertificateSigningRequestStatus{
		Certificate: certBuffer.Bytes(),
		Conditions:  []certificates.CertificateSigningRequestCondition{},
	}
	_, err = kubeClient.CertificatesV1().CertificateSigningRequests().UpdateStatus(context.TODO(), csr, metav1.UpdateOptions{})
	if err != nil {
		return err
	}

	// approve the csr
	approved, err := kubeClient.CertificatesV1().CertificateSigningRequests().Get(context.TODO(), csr.Name, metav1.GetOptions{})
	if err != nil {
		return err
	}
	approved.Status.Conditions = append(approved.Status.Conditions, certificates.CertificateSigningRequestCondition{
		Type:           certificates.CertificateApproved,
		Status:         corev1.ConditionTrue,
		Reason:         "Approved",
		Message:        "CSR Approved.",
		LastUpdateTime: metav1.Now(),
	})
	_, err = kubeClient.CertificatesV1().CertificateSigningRequests().UpdateApproval(context.TODO(), approved.Name, approved, metav1.UpdateOptions{})
	return err
}

func GetManagedCluster(clusterClient clusterclientset.Interface, spokeClusterName string) (*clusterv1.ManagedCluster, error) {
	spokeCluster, err := clusterClient.ClusterV1().ManagedClusters().Get(context.TODO(), spokeClusterName, metav1.GetOptions{})
	if err != nil {
		return nil, err
	}
	return spokeCluster, nil
}

func AcceptManagedCluster(clusterClient clusterclientset.Interface, spokeClusterName string) error {
	spokeCluster, err := GetManagedCluster(clusterClient, spokeClusterName)
	if err != nil {
		return err
	}
	spokeCluster.Spec.HubAcceptsClient = true
	spokeCluster.Spec.LeaseDurationSeconds = TestLeaseDurationSeconds
	_, err = clusterClient.ClusterV1().ManagedClusters().Update(context.TODO(), spokeCluster, metav1.UpdateOptions{})
	return err
}

func CreateNode(kubeClient kubernetes.Interface, name string, capacity, allocatable corev1.ResourceList) error {
	node := &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
		Status: corev1.NodeStatus{
			Capacity:    capacity,
			Allocatable: allocatable,
		},
	}
	_, err := kubeClient.CoreV1().Nodes().Create(context.TODO(), node, metav1.CreateOptions{})
	return err
}

func CordonNode(kubeClient kubernetes.Interface, name string) error {
	node, err := kubeClient.CoreV1().Nodes().Get(context.TODO(), name, metav1.GetOptions{})
	if err != nil {
		return err
	}

	node = node.DeepCopy()
	node.Spec.Unschedulable = true
	_, err = kubeClient.CoreV1().Nodes().Update(context.TODO(), node, metav1.UpdateOptions{})
	return err
}

func NewResourceList(cpu, mem int) corev1.ResourceList {
	return corev1.ResourceList{
		corev1.ResourceCPU:    *resource.NewQuantity(int64(cpu), resource.DecimalExponent),
		corev1.ResourceMemory: *resource.NewQuantity(int64(1024*1024*mem), resource.BinarySI),
	}
}

func CmpResourceQuantity(key string, nodeResourceList corev1.ResourceList, clusterResorceList clusterv1.ResourceList) bool {
	nodeResource, ok := nodeResourceList[corev1.ResourceName(key)]
	if !ok {
		return false
	}
	clusterResouce, ok := clusterResorceList[clusterv1.ResourceName(key)]
	if !ok {
		return false
	}
	return nodeResource.Equal(clusterResouce)
}

func NewIntegrationTestEventRecorder(componet string) events.Recorder {
	return &IntegrationTestEventRecorder{component: componet}
}

type IntegrationTestEventRecorder struct {
	component string
}

func (r *IntegrationTestEventRecorder) ComponentName() string {
	return r.component
}

func (r *IntegrationTestEventRecorder) ForComponent(c string) events.Recorder {
	return &IntegrationTestEventRecorder{component: c}
}

func (r *IntegrationTestEventRecorder) WithComponentSuffix(suffix string) events.Recorder {
	return r.ForComponent(fmt.Sprintf("%s-%s", r.ComponentName(), suffix))
}

func (r *IntegrationTestEventRecorder) Event(reason, message string) {
	fmt.Fprintf(ginkgo.GinkgoWriter, "Event: [%s] %v: %v \n", r.component, reason, message)
}

func (r *IntegrationTestEventRecorder) Eventf(reason, messageFmt string, args ...interface{}) {
	r.Event(reason, fmt.Sprintf(messageFmt, args...))
}

func (r *IntegrationTestEventRecorder) Warning(reason, message string) {
	fmt.Fprintf(ginkgo.GinkgoWriter, "Warning: [%s] %v: %v \n", r.component, reason, message)
}

func (r *IntegrationTestEventRecorder) Warningf(reason, messageFmt string, args ...interface{}) {
	r.Warning(reason, fmt.Sprintf(messageFmt, args...))
}

func (r *IntegrationTestEventRecorder) Shutdown() {
	return
}
