package grpc

import (
	"context"
	"crypto/x509/pkix"
	"net"
	"testing"
	"time"

	certificatesv1 "k8s.io/api/certificates/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/informers"
	kubefake "k8s.io/client-go/kubernetes/fake"
	clienttesting "k8s.io/client-go/testing"
	certutil "k8s.io/client-go/util/cert"
	"k8s.io/client-go/util/keyutil"

	operatorv1 "open-cluster-management.io/api/operator/v1"

	testingcommon "open-cluster-management.io/ocm/pkg/common/testing"
)

func TestSignCSR(t *testing.T) {
	cases := []struct {
		name            string
		csrs            []runtime.Object
		validateActions func(t *testing.T, actions []clienttesting.Action)
	}{
		{
			name: "no csr",
			csrs: []runtime.Object{},
			validateActions: func(t *testing.T, actions []clienttesting.Action) {
				testingcommon.AssertNoActions(t, actions)
			},
		},
		{
			name: "unapproved csr",
			csrs: []runtime.Object{
				&certificatesv1.CertificateSigningRequest{
					ObjectMeta: metav1.ObjectMeta{Name: "test_csr"},
				},
			},
			validateActions: func(t *testing.T, actions []clienttesting.Action) {
				testingcommon.AssertNoActions(t, actions)
			},
		},
		{
			name: "approved csr with cert",
			csrs: []runtime.Object{
				&certificatesv1.CertificateSigningRequest{
					ObjectMeta: metav1.ObjectMeta{Name: "test_csr"},
					Status: certificatesv1.CertificateSigningRequestStatus{
						Conditions: []certificatesv1.CertificateSigningRequestCondition{
							{
								Type: certificatesv1.CertificateApproved,
							},
						},
						Certificate: []byte("cert"),
					},
				},
			},
			validateActions: func(t *testing.T, actions []clienttesting.Action) {
				testingcommon.AssertNoActions(t, actions)
			},
		},
		{
			name: "wrong signer",
			csrs: []runtime.Object{
				&certificatesv1.CertificateSigningRequest{
					ObjectMeta: metav1.ObjectMeta{Name: "test_csr"},
					Spec: certificatesv1.CertificateSigningRequestSpec{
						SignerName: "wrong-signer",
					},
					Status: certificatesv1.CertificateSigningRequestStatus{
						Conditions: []certificatesv1.CertificateSigningRequestCondition{
							{
								Type: certificatesv1.CertificateApproved,
							},
						},
					},
				},
			},
			validateActions: func(t *testing.T, actions []clienttesting.Action) {
				testingcommon.AssertNoActions(t, actions)
			},
		},
		{
			name: "approved csr without cert",
			csrs: []runtime.Object{
				func() *certificatesv1.CertificateSigningRequest {
					clientKey, _ := keyutil.MakeEllipticPrivateKeyPEM()
					privateKey, _ := keyutil.ParsePrivateKeyPEM(clientKey)

					request, _ := certutil.MakeCSR(privateKey, &pkix.Name{CommonName: "test", Organization: []string{"test"}}, []string{"test.localhost"}, nil)

					return &certificatesv1.CertificateSigningRequest{
						ObjectMeta: metav1.ObjectMeta{
							Name: "test_csr",
						},
						Spec: certificatesv1.CertificateSigningRequestSpec{
							Usages: []certificatesv1.KeyUsage{
								certificatesv1.UsageClientAuth,
							},
							SignerName: operatorv1.GRPCAuthSigner,
							Username:   "system:open-cluster-management:test",
							Request:    request,
						},
						Status: certificatesv1.CertificateSigningRequestStatus{
							Conditions: []certificatesv1.CertificateSigningRequestCondition{
								{
									Type: certificatesv1.CertificateApproved,
								},
							},
						},
					}
				}(),
			},
			validateActions: func(t *testing.T, actions []clienttesting.Action) {
				testingcommon.AssertActions(t, actions, "update")
			},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			ca, key, err := certutil.GenerateSelfSignedCertKey("test", []net.IP{}, []string{})
			if err != nil {
				t.Fatalf("Failed to generate self signed CA config: %v", err)
			}

			csrClient := kubefake.NewSimpleClientset(c.csrs...)
			csrInformers := informers.NewSharedInformerFactory(csrClient, 10*time.Minute)
			csrInformer := csrInformers.Certificates().V1().CertificateSigningRequests()
			for _, csr := range c.csrs {
				csrInformer.Informer().GetStore().Add(csr)
			}

			ctrl := &csrSignController{
				kubeClient: csrClient,
				csrLister:  csrInformer.Lister(),
				caData:     ca,
				caKey:      key,
				duration:   1 * time.Hour,
			}

			if err := ctrl.sync(context.Background(), testingcommon.NewFakeSyncContext(t, "test_csr"), "test_csr"); err != nil {
				t.Errorf("unexpected error: %v", err)
			}

			c.validateActions(t, csrClient.Actions())
		})
	}
}

func TestEventFilter(t *testing.T) {
	tests := []struct {
		name     string
		input    any
		expected bool
	}{
		{
			name:     "nil input",
			input:    nil,
			expected: false,
		},
		{
			name: "v1 CSR with matching signer",
			input: &certificatesv1.CertificateSigningRequest{
				Spec: certificatesv1.CertificateSigningRequestSpec{
					SignerName: operatorv1.GRPCAuthSigner,
				},
			},
			expected: true,
		},
		{
			name: "v1 CSR with non-matching signer",
			input: &certificatesv1.CertificateSigningRequest{
				Spec: certificatesv1.CertificateSigningRequestSpec{
					SignerName: "example.com/custom",
				},
			},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := eventFilter(tt.input); got != tt.expected {
				t.Errorf("eventFilter() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestGetCSRInfo(t *testing.T) {
	csr := &certificatesv1.CertificateSigningRequest{
		ObjectMeta: metav1.ObjectMeta{
			Annotations: map[string]string{
				operatorv1.CSRUsernameAnnotation: "test",
			},
		},
		Spec: certificatesv1.CertificateSigningRequestSpec{
			SignerName: operatorv1.GRPCAuthSigner,
		},
	}

	info := getCSRInfo(csr)
	if info.Username != "test" {
		t.Errorf("unexpected username %s", info.Username)
	}
}
