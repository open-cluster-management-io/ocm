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

	certificates "k8s.io/api/certificates/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"
	certutil "k8s.io/client-go/util/cert"
	"k8s.io/client-go/util/keyutil"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
)

const (
	TestLeaseDurationSeconds = 1
	TestDir                  = "/tmp/registration-integration-test"
)

const AutoApprovalBootstrapUser = "autoapproval-user"

var (
	CertDir          = path.Join(TestDir, "client-certs")
	caFile           = path.Join(CertDir, "ca.crt")
	caKeyFile        = path.Join(CertDir, "ca.key")
	bootstrapUser    = "cluster-admin"
	bootstrapGroups  = []string{"system:masters"}
	DefaultTestAuthn = NewTestAuthn(caFile, caKeyFile)
)

type TestAuthn struct {
	caFile    string
	caKeyFile string
}

func createKubeConfigByClientCert(context string, securePort string, serverCertFile, certFile, keyFile string) (*clientcmdapi.Config, error) {
	caData, err := ioutil.ReadFile(serverCertFile)
	if err != nil {
		return nil, err
	}

	config := clientcmdapi.NewConfig()
	config.Clusters["hub"] = &clientcmdapi.Cluster{
		Server:                   fmt.Sprintf("https://127.0.0.1:%s", securePort),
		CertificateAuthorityData: caData,
	}
	config.AuthInfos["user"] = &clientcmdapi.AuthInfo{
		ClientCertificate: certFile,
		ClientKey:         keyFile,
	}
	config.Contexts[context] = &clientcmdapi.Context{
		Cluster:  "hub",
		AuthInfo: "user",
	}
	config.CurrentContext = context

	return config, nil
}

func NewTestAuthn(caFile, caKeyFile string) *TestAuthn {
	return &TestAuthn{
		caFile:    caFile,
		caKeyFile: caKeyFile,
	}
}

func (t *TestAuthn) Configure(workDir string, args *envtest.Arguments) error {
	args.Set("client-ca-file", t.caFile)
	return nil
}

// Start runs this authenticator.  Will be called just before API server start.
//
// Must be called after Configure.
func (t *TestAuthn) Start() error {
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
	if err := ioutil.WriteFile(t.caFile, caCertBuffer.Bytes(), 0644); err != nil {
		return err
	}

	caKeyBuffer := bytes.Buffer{}
	if err := pem.Encode(
		&caKeyBuffer, &pem.Block{Type: keyutil.RSAPrivateKeyBlockType, Bytes: x509.MarshalPKCS1PrivateKey(caKey)}); err != nil {
		return err
	}
	if err := ioutil.WriteFile(t.caKeyFile, caKeyBuffer.Bytes(), 0644); err != nil {
		return err
	}

	return nil
}

// AddUser provisions a user, returning a copy of the given base rest.Config
// configured to authenticate as that users.
//
// May only be called while the authenticator is "running".
func (t *TestAuthn) AddUser(user envtest.User, baseCfg *rest.Config) (*rest.Config, error) {
	crt, key, err := t.signClientCertKeyWithCA(user.Name, user.Groups, time.Hour*24)
	if err != nil {
		return nil, err
	}

	cfg := rest.CopyConfig(baseCfg)
	cfg.CertData = crt
	cfg.KeyData = key

	return cfg, nil
}

// Stop shuts down this authenticator.
func (t *TestAuthn) Stop() error {
	return nil
}

func (t *TestAuthn) CreateBootstrapKubeConfigWithCertAge(configFileName, serverCertFile, securePort string, certAge time.Duration) error {
	return t.CreateBootstrapKubeConfig(configFileName, serverCertFile, securePort, bootstrapUser, certAge)
}

func (t *TestAuthn) CreateBootstrapKubeConfigWithUser(configFileName, serverCertFile, securePort, bootstrapUser string) error {
	return t.CreateBootstrapKubeConfig(configFileName, serverCertFile, securePort, bootstrapUser, 24*time.Hour)
}

func (t *TestAuthn) CreateBootstrapKubeConfig(configFileName, serverCertFile, securePort, bootstrapUser string, certAge time.Duration) error {
	certData, keyData, err := t.signClientCertKeyWithCA(bootstrapUser, bootstrapGroups, certAge)
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

	config, err := createKubeConfigByClientCert(configFileName, securePort, serverCertFile, path.Join(configDir, "bootstrap.crt"), path.Join(configDir, "bootstrap.key"))
	if err != nil {
		return err
	}

	return clientcmd.WriteToFile(*config, configFileName)
}

func (t *TestAuthn) signClientCertKeyWithCA(user string, groups []string, maxAge time.Duration) ([]byte, []byte, error) {
	now := time.Now()
	caData, err := ioutil.ReadFile(t.caFile)
	if err != nil {
		return nil, nil, err
	}
	caBlock, _ := pem.Decode(caData)
	caCert, err := x509.ParseCertificate(caBlock.Bytes)
	if err != nil {
		return nil, nil, err
	}

	caKeyData, err := ioutil.ReadFile(t.caKeyFile)
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
			Subject:               pkix.Name{Organization: groups, CommonName: user},
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
		return nil, fmt.Errorf("cluster-name is not found")
	}

	if _, existed := secret.Data["agent-name"]; !existed {
		return nil, fmt.Errorf("agent-name is not found")
	}

	if _, existed := secret.Data["kubeconfig"]; !existed {
		return nil, fmt.Errorf("kubeconfig is not found")
	}

	if _, existed := secret.Data["tls.crt"]; !existed {
		return nil, fmt.Errorf("tls.crt is not found")
	}

	if _, existed := secret.Data["tls.key"]; !existed {
		return nil, fmt.Errorf("tls.key is not found")
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
		return nil, fmt.Errorf("failed to find unapproved csr for spoke cluster %q", spokeClusterName)
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
		return nil, fmt.Errorf("failed to find unapproved csr for spoke cluster %q", spokeClusterName)
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
		return nil, fmt.Errorf("failed to find autoapproved csr for spoke cluster %q", spokeClusterName)
	}

	return autoApproved, nil
}

func (t *TestAuthn) ApproveSpokeClusterCSRWithExpiredCert(kubeClient kubernetes.Interface, spokeClusterName string) error {
	now := time.Now()

	csr, err := FindUnapprovedSpokeCSR(kubeClient, spokeClusterName)
	if err != nil {
		return err
	}

	return t.ApproveCSR(kubeClient, csr, now.UTC(), now.Add(-1*time.Hour).UTC())
}

func (t *TestAuthn) ApproveSpokeClusterCSR(kubeClient kubernetes.Interface, spokeClusterName string, certAge time.Duration) error {
	now := time.Now()

	csr, err := FindUnapprovedSpokeCSR(kubeClient, spokeClusterName)
	if err != nil {
		return err
	}

	return t.ApproveCSR(kubeClient, csr, now.UTC(), now.Add(certAge).UTC())
}

func (t *TestAuthn) ApproveCSR(kubeClient kubernetes.Interface, csr *certificates.CertificateSigningRequest, notBefore, notAfter time.Time) error {
	if err := t.FillCertificateToApprovedCSR(kubeClient, csr, notBefore, notAfter); err != nil {
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

func (t *TestAuthn) FillCertificateToApprovedCSR(kubeClient kubernetes.Interface, csr *certificates.CertificateSigningRequest, notBefore, notAfter time.Time) error {
	block, _ := pem.Decode(csr.Spec.Request)
	cr, err := x509.ParseCertificateRequest(block.Bytes)
	if err != nil {
		return err
	}

	caData, err := ioutil.ReadFile(t.caFile)
	if err != nil {
		return err
	}
	caBlock, _ := pem.Decode(caData)
	caCert, err := x509.ParseCertificate(caBlock.Bytes)
	if err != nil {
		return err
	}

	caKeyData, err := ioutil.ReadFile(t.caKeyFile)
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
	csr.Status.Certificate = certBuffer.Bytes()
	_, err = kubeClient.CertificatesV1().CertificateSigningRequests().UpdateStatus(context.TODO(), csr, metav1.UpdateOptions{})
	if err != nil {
		return err
	}

	return nil
}
