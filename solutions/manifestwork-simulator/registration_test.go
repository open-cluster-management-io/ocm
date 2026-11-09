// Copyright Contributors to the Open Cluster Management project

package main

import (
	"context"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"testing"
	"time"

	certificatesv1 "k8s.io/api/certificates/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	clusterv1 "open-cluster-management.io/api/cluster/v1"
)

func TestRegistrationCSRUsesOCMManagedClusterIdentity(t *testing.T) {
	const clusterName = "sim-test-c0001"
	csr, _, err := newRegistrationCSR("test", clusterName, 1)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := csr.Spec.SignerName, certificatesv1.KubeAPIServerClientSignerName; got != want {
		t.Fatalf("signer = %q, want %q", got, want)
	}
	if csr.Labels[runIDLabel] != "test" || csr.Labels[clusterv1.ClusterNameLabelKey] != clusterName {
		t.Fatalf("CSR ownership/cluster labels are wrong: %#v", csr.Labels)
	}
	block, _ := pem.Decode(csr.Spec.Request)
	if block == nil || block.Type != "CERTIFICATE REQUEST" {
		t.Fatal("CSR request is not PEM-encoded")
	}
	request, err := x509.ParseCertificateRequest(block.Bytes)
	if err != nil {
		t.Fatalf("parse CSR request: %v", err)
	}
	if err := request.CheckSignature(); err != nil {
		t.Fatalf("verify CSR signature: %v", err)
	}
	if got, want := request.Subject.CommonName, "system:open-cluster-management:"+clusterName+":"+registrationAgentName; got != want {
		t.Fatalf("CSR common name = %q, want %q", got, want)
	}
	wantOrganizations := map[string]bool{
		"system:open-cluster-management:" + clusterName:   true,
		"system:open-cluster-management:managed-clusters": true,
	}
	for _, organization := range request.Subject.Organization {
		delete(wantOrganizations, organization)
	}
	if len(wantOrganizations) != 0 {
		t.Fatalf("CSR organizations missing expected OCM values: %#v", wantOrganizations)
	}
}

func TestPendingRegistrationCSRIsNotManuallyApproved(t *testing.T) {
	app, kube, _ := newTestApp(t, true)
	csr := &certificatesv1.CertificateSigningRequest{ObjectMeta: metav1.ObjectMeta{Name: "pending-csr"}, Spec: certificatesv1.CertificateSigningRequestSpec{
		SignerName: certificatesv1.KubeAPIServerClientSignerName,
	}}
	if _, err := kube.CertificatesV1().CertificateSigningRequests().Create(context.Background(), csr, metav1.CreateOptions{}); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	if _, err := app.waitForIssuedCertificate(ctx, csr.Name); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("pending CSR wait error = %v, want deadline exceeded", err)
	}
	if got := app.registrationCSRsApproved.Load(); got != 0 {
		t.Fatalf("simulator counted %d approvals for a pending CSR", got)
	}
	for _, action := range kube.Actions() {
		if action.GetVerb() == "update" && action.GetSubresource() == "approval" {
			t.Fatal("simulator must not approve its own CSR")
		}
	}
}
