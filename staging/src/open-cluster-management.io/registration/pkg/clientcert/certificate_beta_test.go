package clientcert

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	certificates "k8s.io/api/certificates/v1beta1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/listers/certificates/v1beta1"
	"k8s.io/client-go/tools/cache"
)

func TestV1beta1CSRControlApprovedAndIssued(t *testing.T) {
	cases := []struct {
		name         string
		csrName      string
		existingObjs []runtime.Object
		isApproved   bool
		isIssued     bool
		createdCSR   *certificates.CertificateSigningRequest
		expectedErr  error
	}{
		{
			name:         "approved but not issued",
			csrName:      "foo",
			existingObjs: []runtime.Object{newV1beta1CSR("foo", true, nil)},
			isApproved:   true,
			isIssued:     false,
		},
		{
			name:         "approved and issued",
			csrName:      "foo",
			existingObjs: []runtime.Object{newV1beta1CSR("foo", true, []byte(`test`))},
			isApproved:   true,
			isIssued:     true,
		},
		{
			name:         "not approved and not issued",
			csrName:      "foo",
			existingObjs: []runtime.Object{newV1beta1CSR("foo", false, nil)},
			isApproved:   false,
			isIssued:     false,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			indexer := cache.NewIndexer(
				cache.MetaNamespaceKeyFunc,
				cache.Indexers{
					cache.NamespaceIndex: cache.MetaNamespaceIndexFunc,
				})
			for _, obj := range c.existingObjs {
				require.NoError(t, indexer.Add(obj))
			}
			lister := v1beta1.NewCertificateSigningRequestLister(indexer)
			client := fake.NewSimpleClientset(c.existingObjs...)
			ctrl := v1beta1CSRControl{
				hubCSRLister: lister,
				hubCSRClient: client.CertificatesV1beta1().CertificateSigningRequests(),
			}

			actualApproved, err := ctrl.isApproved(c.csrName)
			assert.NoError(t, err)
			assert.Equal(t, c.isApproved, actualApproved)

			issuedCertData, err := ctrl.getIssuedCertificate(c.csrName)
			assert.NoError(t, err)
			assert.Equal(t, c.isIssued, len(issuedCertData) > 0)
		})
	}

}

func newV1beta1CSR(name string, isApproved bool, certData []byte) *certificates.CertificateSigningRequest {
	csr := &certificates.CertificateSigningRequest{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
		Status: certificates.CertificateSigningRequestStatus{
			Certificate: certData,
		},
	}
	if isApproved {
		csr.Status.Conditions = []certificates.CertificateSigningRequestCondition{
			{
				Type:   certificates.CertificateApproved,
				Status: corev1.ConditionTrue,
			},
		}
	}
	return csr
}
