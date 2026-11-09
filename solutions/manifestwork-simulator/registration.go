// Copyright Contributors to the Open Cluster Management project

package main

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"strconv"
	"time"

	certificatesv1 "k8s.io/api/certificates/v1"
	coordv1 "k8s.io/api/coordination/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"

	workclientset "open-cluster-management.io/api/client/work/clientset/versioned"
	clusterv1 "open-cluster-management.io/api/cluster/v1"
)

const (
	registrationAgentName       = "simulator-agent"
	managedClusterLeaseName     = "managed-cluster-lease"
	managedClusterLeaseDuration = int32(60)
)

func (a *App) registerCluster(ctx context.Context, name string, index int) error {
	// The real bootstrap flow creates the ManagedCluster with the bootstrap
	// identity; the hub then accepts it and provisions the cluster namespace
	// and per-cluster RBAC while the client certificate is still pending.
	a.setRegistrationStage(name, stageManagedCluster)
	managedCluster := &clusterv1.ManagedCluster{ObjectMeta: metav1.ObjectMeta{
		Name:   name,
		Labels: map[string]string{runIDLabel: a.runID, clusterMarkerLabel: "true", clusterIndexLabel: strconv.Itoa(index)},
	}, Spec: clusterv1.ManagedClusterSpec{LeaseDurationSeconds: managedClusterLeaseDuration}}
	if _, err := a.bootstrapManagedClusters.Create(ctx, managedCluster, metav1.CreateOptions{}); err != nil {
		if !apierrors.IsAlreadyExists(err) {
			return fmt.Errorf("register ManagedCluster %s with the bootstrap identity: %w", name, err)
		}
		existing, getErr := a.managedClusters.Get(ctx, name, metav1.GetOptions{})
		if getErr != nil || existing.Labels[runIDLabel] != a.runID || existing.Labels[clusterMarkerLabel] != "true" {
			return fmt.Errorf("refusing to reuse ManagedCluster %s not owned by simulator run %s", name, a.runID)
		}
	}
	a.managedClusterRegistrations.Add(1)

	a.setRegistrationStage(name, stageCSR)
	csr, key, err := newRegistrationCSR(a.runID, name, index)
	if err != nil {
		return err
	}
	csrName := csr.Name
	if _, err := a.bootstrapClient.CertificatesV1().CertificateSigningRequests().Create(ctx, csr, metav1.CreateOptions{}); err != nil {
		if !apierrors.IsAlreadyExists(err) {
			return fmt.Errorf("create auto-approved registration CSR for %s: %w", name, err)
		}
		existing, getErr := a.core.CertificatesV1().CertificateSigningRequests().Get(ctx, csrName, metav1.GetOptions{})
		if getErr != nil || existing.Labels[runIDLabel] != a.runID || existing.Labels[clusterv1.ClusterNameLabelKey] != name {
			return fmt.Errorf("refusing to reuse CSR %s not owned by simulator run %s", csrName, a.runID)
		}
	}
	a.registrationCSRs.Add(1)
	a.mu.Lock()
	if cluster := a.clusters[name]; cluster != nil {
		cluster.csrName = csrName
	}
	a.mu.Unlock()

	a.setRegistrationStage(name, stageCertificate)
	certPEM, err := a.waitForIssuedCertificate(ctx, csrName)
	if err != nil {
		return err
	}
	keyPEM, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		return fmt.Errorf("encode registration key for %s: %w", name, err)
	}
	agentConfig := rest.CopyConfig(a.ocmRESTConfig)
	agentConfig.BearerToken, agentConfig.BearerTokenFile = "", ""
	agentConfig.Username, agentConfig.Password = "", ""
	agentConfig.TLSClientConfig.CertFile, agentConfig.TLSClientConfig.KeyFile = "", ""
	agentConfig.TLSClientConfig.CertData = certPEM
	agentConfig.TLSClientConfig.KeyData = pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyPEM})
	agentConfig.ExecProvider, agentConfig.AuthProvider = nil, nil
	agentConfig.Impersonate = rest.ImpersonationConfig{}
	agentCore, err := kubernetes.NewForConfig(agentConfig)
	if err != nil {
		return fmt.Errorf("create simulated agent client for %s: %w", name, err)
	}
	agentWorkSet, err := workclientset.NewForConfig(agentConfig)
	if err != nil {
		return fmt.Errorf("create simulated agent ManifestWork client for %s: %w", name, err)
	}
	agentWork := agentWorkSet.WorkV1()
	a.setRegistrationStage(name, stageHubAcceptance)
	if err := a.waitForManagedClusterAcceptance(ctx, name); err != nil {
		return err
	}
	a.setRegistrationStage(name, stageNamespace)
	if err := a.markNamespaceOwned(ctx, name, index); err != nil {
		return err
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	a.mu.Lock()
	cluster := a.clusters[name]
	registered := false
	if cluster != nil && !cluster.terminating {
		cluster.agentWorkClient = agentWork
		cluster.registered = true
		cluster.registrationStage = ""
		registered = true
	}
	a.mu.Unlock()
	if !registered {
		return nil
	}
	go a.agentLoop(ctx, name)
	go a.leaseLoop(ctx, name, agentCore)
	return nil
}

func (a *App) markNamespaceOwned(ctx context.Context, name string, index int) error {
	for {
		namespace, err := a.core.CoreV1().Namespaces().Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			if !apierrors.IsNotFound(err) {
				return fmt.Errorf("get accepted simulator cluster namespace %s: %w", name, err)
			}
		} else {
			if namespace.Labels[clusterv1.ClusterNameLabelKey] != name {
				return fmt.Errorf("refusing to mark namespace %s: OCM cluster-name label does not match", name)
			}
			if namespace.Labels == nil {
				namespace.Labels = map[string]string{}
			}
			namespace.Labels[runIDLabel] = a.runID
			namespace.Labels[clusterMarkerLabel] = "true"
			namespace.Labels[clusterIndexLabel] = strconv.Itoa(index)
			if _, err := a.core.CoreV1().Namespaces().Update(ctx, namespace, metav1.UpdateOptions{}); err == nil {
				return nil
			} else if !apierrors.IsConflict(err) {
				return fmt.Errorf("mark OCM cluster namespace %s as simulator-owned: %w", name, err)
			}
		}
		if !waitContext(ctx, 200*time.Millisecond) {
			return fmt.Errorf("wait for namespace %s after hub accepted ManagedCluster: %w", name, ctx.Err())
		}
	}
}

func newRegistrationCSR(runID, clusterName string, index int) (*certificatesv1.CertificateSigningRequest, *ecdsa.PrivateKey, error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, nil, fmt.Errorf("generate registration key for %s: %w", clusterName, err)
	}
	requestDER, err := x509.CreateCertificateRequest(rand.Reader, &x509.CertificateRequest{Subject: pkix.Name{
		CommonName:   "system:open-cluster-management:" + clusterName + ":" + registrationAgentName,
		Organization: []string{"system:open-cluster-management:" + clusterName, "system:open-cluster-management:managed-clusters"},
	}}, key)
	if err != nil {
		return nil, nil, fmt.Errorf("create registration CSR for %s: %w", clusterName, err)
	}
	csr := &certificatesv1.CertificateSigningRequest{ObjectMeta: metav1.ObjectMeta{
		Name: fmt.Sprintf("simcsr-%s-c%04d", runID, index),
		Labels: map[string]string{
			runIDLabel: runID, clusterMarkerLabel: "true", clusterIndexLabel: strconv.Itoa(index),
			clusterv1.ClusterNameLabelKey: clusterName,
		},
	}, Spec: certificatesv1.CertificateSigningRequestSpec{
		Request:    pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE REQUEST", Bytes: requestDER}),
		SignerName: certificatesv1.KubeAPIServerClientSignerName,
		Usages:     []certificatesv1.KeyUsage{certificatesv1.UsageDigitalSignature, certificatesv1.UsageKeyEncipherment, certificatesv1.UsageClientAuth},
	}}
	return csr, key, nil
}

func (a *App) waitForIssuedCertificate(ctx context.Context, csrName string) ([]byte, error) {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	approvedCounted := false
	for {
		csr, err := a.core.CertificatesV1().CertificateSigningRequests().Get(ctx, csrName, metav1.GetOptions{})
		if err != nil {
			return nil, fmt.Errorf("read registration CSR %s: %w", csrName, err)
		}
		for _, condition := range csr.Status.Conditions {
			if condition.Type == certificatesv1.CertificateDenied || condition.Type == certificatesv1.CertificateFailed {
				return nil, fmt.Errorf("registration CSR %s was not approved/signed: %s: %s", csrName, condition.Reason, condition.Message)
			}
			if condition.Type == certificatesv1.CertificateApproved && condition.Status == corev1.ConditionTrue && !approvedCounted {
				a.registrationCSRsApproved.Add(1)
				approvedCounted = true
			}
		}
		if len(csr.Status.Certificate) != 0 {
			return csr.Status.Certificate, nil
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-ticker.C:
		}
	}
}

func (a *App) waitForManagedClusterAcceptance(ctx context.Context, name string) error {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	for {
		cluster, err := a.managedClusters.Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			return fmt.Errorf("wait for hub acceptance of ManagedCluster %s: %w", name, err)
		}
		if meta.IsStatusConditionTrue(cluster.Status.Conditions, clusterv1.ManagedClusterConditionHubAccepted) {
			return nil
		}
		if meta.IsStatusConditionTrue(cluster.Status.Conditions, clusterv1.ManagedClusterConditionHubDenied) {
			return fmt.Errorf("hub denied simulator ManagedCluster %s", name)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

func (a *App) leaseLoop(ctx context.Context, clusterName string, client kubernetes.Interface) {
	interval := time.Duration(managedClusterLeaseDuration) * time.Second
	backoff := 5 * time.Second
	for ctx.Err() == nil {
		if err := a.writeLease(ctx, clusterName, client); err != nil {
			if ctx.Err() != nil {
				return
			}
			a.reportError(fmt.Errorf("simulated registration lease for %s: %w", clusterName, err))
			if !waitContext(ctx, backoff) {
				return
			}
			if backoff < 30*time.Second {
				backoff *= 2
			}
			continue
		}
		backoff = 5 * time.Second
		if !waitContext(ctx, interval) {
			return
		}
	}
}

func (a *App) writeLease(ctx context.Context, clusterName string, client kubernetes.Interface) error {
	leases := client.CoordinationV1().Leases(clusterName)
	now := metav1.NewMicroTime(time.Now())
	duration := managedClusterLeaseDuration
	holder := clusterName
	lease, err := leases.Get(ctx, managedClusterLeaseName, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		_, err = leases.Create(ctx, &coordv1.Lease{ObjectMeta: metav1.ObjectMeta{
			Name: managedClusterLeaseName, Namespace: clusterName,
			Labels: map[string]string{runIDLabel: a.runID, clusterMarkerLabel: "true"},
		}, Spec: coordv1.LeaseSpec{HolderIdentity: &holder, LeaseDurationSeconds: &duration, RenewTime: &now}}, metav1.CreateOptions{})
		if err == nil {
			a.leaseWrites.Add(1)
			return nil
		}
		return err
	}
	if err != nil {
		return err
	}
	lease.Spec.HolderIdentity = &holder
	lease.Spec.LeaseDurationSeconds = &duration
	lease.Spec.RenewTime = &now
	if _, err := leases.Update(ctx, lease, metav1.UpdateOptions{}); err != nil {
		return err
	}
	a.leaseWrites.Add(1)
	return nil
}

func (a *App) cleanupRegistration(ctx context.Context, name string) error {
	a.mu.RLock()
	runtime := a.clusters[name]
	var registrationDone <-chan struct{}
	if runtime != nil {
		registrationDone = runtime.registrationDone
	}
	a.mu.RUnlock()
	if registrationDone != nil {
		select {
		case <-registrationDone:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	cluster, err := a.managedClusters.Get(ctx, name, metav1.GetOptions{})
	if err == nil {
		if cluster.Labels[runIDLabel] != a.runID || cluster.Labels[clusterMarkerLabel] != "true" {
			return fmt.Errorf("refusing to delete ManagedCluster %s not owned by simulator run %s", name, a.runID)
		}
		if err := a.managedClusters.Delete(ctx, name, deleteByUID(cluster.UID)); err != nil && !apierrors.IsNotFound(err) {
			return fmt.Errorf("delete simulator ManagedCluster %s: %w", name, err)
		}
		for {
			_, err = a.managedClusters.Get(ctx, name, metav1.GetOptions{})
			if apierrors.IsNotFound(err) {
				break
			}
			if err != nil {
				return fmt.Errorf("wait for simulator ManagedCluster %s deletion: %w", name, err)
			}
			if !waitContext(ctx, 200*time.Millisecond) {
				return ctx.Err()
			}
		}
	} else if !apierrors.IsNotFound(err) {
		return fmt.Errorf("get simulator ManagedCluster %s: %w", name, err)
	}
	selector := runIDLabel + "=" + a.runID + "," + clusterMarkerLabel + "=true," + clusterv1.ClusterNameLabelKey + "=" + name
	csrs, err := a.core.CertificatesV1().CertificateSigningRequests().List(ctx, metav1.ListOptions{LabelSelector: selector})
	if err != nil {
		return fmt.Errorf("list simulator registration CSRs for %s: %w", name, err)
	}
	for _, csr := range csrs.Items {
		if csr.Labels[runIDLabel] != a.runID || csr.Labels[clusterv1.ClusterNameLabelKey] != name {
			continue
		}
		if err := a.core.CertificatesV1().CertificateSigningRequests().Delete(ctx, csr.Name, deleteByUID(csr.UID)); err != nil && !apierrors.IsNotFound(err) {
			return fmt.Errorf("delete simulator registration CSR %s: %w", csr.Name, err)
		}
	}
	return nil
}

func (a *App) cleanupOrphanRegistrations(ctx context.Context, ownedNamespaces []corev1.Namespace) error {
	existing := make(map[string]struct{}, len(ownedNamespaces))
	for _, namespace := range ownedNamespaces {
		if namespace.Labels[runIDLabel] == a.runID && namespace.Labels[clusterMarkerLabel] == "true" {
			existing[namespace.Name] = struct{}{}
		}
	}
	clusters, err := a.managedClusters.List(ctx, metav1.ListOptions{LabelSelector: runIDLabel + "=" + a.runID + "," + clusterMarkerLabel + "=true"})
	if err != nil {
		return fmt.Errorf("list simulator ManagedClusters during cleanup: %w", err)
	}
	names := make(map[string]struct{}, len(clusters.Items))
	for _, cluster := range clusters.Items {
		if cluster.Labels[runIDLabel] == a.runID && cluster.Labels[clusterMarkerLabel] == "true" {
			names[cluster.Name] = struct{}{}
		}
	}
	csrs, err := a.core.CertificatesV1().CertificateSigningRequests().List(ctx, metav1.ListOptions{LabelSelector: runIDLabel + "=" + a.runID + "," + clusterMarkerLabel + "=true"})
	if err != nil {
		return fmt.Errorf("list simulator registration CSRs during cleanup: %w", err)
	}
	for _, csr := range csrs.Items {
		if csr.Labels[runIDLabel] == a.runID && csr.Labels[clusterMarkerLabel] == "true" {
			if name := csr.Labels[clusterv1.ClusterNameLabelKey]; name != "" {
				names[name] = struct{}{}
			}
		}
	}
	for name := range names {
		if _, found := existing[name]; found {
			continue
		}
		if err := a.cleanupRegistration(ctx, name); err != nil {
			return fmt.Errorf("clean orphaned simulator registration %s: %w", name, err)
		}
	}
	return nil
}
