package work

import (
	"context"
	"encoding/json"
	"fmt"

	jsonpatch "github.com/evanphx/json-patch"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	utilrand "k8s.io/apimachinery/pkg/util/rand"

	workclientset "open-cluster-management.io/api/client/work/clientset/versioned"
	workapiv1 "open-cluster-management.io/api/work/v1"

	"open-cluster-management.io/ocm/pkg/features"
	"open-cluster-management.io/ocm/test/integration/util"
)

var _ = Describe("ManifestWork Executor Subject", func() {
	var cancel context.CancelFunc

	var work *workapiv1.ManifestWork
	var clusterName string
	var manifests []workapiv1.Manifest
	var executor *workapiv1.ManifestWorkExecutor

	var err error

	roleName := "role1"
	executorName := "test-executor"

	BeforeEach(func(ctx SpecContext) {
		err := features.SpokeMutableFeatureGate.Set("ExecutorValidatingCaches=true")
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(func() {
			_ = features.SpokeMutableFeatureGate.Set("ExecutorValidatingCaches=false")
		})

		clusterName = utilrand.String(5)

		ns := &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{Name: clusterName},
		}
		_, err = spokeKubeClient.CoreV1().Namespaces().Create(ctx, ns, metav1.CreateOptions{})
		Expect(err).ToNot(HaveOccurred())

		var runCtx context.Context
		runCtx, cancel = context.WithCancel(context.Background())
		go startWorkAgent(runCtx, clusterName)

		// reset manifests
		manifests = nil
		executor = nil
	})

	JustBeforeEach(func() {
		workName := fmt.Sprintf("executor-work-%s", utilrand.String(5))
		work = util.NewManifestWork(clusterName, workName, manifests)
		work.Spec.Executor = executor
	})

	AfterEach(func(ctx SpecContext) {
		if cancel != nil {
			cancel()
		}
		err := spokeKubeClient.CoreV1().Namespaces().Delete(
			ctx, clusterName, metav1.DeleteOptions{})
		Expect(err).ToNot(HaveOccurred())
	})

	Context("Apply the resource with executor", func() {
		BeforeEach(func() {
			manifests = []workapiv1.Manifest{
				util.ToManifest(util.NewConfigmap(clusterName, cm1, map[string]string{"a": "b"}, []string{})),
				util.ToManifest(util.NewConfigmap(clusterName, cm2, map[string]string{"c": "d"}, []string{})),
			}
			executor = &workapiv1.ManifestWorkExecutor{
				Subject: workapiv1.ManifestWorkExecutorSubject{
					Type: workapiv1.ExecutorSubjectTypeServiceAccount,
					ServiceAccount: &workapiv1.ManifestWorkSubjectServiceAccount{
						Namespace: clusterName,
						Name:      executorName,
					},
				},
			}
		})

		It("Executor does not have permission", func(ctx SpecContext) {
			work, err = hubWorkClient.WorkV1().ManifestWorks(clusterName).Create(
				ctx, work, metav1.CreateOptions{})
			Expect(err).ToNot(HaveOccurred())

			util.AssertWorkCondition(work.Namespace, work.Name, hubWorkClient, workapiv1.WorkApplied,
				metav1.ConditionFalse, []metav1.ConditionStatus{metav1.ConditionFalse, metav1.ConditionFalse},
				eventuallyTimeout, eventuallyInterval)
			util.AssertWorkCondition(work.Namespace, work.Name, hubWorkClient, workapiv1.WorkAvailable,
				metav1.ConditionFalse, []metav1.ConditionStatus{metav1.ConditionFalse, metav1.ConditionFalse},
				eventuallyTimeout, eventuallyInterval)

			// ensure configmaps not exist
			util.AssertNonexistenceOfConfigMaps(manifests, spokeKubeClient, eventuallyTimeout, eventuallyInterval)
		})

		It("Executor does not have permission to partial resources", func(ctx SpecContext) {
			_, err = spokeKubeClient.RbacV1().Roles(clusterName).Create(
				ctx, &rbacv1.Role{
					ObjectMeta: metav1.ObjectMeta{
						Namespace: clusterName,
						Name:      roleName,
					},
					Rules: []rbacv1.PolicyRule{
						{
							Verbs:         []string{"create", "update", "patch", "get", "list", "delete"},
							APIGroups:     []string{""},
							Resources:     []string{"configmaps"},
							ResourceNames: []string{cm1},
						},
					},
				}, metav1.CreateOptions{})
			Expect(err).ToNot(HaveOccurred())
			_, err = spokeKubeClient.RbacV1().RoleBindings(clusterName).Create(
				ctx, &rbacv1.RoleBinding{
					ObjectMeta: metav1.ObjectMeta{
						Namespace: clusterName,
						Name:      roleName,
					},
					Subjects: []rbacv1.Subject{
						{
							Kind:      "ServiceAccount",
							Namespace: clusterName,
							Name:      executorName,
						},
					},
					RoleRef: rbacv1.RoleRef{
						APIGroup: "rbac.authorization.k8s.io",
						Kind:     "Role",
						Name:     roleName,
					},
				}, metav1.CreateOptions{})
			Expect(err).ToNot(HaveOccurred())

			work, err = hubWorkClient.WorkV1().ManifestWorks(clusterName).Create(
				ctx, work, metav1.CreateOptions{})
			Expect(err).ToNot(HaveOccurred())

			util.AssertWorkCondition(work.Namespace, work.Name, hubWorkClient, workapiv1.WorkApplied,
				metav1.ConditionFalse, []metav1.ConditionStatus{metav1.ConditionTrue, metav1.ConditionFalse},
				eventuallyTimeout, eventuallyInterval)
			util.AssertWorkCondition(work.Namespace, work.Name, hubWorkClient, workapiv1.WorkAvailable,
				metav1.ConditionFalse, []metav1.ConditionStatus{metav1.ConditionTrue, metav1.ConditionFalse},
				eventuallyTimeout, eventuallyInterval)

			// ensure configmap cm1 exist and cm2 not exist
			util.AssertExistenceOfConfigMaps(
				[]workapiv1.Manifest{
					util.ToManifest(util.NewConfigmap(clusterName, cm1, map[string]string{"a": "b"}, []string{})),
				}, spokeKubeClient, eventuallyTimeout, eventuallyInterval)
			util.AssertNonexistenceOfConfigMaps(
				[]workapiv1.Manifest{
					util.ToManifest(util.NewConfigmap(clusterName, cm2, map[string]string{"a": "b"}, []string{})),
				}, spokeKubeClient, eventuallyTimeout, eventuallyInterval)
		})

		It("Executor has permission for all resources", func(ctx SpecContext) {
			_, err = spokeKubeClient.RbacV1().Roles(clusterName).Create(
				ctx, &rbacv1.Role{
					ObjectMeta: metav1.ObjectMeta{
						Namespace: clusterName,
						Name:      roleName,
					},
					Rules: []rbacv1.PolicyRule{
						{
							Verbs:         []string{"create", "update", "patch", "get", "list", "delete"},
							APIGroups:     []string{""},
							Resources:     []string{"configmaps"},
							ResourceNames: []string{cm1, cm2},
						},
					},
				}, metav1.CreateOptions{})
			Expect(err).ToNot(HaveOccurred())
			_, err = spokeKubeClient.RbacV1().RoleBindings(clusterName).Create(
				ctx, &rbacv1.RoleBinding{
					ObjectMeta: metav1.ObjectMeta{
						Namespace: clusterName,
						Name:      roleName,
					},
					Subjects: []rbacv1.Subject{
						{
							Kind:      "ServiceAccount",
							Namespace: clusterName,
							Name:      executorName,
						},
					},
					RoleRef: rbacv1.RoleRef{
						APIGroup: "rbac.authorization.k8s.io",
						Kind:     "Role",
						Name:     roleName,
					},
				}, metav1.CreateOptions{})
			Expect(err).ToNot(HaveOccurred())

			work, err = hubWorkClient.WorkV1().ManifestWorks(clusterName).Create(
				ctx, work, metav1.CreateOptions{})
			Expect(err).ToNot(HaveOccurred())

			util.AssertWorkCondition(work.Namespace, work.Name, hubWorkClient, workapiv1.WorkApplied,
				metav1.ConditionTrue, []metav1.ConditionStatus{metav1.ConditionTrue, metav1.ConditionTrue},
				eventuallyTimeout, eventuallyInterval)
			util.AssertWorkCondition(work.Namespace, work.Name, hubWorkClient, workapiv1.WorkAvailable,
				metav1.ConditionTrue, []metav1.ConditionStatus{metav1.ConditionTrue, metav1.ConditionTrue},
				eventuallyTimeout, eventuallyInterval)

			// ensure configmaps all exist
			util.AssertExistenceOfConfigMaps(manifests, spokeKubeClient, eventuallyTimeout, eventuallyInterval)
		})
	})

	Context("Apply the resource with executor deleting validating", func() {
		BeforeEach(func() {
			manifests = []workapiv1.Manifest{
				util.ToManifest(util.NewConfigmap(clusterName, cm1, map[string]string{"a": "b"}, []string{})),
				util.ToManifest(util.NewConfigmap(clusterName, cm2, map[string]string{"c": "d"}, []string{})),
			}
			executor = &workapiv1.ManifestWorkExecutor{
				Subject: workapiv1.ManifestWorkExecutorSubject{
					Type: workapiv1.ExecutorSubjectTypeServiceAccount,
					ServiceAccount: &workapiv1.ManifestWorkSubjectServiceAccount{
						Namespace: clusterName,
						Name:      executorName,
					},
				},
			}
		})

		It("Executor does not have delete permission and delete option is foreground", func(ctx SpecContext) {
			_, err = spokeKubeClient.RbacV1().Roles(clusterName).Create(
				ctx, &rbacv1.Role{
					ObjectMeta: metav1.ObjectMeta{
						Namespace: clusterName,
						Name:      roleName,
					},
					Rules: []rbacv1.PolicyRule{
						{
							Verbs:         []string{"create", "update", "patch", "get", "list"},
							APIGroups:     []string{""},
							Resources:     []string{"configmaps"},
							ResourceNames: []string{cm1, cm2},
						},
					},
				}, metav1.CreateOptions{})
			Expect(err).ToNot(HaveOccurred())
			_, err = spokeKubeClient.RbacV1().RoleBindings(clusterName).Create(
				ctx, &rbacv1.RoleBinding{
					ObjectMeta: metav1.ObjectMeta{
						Namespace: clusterName,
						Name:      roleName,
					},
					Subjects: []rbacv1.Subject{
						{
							Kind:      "ServiceAccount",
							Namespace: clusterName,
							Name:      executorName,
						},
					},
					RoleRef: rbacv1.RoleRef{
						APIGroup: "rbac.authorization.k8s.io",
						Kind:     "Role",
						Name:     roleName,
					},
				}, metav1.CreateOptions{})
			Expect(err).ToNot(HaveOccurred())

			work, err = hubWorkClient.WorkV1().ManifestWorks(clusterName).Create(
				ctx, work, metav1.CreateOptions{})
			Expect(err).ToNot(HaveOccurred())

			util.AssertWorkCondition(work.Namespace, work.Name, hubWorkClient, workapiv1.WorkApplied,
				metav1.ConditionFalse, []metav1.ConditionStatus{metav1.ConditionFalse, metav1.ConditionFalse},
				eventuallyTimeout, eventuallyInterval)
			util.AssertWorkCondition(work.Namespace, work.Name, hubWorkClient, workapiv1.WorkAvailable,
				metav1.ConditionFalse, []metav1.ConditionStatus{metav1.ConditionFalse, metav1.ConditionFalse},
				eventuallyTimeout, eventuallyInterval)

			// ensure configmaps not exist
			util.AssertNonexistenceOfConfigMaps(manifests, spokeKubeClient, eventuallyTimeout, eventuallyInterval)
		})

		It("Executor does not have delete permission and delete option is orphan", func(ctx SpecContext) {
			_, err = spokeKubeClient.RbacV1().Roles(clusterName).Create(
				ctx, &rbacv1.Role{
					ObjectMeta: metav1.ObjectMeta{
						Namespace: clusterName,
						Name:      roleName,
					},
					Rules: []rbacv1.PolicyRule{
						{
							Verbs:         []string{"create", "update", "patch", "get", "list"},
							APIGroups:     []string{""},
							Resources:     []string{"configmaps"},
							ResourceNames: []string{cm1, cm2},
						},
					},
				}, metav1.CreateOptions{})
			Expect(err).ToNot(HaveOccurred())
			_, err = spokeKubeClient.RbacV1().RoleBindings(clusterName).Create(
				ctx, &rbacv1.RoleBinding{
					ObjectMeta: metav1.ObjectMeta{
						Namespace: clusterName,
						Name:      roleName,
					},
					Subjects: []rbacv1.Subject{
						{
							Kind:      "ServiceAccount",
							Namespace: clusterName,
							Name:      executorName,
						},
					},
					RoleRef: rbacv1.RoleRef{
						APIGroup: "rbac.authorization.k8s.io",
						Kind:     "Role",
						Name:     roleName,
					},
				}, metav1.CreateOptions{})
			Expect(err).ToNot(HaveOccurred())

			work.Spec.DeleteOption = &workapiv1.DeleteOption{
				PropagationPolicy: workapiv1.DeletePropagationPolicyTypeOrphan,
			}
			work, err = hubWorkClient.WorkV1().ManifestWorks(clusterName).Create(
				ctx, work, metav1.CreateOptions{})
			Expect(err).ToNot(HaveOccurred())

			util.AssertWorkCondition(work.Namespace, work.Name, hubWorkClient, workapiv1.WorkApplied,
				metav1.ConditionTrue, []metav1.ConditionStatus{metav1.ConditionTrue, metav1.ConditionTrue},
				eventuallyTimeout, eventuallyInterval)
			util.AssertWorkCondition(work.Namespace, work.Name, hubWorkClient, workapiv1.WorkAvailable,
				metav1.ConditionTrue, []metav1.ConditionStatus{metav1.ConditionTrue, metav1.ConditionTrue},
				eventuallyTimeout, eventuallyInterval)

			// ensure configmaps all exist
			util.AssertExistenceOfConfigMaps(manifests, spokeKubeClient, eventuallyTimeout, eventuallyInterval)
		})

		It("Executor does not have delete permission and delete option is selectively orphan", func(ctx SpecContext) {
			_, err = spokeKubeClient.RbacV1().Roles(clusterName).Create(
				ctx, &rbacv1.Role{
					ObjectMeta: metav1.ObjectMeta{
						Namespace: clusterName,
						Name:      roleName,
					},
					Rules: []rbacv1.PolicyRule{
						{
							Verbs:         []string{"create", "update", "patch", "get", "list"},
							APIGroups:     []string{""},
							Resources:     []string{"configmaps"},
							ResourceNames: []string{cm1, cm2},
						},
					},
				}, metav1.CreateOptions{})
			Expect(err).ToNot(HaveOccurred())
			_, err = spokeKubeClient.RbacV1().RoleBindings(clusterName).Create(
				ctx, &rbacv1.RoleBinding{
					ObjectMeta: metav1.ObjectMeta{
						Namespace: clusterName,
						Name:      roleName,
					},
					Subjects: []rbacv1.Subject{
						{
							Kind:      "ServiceAccount",
							Namespace: clusterName,
							Name:      executorName,
						},
					},
					RoleRef: rbacv1.RoleRef{
						APIGroup: "rbac.authorization.k8s.io",
						Kind:     "Role",
						Name:     roleName,
					},
				}, metav1.CreateOptions{})
			Expect(err).ToNot(HaveOccurred())

			work.Spec.DeleteOption = &workapiv1.DeleteOption{
				PropagationPolicy: workapiv1.DeletePropagationPolicyTypeSelectivelyOrphan,
				SelectivelyOrphan: &workapiv1.SelectivelyOrphan{
					OrphaningRules: []workapiv1.OrphaningRule{
						{
							Resource:  "configmaps",
							Namespace: clusterName,
							Name:      cm1,
						},
					},
				},
			}
			work, err = hubWorkClient.WorkV1().ManifestWorks(clusterName).Create(
				ctx, work, metav1.CreateOptions{})
			Expect(err).ToNot(HaveOccurred())

			util.AssertWorkCondition(work.Namespace, work.Name, hubWorkClient, workapiv1.WorkApplied,
				metav1.ConditionFalse, []metav1.ConditionStatus{metav1.ConditionTrue, metav1.ConditionFalse},
				eventuallyTimeout, eventuallyInterval)
			util.AssertWorkCondition(work.Namespace, work.Name, hubWorkClient, workapiv1.WorkAvailable,
				metav1.ConditionFalse, []metav1.ConditionStatus{metav1.ConditionTrue, metav1.ConditionFalse},
				eventuallyTimeout, eventuallyInterval)

			// ensure configmap cm1 exist and cm2 not exist
			util.AssertExistenceOfConfigMaps(
				[]workapiv1.Manifest{
					util.ToManifest(util.NewConfigmap(clusterName, cm1, map[string]string{"a": "b"}, []string{})),
				}, spokeKubeClient, eventuallyTimeout, eventuallyInterval)
			util.AssertNonexistenceOfConfigMaps(
				[]workapiv1.Manifest{
					util.ToManifest(util.NewConfigmap(clusterName, cm2, map[string]string{"a": "b"}, []string{})),
				}, spokeKubeClient, eventuallyTimeout, eventuallyInterval)
		})
	})

	Context("Apply the resource with executor escalation validating", func() {
		BeforeEach(func() {
			manifests = []workapiv1.Manifest{
				util.ToManifest(util.NewConfigmap(clusterName, cm1, map[string]string{"a": "b"}, []string{})),
				util.ToManifest(util.NewRoleForManifest(clusterName, "role-cm-creator", rbacv1.PolicyRule{
					Verbs:     []string{"create", "update", "patch", "get", "list", "delete"},
					APIGroups: []string{""},
					Resources: []string{"configmaps"},
				})),
				util.ToManifest(util.NewRoleBindingForManifest(clusterName, "role-cm-creator-binding",
					rbacv1.RoleRef{
						Kind: "Role",
						Name: "role-cm-creator",
					},
					rbacv1.Subject{
						Kind:      "ServiceAccount",
						Namespace: clusterName,
						Name:      executorName,
					})),
			}
			executor = &workapiv1.ManifestWorkExecutor{
				Subject: workapiv1.ManifestWorkExecutorSubject{
					Type: workapiv1.ExecutorSubjectTypeServiceAccount,
					ServiceAccount: &workapiv1.ManifestWorkSubjectServiceAccount{
						Namespace: clusterName,
						Name:      executorName,
					},
				},
			}
		})

		It("no permission", func(ctx SpecContext) {
			_, err = spokeKubeClient.RbacV1().Roles(clusterName).Create(
				ctx, &rbacv1.Role{
					ObjectMeta: metav1.ObjectMeta{
						Name:      roleName,
						Namespace: clusterName,
					},
					Rules: []rbacv1.PolicyRule{
						{
							// no "escalate" and "bind" verb
							Verbs:     []string{"create", "update", "patch", "get", "list", "delete"},
							APIGroups: []string{"rbac.authorization.k8s.io"},
							Resources: []string{"roles", "rolebindings"},
						},
					},
				}, metav1.CreateOptions{})
			Expect(err).ToNot(HaveOccurred())
			_, err = spokeKubeClient.RbacV1().RoleBindings(clusterName).Create(
				ctx, &rbacv1.RoleBinding{
					ObjectMeta: metav1.ObjectMeta{
						Name:      roleName,
						Namespace: clusterName,
					},
					Subjects: []rbacv1.Subject{
						{
							Kind:      "ServiceAccount",
							Namespace: clusterName,
							Name:      executorName,
						},
					},
					RoleRef: rbacv1.RoleRef{
						APIGroup: "rbac.authorization.k8s.io",
						Kind:     "Role",
						Name:     roleName,
					},
				}, metav1.CreateOptions{})
			Expect(err).ToNot(HaveOccurred())

			work, err = hubWorkClient.WorkV1().ManifestWorks(clusterName).Create(
				ctx, work, metav1.CreateOptions{})
			Expect(err).ToNot(HaveOccurred())

			util.AssertWorkCondition(work.Namespace, work.Name, hubWorkClient, workapiv1.WorkApplied,
				metav1.ConditionFalse,
				[]metav1.ConditionStatus{metav1.ConditionFalse, metav1.ConditionFalse, metav1.ConditionFalse},
				eventuallyTimeout, eventuallyInterval)
			util.AssertWorkCondition(work.Namespace, work.Name, hubWorkClient, workapiv1.WorkAvailable,
				metav1.ConditionFalse,
				[]metav1.ConditionStatus{metav1.ConditionFalse, metav1.ConditionFalse, metav1.ConditionFalse},
				eventuallyTimeout, eventuallyInterval)

			// ensure configmap not exist
			util.AssertNonexistenceOfConfigMaps(
				[]workapiv1.Manifest{
					util.ToManifest(util.NewConfigmap(clusterName, cm1, map[string]string{"a": "b"}, []string{})),
				}, spokeKubeClient, eventuallyTimeout, eventuallyInterval)
		})

		It("no permission for already existing resource", func(ctx SpecContext) {
			_, err = spokeKubeClient.RbacV1().Roles(clusterName).Create(
				ctx, &rbacv1.Role{
					ObjectMeta: metav1.ObjectMeta{
						Name:      roleName,
						Namespace: clusterName,
					},
					Rules: []rbacv1.PolicyRule{
						{
							// no "escalate" and "bind" verb
							Verbs:     []string{"create", "update", "patch", "get", "list", "delete"},
							APIGroups: []string{"rbac.authorization.k8s.io"},
							Resources: []string{"roles", "rolebindings"},
						},
					},
				}, metav1.CreateOptions{})
			Expect(err).ToNot(HaveOccurred())
			_, err = spokeKubeClient.RbacV1().RoleBindings(clusterName).Create(
				ctx, &rbacv1.RoleBinding{
					ObjectMeta: metav1.ObjectMeta{
						Name:      roleName,
						Namespace: clusterName,
					},
					Subjects: []rbacv1.Subject{
						{
							Kind:      "ServiceAccount",
							Namespace: clusterName,
							Name:      executorName,
						},
					},
					RoleRef: rbacv1.RoleRef{
						APIGroup: "rbac.authorization.k8s.io",
						Kind:     "Role",
						Name:     roleName,
					},
				}, metav1.CreateOptions{})
			Expect(err).ToNot(HaveOccurred())

			// make the role exist with lower permission
			_, err = spokeKubeClient.RbacV1().Roles(clusterName).Create(
				ctx, &rbacv1.Role{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "role-cm-creator",
						Namespace: clusterName,
					},
					Rules: []rbacv1.PolicyRule{
						{
							Verbs:     []string{"get", "list"},
							APIGroups: []string{""},
							Resources: []string{"configmaps"},
						},
					},
				}, metav1.CreateOptions{})
			Expect(err).ToNot(HaveOccurred())

			work, err = hubWorkClient.WorkV1().ManifestWorks(clusterName).Create(
				ctx, work, metav1.CreateOptions{})
			Expect(err).ToNot(HaveOccurred())

			util.AssertWorkCondition(work.Namespace, work.Name, hubWorkClient, workapiv1.WorkApplied,
				metav1.ConditionFalse,
				[]metav1.ConditionStatus{metav1.ConditionFalse, metav1.ConditionFalse, metav1.ConditionFalse},
				eventuallyTimeout, eventuallyInterval)
			util.AssertWorkCondition(work.Namespace, work.Name, hubWorkClient, workapiv1.WorkAvailable,
				metav1.ConditionFalse,
				// the cluster role already esists, so the ailable status is true enen if the applied status is false
				[]metav1.ConditionStatus{metav1.ConditionFalse, metav1.ConditionTrue, metav1.ConditionFalse},
				eventuallyTimeout, eventuallyInterval)

			// ensure configmap not exist
			util.AssertNonexistenceOfConfigMaps(
				[]workapiv1.Manifest{
					util.ToManifest(util.NewConfigmap(clusterName, cm1, map[string]string{"a": "b"}, []string{})),
				}, spokeKubeClient, eventuallyTimeout, eventuallyInterval)
		})

		It("with permission", func(ctx SpecContext) {
			_, err = spokeKubeClient.RbacV1().Roles(clusterName).Create(
				ctx, &rbacv1.Role{
					ObjectMeta: metav1.ObjectMeta{
						Name:      roleName,
						Namespace: clusterName,
					},
					Rules: []rbacv1.PolicyRule{
						{
							// with "escalate" and "bind" verb
							Verbs:     []string{"create", "update", "patch", "get", "list", "delete", "escalate", "bind"},
							APIGroups: []string{"rbac.authorization.k8s.io"},
							Resources: []string{"roles"},
						},
						{
							Verbs:     []string{"create", "update", "patch", "get", "list", "delete"},
							APIGroups: []string{"rbac.authorization.k8s.io"},
							Resources: []string{"rolebindings"},
						},
					},
				}, metav1.CreateOptions{})
			Expect(err).ToNot(HaveOccurred())
			_, err = spokeKubeClient.RbacV1().RoleBindings(clusterName).Create(
				ctx, &rbacv1.RoleBinding{
					ObjectMeta: metav1.ObjectMeta{
						Name:      roleName,
						Namespace: clusterName,
					},
					Subjects: []rbacv1.Subject{
						{
							Kind:      "ServiceAccount",
							Namespace: clusterName,
							Name:      executorName,
						},
					},
					RoleRef: rbacv1.RoleRef{
						APIGroup: "rbac.authorization.k8s.io",
						Kind:     "Role",
						Name:     roleName,
					},
				}, metav1.CreateOptions{})
			Expect(err).ToNot(HaveOccurred())

			work, err = hubWorkClient.WorkV1().ManifestWorks(clusterName).Create(
				ctx, work, metav1.CreateOptions{})
			Expect(err).ToNot(HaveOccurred())

			util.AssertWorkCondition(work.Namespace, work.Name, hubWorkClient, workapiv1.WorkApplied,
				metav1.ConditionTrue,
				[]metav1.ConditionStatus{metav1.ConditionTrue, metav1.ConditionTrue, metav1.ConditionTrue},
				eventuallyTimeout*3, eventuallyInterval)
			util.AssertWorkCondition(work.Namespace, work.Name, hubWorkClient, workapiv1.WorkAvailable,
				metav1.ConditionTrue,
				[]metav1.ConditionStatus{metav1.ConditionTrue, metav1.ConditionTrue, metav1.ConditionTrue},
				eventuallyTimeout, eventuallyInterval)

			// ensure configmaps exist
			util.AssertExistenceOfConfigMaps(
				[]workapiv1.Manifest{
					util.ToManifest(util.NewConfigmap(clusterName, cm1, map[string]string{"a": "b"}, []string{})),
				}, spokeKubeClient, eventuallyTimeout, eventuallyInterval)
		})

		It("with permission for already exist resource", func(ctx SpecContext) {
			_, err = spokeKubeClient.RbacV1().Roles(clusterName).Create(
				ctx, &rbacv1.Role{
					ObjectMeta: metav1.ObjectMeta{
						Name:      roleName,
						Namespace: clusterName,
					},
					Rules: []rbacv1.PolicyRule{
						{
							// with "escalate" and "bind" verb
							Verbs:     []string{"create", "update", "patch", "get", "list", "delete", "escalate", "bind"},
							APIGroups: []string{"rbac.authorization.k8s.io"},
							Resources: []string{"roles"},
						},
						{
							Verbs:     []string{"create", "update", "patch", "get", "list", "delete"},
							APIGroups: []string{"rbac.authorization.k8s.io"},
							Resources: []string{"rolebindings"},
						},
					},
				}, metav1.CreateOptions{})
			Expect(err).ToNot(HaveOccurred())
			_, err = spokeKubeClient.RbacV1().RoleBindings(clusterName).Create(
				ctx, &rbacv1.RoleBinding{
					ObjectMeta: metav1.ObjectMeta{
						Name:      roleName,
						Namespace: clusterName,
					},
					Subjects: []rbacv1.Subject{
						{
							Kind:      "ServiceAccount",
							Namespace: clusterName,
							Name:      executorName,
						},
					},
					RoleRef: rbacv1.RoleRef{
						APIGroup: "rbac.authorization.k8s.io",
						Kind:     "Role",
						Name:     roleName,
					},
				}, metav1.CreateOptions{})
			Expect(err).ToNot(HaveOccurred())

			// make the role exist with lower permission
			_, err = spokeKubeClient.RbacV1().Roles(clusterName).Create(
				ctx, &rbacv1.Role{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "role-cm-creator",
						Namespace: clusterName,
					},
					Rules: []rbacv1.PolicyRule{
						{
							Verbs:     []string{"get", "list"},
							APIGroups: []string{""},
							Resources: []string{"configmaps"},
						},
					},
				}, metav1.CreateOptions{})
			Expect(err).ToNot(HaveOccurred())

			work, err = hubWorkClient.WorkV1().ManifestWorks(clusterName).Create(
				ctx, work, metav1.CreateOptions{})
			Expect(err).ToNot(HaveOccurred())

			util.AssertWorkCondition(work.Namespace, work.Name, hubWorkClient, workapiv1.WorkApplied,
				metav1.ConditionTrue,
				[]metav1.ConditionStatus{metav1.ConditionTrue, metav1.ConditionTrue, metav1.ConditionTrue},
				eventuallyTimeout*3, eventuallyInterval)
			util.AssertWorkCondition(work.Namespace, work.Name, hubWorkClient, workapiv1.WorkAvailable,
				metav1.ConditionTrue,
				[]metav1.ConditionStatus{metav1.ConditionTrue, metav1.ConditionTrue, metav1.ConditionTrue},
				eventuallyTimeout, eventuallyInterval)

			// ensure configmaps exist
			util.AssertExistenceOfConfigMaps(
				[]workapiv1.Manifest{
					util.ToManifest(util.NewConfigmap(clusterName, cm1, map[string]string{"a": "b"}, []string{})),
				}, spokeKubeClient, eventuallyTimeout, eventuallyInterval)
		})
	})

	Context("Caches are in effect", func() {
		createRBAC := func(ctx context.Context, clusterName, executorName string) {
			_, err := spokeKubeClient.RbacV1().Roles(clusterName).Create(
				ctx, &rbacv1.Role{
					ObjectMeta: metav1.ObjectMeta{
						Namespace: clusterName,
						Name:      roleName,
					},
					Rules: []rbacv1.PolicyRule{
						{
							Verbs:         []string{"create", "update", "patch", "get", "list", "delete"},
							APIGroups:     []string{""},
							Resources:     []string{"configmaps"},
							ResourceNames: []string{cm1, cm2},
						},
					},
				}, metav1.CreateOptions{})
			Expect(err).ToNot(HaveOccurred())
			_, err = spokeKubeClient.RbacV1().RoleBindings(clusterName).Create(
				ctx, &rbacv1.RoleBinding{
					ObjectMeta: metav1.ObjectMeta{
						Namespace: clusterName,
						Name:      roleName,
					},
					Subjects: []rbacv1.Subject{
						{
							Kind:      "ServiceAccount",
							Namespace: clusterName,
							Name:      executorName,
						},
					},
					RoleRef: rbacv1.RoleRef{
						APIGroup: "rbac.authorization.k8s.io",
						Kind:     "Role",
						Name:     roleName,
					},
				}, metav1.CreateOptions{})
			Expect(err).ToNot(HaveOccurred())
		}
		deleteRBAC := func(ctx context.Context, clusterName string) {
			err := spokeKubeClient.RbacV1().Roles(clusterName).Delete(
				ctx, roleName, metav1.DeleteOptions{})
			Expect(err).ToNot(HaveOccurred())
			err = spokeKubeClient.RbacV1().RoleBindings(clusterName).Delete(
				ctx, roleName, metav1.DeleteOptions{})
			Expect(err).ToNot(HaveOccurred())
		}
		BeforeEach(func() {
			manifests = []workapiv1.Manifest{
				util.ToManifest(util.NewConfigmap(clusterName, cm1, map[string]string{"a": "b"}, []string{})),
			}
			executor = &workapiv1.ManifestWorkExecutor{
				Subject: workapiv1.ManifestWorkExecutorSubject{
					Type: workapiv1.ExecutorSubjectTypeServiceAccount,
					ServiceAccount: &workapiv1.ManifestWorkSubjectServiceAccount{
						Namespace: clusterName,
						Name:      executorName,
					},
				},
			}
		})

		It("Permission change", func(ctx SpecContext) {
			work, err = hubWorkClient.WorkV1().ManifestWorks(clusterName).Create(
				ctx, work, metav1.CreateOptions{})
			Expect(err).ToNot(HaveOccurred())

			util.AssertWorkCondition(work.Namespace, work.Name, hubWorkClient, workapiv1.WorkApplied,
				metav1.ConditionFalse, []metav1.ConditionStatus{metav1.ConditionFalse},
				eventuallyTimeout, eventuallyInterval)
			util.AssertWorkCondition(work.Namespace, work.Name, hubWorkClient, workapiv1.WorkAvailable,
				metav1.ConditionFalse, []metav1.ConditionStatus{metav1.ConditionFalse},
				eventuallyTimeout, eventuallyInterval)

			By("ensure configmaps do not exist")
			util.AssertNonexistenceOfConfigMaps(manifests, spokeKubeClient, eventuallyTimeout, eventuallyInterval)

			createRBAC(ctx, clusterName, executorName)
			addConfigMapToManifestWork(ctx, hubWorkClient, work.Name, clusterName, cm2)

			util.AssertWorkCondition(work.Namespace, work.Name, hubWorkClient, workapiv1.WorkApplied,
				metav1.ConditionTrue, []metav1.ConditionStatus{metav1.ConditionTrue, metav1.ConditionTrue},
				3*eventuallyTimeout, eventuallyInterval)
			util.AssertWorkCondition(work.Namespace, work.Name, hubWorkClient, workapiv1.WorkAvailable,
				metav1.ConditionTrue, []metav1.ConditionStatus{metav1.ConditionTrue, metav1.ConditionTrue},
				3*eventuallyTimeout, eventuallyInterval)

			By("ensure configmaps cm1 and cm2 exist")
			util.AssertExistenceOfConfigMaps(manifests, spokeKubeClient, eventuallyTimeout, eventuallyInterval)

			deleteRBAC(ctx, clusterName)
			addConfigMapToManifestWork(ctx, hubWorkClient, work.Name, clusterName, "cm3")

			util.AssertWorkCondition(work.Namespace, work.Name, hubWorkClient, workapiv1.WorkApplied,
				metav1.ConditionFalse, []metav1.ConditionStatus{metav1.ConditionFalse, metav1.ConditionFalse,
					metav1.ConditionFalse}, 3*eventuallyTimeout, eventuallyInterval)
			util.AssertWorkCondition(work.Namespace, work.Name, hubWorkClient, workapiv1.WorkAvailable,
				metav1.ConditionFalse, []metav1.ConditionStatus{metav1.ConditionTrue, metav1.ConditionTrue,
					metav1.ConditionFalse}, 3*eventuallyTimeout, eventuallyInterval)

			By("ensure configmap cm1 cm2 exist(will not delete the applied resource even the permison is revoked) but cm3 does not exist")
			util.AssertExistenceOfConfigMaps(
				[]workapiv1.Manifest{
					util.ToManifest(util.NewConfigmap(clusterName, cm1, map[string]string{"a": "b"}, nil)),
				}, spokeKubeClient, eventuallyTimeout, eventuallyInterval)
			util.AssertExistenceOfConfigMaps(
				[]workapiv1.Manifest{
					util.ToManifest(util.NewConfigmap(clusterName, cm2, map[string]string{"a": "b"}, nil)),
				}, spokeKubeClient, eventuallyTimeout, eventuallyInterval)
			util.AssertNonexistenceOfConfigMaps(
				[]workapiv1.Manifest{
					util.ToManifest(util.NewConfigmap(clusterName, "cm3", map[string]string{"a": "b"}, nil)),
				}, spokeKubeClient, eventuallyTimeout, eventuallyInterval)
		})
	})
})

func addConfigMapToManifestWork(ctx context.Context, manifestWorkClient workclientset.Interface, manifestWorkName string,
	clusterName string, appendConfigMapName string) {
	manifestWork, err := manifestWorkClient.WorkV1().ManifestWorks(clusterName).Get(
		ctx, manifestWorkName, metav1.GetOptions{})
	Expect(err).ToNot(HaveOccurred())

	oldData, err := json.Marshal(manifestWork)
	Expect(err).ToNot(HaveOccurred())

	newManifests := manifestWork.DeepCopy()
	newManifests.Spec.Workload.Manifests = append(newManifests.Spec.Workload.Manifests,
		util.ToManifest(util.NewConfigmap(clusterName, appendConfigMapName, map[string]string{"a": "b"}, []string{})))
	newData, err := json.Marshal(newManifests)
	Expect(err).ToNot(HaveOccurred())

	patchBytes, err := jsonpatch.CreateMergePatch(oldData, newData)
	Expect(err).ToNot(HaveOccurred())

	_, err = manifestWorkClient.WorkV1().ManifestWorks(clusterName).Patch(ctx,
		manifestWorkName, types.MergePatchType, patchBytes, metav1.PatchOptions{})
	Expect(err).ToNot(HaveOccurred())
}
