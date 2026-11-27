package csr

import (
	"context"
	"crypto/x509/pkix"
	"fmt"
	"os"
	"path"
	"reflect"
	"strings"
	"testing"
	"time"

	certificates "k8s.io/api/certificates/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/rand"
	"k8s.io/client-go/informers"
	kubefake "k8s.io/client-go/kubernetes/fake"
	certificatesv1listers "k8s.io/client-go/listers/certificates/v1"
	clienttesting "k8s.io/client-go/testing"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/util/keyutil"
	"k8s.io/klog/v2/ktesting"

	addonv1beta1 "open-cluster-management.io/api/addon/v1beta1"
	addonfake "open-cluster-management.io/api/client/addon/clientset/versioned/fake"
	addoninformers "open-cluster-management.io/api/client/addon/informers/externalversions"
	clusterv1 "open-cluster-management.io/api/cluster/v1"
	ocmfeature "open-cluster-management.io/api/feature"
	"open-cluster-management.io/sdk-go/pkg/basecontroller/events"

	testingcommon "open-cluster-management.io/ocm/pkg/common/testing"
	"open-cluster-management.io/ocm/pkg/features"
	testinghelpers "open-cluster-management.io/ocm/pkg/registration/helpers/testing"
	"open-cluster-management.io/ocm/pkg/registration/hub/user"
	"open-cluster-management.io/ocm/pkg/registration/register"
	registertesting "open-cluster-management.io/ocm/pkg/registration/register/testing"
	"open-cluster-management.io/ocm/pkg/registration/register/token"
)

const (
	testNamespace  = "testns"
	testAgentName  = "testagent"
	testSecretName = "testsecret"
	testCSRName    = "testcsr"
)

var commonName = fmt.Sprintf("%s%s:%s", user.SubjectPrefix, testinghelpers.TestManagedClusterName, testAgentName)

func TestProcess(t *testing.T) {
	testSubject := &pkix.Name{
		CommonName: commonName,
	}

	cases := []struct {
		name              string
		queueKey          string
		secret            *corev1.Secret
		approvedCSRCert   *testinghelpers.TestCert
		keyDataExpected   bool
		csrNameExpected   bool
		expectedCondition *metav1.Condition
		validateActions   func(t *testing.T, hubActions []clienttesting.Action, secret *corev1.Secret)
	}{
		{
			name:     "syc csr after bootstrap",
			queueKey: testSecretName,
			secret: testinghelpers.NewHubKubeconfigSecret(
				testNamespace, testSecretName, "1", nil,
				map[string][]byte{
					register.ClusterNameFile: []byte(testinghelpers.TestManagedClusterName),
					register.AgentNameFile:   []byte(testAgentName),
				}),
			expectedCondition: &metav1.Condition{
				Type:   ClusterCertificateRotatedCondition,
				Status: metav1.ConditionTrue,
			},
			approvedCSRCert: testinghelpers.NewTestCert(commonName, 10*time.Second),
			validateActions: func(t *testing.T, hubActions []clienttesting.Action, secret *corev1.Secret) {
				logger, _ := ktesting.NewTestContext(t)
				testingcommon.AssertActions(t, hubActions, "get", "get")
				valid, err := IsCertificateValid(logger, secret.Data[TLSCertFile], testSubject)
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}
				if !valid {
					t.Error("client certificate is invalid")
				}
				if _, ok := secret.Data[PendingCSRNameFile]; ok {
					t.Error("pending csr name should be cleared after certificate is stored")
				}
				if _, ok := secret.Data[PendingCSRKeyFile]; ok {
					t.Error("pending csr key should be cleared after certificate is stored")
				}
			},
		},
		{
			name:     "sync a valid hub kubeconfig secret",
			queueKey: testSecretName,
			secret: testinghelpers.NewHubKubeconfigSecret(
				testNamespace, testSecretName, "1",
				testinghelpers.NewTestCert(commonName, 10000*time.Second), map[string][]byte{
					register.ClusterNameFile: []byte(testinghelpers.TestManagedClusterName),
					register.AgentNameFile:   []byte(testAgentName),
					register.KubeconfigFile: testinghelpers.NewKubeconfig(
						"c1", "https://127.0.0.1:6001", "", "", nil, nil, nil),
				}),
			validateActions: func(t *testing.T, hubActions []clienttesting.Action, secret *corev1.Secret) {
				testingcommon.AssertNoActions(t, hubActions)
				if secret != nil {
					t.Errorf("expect the secret not to be generated")
				}
			},
		},
		{
			name:     "sync an expiring hub kubeconfig secret",
			queueKey: testSecretName,
			secret: testinghelpers.NewHubKubeconfigSecret(
				testNamespace, testSecretName, "1",
				testinghelpers.NewTestCert(commonName, -3*time.Second),
				map[string][]byte{
					register.ClusterNameFile: []byte(testinghelpers.TestManagedClusterName),
					register.AgentNameFile:   []byte(testAgentName),
					register.KubeconfigFile: testinghelpers.NewKubeconfig(
						"c1", "https://127.0.0.1:6001", "", "", nil, nil, nil),
				}),
			keyDataExpected: true,
			csrNameExpected: true,
			validateActions: func(t *testing.T, hubActions []clienttesting.Action, secret *corev1.Secret) {
				testingcommon.AssertActions(t, hubActions, "create")
				actual := hubActions[0].(clienttesting.CreateActionImpl).Object
				if _, ok := actual.(*unstructured.Unstructured); !ok {
					t.Errorf("expected csr was created, but failed")
				}
				if secret == nil {
					t.Fatal("expected secret with persisted pending CSR state")
				}
				if len(secret.Data[PendingCSRNameFile]) == 0 {
					t.Error("expected pending csr name to be persisted")
				}
				if len(secret.Data[PendingCSRKeyFile]) == 0 {
					t.Error("expected pending csr key to be persisted")
				}
			},
		},
		{
			name:     "sync when additional secret data changes",
			queueKey: testSecretName,
			secret: testinghelpers.NewHubKubeconfigSecret(
				testNamespace, testSecretName, "1",
				testinghelpers.NewTestCert(commonName, 10000*time.Second),
				map[string][]byte{
					register.ClusterNameFile: []byte(testinghelpers.TestManagedClusterName),
					register.AgentNameFile:   []byte("invalid-name"),
				}),
			keyDataExpected: true,
			csrNameExpected: true,
			validateActions: func(t *testing.T, hubActions []clienttesting.Action, secret *corev1.Secret) {
				testingcommon.AssertActions(t, hubActions, "create")
				actual := hubActions[0].(clienttesting.CreateActionImpl).Object
				if _, ok := actual.(*unstructured.Unstructured); !ok {
					t.Errorf("expected csr was created, but failed")
				}
				if secret == nil || len(secret.Data[PendingCSRKeyFile]) == 0 {
					t.Error("expected pending csr key to be persisted")
				}
			},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			ctrl := &mockCSRControl{}
			var csrs []runtime.Object
			if c.approvedCSRCert != nil {
				csr := testinghelpers.NewApprovedCSR(testinghelpers.CSRHolder{Name: testCSRName})
				csr.Status.Certificate = c.approvedCSRCert.Cert
				csrs = append(csrs, csr)
				ctrl.approved = true
				ctrl.issuedCertData = c.approvedCSRCert.Cert
			}
			hubKubeClient := kubefake.NewClientset(csrs...)
			ctrl.csrClient = &hubKubeClient.Fake

			// GenerateName is not working for fake clent, we set the name with prepend reactor
			hubKubeClient.PrependReactor(
				"create",
				"certificatesigningrequests",
				func(action clienttesting.Action) (handled bool, ret runtime.Object, err error) {
					return true, testinghelpers.NewCSR(testinghelpers.CSRHolder{Name: testCSRName}), nil
				},
			)

			additionalSecretData := map[string][]byte{
				register.ClusterNameFile: []byte(testinghelpers.TestManagedClusterName),
				register.AgentNameFile:   []byte(testAgentName),
			}
			csrOption := &CSROption{
				ObjectMeta: metav1.ObjectMeta{
					GenerateName: "test-",
				},
				Subject:    testSubject,
				SignerName: certificates.KubeAPIServerClientSignerName,
			}

			driver := &CSRDriver{
				csrControl:      ctrl,
				haltCSRCreation: func() bool { return false },
				csrOption:       csrOption,
				opt:             NewCSROption(),
			}

			if c.approvedCSRCert != nil {
				driver.csrName = testCSRName
				driver.keyData = c.approvedCSRCert.Key
			}

			syncCtx := testingcommon.NewFakeSyncContext(t, "test")

			secret, cond, err := driver.Process(
				context.TODO(), "test", c.secret, additionalSecretData, syncCtx.Recorder())
			if err != nil {
				t.Errorf("unexpected error %v", err)
			}

			hasKeyData := driver.keyData != nil
			if c.keyDataExpected != hasKeyData {
				t.Error("controller.keyData should be set")
			}

			hasCSRName := driver.csrName != ""
			if c.csrNameExpected != hasCSRName {
				t.Error("controller.csrName should be set")
			}

			if !conditionEqual(c.expectedCondition, cond) {
				t.Errorf("condition is not correct, expected %v, got %v", c.expectedCondition, cond)
			}

			c.validateActions(t, hubKubeClient.Actions(), secret)
		})
	}
}

func TestProcessRestoresPendingCSRAfterRestart(t *testing.T) {
	testSubject := &pkix.Name{CommonName: commonName}
	approvedCSRCert := testinghelpers.NewTestCert(commonName, 10*time.Second)

	csr := testinghelpers.NewApprovedCSR(testinghelpers.CSRHolder{Name: testCSRName})
	csr.Status.Certificate = approvedCSRCert.Cert
	hubKubeClient := kubefake.NewClientset(csr)
	ctrl := &mockCSRControl{
		csrClient:      &hubKubeClient.Fake,
		approved:       true,
		issuedCertData: approvedCSRCert.Cert,
	}

	// Simulate a fresh process after restart: in-memory state empty, pending state only in secret.
	secret := testinghelpers.NewHubKubeconfigSecret(
		testNamespace, testSecretName, "1", nil,
		map[string][]byte{
			register.ClusterNameFile: []byte(testinghelpers.TestManagedClusterName),
			register.AgentNameFile:   []byte(testAgentName),
			PendingCSRNameFile:       []byte(testCSRName),
			PendingCSRKeyFile:        approvedCSRCert.Key,
		})

	driver := &CSRDriver{
		csrControl:      ctrl,
		haltCSRCreation: func() bool { return false },
		csrOption: &CSROption{
			ObjectMeta: metav1.ObjectMeta{GenerateName: "test-"},
			Subject:    testSubject,
			SignerName: certificates.KubeAPIServerClientSignerName,
		},
		opt: NewCSROption(),
	}

	syncCtx := testingcommon.NewFakeSyncContext(t, "test")
	updated, cond, err := driver.Process(context.TODO(), "test", secret, map[string][]byte{
		register.ClusterNameFile: []byte(testinghelpers.TestManagedClusterName),
		register.AgentNameFile:   []byte(testAgentName),
	}, syncCtx.Recorder())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cond == nil || cond.Status != metav1.ConditionTrue {
		t.Fatalf("expected success condition, got %v", cond)
	}
	if updated == nil {
		t.Fatal("expected updated secret")
	}
	logger, _ := ktesting.NewTestContext(t)
	valid, err := IsCertificateValid(logger, updated.Data[TLSCertFile], testSubject)
	if err != nil || !valid {
		t.Fatalf("client certificate invalid: %v", err)
	}
	if string(updated.Data[TLSKeyFile]) != string(approvedCSRCert.Key) {
		t.Error("expected tls.key to match restored pending key")
	}
	if _, ok := updated.Data[PendingCSRNameFile]; ok {
		t.Error("pending csr name should be cleared")
	}
	if _, ok := updated.Data[PendingCSRKeyFile]; ok {
		t.Error("pending csr key should be cleared")
	}
	if driver.csrName != "" || driver.keyData != nil {
		t.Error("in-memory pending state should be reset after certificate is stored")
	}
}

func TestProcessKeepsPendingCSRWhileWaiting(t *testing.T) {
	testSubject := &pkix.Name{CommonName: commonName}
	keyData, err := keyutil.MakeEllipticPrivateKeyPEM()
	if err != nil {
		t.Fatalf("failed to generate key: %v", err)
	}

	// CSR must exist on the hub (unapproved) so restore does not treat it as NotFound.
	pendingCSR := testinghelpers.NewCSR(testinghelpers.CSRHolder{Name: testCSRName})
	hubKubeClient := kubefake.NewClientset(pendingCSR)
	ctrl := &mockCSRControl{
		csrClient: &hubKubeClient.Fake,
		approved:  false,
	}

	secret := testinghelpers.NewHubKubeconfigSecret(
		testNamespace, testSecretName, "1", nil,
		map[string][]byte{
			register.ClusterNameFile: []byte(testinghelpers.TestManagedClusterName),
			register.AgentNameFile:   []byte(testAgentName),
			PendingCSRNameFile:       []byte(testCSRName),
			PendingCSRKeyFile:        keyData,
		})

	driver := &CSRDriver{
		csrControl:      ctrl,
		haltCSRCreation: func() bool { return false },
		csrOption: &CSROption{
			ObjectMeta: metav1.ObjectMeta{GenerateName: "test-"},
			Subject:    testSubject,
			SignerName: certificates.KubeAPIServerClientSignerName,
		},
		opt: NewCSROption(),
	}

	syncCtx := testingcommon.NewFakeSyncContext(t, "test")
	updated, cond, err := driver.Process(context.TODO(), "test", secret, map[string][]byte{
		register.ClusterNameFile: []byte(testinghelpers.TestManagedClusterName),
		register.AgentNameFile:   []byte(testAgentName),
	}, syncCtx.Recorder())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if updated != nil || cond != nil {
		t.Fatalf("expected no secret/condition update while waiting, got secret=%v cond=%v", updated, cond)
	}
	if driver.csrName != testCSRName {
		t.Errorf("expected restored csr name %q, got %q", testCSRName, driver.csrName)
	}
	if string(driver.keyData) != string(keyData) {
		t.Error("expected restored pending key in memory")
	}
}

func TestProcessRecreatesMissingPendingCSR(t *testing.T) {
	testSubject := &pkix.Name{CommonName: commonName}
	keyData, err := keyutil.MakeEllipticPrivateKeyPEM()
	if err != nil {
		t.Fatalf("failed to generate key: %v", err)
	}

	// Lister and hub API both miss the persisted CSR. The hub NotFound must stay
	// wrapped so Process can drop the stale pending state and create a replacement.
	indexer := cache.NewIndexer(cache.MetaNamespaceKeyFunc, cache.Indexers{
		cache.NamespaceIndex: cache.MetaNamespaceIndexFunc,
	})
	hubKubeClient := kubefake.NewClientset()
	ctrl := &v1CSRControl{
		hubCSRLister: certificatesv1listers.NewCertificateSigningRequestLister(indexer),
		hubCSRClient: hubKubeClient.CertificatesV1().CertificateSigningRequests(),
	}
	if _, lookupErr := ctrl.IsApproved(testCSRName); !apierrors.IsNotFound(lookupErr) {
		t.Fatalf("missing CSR must surface NotFound, got %v", lookupErr)
	}

	secret := testinghelpers.NewHubKubeconfigSecret(
		testNamespace, testSecretName, "1", nil,
		map[string][]byte{
			register.ClusterNameFile: []byte(testinghelpers.TestManagedClusterName),
			register.AgentNameFile:   []byte(testAgentName),
			PendingCSRNameFile:       []byte(testCSRName),
			PendingCSRKeyFile:        keyData,
		})

	driver := &CSRDriver{
		csrControl:      ctrl,
		haltCSRCreation: func() bool { return false },
		csrOption: &CSROption{
			ObjectMeta: metav1.ObjectMeta{Name: "replacement-csr"},
			Subject:    testSubject,
			SignerName: certificates.KubeAPIServerClientSignerName,
		},
		opt: NewCSROption(),
	}

	syncCtx := testingcommon.NewFakeSyncContext(t, "test")
	updated, cond, err := driver.Process(context.TODO(), "test", secret, map[string][]byte{
		register.ClusterNameFile: []byte(testinghelpers.TestManagedClusterName),
		register.AgentNameFile:   []byte(testAgentName),
	}, syncCtx.Recorder())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cond != nil {
		t.Fatalf("expected no condition while the replacement CSR is pending, got %v", cond)
	}
	if updated == nil {
		t.Fatal("expected secret with the replacement CSR persisted")
	}
	gotName := string(updated.Data[PendingCSRNameFile])
	if gotName != "replacement-csr" {
		t.Fatalf("expected replacement CSR name to be persisted, got %q", gotName)
	}
	if len(updated.Data[PendingCSRKeyFile]) == 0 {
		t.Fatal("expected replacement private key to be persisted")
	}
	if string(updated.Data[PendingCSRKeyFile]) == string(keyData) {
		t.Fatal("replacement private key should be newly generated")
	}
	if driver.csrName != gotName || string(driver.keyData) != string(updated.Data[PendingCSRKeyFile]) {
		t.Fatalf("in-memory pending state = (%q, %d bytes), secret = (%q, %d bytes)",
			driver.csrName, len(driver.keyData), gotName, len(updated.Data[PendingCSRKeyFile]))
	}
	if _, err := hubKubeClient.CertificatesV1().CertificateSigningRequests().Get(
		context.TODO(), gotName, metav1.GetOptions{}); err != nil {
		t.Fatalf("replacement CSR was not created on the hub: %v", err)
	}
}

func TestProcessSavesKeyMismatchThenCreatesReplacement(t *testing.T) {
	testSubject := &pkix.Name{CommonName: commonName}
	issued := testinghelpers.NewTestCert(commonName, 10*time.Second)
	pendingKey, err := keyutil.MakeEllipticPrivateKeyPEM()
	if err != nil {
		t.Fatalf("failed to generate key: %v", err)
	}

	csr := testinghelpers.NewApprovedCSR(testinghelpers.CSRHolder{Name: testCSRName})
	csr.Status.Certificate = issued.Cert
	hubKubeClient := kubefake.NewClientset(csr)
	ctrl := &mockCSRControl{
		csrClient:      &hubKubeClient.Fake,
		approved:       true,
		issuedCertData: issued.Cert,
	}

	secret := testinghelpers.NewHubKubeconfigSecret(
		testNamespace, testSecretName, "1", nil,
		map[string][]byte{
			register.ClusterNameFile: []byte(testinghelpers.TestManagedClusterName),
			register.AgentNameFile:   []byte(testAgentName),
			PendingCSRNameFile:       []byte(testCSRName),
			PendingCSRKeyFile:        pendingKey,
		})

	driver := &CSRDriver{
		csrControl:      ctrl,
		haltCSRCreation: func() bool { return false },
		csrOption: &CSROption{
			ObjectMeta: metav1.ObjectMeta{GenerateName: "test-"},
			Subject:    testSubject,
			SignerName: certificates.KubeAPIServerClientSignerName,
		},
		opt: NewCSROption(),
	}

	syncCtx := testingcommon.NewFakeSyncContext(t, "test")
	additional := map[string][]byte{
		register.ClusterNameFile: []byte(testinghelpers.TestManagedClusterName),
		register.AgentNameFile:   []byte(testAgentName),
	}
	cleared, cond, err := driver.Process(context.TODO(), "test", secret, additional, syncCtx.Recorder())
	if err != nil {
		t.Fatalf("mismatch must return no error so the cleared secret is saved, got %v", err)
	}
	if cond == nil || cond.Status != metav1.ConditionFalse {
		t.Fatalf("expected failed condition, got %v", cond)
	}
	if _, ok := cleared.Data[PendingCSRNameFile]; ok {
		t.Fatal("pending csr name should be cleared before save")
	}
	if _, ok := cleared.Data[PendingCSRKeyFile]; ok {
		t.Fatal("pending csr key should be cleared before save")
	}
	if driver.csrName != "" || driver.keyData != nil {
		t.Fatal("in-memory pending state should be reset")
	}

	// What secretController.sync persists when Process returns the secret and no error.
	saved := cleared.DeepCopy()
	replaced, cond, err := driver.Process(context.TODO(), "test", saved, additional, syncCtx.Recorder())
	if err != nil {
		t.Fatalf("replacement reconcile failed: %v", err)
	}
	if cond != nil {
		t.Fatalf("expected no condition while the replacement CSR is pending, got %v", cond)
	}
	if replaced == nil {
		t.Fatal("expected secret with the replacement CSR persisted")
	}
	gotName := string(replaced.Data[PendingCSRNameFile])
	if gotName == "" || gotName == testCSRName {
		t.Fatalf("expected a replacement CSR name, got %q", gotName)
	}
	if len(replaced.Data[PendingCSRKeyFile]) == 0 {
		t.Fatal("expected replacement private key to be persisted")
	}
}

func TestProcessKeepsPendingCSROnTransientLookupError(t *testing.T) {
	keyData, err := keyutil.MakeEllipticPrivateKeyPEM()
	if err != nil {
		t.Fatalf("failed to generate key: %v", err)
	}
	secret := testinghelpers.NewHubKubeconfigSecret(
		testNamespace, testSecretName, "1", nil,
		map[string][]byte{
			register.ClusterNameFile: []byte(testinghelpers.TestManagedClusterName),
			register.AgentNameFile:   []byte(testAgentName),
			PendingCSRNameFile:       []byte(testCSRName),
			PendingCSRKeyFile:        keyData,
		})
	driver := &CSRDriver{
		csrControl:      &lookupErrCSRControl{err: fmt.Errorf("temporary hub failure")},
		haltCSRCreation: func() bool { return false },
		csrOption: &CSROption{
			ObjectMeta: metav1.ObjectMeta{GenerateName: "test-"},
			Subject:    &pkix.Name{CommonName: commonName},
			SignerName: certificates.KubeAPIServerClientSignerName,
		},
		opt: NewCSROption(),
	}

	syncCtx := testingcommon.NewFakeSyncContext(t, "test")
	updated, cond, err := driver.Process(context.TODO(), "test", secret, nil, syncCtx.Recorder())
	if err == nil {
		t.Fatal("expected transient lookup error")
	}
	if updated != nil {
		t.Fatal("transient lookup must not return a secret to save")
	}
	if cond == nil || cond.Status != metav1.ConditionFalse {
		t.Fatalf("expected failed condition, got %v", cond)
	}
	if string(secret.Data[PendingCSRNameFile]) != testCSRName || string(secret.Data[PendingCSRKeyFile]) != string(keyData) {
		t.Fatal("pending csr name and key must stay unchanged")
	}
	if driver.csrName != testCSRName || string(driver.keyData) != string(keyData) {
		t.Fatal("in-memory pending state must stay unchanged")
	}
}

type lookupErrCSRControl struct {
	err error
}

func (c *lookupErrCSRControl) Create(context.Context, events.Recorder, metav1.ObjectMeta, []byte, string, *int32) (string, error) {
	return "", fmt.Errorf("create should not be called")
}

func (c *lookupErrCSRControl) IsApproved(string) (bool, error) {
	return false, c.err
}

func (c *lookupErrCSRControl) GetIssuedCertificate(string) ([]byte, error) {
	return nil, c.err
}

func (c *lookupErrCSRControl) Informer() cache.SharedIndexInformer {
	return nil
}

// TestProcessKeepsPendingCSROnTransientLookupError stubs CSRControl, so it cannot see whether a
// real lookup reports the failure at all. The watcher store miss falls back to a live hub GET,
// and only that GET decides absence: a failure there has to surface as an error instead of an
// empty csr with no error, which the caller would read as "not approved yet".
func TestCSRControlReturnsTransientHubLookupError(t *testing.T) {
	indexer := cache.NewIndexer(cache.MetaNamespaceKeyFunc, cache.Indexers{
		cache.NamespaceIndex: cache.MetaNamespaceIndexFunc,
	})
	hubKubeClient := kubefake.NewClientset()
	hubKubeClient.PrependReactor("get", "certificatesigningrequests",
		func(clienttesting.Action) (bool, runtime.Object, error) {
			return true, nil, apierrors.NewInternalError(fmt.Errorf("hub is unavailable"))
		})
	ctrl := &v1CSRControl{
		hubCSRLister: certificatesv1listers.NewCertificateSigningRequestLister(indexer),
		hubCSRClient: hubKubeClient.CertificatesV1().CertificateSigningRequests(),
	}

	if _, err := ctrl.IsApproved(testCSRName); err == nil || apierrors.IsNotFound(err) {
		t.Errorf("IsApproved must report the hub failure, got %v", err)
	}
	if _, err := ctrl.GetIssuedCertificate(testCSRName); err == nil || apierrors.IsNotFound(err) {
		t.Errorf("GetIssuedCertificate must report the hub failure, got %v", err)
	}
}

// Dropping a missing csr and creating its replacement are independent: when the current
// certificate is still healthy no replacement is created, and the drop is lost unless the secret
// is still handed back for saving.
func TestProcessSavesClearedPendingCSRWithoutReplacement(t *testing.T) {
	// Plenty of life left on the certificate keeps shouldCreateCSR from firing.
	currentCert := testinghelpers.NewTestCert(commonName, 1*time.Hour)
	orphanKey, err := keyutil.MakeEllipticPrivateKeyPEM()
	if err != nil {
		t.Fatalf("failed to generate key: %v", err)
	}

	// Neither the watcher store nor the hub has the saved csr.
	indexer := cache.NewIndexer(cache.MetaNamespaceKeyFunc, cache.Indexers{
		cache.NamespaceIndex: cache.MetaNamespaceIndexFunc,
	})
	hubKubeClient := kubefake.NewClientset()
	ctrl := &v1CSRControl{
		hubCSRLister: certificatesv1listers.NewCertificateSigningRequestLister(indexer),
		hubCSRClient: hubKubeClient.CertificatesV1().CertificateSigningRequests(),
	}

	secret := testinghelpers.NewHubKubeconfigSecret(
		testNamespace, testSecretName, "1", currentCert,
		map[string][]byte{
			register.ClusterNameFile: []byte(testinghelpers.TestManagedClusterName),
			register.AgentNameFile:   []byte(testAgentName),
			PendingCSRNameFile:       []byte(testCSRName),
			PendingCSRKeyFile:        orphanKey,
		})

	driver := &CSRDriver{
		csrControl:      ctrl,
		haltCSRCreation: func() bool { return false },
		csrOption: &CSROption{
			ObjectMeta: metav1.ObjectMeta{GenerateName: "test-"},
			Subject:    &pkix.Name{CommonName: commonName},
			SignerName: certificates.KubeAPIServerClientSignerName,
		},
		opt: NewCSROption(),
	}

	syncCtx := testingcommon.NewFakeSyncContext(t, "test")
	updated, cond, err := driver.Process(context.TODO(), "test", secret, map[string][]byte{
		register.ClusterNameFile: []byte(testinghelpers.TestManagedClusterName),
		register.AgentNameFile:   []byte(testAgentName),
	}, syncCtx.Recorder())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cond != nil {
		t.Fatalf("expected no condition, got %v", cond)
	}
	if updated == nil {
		t.Fatal("secret must be returned so the cleared pending state is saved")
	}
	if _, ok := updated.Data[PendingCSRNameFile]; ok {
		t.Error("stale pending csr name should be cleared")
	}
	if _, ok := updated.Data[PendingCSRKeyFile]; ok {
		t.Error("stale pending csr key should be cleared")
	}
	if string(updated.Data[TLSCertFile]) != string(currentCert.Cert) {
		t.Error("the in-use certificate must be left untouched")
	}
	for _, action := range hubKubeClient.Actions() {
		if action.GetVerb() == "create" {
			t.Error("no csr should be created while the current certificate is healthy")
		}
	}
}

var _ CSRControl = &mockCSRControl{}

func conditionEqual(expected, actual *metav1.Condition) bool {
	if expected == nil && actual == nil {
		return true
	}

	if expected == nil || actual == nil {
		return false
	}

	if expected.Type != actual.Type {
		return false
	}

	if string(expected.Status) != string(actual.Status) {
		return false
	}

	return true
}

type mockCSRControl struct {
	approved       bool
	issuedCertData []byte
	csrClient      *clienttesting.Fake
}

func (m *mockCSRControl) Create(
	_ context.Context, _ events.Recorder, objMeta metav1.ObjectMeta, _ []byte, _ string, _ *int32) (string, error) {
	mockCSR := &unstructured.Unstructured{}
	_, err := m.csrClient.Invokes(clienttesting.CreateActionImpl{
		ActionImpl: clienttesting.ActionImpl{
			Verb:     "create",
			Resource: certificates.SchemeGroupVersion.WithResource("certificatesigningrequests"),
		},
		Object: mockCSR,
	}, nil)
	return objMeta.Name + rand.String(4), err
}

func (m *mockCSRControl) IsApproved(name string) (bool, error) {
	_, err := m.csrClient.Invokes(clienttesting.GetActionImpl{
		ActionImpl: clienttesting.ActionImpl{
			Verb:     "get",
			Resource: certificates.SchemeGroupVersion.WithResource("certificatesigningrequests"),
		},
		Name: name,
	}, nil)

	return m.approved, err
}

func (m *mockCSRControl) GetIssuedCertificate(name string) ([]byte, error) {
	_, err := m.csrClient.Invokes(clienttesting.GetActionImpl{
		ActionImpl: clienttesting.ActionImpl{
			Verb:     "get",
			Resource: certificates.SchemeGroupVersion.WithResource("certificatesigningrequests"),
		},
		Name: name,
	}, nil)
	return m.issuedCertData, err
}

func (m *mockCSRControl) Informer() cache.SharedIndexInformer {
	client := kubefake.NewClientset()
	informerFactory := informers.NewSharedInformerFactory(client, 0)
	return informerFactory.Certificates().V1().CertificateSigningRequests().Informer()
}

func TestIsHubKubeConfigValidFunc(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "testvalidhubclientconfig")
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	defer os.RemoveAll(tempDir)

	cert1 := testinghelpers.NewTestCert("system:open-cluster-management:cluster1:agent1", 60*time.Second)
	cert2 := testinghelpers.NewTestCert("test", 60*time.Second)

	kubeconfig := testinghelpers.NewKubeconfig("c1", "https://127.0.0.1:6001", "", "", nil, nil, nil)

	cases := []struct {
		name               string
		clusterName        string
		agentName          string
		kubeconfig         []byte
		bootstapKubeconfig []byte
		tlsCert            []byte
		tlsKey             []byte
		isValid            bool
	}{
		{
			name:    "no kubeconfig",
			isValid: false,
		},
		{
			name:               "no tls key",
			kubeconfig:         kubeconfig,
			bootstapKubeconfig: testinghelpers.NewKubeconfig("c1", "https://127.0.0.1:6001", "", "/etc/ca.crt", nil, nil, nil),
			isValid:            false,
		},
		{
			name:               "no tls cert",
			kubeconfig:         kubeconfig,
			bootstapKubeconfig: testinghelpers.NewKubeconfig("c1", "https://127.0.0.1:6001", "", "/etc/ca.crt", nil, nil, nil),
			tlsKey:             cert1.Key,
			isValid:            false,
		},
		{
			name:               "cert is not issued for cluster1:agent1",
			clusterName:        "cluster1",
			agentName:          "agent1",
			kubeconfig:         kubeconfig,
			bootstapKubeconfig: testinghelpers.NewKubeconfig("c1", "https://127.0.0.1:6001", "", "/etc/ca.crt", nil, nil, nil),
			tlsKey:             cert2.Key,
			tlsCert:            cert2.Cert,
			isValid:            false,
		},
		{
			name:               "context cluster changes",
			clusterName:        "cluster1",
			agentName:          "agent1",
			kubeconfig:         kubeconfig,
			bootstapKubeconfig: testinghelpers.NewKubeconfig("c2", "https://127.0.0.1:6001", "", "", nil, nil, nil),
			tlsKey:             cert1.Key,
			tlsCert:            cert1.Cert,
			isValid:            false,
		},
		{
			name:               "hub server url changes",
			clusterName:        "cluster1",
			agentName:          "agent1",
			kubeconfig:         kubeconfig,
			bootstapKubeconfig: testinghelpers.NewKubeconfig("c1", "https://127.0.0.2:6001", "", "", nil, nil, nil),
			tlsKey:             cert1.Key,
			tlsCert:            cert1.Cert,
			isValid:            false,
		},
		{
			name:               "proxy url changes",
			clusterName:        "cluster1",
			agentName:          "agent1",
			kubeconfig:         kubeconfig,
			bootstapKubeconfig: testinghelpers.NewKubeconfig("c1", "https://127.0.0.1:6001", "https://127.0.0.1:3129", "", nil, nil, nil),
			tlsKey:             cert1.Key,
			tlsCert:            cert1.Cert,
			isValid:            false,
		},
		{
			name:               "ca bundle changes",
			clusterName:        "cluster1",
			agentName:          "agent1",
			kubeconfig:         kubeconfig,
			bootstapKubeconfig: testinghelpers.NewKubeconfig("c1", "https://127.0.0.1:6001", "", "", []byte("test"), nil, nil),
			tlsKey:             cert1.Key,
			tlsCert:            cert1.Cert,
			isValid:            false,
		},
		{
			name:               "ca changes",
			clusterName:        "cluster1",
			agentName:          "agent1",
			kubeconfig:         kubeconfig,
			bootstapKubeconfig: testinghelpers.NewKubeconfig("c1", "https://127.0.0.1:6001", "", "/etc/ca.crt", nil, nil, nil),
			tlsKey:             cert1.Key,
			tlsCert:            cert1.Cert,
			isValid:            false,
		},
		{
			name:               "invalid issuer",
			clusterName:        "cluster2",
			agentName:          "agent1",
			kubeconfig:         kubeconfig,
			bootstapKubeconfig: testinghelpers.NewKubeconfig("c1", "https://127.0.0.1:6001", "", "", nil, nil, nil),
			tlsKey:             cert1.Key,
			tlsCert:            cert1.Cert,
			isValid:            false,
		},
		{
			name:               "valid hub client config",
			clusterName:        "cluster1",
			agentName:          "agent1",
			kubeconfig:         kubeconfig,
			bootstapKubeconfig: testinghelpers.NewKubeconfig("c1", "https://127.0.0.1:6001", "", "", nil, nil, nil),
			tlsKey:             cert1.Key,
			tlsCert:            cert1.Cert,
			isValid:            true,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			secretOption := register.SecretOption{
				ClusterName:       c.clusterName,
				AgentName:         c.agentName,
				HubKubeconfigDir:  tempDir,
				HubKubeconfigFile: path.Join(tempDir, "kubeconfig"),
			}
			driver, _ := NewCSRDriver(NewCSROption(), secretOption)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if c.kubeconfig != nil {
				testinghelpers.WriteFile(path.Join(tempDir, "kubeconfig"), c.kubeconfig)
			}
			if c.tlsKey != nil {
				testinghelpers.WriteFile(path.Join(tempDir, "tls.key"), c.tlsKey)
			}
			if c.tlsCert != nil {
				testinghelpers.WriteFile(path.Join(tempDir, "tls.crt"), c.tlsCert)
			}
			if c.bootstapKubeconfig != nil {
				testinghelpers.WriteFile(path.Join(tempDir, "bootstrap-kubeconfig"), c.bootstapKubeconfig)
				if err != nil {
					t.Fatal(err)
				}
				secretOption.BootStrapKubeConfigFile = path.Join(tempDir, "bootstrap-kubeconfig")
			}

			valid, err := register.IsHubKubeConfigValidFunc(driver, secretOption)(context.TODO())
			if err != nil {
				t.Errorf("unexpected error: %v", err)
			}
			if c.isValid != valid {
				t.Errorf("expect %t, but %t", c.isValid, valid)
			}
		})
	}
}

func TestFilterCSREvents(t *testing.T) {
	clusterName := "cluster1"
	signerName := "signer1"
	addOnName := "addon1"

	cases := []struct {
		name     string
		csr      *certificates.CertificateSigningRequest
		expected bool
	}{
		{
			name: "csr not from the managed cluster",
			csr:  &certificates.CertificateSigningRequest{},
		},
		{
			name: "csr not for the addon",
			csr:  &certificates.CertificateSigningRequest{},
		},
		{
			name: "csr with different signer name",
			csr:  &certificates.CertificateSigningRequest{},
		},
		{
			name: "valid csr",
			csr: &certificates.CertificateSigningRequest{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						// the labels are only hints. Anyone could set/modify them.
						clusterv1.ClusterNameLabelKey: clusterName,
						addonv1beta1.AddonLabelKey:    addOnName,
					},
				},
				Spec: certificates.CertificateSigningRequestSpec{
					SignerName: signerName,
				},
			},
			expected: true,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			filterFunc := createCSREventFilterFunc(clusterName, addOnName, signerName)
			actual := filterFunc(c.csr)
			if actual != c.expected {
				t.Errorf("Expected %v but got %v", c.expected, actual)
			}
		})
	}
}

func TestIndexByClusterName(t *testing.T) {
	testcases := []struct {
		name     string
		csr      *certificates.CertificateSigningRequest
		expected []string
	}{
		{
			name:     "no index",
			csr:      &certificates.CertificateSigningRequest{},
			expected: []string{},
		},
		{
			name: "has cluster label",
			csr: &certificates.CertificateSigningRequest{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{clusterv1.ClusterNameLabelKey: "cluster1"},
				},
			},
			expected: []string{"cluster1"},
		},
		{
			name: "has cluster label",
			csr: &certificates.CertificateSigningRequest{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						clusterv1.ClusterNameLabelKey: "cluster1",
						addonv1beta1.AddonLabelKey:    "addon1",
					},
				},
			},
			expected: []string{},
		},
	}

	for _, tt := range testcases {
		t.Run(tt.name, func(t *testing.T) {
			actual, err := indexByClusterFunc(tt.csr)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(actual, tt.expected) {
				t.Errorf("Expected %v but got %v", tt.expected, actual)
			}
		})
	}
}

func TestIndexByAddonFunc(t *testing.T) {
	testcases := []struct {
		name     string
		csr      *certificates.CertificateSigningRequest
		expected []string
	}{
		{
			name:     "no index",
			csr:      &certificates.CertificateSigningRequest{},
			expected: []string{},
		},
		{
			name: "has cluster label",
			csr: &certificates.CertificateSigningRequest{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{clusterv1.ClusterNameLabelKey: "cluster1"},
				},
			},
			expected: []string{},
		},
		{
			name: "has cluster label",
			csr: &certificates.CertificateSigningRequest{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						clusterv1.ClusterNameLabelKey: "cluster1",
						addonv1beta1.AddonLabelKey:    "addon1",
					},
				},
			},
			expected: []string{"cluster1/addon1"},
		},
	}

	for _, tt := range testcases {
		t.Run(tt.name, func(t *testing.T) {
			actual, err := indexByAddonFunc(tt.csr)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(actual, tt.expected) {
				t.Errorf("Expected %v but got %v", tt.expected, actual)
			}
		})
	}
}

func TestNewCSRDriver(t *testing.T) {
	secretOpts := register.SecretOption{
		ClusterName: "cluster1",
		AgentName:   "agent1",
	}
	driver, err := NewCSRDriver(NewCSROption(), secretOpts)
	if err == nil {
		t.Errorf("expect error, but got nil")
	}
	secretOpts.BootStrapKubeConfigFile = "bootstrap-kubeconfig"
	driver, err = NewCSRDriver(NewCSROption(), secretOpts)
	if err != nil {
		t.Fatal(err)
	}
	if driver.csrOption.Subject.CommonName != fmt.Sprintf("%scluster1:agent1", user.SubjectPrefix) {
		t.Errorf("common name is not set correctly, got %s", driver.csrOption.Subject.CommonName)
	}
	ctrl := &mockCSRControl{}
	hubKubeClient := kubefake.NewClientset()
	ctrl.csrClient = &hubKubeClient.Fake
	driver.csrControl = ctrl

	addonSecretOptions := register.SecretOption{
		ClusterName: "cluster1",
		AgentName:   "addonagent1",
		AddonRegistration: addonv1beta1.RegistrationConfig{
			Type: addonv1beta1.KubeClient,
			KubeClient: &addonv1beta1.KubeClientConfig{
				Subject: addonv1beta1.KubeClientSubject{
					BaseSubject: addonv1beta1.BaseSubject{
						User: "addonagent1",
					},
				},
			},
		},
	}
	regOption := &registertesting.TestAddonAuthConfig{
		KubeClientAuth: "csr",
		CSROption:      NewCSROption(),
	}
	forkedDriver, err := driver.Fork("addon1", regOption, addonSecretOptions)
	if err != nil {
		t.Fatal(err)
	}
	csrAddonDriver := forkedDriver.(*CSRDriver)
	if csrAddonDriver.csrOption.Subject.CommonName != "addonagent1" {
		t.Errorf("common name is not set correctly")
	}
}

func TestCSREventFilterFunc(t *testing.T) {
	filter := createCSREventFilterFunc("cluster1", "addon1", "signer1")
	cases := []struct {
		name     string
		csr      *certificates.CertificateSigningRequest
		expected bool
	}{
		{
			name: "incorrect cluster",
			csr: &certificates.CertificateSigningRequest{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						clusterv1.ClusterNameLabelKey: "cluster2",
						addonv1beta1.AddonLabelKey:    "addon1",
					},
				},
				Spec: certificates.CertificateSigningRequestSpec{
					SignerName: "signer1",
				},
			},
			expected: false,
		},
		{
			name: "incorrect addon",
			csr: &certificates.CertificateSigningRequest{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						clusterv1.ClusterNameLabelKey: "cluster1",
						addonv1beta1.AddonLabelKey:    "addon2",
					},
				},
				Spec: certificates.CertificateSigningRequestSpec{
					SignerName: "signer1",
				},
			},
			expected: false,
		},
		{
			name: "incorrect signer",
			csr: &certificates.CertificateSigningRequest{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						clusterv1.ClusterNameLabelKey: "cluster1",
						addonv1beta1.AddonLabelKey:    "addon1",
					},
				},
				Spec: certificates.CertificateSigningRequestSpec{
					SignerName: "signer2",
				},
			},
			expected: false,
		},
		{
			name: "all correct",
			csr: &certificates.CertificateSigningRequest{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						clusterv1.ClusterNameLabelKey: "cluster1",
						addonv1beta1.AddonLabelKey:    "addon1",
					},
				},
				Spec: certificates.CertificateSigningRequestSpec{
					SignerName: "signer1",
				},
			},
			expected: true,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			passed := filter(c.csr)
			if passed != c.expected {
				t.Errorf("Expected %v but got %v", c.expected, passed)
			}
		})
	}
}

func TestBuildClient(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "testvalidhubclientconfig")
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	cert1 := testinghelpers.NewTestCert("system:open-cluster-management:cluster1:agent1", 60*time.Second)
	defer os.RemoveAll(tempDir)

	kubeconfig := testinghelpers.NewKubeconfig(
		"cluster1", "https://127.0.0.1:6001", "", "", nil, cert1.Key, cert1.Cert)

	cases := []struct {
		name                string
		kubeconfig          []byte
		bootstrapKubeconfig []byte
		bootstrap           bool
		expectErr           bool
	}{
		{
			name:       "bootstrap is not set",
			kubeconfig: kubeconfig,
			bootstrap:  true,
			expectErr:  true,
		},
		{
			name:                "bootstrap is set",
			kubeconfig:          nil,
			bootstrapKubeconfig: kubeconfig,
			bootstrap:           true,
			expectErr:           false,
		},
		{
			name:                "bootstrap is false",
			kubeconfig:          nil,
			bootstrapKubeconfig: kubeconfig,
			bootstrap:           false,
			expectErr:           true,
		},
		{
			name:                "bootstrap is false with correct kubeconfig",
			kubeconfig:          kubeconfig,
			bootstrapKubeconfig: nil,
			bootstrap:           false,
			expectErr:           false,
		},
	}

	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			err = features.SpokeMutableFeatureGate.Add(ocmfeature.DefaultSpokeRegistrationFeatureGates)
			if err != nil {
				t.Fatal(err)
			}

			secretOpts := register.SecretOption{
				ClusterName:             "cluster1",
				AgentName:               "agent1",
				BootStrapKubeConfigFile: "boostrap.yaml",
			}
			if tt.kubeconfig != nil {
				testinghelpers.WriteFile(path.Join(tempDir, "kubeconfig"), tt.kubeconfig)
				secretOpts.HubKubeconfigFile = path.Join(tempDir, "kubeconfig")
			}
			if tt.bootstrapKubeconfig != nil {
				testinghelpers.WriteFile(path.Join(tempDir, "bootstrap-kubeconfig"), tt.bootstrapKubeconfig)
				secretOpts.BootStrapKubeConfigFile = path.Join(tempDir, "bootstrap-kubeconfig")
			}
			driver, _ := NewCSRDriver(NewCSROption(), secretOpts)
			_, err := driver.BuildClients(context.TODO(), secretOpts, tt.bootstrap)
			if (err != nil) != tt.expectErr {
				t.Errorf("expected error %v but got %v", tt.expectErr, err)
			}
		})
	}
}

func TestCSRDriver_Fork_TokenAuth(t *testing.T) {
	// Setup addon client and informer
	addon := &addonv1beta1.ManagedClusterAddOn{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "addon1",
			Namespace: "cluster1",
		},
	}
	addonClient := addonfake.NewSimpleClientset(addon)
	addonInformerFactory := addoninformers.NewSharedInformerFactory(addonClient, 10*time.Minute)
	addonInformer := addonInformerFactory.Addon().V1beta1().ManagedClusterAddOns()

	ctrl := &mockCSRControl{}
	hubKubeClient := kubefake.NewClientset()
	ctrl.csrClient = &hubKubeClient.Fake

	addonClients := &register.AddOnClients{
		AddonClient:   addonClient,
		AddonInformer: addonInformer,
	}

	tests := []struct {
		name           string
		setupDriver    func() *CSRDriver
		addonName      string
		secretOption   register.SecretOption
		regOption      register.AddonAuthConfig
		expectErr      bool
		expectErrMsg   string
		validateResult func(t *testing.T, driver register.RegisterDriver)
	}{
		{
			name: "token auth - success",
			setupDriver: func() *CSRDriver {
				driver := &CSRDriver{
					csrControl:   ctrl,
					tokenControl: &registertesting.MockTokenControl{},
					addonClients: addonClients,
				}
				return driver
			},
			addonName: "addon1",
			secretOption: register.SecretOption{
				ClusterName: "cluster1",
				AddonRegistration: addonv1beta1.RegistrationConfig{
					Type: addonv1beta1.KubeClient,
					KubeClient: &addonv1beta1.KubeClientConfig{
						Driver: "token",
					},
				},
			},
			regOption: &registertesting.TestAddonAuthConfig{
				KubeClientAuth: "token",
				TokenOption:    token.NewTokenOption(),
			},
			expectErr: false,
			validateResult: func(t *testing.T, driver register.RegisterDriver) {
				if _, ok := driver.(*token.TokenDriver); !ok {
					t.Errorf("expected TokenDriver, got %T", driver)
				}
			},
		},
		{
			name: "token auth - invalid token option",
			setupDriver: func() *CSRDriver {
				driver := &CSRDriver{
					csrControl:   ctrl,
					tokenControl: &registertesting.MockTokenControl{},
					addonClients: addonClients,
				}
				return driver
			},
			addonName: "addon1",
			secretOption: register.SecretOption{
				ClusterName: "cluster1",
				AddonRegistration: addonv1beta1.RegistrationConfig{
					Type: addonv1beta1.KubeClient,
					KubeClient: &addonv1beta1.KubeClientConfig{
						Driver: "token",
					},
				},
			},
			regOption: &registertesting.TestAddonAuthConfig{
				KubeClientAuth: "token",
				TokenOption:    nil,
			},
			expectErr:    true,
			expectErrMsg: "token authentication requested but TokenConfiguration is nil",
		},
		{
			name: "token auth - tokenControl not initialized",
			setupDriver: func() *CSRDriver {
				driver := &CSRDriver{
					csrControl:   ctrl,
					tokenControl: nil,
					addonClients: addonClients,
				}
				return driver
			},
			addonName: "addon1",
			secretOption: register.SecretOption{
				ClusterName: "cluster1",
				AddonRegistration: addonv1beta1.RegistrationConfig{
					Type: addonv1beta1.KubeClient,
					KubeClient: &addonv1beta1.KubeClientConfig{
						Driver: "token",
					},
				},
			},
			regOption: &registertesting.TestAddonAuthConfig{
				KubeClientAuth: "token",
				TokenOption:    token.NewTokenOption(),
			},
			expectErr:    true,
			expectErrMsg: "tokenControl is not initialized",
		},
		{
			name: "token auth - addonClients not initialized",
			setupDriver: func() *CSRDriver {
				driver := &CSRDriver{
					csrControl:   ctrl,
					tokenControl: &registertesting.MockTokenControl{},
					addonClients: nil,
				}
				return driver
			},
			addonName: "addon1",
			secretOption: register.SecretOption{
				ClusterName: "cluster1",
				AddonRegistration: addonv1beta1.RegistrationConfig{
					Type: addonv1beta1.KubeClient,
					KubeClient: &addonv1beta1.KubeClientConfig{
						Driver: "token",
					},
				},
			},
			regOption: &registertesting.TestAddonAuthConfig{
				KubeClientAuth: "token",
				TokenOption:    token.NewTokenOption(),
			},
			expectErr:    true,
			expectErrMsg: "addonClients is not initialized",
		},
		{
			name: "csr auth with custom signer - success",
			setupDriver: func() *CSRDriver {
				driver := &CSRDriver{
					csrControl:   ctrl,
					tokenControl: &registertesting.MockTokenControl{},
					addonClients: addonClients,
				}
				return driver
			},
			addonName: "addon1",
			secretOption: register.SecretOption{
				ClusterName: "cluster1",
				AddonRegistration: addonv1beta1.RegistrationConfig{
					Type: addonv1beta1.CustomSigner,
					CustomSigner: &addonv1beta1.CustomSignerConfig{
						Subject: addonv1beta1.Subject{
							BaseSubject: addonv1beta1.BaseSubject{
								User: "custom-addon",
							},
						},
					},
				},
			},
			regOption: &registertesting.TestAddonAuthConfig{
				KubeClientAuth: "csr",
				CSROption:      NewCSROption(),
			},
			expectErr: false,
			validateResult: func(t *testing.T, driver register.RegisterDriver) {
				if _, ok := driver.(*CSRDriver); !ok {
					t.Errorf("expected CSRDriver, got %T", driver)
				}
			},
		},
		{
			name: "csr auth - invalid CSR option",
			setupDriver: func() *CSRDriver {
				driver := &CSRDriver{
					csrControl:   ctrl,
					tokenControl: &registertesting.MockTokenControl{},
					addonClients: addonClients,
				}
				return driver
			},
			addonName: "addon1",
			secretOption: register.SecretOption{
				ClusterName: "cluster1",
				AddonRegistration: addonv1beta1.RegistrationConfig{
					Type: addonv1beta1.KubeClient,
				},
			},
			regOption: &registertesting.TestAddonAuthConfig{
				KubeClientAuth: "csr",
				CSROption:      nil,
			},
			expectErr:    true,
			expectErrMsg: "CSR configuration is nil",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			driver := tt.setupDriver()

			forkedDriver, err := driver.Fork(tt.addonName, tt.regOption, tt.secretOption)

			if tt.expectErr {
				if err == nil {
					t.Errorf("expected error but got nil")
					return
				}
				if tt.expectErrMsg != "" && !strings.Contains(err.Error(), tt.expectErrMsg) {
					t.Errorf("expected error message to contain %q, got %q", tt.expectErrMsg, err.Error())
				}
				return
			}

			if err != nil {
				t.Errorf("unexpected error: %v", err)
				return
			}

			if tt.validateResult != nil {
				tt.validateResult(t, forkedDriver)
			}
		})
	}
}

func TestCSRDriver_SetTokenControl(t *testing.T) {
	driver := &CSRDriver{}
	mockControl := &registertesting.MockTokenControl{}

	driver.SetTokenControl(mockControl)

	if driver.tokenControl != mockControl {
		t.Error("SetTokenControl did not set tokenControl correctly")
	}
}

func TestCSRDriver_SetAddonClients(t *testing.T) {
	driver := &CSRDriver{}

	addon := &addonv1beta1.ManagedClusterAddOn{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "addon1",
			Namespace: "cluster1",
		},
	}
	addonClient := addonfake.NewSimpleClientset(addon)
	addonInformerFactory := addoninformers.NewSharedInformerFactory(addonClient, 10*time.Minute)
	addonInformer := addonInformerFactory.Addon().V1beta1().ManagedClusterAddOns()

	addonClients := &register.AddOnClients{
		AddonClient:   addonClient,
		AddonInformer: addonInformer,
	}

	driver.SetAddonClients(addonClients)

	if driver.addonClients != addonClients {
		t.Error("SetAddonClients did not set addonClients correctly")
	}
}
