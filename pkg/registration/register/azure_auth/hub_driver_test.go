package azure_auth

import (
	"context"
	"fmt"
	"strings"
	"testing"

	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	kubefake "k8s.io/client-go/kubernetes/fake"

	clusterv1 "open-cluster-management.io/api/cluster/v1"
	operatorv1 "open-cluster-management.io/api/operator/v1"
)

const (
	testAzureID      = "11111111-1111-1111-1111-111111111111"
	testOtherAzureID = "22222222-2222-2222-2222-222222222222"
)

func newAzureCluster(name, azureID string) *clusterv1.ManagedCluster {
	return &clusterv1.ManagedCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
			Annotations: map[string]string{
				operatorv1.ClusterAnnotationsKeyPrefix + "/" + ManagedClusterAzureIdentityAnnotation: azureID,
			},
		},
	}
}

func TestAccept(t *testing.T) {
	cases := []struct {
		name       string
		cluster    *clusterv1.ManagedCluster
		patterns   []string
		isAccepted bool
	}{
		{
			name: "no patterns configured: always accepted",
			cluster: &clusterv1.ManagedCluster{
				ObjectMeta: metav1.ObjectMeta{Name: "cluster1"},
			},
			patterns:   nil,
			isAccepted: true,
		},
		{
			name: "patterns configured, cluster not managed by azure-auth: not vetoed",
			cluster: &clusterv1.ManagedCluster{
				ObjectMeta: metav1.ObjectMeta{Name: "cluster1"},
			},
			patterns:   []string{".*"},
			isAccepted: true,
		},
		{
			name:       "patterns configured, azure id matches: accepted",
			cluster:    newAzureCluster("cluster1", testAzureID),
			patterns:   []string{"^11111111-.*$"},
			isAccepted: true,
		},
		{
			name:       "patterns configured, azure id does not match: not accepted",
			cluster:    newAzureCluster("cluster1", testOtherAzureID),
			patterns:   []string{"^11111111-.*$"},
			isAccepted: false,
		},
		{
			name:       "patterns configured, azure id partially matches: not accepted",
			cluster:    newAzureCluster("cluster1", testAzureID+"-evil"),
			patterns:   []string{"^11111111-1111-1111-1111-111111111111$"},
			isAccepted: false,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			driver, err := NewAzureAuthHubDriver(nil, c.patterns, "")
			if err != nil {
				t.Fatalf("unexpected error building driver: %v", err)
			}
			if accepted := driver.Accept(c.cluster); accepted != c.isAccepted {
				t.Errorf("expected Accept()=%v, got %v", c.isAccepted, accepted)
			}
		})
	}
}

func TestNewAzureAuthHubDriverInvalidPattern(t *testing.T) {
	if _, err := NewAzureAuthHubDriver(nil, []string{"("}, ""); err == nil {
		t.Errorf("expected an error for an invalid regex pattern")
	}
}

// existingClusterRoleBinding is a binding CreatePermissions previously created for
// clusterName and azureID.
func existingClusterRoleBinding(clusterName, azureID string) *rbacv1.ClusterRoleBinding {
	return &rbacv1.ClusterRoleBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name: fmt.Sprintf(clusterRoleBindingNameFmt, clusterName),
			Labels: map[string]string{
				clusterv1.ClusterNameLabelKey: clusterName,
				AzureIdentityLabelKey:         azureID,
			},
		},
	}
}

func TestCreatePermissions(t *testing.T) {
	cases := []struct {
		name            string
		cluster         *clusterv1.ManagedCluster
		oidcIssuerURL   string
		existing        []runtime.Object
		expectErr       string
		expectUsername  string
		expectNoBinding bool
	}{
		{
			name:            "cluster not using azure-auth: nothing created",
			cluster:         &clusterv1.ManagedCluster{ObjectMeta: metav1.ObjectMeta{Name: "cluster1"}},
			expectNoBinding: true,
		},
		{
			name:            "empty azure identity annotation: rejected",
			cluster:         newAzureCluster("cluster1", ""),
			expectErr:       "empty azure identity annotation",
			expectNoBinding: true,
		},
		{
			name:            "azure identity not a valid label value: rejected",
			cluster:         newAzureCluster("cluster1", "not/a valid label"),
			expectErr:       "invalid azure identity annotation",
			expectNoBinding: true,
		},
		{
			name:           "AKS native Azure AD integration: binds the bare object ID",
			cluster:        newAzureCluster("cluster1", testAzureID),
			expectUsername: testAzureID,
		},
		{
			name:           "generic OIDC issuer: binds the issuer-prefixed username",
			cluster:        newAzureCluster("cluster1", testAzureID),
			oidcIssuerURL:  "https://login.microsoftonline.com/tenant/v2.0",
			expectUsername: "https://login.microsoftonline.com/tenant/v2.0#" + testAzureID,
		},
		{
			name:           "identity already bound to the same cluster: re-applied",
			cluster:        newAzureCluster("cluster1", testAzureID),
			existing:       []runtime.Object{existingClusterRoleBinding("cluster1", testAzureID)},
			expectUsername: testAzureID,
		},
		{
			name:           "a different identity bound to another cluster: allowed",
			cluster:        newAzureCluster("cluster1", testAzureID),
			existing:       []runtime.Object{existingClusterRoleBinding("cluster2", testOtherAzureID)},
			expectUsername: testAzureID,
		},
		{
			name:            "identity already bound to a different cluster: refused",
			cluster:         newAzureCluster("cluster1", testAzureID),
			existing:        []runtime.Object{existingClusterRoleBinding("cluster2", testAzureID)},
			expectErr:       `already bound to managedcluster "cluster2"`,
			expectNoBinding: true,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			kubeClient := kubefake.NewSimpleClientset(c.existing...)
			driver, err := NewAzureAuthHubDriver(kubeClient, nil, c.oidcIssuerURL)
			if err != nil {
				t.Fatalf("unexpected error building driver: %v", err)
			}

			err = driver.CreatePermissions(context.TODO(), c.cluster)
			switch {
			case c.expectErr == "" && err != nil:
				t.Fatalf("expected no error, got %v", err)
			case c.expectErr != "" && (err == nil || !strings.Contains(err.Error(), c.expectErr)):
				t.Fatalf("expected error containing %q, got %v", c.expectErr, err)
			}

			ctx := context.TODO()
			crb, crbErr := kubeClient.RbacV1().ClusterRoleBindings().Get(ctx,
				fmt.Sprintf(clusterRoleBindingNameFmt, c.cluster.Name), metav1.GetOptions{})
			if c.expectNoBinding {
				if crbErr == nil {
					t.Fatalf("expected no ClusterRoleBinding for %q, got %v", c.cluster.Name, crb)
				}
				return
			}
			if crbErr != nil {
				t.Fatalf("expected a ClusterRoleBinding: %v", crbErr)
			}
			regRB, err := kubeClient.RbacV1().RoleBindings(c.cluster.Name).Get(ctx,
				fmt.Sprintf(registrationRoleBindingNameFmt, c.cluster.Name), metav1.GetOptions{})
			if err != nil {
				t.Fatalf("expected a registration RoleBinding: %v", err)
			}
			workRB, err := kubeClient.RbacV1().RoleBindings(c.cluster.Name).Get(ctx,
				fmt.Sprintf(workRoleBindingNameFmt, c.cluster.Name), metav1.GetOptions{})
			if err != nil {
				t.Fatalf("expected a work RoleBinding: %v", err)
			}

			for _, b := range []struct {
				kind     string
				meta     metav1.ObjectMeta
				roleRef  rbacv1.RoleRef
				subjects []rbacv1.Subject
				roleName string
			}{
				{"ClusterRoleBinding", crb.ObjectMeta, crb.RoleRef, crb.Subjects,
					fmt.Sprintf(managedClusterRoleNameFmt, c.cluster.Name)},
				{"registration RoleBinding", regRB.ObjectMeta, regRB.RoleRef, regRB.Subjects, managedClusterRegistrationRole},
				{"work RoleBinding", workRB.ObjectMeta, workRB.RoleRef, workRB.Subjects, managedClusterWorkRole},
			} {
				if b.roleRef.Name != b.roleName || b.roleRef.Kind != "ClusterRole" {
					t.Errorf("%s: expected roleRef ClusterRole %q, got %+v", b.kind, b.roleName, b.roleRef)
				}
				if len(b.subjects) != 1 || b.subjects[0].Kind != rbacv1.UserKind || b.subjects[0].Name != c.expectUsername {
					t.Errorf("%s: expected a single User subject %q, got %+v", b.kind, c.expectUsername, b.subjects)
				}
				if b.meta.Labels[clusterv1.ClusterNameLabelKey] != c.cluster.Name {
					t.Errorf("%s: expected cluster-name label %q, got %v", b.kind, c.cluster.Name, b.meta.Labels)
				}
				if b.meta.Labels[AzureIdentityLabelKey] != testAzureID {
					t.Errorf("%s: expected azure-identity label %q, got %v", b.kind, testAzureID, b.meta.Labels)
				}
			}
		})
	}
}

func TestCleanup(t *testing.T) {
	cluster := newAzureCluster("cluster1", testAzureID)
	kubeClient := kubefake.NewSimpleClientset()
	driver, err := NewAzureAuthHubDriver(kubeClient, nil, "")
	if err != nil {
		t.Fatalf("unexpected error building driver: %v", err)
	}
	ctx := context.TODO()

	if err := driver.CreatePermissions(ctx, cluster); err != nil {
		t.Fatalf("CreatePermissions failed: %v", err)
	}
	if err := driver.Cleanup(ctx, cluster); err != nil {
		t.Fatalf("Cleanup failed: %v", err)
	}
	// Cleanup is idempotent.
	if err := driver.Cleanup(ctx, cluster); err != nil {
		t.Fatalf("second Cleanup failed: %v", err)
	}

	crbs, _ := kubeClient.RbacV1().ClusterRoleBindings().List(ctx, metav1.ListOptions{})
	rbs, _ := kubeClient.RbacV1().RoleBindings(cluster.Name).List(ctx, metav1.ListOptions{})
	if len(crbs.Items) != 0 || len(rbs.Items) != 0 {
		t.Errorf("expected all bindings removed, got %d ClusterRoleBindings and %d RoleBindings", len(crbs.Items), len(rbs.Items))
	}

	// Once cleaned up, the identity can be claimed by a different cluster.
	if err := driver.CreatePermissions(ctx, newAzureCluster("cluster2", testAzureID)); err != nil {
		t.Errorf("expected the released identity to be bindable to another cluster, got %v", err)
	}
}
