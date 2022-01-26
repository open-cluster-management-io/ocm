package testing

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	cryptorand "crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"io/ioutil"
	"math/big"
	"math/rand"
	"net"
	"testing"
	"time"

	clusterv1 "open-cluster-management.io/api/cluster/v1"
	workapiv1 "open-cluster-management.io/api/work/v1"

	"github.com/openshift/library-go/pkg/operator/events"
	"github.com/openshift/library-go/pkg/operator/events/eventstesting"

	certv1 "k8s.io/api/certificates/v1"
	coordv1 "k8s.io/api/coordination/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	kubeversion "k8s.io/client-go/pkg/version"
	"k8s.io/client-go/tools/clientcmd"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"
	certutil "k8s.io/client-go/util/cert"
	"k8s.io/client-go/util/keyutil"
	"k8s.io/client-go/util/workqueue"
)

const (
	TestLeaseDurationSeconds int32 = 1
	TestManagedClusterName         = "testmanagedcluster"
)

type FakeSyncContext struct {
	spokeName string
	recorder  events.Recorder
	queue     workqueue.RateLimitingInterface
}

func (f FakeSyncContext) Queue() workqueue.RateLimitingInterface { return f.queue }
func (f FakeSyncContext) QueueKey() string                       { return f.spokeName }
func (f FakeSyncContext) Recorder() events.Recorder              { return f.recorder }

func NewFakeSyncContext(t *testing.T, clusterName string) *FakeSyncContext {
	return &FakeSyncContext{
		spokeName: clusterName,
		recorder:  eventstesting.NewTestingEventRecorder(t),
		queue:     workqueue.NewRateLimitingQueue(workqueue.DefaultControllerRateLimiter()),
	}
}

func NewManagedCluster() *clusterv1.ManagedCluster {
	return &clusterv1.ManagedCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name: TestManagedClusterName,
		},
	}
}

func NewAcceptingManagedCluster() *clusterv1.ManagedCluster {
	managedCluster := NewManagedCluster()
	managedCluster.Finalizers = []string{"cluster.open-cluster-management.io/api-resource-cleanup"}
	managedCluster.Spec.HubAcceptsClient = true
	return managedCluster
}

func NewAcceptedManagedCluster() *clusterv1.ManagedCluster {
	managedCluster := NewAcceptingManagedCluster()
	acceptedCondtion := NewManagedClusterCondition(
		clusterv1.ManagedClusterConditionHubAccepted,
		"True",
		"HubClusterAdminAccepted",
		"Accepted by hub cluster admin",
		nil,
	)
	managedCluster.Status.Conditions = append(managedCluster.Status.Conditions, acceptedCondtion)
	managedCluster.Spec.LeaseDurationSeconds = TestLeaseDurationSeconds
	return managedCluster
}

func NewAvailableManagedCluster() *clusterv1.ManagedCluster {
	managedCluster := NewAcceptedManagedCluster()
	availableCondition := NewManagedClusterCondition(
		clusterv1.ManagedClusterConditionAvailable,
		"True",
		"ManagedClusterAvailable",
		"Managed cluster is available",
		nil,
	)
	managedCluster.Status.Conditions = append(managedCluster.Status.Conditions, availableCondition)
	return managedCluster
}

func NewUnAvailableManagedCluster() *clusterv1.ManagedCluster {
	managedCluster := NewAcceptedManagedCluster()
	condition := NewManagedClusterCondition(
		clusterv1.ManagedClusterConditionAvailable,
		"False",
		"ManagedClusterUnAvailable",
		"Managed cluster is Unavailable",
		nil,
	)
	managedCluster.Status.Conditions = append(managedCluster.Status.Conditions, condition)
	return managedCluster
}

func NewUnknownManagedCluster() *clusterv1.ManagedCluster {
	managedCluster := NewAcceptedManagedCluster()
	availableCondtion := NewManagedClusterCondition(
		clusterv1.ManagedClusterConditionAvailable,
		"Unknown",
		"ManagedClusterUnknown",
		"Managed cluster is unknown",
		nil,
	)
	managedCluster.Status.Conditions = append(managedCluster.Status.Conditions, availableCondtion)
	return managedCluster
}

func NewJoinedManagedCluster() *clusterv1.ManagedCluster {
	managedCluster := NewAcceptedManagedCluster()
	joinedCondtion := NewManagedClusterCondition(
		clusterv1.ManagedClusterConditionJoined,
		"True",
		"ManagedClusterJoined",
		"Managed cluster joined",
		nil,
	)
	managedCluster.Status.Conditions = append(managedCluster.Status.Conditions, joinedCondtion)
	return managedCluster
}

func NewManagedClusterWithStatus(capacity, allocatable corev1.ResourceList) *clusterv1.ManagedCluster {
	managedCluster := NewJoinedManagedCluster()
	managedCluster.Status.Capacity = clusterv1.ResourceList{
		"cpu":    capacity.Cpu().DeepCopy(),
		"memory": capacity.Memory().DeepCopy(),
	}
	managedCluster.Status.Allocatable = clusterv1.ResourceList{
		"cpu":    allocatable.Cpu().DeepCopy(),
		"memory": allocatable.Memory().DeepCopy(),
	}
	managedCluster.Status.Version = clusterv1.ManagedClusterVersion{
		Kubernetes: kubeversion.Get().GitVersion,
	}
	return managedCluster
}

func NewDeniedManagedCluster() *clusterv1.ManagedCluster {
	managedCluster := NewAcceptedManagedCluster()
	managedCluster.Spec.HubAcceptsClient = false
	return managedCluster
}

func NewDeletingManagedCluster() *clusterv1.ManagedCluster {
	now := metav1.Now()
	return &clusterv1.ManagedCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:              TestManagedClusterName,
			DeletionTimestamp: &now,
			Finalizers:        []string{"cluster.open-cluster-management.io/api-resource-cleanup"},
		},
	}
}

func NewManagedClusterCondition(name, status, reason, message string, lastTransition *metav1.Time) metav1.Condition {
	ret := metav1.Condition{
		Type:    name,
		Status:  metav1.ConditionStatus(status),
		Reason:  reason,
		Message: message,
	}
	if lastTransition != nil {
		ret.LastTransitionTime = *lastTransition
	}
	return ret
}

func NewManagedClusterLease(name string, renewTime time.Time) *coordv1.Lease {
	return &coordv1.Lease{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: TestManagedClusterName,
		},
		Spec: coordv1.LeaseSpec{
			RenewTime: &metav1.MicroTime{Time: renewTime},
		},
	}
}

func NewAddOnLease(namespace, name string, renewTime time.Time) *coordv1.Lease {
	return &coordv1.Lease{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Spec: coordv1.LeaseSpec{
			RenewTime: &metav1.MicroTime{Time: renewTime},
		},
	}
}

func NewNamespace(name string, terminated bool) *corev1.Namespace {
	namespace := &corev1.Namespace{}
	namespace.Name = name
	if terminated {
		now := metav1.Now()
		namespace.DeletionTimestamp = &now
	}
	return namespace
}

func NewManifestWork(namespace, name string, finalizers []string, deletionTimestamp *metav1.Time) *workapiv1.ManifestWork {
	work := &workapiv1.ManifestWork{
		ObjectMeta: metav1.ObjectMeta{
			Namespace:         namespace,
			Name:              name,
			Finalizers:        finalizers,
			DeletionTimestamp: deletionTimestamp,
		},
	}
	return work
}

func NewRole(namespace, name string, finalizers []string, terminated bool) *rbacv1.Role {
	role := &rbacv1.Role{}
	role.Namespace = namespace
	role.Name = name
	role.Finalizers = finalizers
	if terminated {
		now := metav1.Now()
		role.DeletionTimestamp = &now
	}
	return role
}

func NewRoleBinding(namespace, name string, finalizers []string, terminated bool) *rbacv1.RoleBinding {
	rolebinding := &rbacv1.RoleBinding{}
	rolebinding.Namespace = namespace
	rolebinding.Name = name
	rolebinding.Finalizers = finalizers
	if terminated {
		now := metav1.Now()
		rolebinding.DeletionTimestamp = &now
	}
	return rolebinding
}

func NewResourceList(cpu, mem int) corev1.ResourceList {
	return corev1.ResourceList{
		corev1.ResourceCPU:    *resource.NewQuantity(int64(cpu), resource.DecimalExponent),
		corev1.ResourceMemory: *resource.NewQuantity(int64(1024*1024*mem), resource.BinarySI),
	}
}

func NewNode(name string, capacity, allocatable corev1.ResourceList) *corev1.Node {
	return &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
		Status: corev1.NodeStatus{
			Capacity:    capacity,
			Allocatable: allocatable,
		},
	}
}

func NewUnstructuredObj(apiVersion, kind, namespace, name string) *unstructured.Unstructured {
	return &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": apiVersion,
			"kind":       kind,
			"metadata": map[string]interface{}{
				"namespace": namespace,
				"name":      name,
			},
		},
	}
}

type CSRHolder struct {
	Name         string
	Labels       map[string]string
	SignerName   string
	CN           string
	Orgs         []string
	Username     string
	ReqBlockType string
}

func NewCSR(holder CSRHolder) *certv1.CertificateSigningRequest {
	insecureRand := rand.New(rand.NewSource(0))
	pk, err := ecdsa.GenerateKey(elliptic.P256(), insecureRand)
	if err != nil {
		panic(err)
	}
	csrb, err := x509.CreateCertificateRequest(insecureRand, &x509.CertificateRequest{
		Subject: pkix.Name{
			CommonName:   holder.CN,
			Organization: holder.Orgs,
		},
		DNSNames:       []string{},
		EmailAddresses: []string{},
		IPAddresses:    []net.IP{},
	}, pk)
	if err != nil {
		panic(err)
	}
	return &certv1.CertificateSigningRequest{
		ObjectMeta: metav1.ObjectMeta{
			Name:         holder.Name,
			GenerateName: "csr-",
			Labels:       holder.Labels,
		},
		Spec: certv1.CertificateSigningRequestSpec{
			Username:   holder.Username,
			Usages:     []certv1.KeyUsage{},
			SignerName: holder.SignerName,
			Request:    pem.EncodeToMemory(&pem.Block{Type: holder.ReqBlockType, Bytes: csrb}),
		},
	}
}

func NewDeniedCSR(holder CSRHolder) *certv1.CertificateSigningRequest {
	csr := NewCSR(holder)
	csr.Status.Conditions = append(csr.Status.Conditions, certv1.CertificateSigningRequestCondition{
		Type:   certv1.CertificateDenied,
		Status: corev1.ConditionTrue,
	})
	return csr
}

func NewApprovedCSR(holder CSRHolder) *certv1.CertificateSigningRequest {
	csr := NewCSR(holder)
	csr.Status.Conditions = append(csr.Status.Conditions, certv1.CertificateSigningRequestCondition{
		Type:   certv1.CertificateApproved,
		Status: corev1.ConditionTrue,
	})
	return csr
}

func NewKubeconfig(key, cert []byte) []byte {
	var clientKey, clientCertificate string
	var clientKeyData, clientCertificateData []byte
	if key != nil {
		clientKeyData = key
	} else {
		clientKey = "tls.key"
	}
	if cert != nil {
		clientCertificateData = cert
	} else {
		clientCertificate = "tls.crt"
	}

	kubeconfig := clientcmdapi.Config{
		Clusters: map[string]*clientcmdapi.Cluster{"default-cluster": {
			Server:                "https://127.0.0.1:6001",
			InsecureSkipTLSVerify: true,
		}},
		AuthInfos: map[string]*clientcmdapi.AuthInfo{"default-auth": {
			ClientCertificate:     clientCertificate,
			ClientCertificateData: clientCertificateData,
			ClientKey:             clientKey,
			ClientKeyData:         clientKeyData,
		}},
		Contexts: map[string]*clientcmdapi.Context{"default-context": {
			Cluster:   "default-cluster",
			AuthInfo:  "default-auth",
			Namespace: "default",
		}},
		CurrentContext: "default-context",
	}

	kubeconfigData, err := clientcmd.Write(kubeconfig)
	if err != nil {
		panic(err)
	}
	return kubeconfigData
}

type TestCert struct {
	Cert []byte
	Key  []byte
}

func NewHubKubeconfigSecret(namespace, name, resourceVersion string, cert *TestCert, data map[string][]byte) *corev1.Secret {
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Namespace:       namespace,
			Name:            name,
			ResourceVersion: resourceVersion,
		},
		Data: data,
	}
	if cert != nil && cert.Cert != nil {
		secret.Data["tls.crt"] = cert.Cert
	}
	if cert != nil && cert.Key != nil {
		secret.Data["tls.key"] = cert.Key
	}
	return secret
}

func NewTestCertWithSubject(subject pkix.Name, duration time.Duration) *TestCert {
	caKey, err := rsa.GenerateKey(cryptorand.Reader, 2048)
	if err != nil {
		panic(err)
	}

	caCert, err := certutil.NewSelfSignedCACert(certutil.Config{CommonName: "open-cluster-management.io"}, caKey)
	if err != nil {
		panic(err)
	}

	key, err := rsa.GenerateKey(cryptorand.Reader, 2048)
	if err != nil {
		panic(err)
	}

	certDERBytes, err := x509.CreateCertificate(
		cryptorand.Reader,
		&x509.Certificate{
			Subject:      subject,
			SerialNumber: big.NewInt(1),
			NotBefore:    caCert.NotBefore,
			NotAfter:     time.Now().Add(duration).UTC(),
			KeyUsage:     x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature,
			ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
		},
		caCert,
		key.Public(),
		caKey,
	)
	if err != nil {
		panic(err)
	}

	cert, err := x509.ParseCertificate(certDERBytes)
	if err != nil {
		panic(err)
	}

	return &TestCert{
		Cert: pem.EncodeToMemory(&pem.Block{
			Type:  certutil.CertificateBlockType,
			Bytes: cert.Raw,
		}),
		Key: pem.EncodeToMemory(&pem.Block{
			Type:  keyutil.RSAPrivateKeyBlockType,
			Bytes: x509.MarshalPKCS1PrivateKey(key),
		}),
	}
}

func NewTestCert(commonName string, duration time.Duration) *TestCert {
	return NewTestCertWithSubject(pkix.Name{
		CommonName: commonName,
	}, duration)
}

func WriteFile(filename string, data []byte) {
	if err := ioutil.WriteFile(filename, data, 0644); err != nil {
		panic(err)
	}
}
