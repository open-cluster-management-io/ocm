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
	makeCSRRequest := func(t *testing.T, orgs []string) []byte {
		t.Helper()
		clientKey, err := keyutil.MakeEllipticPrivateKeyPEM()
		if err != nil {
			t.Fatalf("failed to make key: %v", err)
		}
		privateKey, err := keyutil.ParsePrivateKeyPEM(clientKey)
		if err != nil {
			t.Fatalf("failed to parse key: %v", err)
		}
		request, err := certutil.MakeCSR(privateKey, &pkix.Name{
			CommonName:   "system:open-cluster-management:cluster1:agent1",
			Organization: orgs,
		}, nil, nil)
		if err != nil {
			t.Fatalf("failed to make CSR: %v", err)
		}
		return request
	}

	cases := []struct {
		name             string
		csr              *certificatesv1.CertificateSigningRequest
		expectedUsername string
		expectedUID      string
		checkGroups      bool
		expectedGroups   []string
	}{
		{
			name: "username comes from annotation, not spec",
			csr: &certificatesv1.CertificateSigningRequest{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						operatorv1.CSRUsernameAnnotation: "system:open-cluster-management:cluster1:agent1",
					},
				},
				Spec: certificatesv1.CertificateSigningRequestSpec{
					Username: "system:serviceaccount:grpc-server:sa",
				},
			},
			expectedUsername: "system:open-cluster-management:cluster1:agent1",
		},
		{
			name: "uid is always empty regardless of spec",
			csr: &certificatesv1.CertificateSigningRequest{
				Spec: certificatesv1.CertificateSigningRequestSpec{
					UID: "grpc-server-sa-uid",
				},
			},
			expectedUID: "",
		},
		{
			name: "groups come from CSR request subject organization",
			csr: &certificatesv1.CertificateSigningRequest{
				Spec: certificatesv1.CertificateSigningRequestSpec{
					Groups:  []string{"system:serviceaccounts", "system:serviceaccounts:grpc-server"},
					Request: makeCSRRequest(t, []string{"system:open-cluster-management:cluster1"}),
				},
			},
			checkGroups:    true,
			expectedGroups: []string{"system:open-cluster-management:cluster1"},
		},
		{
			name: "groups are nil when no request body, ignoring spec groups from gRPC server SA",
			csr: &certificatesv1.CertificateSigningRequest{
				Spec: certificatesv1.CertificateSigningRequestSpec{
					Groups: []string{"system:serviceaccounts"},
				},
			},
			checkGroups:    true,
			expectedGroups: nil,
		},
		{
			name: "groups are nil when request body is malformed, ignoring spec groups from gRPC server SA",
			csr: &certificatesv1.CertificateSigningRequest{
				Spec: certificatesv1.CertificateSigningRequestSpec{
					Groups:  []string{"system:serviceaccounts"},
					Request: []byte("not-a-valid-pem-block"),
				},
			},
			checkGroups:    true,
			expectedGroups: nil,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			info := getCSRInfo(c.csr)
			if c.expectedUsername != "" && info.Username != c.expectedUsername {
				t.Errorf("expected username %q, got %q", c.expectedUsername, info.Username)
			}
			if info.UID != c.expectedUID {
				t.Errorf("expected uid %q, got %q", c.expectedUID, info.UID)
			}
			if c.checkGroups {
				if len(info.Groups) != len(c.expectedGroups) {
					t.Errorf("expected groups %v, got %v", c.expectedGroups, info.Groups)
				} else {
					for i, g := range c.expectedGroups {
						if info.Groups[i] != g {
							t.Errorf("expected group[%d] %q, got %q", i, g, info.Groups[i])
						}
					}
				}
			}
		})
	}
}
