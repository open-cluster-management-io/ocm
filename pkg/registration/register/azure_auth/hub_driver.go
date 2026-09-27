package azure_auth

import (
	"context"
	"fmt"
	"regexp"

	rbacv1 "k8s.io/api/rbac/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/validation"
	"k8s.io/client-go/kubernetes"
	"k8s.io/klog/v2"

	clusterv1 "open-cluster-management.io/api/cluster/v1"
	operatorv1 "open-cluster-management.io/api/operator/v1"

	"open-cluster-management.io/ocm/pkg/registration/register"
)

// These names intentionally reuse the ClusterRoles the CSR-based registration flow
// already renders for every accepted managed cluster (see
// pkg/registration/hub/manifests/rbac/managedcluster-clusterrole.yaml,
// managedcluster-registration-clusterrole.yaml and managedcluster-work-clusterrole.yaml,
// applied by pkg/registration/hub/managedcluster.Controller for every accepted
// ManagedCluster regardless of driver). AzureAuthHubDriver does not create or manage
// those ClusterRoles; it only binds the Azure AD identity to them via separate
// Role/ClusterRoleBindings, so the effective permissions granted to a cluster joining
// via azure-auth are identical to those granted to one joining via CSR.
//
// The CSR flow gets these same permissions by presenting a client cert whose
// Organization (system:open-cluster-management:<name>) kube-apiserver maps to an RBAC
// Group - the common controller's own RoleBindings for the registration and work
// ClusterRoles target that Group directly. Azure AD tokens carry no such group claim
// mapped to an OCM-specific group, so azure-auth binds the identity's object ID as a
// User to each ClusterRole individually instead.
const (
	managedClusterRoleNameFmt      = "open-cluster-management:managedcluster:%s"
	managedClusterRegistrationRole = "open-cluster-management:managedcluster:registration"
	managedClusterWorkRole         = "open-cluster-management:managedcluster:work"
	clusterRoleBindingNameFmt      = "open-cluster-management:managedcluster:%s:azure"
	registrationRoleBindingNameFmt = "open-cluster-management:managedcluster:%s:registration:azure"
	workRoleBindingNameFmt         = "open-cluster-management:managedcluster:%s:work:azure"
)

// AzureIdentityLabelKey labels every binding CreatePermissions creates with the Azure AD
// object ID it grants access to, alongside the cluster-name label, so CreatePermissions
// can detect one identity being claimed by more than one managed cluster.
const AzureIdentityLabelKey = "open-cluster-management.io/azure-identity"

// AzureAuthHubDriver is the hub-side register.HubDriver counterpart to
// AzureAuthDriver. It does not participate in CSR approval - there is no CSR - it
// instead grants the managed cluster's Azure AD identity (recorded via the
// ManagedClusterAzureIdentityAnnotation annotation by AzureAuthDriver.ManagedClusterDecorator)
// the same RBAC permissions the CSR flow grants its issued certificate's
// CommonName/Organization, by creating dedicated Role/ClusterRoleBindings.
type AzureAuthHubDriver struct {
	kubeClient             kubernetes.Interface
	autoApprovedIDPatterns []*regexp.Regexp
	// oidcIssuerURL is empty for AKS's native Azure AD integration, where the apiserver
	// uses the bare object ID as the username. When the hub apiserver instead trusts
	// Azure AD as a generic OIDC issuer (with --oidc-username-claim=oid and no
	// --oidc-username-prefix), it is that apiserver's exact --oidc-issuer-url.
	oidcIssuerURL string
}

// username returns the Kubernetes username the hub apiserver derives for azureID.
func (a *AzureAuthHubDriver) username(azureID string) string {
	if a.oidcIssuerURL == "" {
		return azureID
	}
	// kube-apiserver's default prefix for a username claim other than "email".
	return a.oidcIssuerURL + "#" + azureID
}

func (a *AzureAuthHubDriver) allows(cluster *clusterv1.ManagedCluster) bool {
	_, ok := cluster.Annotations[operatorv1.ClusterAnnotationsKeyPrefix+"/"+ManagedClusterAzureIdentityAnnotation]
	return ok
}

// Accept auto-accepts a managed cluster using azure-auth if its recorded Azure AD
// object ID matches one of the configured auto-approval patterns. Clusters not using
// azure-auth (no annotation present), and all clusters when no patterns are
// configured, are left for other drivers/an admin to accept - Accept returns true in
// that case only to avoid vetoing them, mirroring awsirsa.AWSIRSAHubDriver.Accept.
func (a *AzureAuthHubDriver) Accept(cluster *clusterv1.ManagedCluster) bool {
	if a.autoApprovedIDPatterns == nil {
		return true
	}
	if !a.allows(cluster) {
		return true
	}

	azureID := cluster.Annotations[operatorv1.ClusterAnnotationsKeyPrefix+"/"+ManagedClusterAzureIdentityAnnotation]
	for _, p := range a.autoApprovedIDPatterns {
		if p.FindString(azureID) == azureID && len(azureID) > 0 {
			return true
		}
	}
	return false
}

// CreatePermissions is run when hubAcceptsClient is set to true on the ManagedCluster.
// It binds the cluster's Azure AD identity, as a Kubernetes "User" subject, to the
// same ClusterRoles the CSR flow's own bindings use.
//
// This assumes the hub apiserver is already configured to authenticate the presented
// Azure AD token - either via AKS's native Azure AD integration (username is the bare
// oid), or a hub apiserver trusting Azure AD as a generic OIDC issuer (username is
// "<issuer>#<oid>", see username). AzureAuthHubDriver does not configure that trust
// itself; it only manages the Kubernetes-side RBAC bindings.
//
// Before binding, it refuses to proceed if the same object ID is already bound to a
// different managed cluster, since both clusters' bindings would then target the same
// Kubernetes user and that identity would hold the union of their permissions. The
// check runs on every acceptance path, manual or automatic, because CreatePermissions
// runs regardless of how the cluster was accepted. It is check-then-act, not atomic.
func (a *AzureAuthHubDriver) CreatePermissions(ctx context.Context, cluster *clusterv1.ManagedCluster) error {
	logger := klog.FromContext(ctx)
	if !a.allows(cluster) {
		return nil
	}
	azureID := cluster.Annotations[operatorv1.ClusterAnnotationsKeyPrefix+"/"+ManagedClusterAzureIdentityAnnotation]
	if azureID == "" {
		return fmt.Errorf("managedcluster %q has an empty azure identity annotation", cluster.Name)
	}
	if errs := validation.IsValidLabelValue(azureID); len(errs) > 0 {
		return fmt.Errorf("managedcluster %q has an invalid azure identity annotation %q: %v", cluster.Name, azureID, errs)
	}
	logger.V(4).Info("ManagedCluster is joined using azure-auth registration-auth", "ManagedCluster", cluster.Name, "azureID", azureID)

	if err := a.checkIdentityNotClaimed(ctx, cluster.Name, azureID); err != nil {
		return err
	}

	labels := map[string]string{
		clusterv1.ClusterNameLabelKey: cluster.Name,
		AzureIdentityLabelKey:         azureID,
	}
	subjects := []rbacv1.Subject{{
		Kind:     rbacv1.UserKind,
		APIGroup: rbacv1.GroupName,
		Name:     a.username(azureID),
	}}

	if err := a.applyClusterRoleBinding(ctx, &rbacv1.ClusterRoleBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name:   fmt.Sprintf(clusterRoleBindingNameFmt, cluster.Name),
			Labels: labels,
		},
		RoleRef: rbacv1.RoleRef{
			APIGroup: rbacv1.GroupName,
			Kind:     "ClusterRole",
			Name:     fmt.Sprintf(managedClusterRoleNameFmt, cluster.Name),
		},
		Subjects: subjects,
	}); err != nil {
		return err
	}

	if err := a.applyRoleBinding(ctx, cluster.Name, &rbacv1.RoleBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf(registrationRoleBindingNameFmt, cluster.Name),
			Namespace: cluster.Name,
			Labels:    labels,
		},
		RoleRef: rbacv1.RoleRef{
			APIGroup: rbacv1.GroupName,
			Kind:     "ClusterRole",
			Name:     managedClusterRegistrationRole,
		},
		Subjects: subjects,
	}); err != nil {
		return err
	}

	return a.applyRoleBinding(ctx, cluster.Name, &rbacv1.RoleBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf(workRoleBindingNameFmt, cluster.Name),
			Namespace: cluster.Name,
			Labels:    labels,
		},
		RoleRef: rbacv1.RoleRef{
			APIGroup: rbacv1.GroupName,
			Kind:     "ClusterRole",
			Name:     managedClusterWorkRole,
		},
		Subjects: subjects,
	})
}

// Cleanup is run when hubAcceptClient is set false or the cluster is deleting. It
// removes the Role/ClusterRoleBindings created by CreatePermissions. The shared
// ClusterRoles themselves are owned and cleaned up by the common managed cluster
// controller, not by this driver.
func (a *AzureAuthHubDriver) Cleanup(ctx context.Context, cluster *clusterv1.ManagedCluster) error {
	if !a.allows(cluster) {
		return nil
	}

	logger := klog.FromContext(ctx)
	crbName := fmt.Sprintf(clusterRoleBindingNameFmt, cluster.Name)
	if err := a.kubeClient.RbacV1().ClusterRoleBindings().Delete(ctx, crbName, metav1.DeleteOptions{}); err != nil && !apierrors.IsNotFound(err) {
		logger.V(4).Error(err, "Failed to delete ClusterRoleBinding", "ClusterRoleBinding", crbName)
		return err
	}

	rbName := fmt.Sprintf(registrationRoleBindingNameFmt, cluster.Name)
	if err := a.kubeClient.RbacV1().RoleBindings(cluster.Name).Delete(ctx, rbName, metav1.DeleteOptions{}); err != nil && !apierrors.IsNotFound(err) {
		logger.V(4).Error(err, "Failed to delete RoleBinding", "RoleBinding", rbName, "Namespace", cluster.Name)
		return err
	}

	workRbName := fmt.Sprintf(workRoleBindingNameFmt, cluster.Name)
	if err := a.kubeClient.RbacV1().RoleBindings(cluster.Name).Delete(ctx, workRbName, metav1.DeleteOptions{}); err != nil && !apierrors.IsNotFound(err) {
		logger.V(4).Error(err, "Failed to delete RoleBinding", "RoleBinding", workRbName, "Namespace", cluster.Name)
		return err
	}

	return nil
}

// checkIdentityNotClaimed returns an error if azureID already has bindings created for
// a managed cluster other than clusterName. Every CreatePermissions call creates the
// cluster-scoped ClusterRoleBinding first, so listing ClusterRoleBindings alone covers
// every cluster the identity was ever bound to.
func (a *AzureAuthHubDriver) checkIdentityNotClaimed(ctx context.Context, clusterName, azureID string) error {
	crbs, err := a.kubeClient.RbacV1().ClusterRoleBindings().List(ctx, metav1.ListOptions{
		LabelSelector: fmt.Sprintf("%s=%s", AzureIdentityLabelKey, azureID),
	})
	if err != nil {
		return err
	}
	for _, crb := range crbs.Items {
		if owner := crb.Labels[clusterv1.ClusterNameLabelKey]; owner != clusterName {
			return fmt.Errorf("azure identity %q of managedcluster %q is already bound to managedcluster %q; "+
				"each managed cluster must use a distinct Azure AD identity", azureID, clusterName, owner)
		}
	}
	return nil
}

// Run is a no-op: unlike the CSR driver, azure-auth has no pending-request queue on
// the hub to reconcile. It exists only to satisfy the register.HubDriver interface.
func (a *AzureAuthHubDriver) Run(_ context.Context, _ int) {}

func (a *AzureAuthHubDriver) applyClusterRoleBinding(ctx context.Context, crb *rbacv1.ClusterRoleBinding) error {
	_, err := a.kubeClient.RbacV1().ClusterRoleBindings().Create(ctx, crb, metav1.CreateOptions{})
	if apierrors.IsAlreadyExists(err) {
		existing, getErr := a.kubeClient.RbacV1().ClusterRoleBindings().Get(ctx, crb.Name, metav1.GetOptions{})
		if getErr != nil {
			return getErr
		}
		existing.RoleRef = crb.RoleRef
		existing.Subjects = crb.Subjects
		existing.Labels = crb.Labels
		_, err = a.kubeClient.RbacV1().ClusterRoleBindings().Update(ctx, existing, metav1.UpdateOptions{})
	}
	return err
}

func (a *AzureAuthHubDriver) applyRoleBinding(ctx context.Context, namespace string, rb *rbacv1.RoleBinding) error {
	_, err := a.kubeClient.RbacV1().RoleBindings(namespace).Create(ctx, rb, metav1.CreateOptions{})
	if apierrors.IsAlreadyExists(err) {
		existing, getErr := a.kubeClient.RbacV1().RoleBindings(namespace).Get(ctx, rb.Name, metav1.GetOptions{})
		if getErr != nil {
			return getErr
		}
		existing.RoleRef = rb.RoleRef
		existing.Subjects = rb.Subjects
		existing.Labels = rb.Labels
		_, err = a.kubeClient.RbacV1().RoleBindings(namespace).Update(ctx, existing, metav1.UpdateOptions{})
	}
	return err
}

func NewAzureAuthHubDriver(kubeClient kubernetes.Interface, autoApprovedIdentityPatterns []string, oidcIssuerURL string) (register.HubDriver, error) {
	compiledPatterns := make([]*regexp.Regexp, len(autoApprovedIdentityPatterns))
	for i, s := range autoApprovedIdentityPatterns {
		p, err := regexp.Compile(s)
		if err != nil {
			return nil, fmt.Errorf("failed to process auto approval azure identity pattern: %w", err)
		}
		compiledPatterns[i] = p
	}

	return &AzureAuthHubDriver{
		kubeClient:             kubeClient,
		autoApprovedIDPatterns: compiledPatterns,
		oidcIssuerURL:          oidcIssuerURL,
	}, nil
}

var _ register.HubDriver = &AzureAuthHubDriver{}
