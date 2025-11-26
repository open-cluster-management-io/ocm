package apply

import (
	"context"
	"testing"
	"time"

	"github.com/openshift/library-go/pkg/operator/resource/resourceread"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/client-go/informers"
	kubefake "k8s.io/client-go/kubernetes/fake"
	clienttesting "k8s.io/client-go/testing"

	"open-cluster-management.io/sdk-go/pkg/basecontroller/events"

	testingcommon "open-cluster-management.io/ocm/pkg/common/testing"
)

func TestPermissionApply(t *testing.T) {
	tc := []struct {
		name             string
		manifest         string
		existingManifest string
		// filtered indicates if the existing manifest is in the informer cache or not. For example,
		// it may not match the expected label/field selector of the informer factory.
		filtered       bool
		validateAction func(t *testing.T, actions []clienttesting.Action)
	}{
		{
			name: "create clusterrole",
			manifest: `
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
  name: test
rules:
- apiGroups: [""]
  resources: ["configmaps", "events"]
  verbs: ["get", "list", "watch"] 
`,
			validateAction: func(t *testing.T, actions []clienttesting.Action) {
				testingcommon.AssertActions(t, actions, "create")
			},
		},
		{
			name: "upate clusterrole",
			existingManifest: `
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
  name: test
rules:
- apiGroups: [""]
  resources: ["configmaps", "events"]
  verbs: ["get", "list", "watch", "create"] 
`,
			manifest: `
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
  name: test
rules:
- apiGroups: [""]
  resources: ["configmaps", "events"]
  verbs: ["get", "list", "watch"] 
`,
			validateAction: func(t *testing.T, actions []clienttesting.Action) {
				testingcommon.AssertActions(t, actions, "update")
			},
		},
		{
			name: "upate clusterrole with no cache",
			existingManifest: `
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
  name: test
rules:
- apiGroups: [""]
  resources: ["configmaps", "events"]
  verbs: ["get", "list", "watch", "create"] 
`,
			manifest: `
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
  name: test
rules:
- apiGroups: [""]
  resources: ["configmaps", "events"]
  verbs: ["get", "list", "watch"] 
`,
			filtered: true,
			validateAction: func(t *testing.T, actions []clienttesting.Action) {
				testingcommon.AssertActions(t, actions, "create", "get", "update")
			},
		},
		{
			name: "compare and no update clusterrole",
			existingManifest: `
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
  name: test
rules:
- apiGroups: [""]
  resources: ["configmaps", "events"]
  verbs: ["get", "list", "watch"] 
`,
			manifest: `
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
  name: test
rules:
- apiGroups: [""]
  resources: ["configmaps", "events"]
  verbs: ["get", "list", "watch"] 
`,
			validateAction: testingcommon.AssertNoActions,
		},
		{
			name: "create clusterrolebinding",
			manifest: `
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRoleBinding
metadata:
  name: test
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: ClusterRole
  name: test
subjects:
  - kind: ServiceAccount
    name: test
    namespace: test
`,
			validateAction: func(t *testing.T, actions []clienttesting.Action) {
				testingcommon.AssertActions(t, actions, "create")
			},
		},
		{
			name: "update clusterrolebinding",
			existingManifest: `
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRoleBinding
metadata:
  name: test
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: ClusterRole
  name: test1
subjects:
  - kind: ServiceAccount
    name: test
    namespace: test1
`,
			manifest: `
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRoleBinding
metadata:
  name: test
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: ClusterRole
  name: test
subjects:
  - kind: ServiceAccount
    name: test
    namespace: test
`,
			validateAction: func(t *testing.T, actions []clienttesting.Action) {
				testingcommon.AssertActions(t, actions, "update")
			},
		},
		{
			name: "no update clusterrolebinding",
			existingManifest: `
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRoleBinding
metadata:
  name: test
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: ClusterRole
  name: test
subjects:
  - kind: ServiceAccount
    name: test
    namespace: test
`,
			manifest: `
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRoleBinding
metadata:
  name: test
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: ClusterRole
  name: test
subjects:
  - kind: ServiceAccount
    name: test
    namespace: test
`,
			validateAction: testingcommon.AssertNoActions,
		},
		{
			name: "create role",
			manifest: `
apiVersion: rbac.authorization.k8s.io/v1
kind: Role
metadata:
  name: test
  namespace: default
rules:
- apiGroups: [""]
  resources: ["configmaps", "events"]
  verbs: ["get", "list", "watch"] 
`,
			validateAction: func(t *testing.T, actions []clienttesting.Action) {
				testingcommon.AssertActions(t, actions, "create")
			},
		},
		{
			name: "upate role",
			existingManifest: `
apiVersion: rbac.authorization.k8s.io/v1
kind: Role
metadata:
  name: test
  namespace: default
rules:
- apiGroups: [""]
  resources: ["configmaps", "events"]
  verbs: ["get", "list", "watch", "create"] 
`,
			manifest: `
apiVersion: rbac.authorization.k8s.io/v1
kind: Role
metadata:
  name: test
  namespace: default
rules:
- apiGroups: [""]
  resources: ["configmaps", "events"]
  verbs: ["get", "list", "watch"] 
`,
			validateAction: func(t *testing.T, actions []clienttesting.Action) {
				testingcommon.AssertActions(t, actions, "update")
			},
		},
		{
			name: "compare and no update clusterrole",
			existingManifest: `
apiVersion: rbac.authorization.k8s.io/v1
kind: Role
metadata:
  name: test
  namespace: default
rules:
- apiGroups: [""]
  resources: ["configmaps", "events"]
  verbs: ["get", "list", "watch"] 
`,
			manifest: `
apiVersion: rbac.authorization.k8s.io/v1
kind: Role
metadata:
  name: test
  namespace: default
rules:
- apiGroups: [""]
  resources: ["configmaps", "events"]
  verbs: ["get", "list", "watch"] 
`,
			validateAction: testingcommon.AssertNoActions,
		},
		{
			name: "create rolebinding",
			manifest: `
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRoleBinding
metadata:
  name: test
  namespace: default
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: Role
  name: test
subjects:
  - kind: ServiceAccount
    name: test
    namespace: test
`,
			validateAction: func(t *testing.T, actions []clienttesting.Action) {
				testingcommon.AssertActions(t, actions, "create")
			},
		},
		{
			name: "update rolebinding",
			existingManifest: `
apiVersion: rbac.authorization.k8s.io/v1
kind: RoleBinding
metadata:
  name: test
  namespace: default
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: ClusterRole
  name: test1
subjects:
  - kind: ServiceAccount
    name: test
    namespace: test1
`,
			manifest: `
apiVersion: rbac.authorization.k8s.io/v1
kind: RoleBinding
metadata:
  name: test
  namespace: default
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: ClusterRole
  name: test
subjects:
  - kind: ServiceAccount
    name: test
    namespace: test
`,
			validateAction: func(t *testing.T, actions []clienttesting.Action) {
				testingcommon.AssertActions(t, actions, "update")
			},
		},
		{
			name: "no update clusterrolebinding",
			existingManifest: `
apiVersion: rbac.authorization.k8s.io/v1
kind: RoleBinding
metadata:
  name: test
  namespace: default
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: ClusterRole
  name: test
subjects:
  - kind: ServiceAccount
    name: test
    namespace: test
`,
			manifest: `
apiVersion: rbac.authorization.k8s.io/v1
kind: RoleBinding
metadata:
  name: test
  namespace: default
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: ClusterRole
  name: test
subjects:
  - kind: ServiceAccount
    name: test
    namespace: test
`,
			validateAction: testingcommon.AssertNoActions,
		},
	}

	for _, c := range tc {
		t.Run(c.name, func(t *testing.T) {
			var kubeClient *kubefake.Clientset
			var informerFactory informers.SharedInformerFactory
			if len(c.existingManifest) > 0 {
				o, err := resourceread.ReadGenericWithUnstructured([]byte(c.existingManifest))
				if err != nil {
					t.Fatal(err)
				}
				kubeClient = kubefake.NewSimpleClientset(o)
				informerFactory = informers.NewSharedInformerFactory(kubeClient, 3*time.Minute)
				if !c.filtered {
					switch t := o.(type) {
					case *rbacv1.ClusterRole:
						err = informerFactory.Rbac().V1().ClusterRoles().Informer().GetStore().Add(t)
					case *rbacv1.ClusterRoleBinding:
						err = informerFactory.Rbac().V1().ClusterRoleBindings().Informer().GetStore().Add(t)
					case *rbacv1.Role:
						err = informerFactory.Rbac().V1().Roles().Informer().GetStore().Add(t)
					case *rbacv1.RoleBinding:
						err = informerFactory.Rbac().V1().RoleBindings().Informer().GetStore().Add(t)
					}
					if err != nil {
						t.Fatal(err)
					}
				}
			} else {
				kubeClient = kubefake.NewSimpleClientset()
				informerFactory = informers.NewSharedInformerFactory(kubeClient, 3*time.Minute)
			}

			applier := NewPermissionApplier(
				kubeClient,
				informerFactory.Rbac().V1().Roles().Lister(),
				informerFactory.Rbac().V1().RoleBindings().Lister(),
				informerFactory.Rbac().V1().ClusterRoles().Lister(),
				informerFactory.Rbac().V1().ClusterRoleBindings().Lister(),
			)
			results := applier.Apply(context.TODO(), events.NewContextualLoggingEventRecorder(t.Name()),
				func(name string) ([]byte, error) {
					return []byte(c.manifest), nil
				}, "test")

			for _, r := range results {
				if r.Error != nil {
					t.Error(r.Error)
				}
			}
			c.validateAction(t, kubeClient.Actions())
		})
	}
}
