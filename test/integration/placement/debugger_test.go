package placement

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/rand"
	"k8s.io/apiserver/pkg/authentication/user"
	"k8s.io/apiserver/pkg/endpoints/request"
	"k8s.io/client-go/tools/cache"

	controllers "open-cluster-management.io/ocm/pkg/placement/controllers"
	"open-cluster-management.io/ocm/pkg/placement/debugger"
	testinghelpers "open-cluster-management.io/ocm/pkg/placement/helpers/testing"
)

var _ = Describe("DebugService", func() {
	var namespace string
	var placementName string
	var clusterSet1Name string
	var suffix string

	BeforeEach(func(ctx SpecContext) {
		suffix = rand.String(5)
		namespace = fmt.Sprintf("ns-%s", suffix)
		placementName = fmt.Sprintf("placement-%s", suffix)
		clusterSet1Name = fmt.Sprintf("clusterset-%s", suffix)

		// create testing namespace
		ns := &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name: namespace,
			},
		}
		_, err := kubeClient.CoreV1().Namespaces().Create(ctx, ns, metav1.CreateOptions{})
		Expect(err).ToNot(HaveOccurred())
	})

	AfterEach(func(ctx SpecContext) {
		err := kubeClient.CoreV1().Namespaces().Delete(ctx, namespace, metav1.DeleteOptions{})
		Expect(err).ToNot(HaveOccurred())

		assertCleanupClusters(ctx)
	})

	Context("Debug Endpoint Permission Check with Real RBAC", func() {
		var serviceAccountName string
		var roleName string
		var roleBindingName string

		BeforeEach(func() {
			serviceAccountName = fmt.Sprintf("test-sa-%s", suffix)
			roleName = fmt.Sprintf("test-role-%s", suffix)
			roleBindingName = fmt.Sprintf("test-rb-%s", suffix)
		})

		AfterEach(func(ctx SpecContext) {
			By("Cleanup RBAC resources")
			_ = kubeClient.CoreV1().ServiceAccounts(namespace).Delete(ctx, serviceAccountName, metav1.DeleteOptions{})
			_ = kubeClient.RbacV1().Roles(namespace).Delete(ctx, roleName, metav1.DeleteOptions{})
			_ = kubeClient.RbacV1().RoleBindings(namespace).Delete(ctx, roleBindingName, metav1.DeleteOptions{})
			_ = clusterClient.ClusterV1beta1().Placements(namespace).Delete(ctx, placementName, metav1.DeleteOptions{})
		})

		It("Should allow request with valid ServiceAccount token and proper RBAC", func(ctx SpecContext) {
			// Create ClusterSet, binding and clusters
			assertBindingClusterSet(ctx, clusterSet1Name, namespace)
			assertCreatingClusters(ctx, clusterSet1Name, 2)

			// Create placement
			placement := testinghelpers.NewPlacement(namespace, placementName).
				WithClusterSets(clusterSet1Name).
				Build()
			assertCreatingPlacement(ctx, placement)

			// Create ServiceAccount
			sa := &corev1.ServiceAccount{
				ObjectMeta: metav1.ObjectMeta{
					Name:      serviceAccountName,
					Namespace: namespace,
				},
			}
			_, err := kubeClient.CoreV1().ServiceAccounts(namespace).Create(ctx, sa, metav1.CreateOptions{})
			Expect(err).ToNot(HaveOccurred())

			// Create Role with permission to get placements (GET debug endpoint checks get verb)
			role := &rbacv1.Role{
				ObjectMeta: metav1.ObjectMeta{
					Name:      roleName,
					Namespace: namespace,
				},
				Rules: []rbacv1.PolicyRule{
					{
						APIGroups: []string{"cluster.open-cluster-management.io"},
						Resources: []string{"placements"},
						Verbs:     []string{"get"},
					},
				},
			}
			_, err = kubeClient.RbacV1().Roles(namespace).Create(ctx, role, metav1.CreateOptions{})
			Expect(err).ToNot(HaveOccurred())

			// Create RoleBinding
			roleBinding := &rbacv1.RoleBinding{
				ObjectMeta: metav1.ObjectMeta{
					Name:      roleBindingName,
					Namespace: namespace,
				},
				Subjects: []rbacv1.Subject{
					{
						Kind:      "ServiceAccount",
						Name:      serviceAccountName,
						Namespace: namespace,
					},
				},
				RoleRef: rbacv1.RoleRef{
					APIGroup: "rbac.authorization.k8s.io",
					Kind:     "Role",
					Name:     roleName,
				},
			}
			_, err = kubeClient.RbacV1().RoleBindings(namespace).Create(ctx, roleBinding, metav1.CreateOptions{})
			Expect(err).ToNot(HaveOccurred())

			// Create debugger using helper function
			runCtx, stopFunc := context.WithCancel(context.Background())
			defer stopFunc()

			debug, clusterInformers, err := controllers.NewDebuggerWithInformers(runCtx, restConfig)
			Expect(err).ToNot(HaveOccurred())

			// Start informers and wait for cache sync
			clusterInformers.Start(runCtx.Done())
			Expect(cache.WaitForCacheSync(runCtx.Done(),
				clusterInformers.Cluster().V1beta1().Placements().Informer().HasSynced,
				clusterInformers.Cluster().V1beta1().PlacementDecisions().Informer().HasSynced,
				clusterInformers.Cluster().V1().ManagedClusters().Informer().HasSynced,
				clusterInformers.Cluster().V1beta2().ManagedClusterSets().Informer().HasSynced,
				clusterInformers.Cluster().V1beta2().ManagedClusterSetBindings().Informer().HasSynced,
				clusterInformers.Cluster().V1alpha1().AddOnPlacementScores().Informer().HasSynced,
			)).To(BeTrue())

			// Create httptest server with handler that injects user info into context
			By("Create test server with user context injection")
			handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				// Inject user info into request context (simulating GenericAPIServer authentication)
				userInfo := &user.DefaultInfo{
					Name: fmt.Sprintf("system:serviceaccount:%s:%s", namespace, serviceAccountName),
					Groups: []string{
						"system:serviceaccounts",
						fmt.Sprintf("system:serviceaccounts:%s", namespace),
					},
				}
				ctx := request.WithUser(r.Context(), userInfo)
				r = r.WithContext(ctx)
				debug.Handler(w, r)
			})
			server := httptest.NewServer(handler)
			defer server.Close()

			// Send request without Authorization header (user info is injected by handler)
			By("Send GET request with user context")
			req, err := http.NewRequest("GET", fmt.Sprintf("%s%s%s/%s", server.URL, debugger.DebugPath, namespace, placementName), nil)
			Expect(err).ToNot(HaveOccurred())

			resp, err := http.DefaultClient.Do(req)
			Expect(err).ToNot(HaveOccurred())
			defer resp.Body.Close()

			// Verify response - should succeed
			var result debugger.DebugResult
			err = json.NewDecoder(resp.Body).Decode(&result)
			Expect(err).ToNot(HaveOccurred())

			By("Verify request was authorized")
			Expect(result.Error).To(BeEmpty(), "Should not have permission error with valid user and RBAC")
			Expect(result.Placement).ToNot(BeNil())
		})

		It("Should reject request when ServiceAccount lacks RBAC permission", func(ctx SpecContext) {
			// Create ClusterSet, binding and clusters
			assertBindingClusterSet(ctx, clusterSet1Name, namespace)
			assertCreatingClusters(ctx, clusterSet1Name, 2)

			// Create placement
			placement := testinghelpers.NewPlacement(namespace, placementName).
				WithClusterSets(clusterSet1Name).
				Build()
			assertCreatingPlacement(ctx, placement)

			// Create ServiceAccount WITHOUT any RBAC permissions
			sa := &corev1.ServiceAccount{
				ObjectMeta: metav1.ObjectMeta{
					Name:      serviceAccountName,
					Namespace: namespace,
				},
			}
			_, err := kubeClient.CoreV1().ServiceAccounts(namespace).Create(ctx, sa, metav1.CreateOptions{})
			Expect(err).ToNot(HaveOccurred())

			// Create debugger using helper function
			runCtx, stopFunc := context.WithCancel(context.Background())
			defer stopFunc()

			debug, clusterInformers, err := controllers.NewDebuggerWithInformers(runCtx, restConfig)
			Expect(err).ToNot(HaveOccurred())

			// Start informers and wait for cache sync
			clusterInformers.Start(runCtx.Done())
			Expect(cache.WaitForCacheSync(runCtx.Done(),
				clusterInformers.Cluster().V1beta1().Placements().Informer().HasSynced,
				clusterInformers.Cluster().V1beta1().PlacementDecisions().Informer().HasSynced,
				clusterInformers.Cluster().V1().ManagedClusters().Informer().HasSynced,
				clusterInformers.Cluster().V1beta2().ManagedClusterSets().Informer().HasSynced,
				clusterInformers.Cluster().V1beta2().ManagedClusterSetBindings().Informer().HasSynced,
				clusterInformers.Cluster().V1alpha1().AddOnPlacementScores().Informer().HasSynced,
			)).To(BeTrue())

			// Create httptest server with handler that injects user info into context
			By("Create test server with user context injection")
			handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				// Inject user info into request context (simulating GenericAPIServer authentication)
				userInfo := &user.DefaultInfo{
					Name: fmt.Sprintf("system:serviceaccount:%s:%s", namespace, serviceAccountName),
					Groups: []string{
						"system:serviceaccounts",
						fmt.Sprintf("system:serviceaccounts:%s", namespace),
					},
				}
				ctx := request.WithUser(r.Context(), userInfo)
				r = r.WithContext(ctx)
				debug.Handler(w, r)
			})
			server := httptest.NewServer(handler)
			defer server.Close()

			// Send request (user has no RBAC permission)
			By("Send GET request with user lacking RBAC permission")
			req, err := http.NewRequest("GET", fmt.Sprintf("%s%s%s/%s", server.URL, debugger.DebugPath, namespace, placementName), nil)
			Expect(err).ToNot(HaveOccurred())

			resp, err := http.DefaultClient.Do(req)
			Expect(err).ToNot(HaveOccurred())
			defer resp.Body.Close()

			// Verify response - should be rejected
			var result debugger.DebugResult
			err = json.NewDecoder(resp.Body).Decode(&result)
			Expect(err).ToNot(HaveOccurred())

			By("Verify request was rejected due to lack of permission")
			Expect(resp.StatusCode).To(Equal(http.StatusForbidden))
			Expect(result.Error).ToNot(BeEmpty(), "Should have permission error")
			Expect(result.Error).To(ContainSubstring("does not have permission to get placements"))
		})

		It("Should accept POST request with placement JSON and valid token", func(ctx SpecContext) {
			// Create ClusterSet, binding and clusters (no placement in cluster)
			assertBindingClusterSet(ctx, clusterSet1Name, namespace)
			assertCreatingClusters(ctx, clusterSet1Name, 3)

			// Create ServiceAccount
			sa := &corev1.ServiceAccount{
				ObjectMeta: metav1.ObjectMeta{
					Name:      serviceAccountName,
					Namespace: namespace,
				},
			}
			_, err := kubeClient.CoreV1().ServiceAccounts(namespace).Create(ctx, sa, metav1.CreateOptions{})
			Expect(err).ToNot(HaveOccurred())

			// Create Role with permission to create placements
			role := &rbacv1.Role{
				ObjectMeta: metav1.ObjectMeta{
					Name:      roleName,
					Namespace: namespace,
				},
				Rules: []rbacv1.PolicyRule{
					{
						APIGroups: []string{"cluster.open-cluster-management.io"},
						Resources: []string{"placements"},
						Verbs:     []string{"create"},
					},
				},
			}
			_, err = kubeClient.RbacV1().Roles(namespace).Create(ctx, role, metav1.CreateOptions{})
			Expect(err).ToNot(HaveOccurred())

			// Create RoleBinding
			roleBinding := &rbacv1.RoleBinding{
				ObjectMeta: metav1.ObjectMeta{
					Name:      roleBindingName,
					Namespace: namespace,
				},
				Subjects: []rbacv1.Subject{
					{
						Kind:      "ServiceAccount",
						Name:      serviceAccountName,
						Namespace: namespace,
					},
				},
				RoleRef: rbacv1.RoleRef{
					APIGroup: "rbac.authorization.k8s.io",
					Kind:     "Role",
					Name:     roleName,
				},
			}
			_, err = kubeClient.RbacV1().RoleBindings(namespace).Create(ctx, roleBinding, metav1.CreateOptions{})
			Expect(err).ToNot(HaveOccurred())

			// Create debugger using helper function
			runCtx, stopFunc := context.WithCancel(context.Background())
			defer stopFunc()

			debug, clusterInformers, err := controllers.NewDebuggerWithInformers(runCtx, restConfig)
			Expect(err).ToNot(HaveOccurred())

			// Start informers and wait for cache sync
			clusterInformers.Start(runCtx.Done())
			Expect(cache.WaitForCacheSync(runCtx.Done(),
				clusterInformers.Cluster().V1beta1().Placements().Informer().HasSynced,
				clusterInformers.Cluster().V1beta1().PlacementDecisions().Informer().HasSynced,
				clusterInformers.Cluster().V1().ManagedClusters().Informer().HasSynced,
				clusterInformers.Cluster().V1beta2().ManagedClusterSets().Informer().HasSynced,
				clusterInformers.Cluster().V1beta2().ManagedClusterSetBindings().Informer().HasSynced,
				clusterInformers.Cluster().V1alpha1().AddOnPlacementScores().Informer().HasSynced,
			)).To(BeTrue())

			// Create httptest server with handler that injects user info into context
			By("Create test server with user context injection")
			handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				// Inject user info into request context (simulating GenericAPIServer authentication)
				userInfo := &user.DefaultInfo{
					Name: fmt.Sprintf("system:serviceaccount:%s:%s", namespace, serviceAccountName),
					Groups: []string{
						"system:serviceaccounts",
						fmt.Sprintf("system:serviceaccounts:%s", namespace),
					},
				}
				ctx := request.WithUser(r.Context(), userInfo)
				r = r.WithContext(ctx)
				debug.Handler(w, r)
			})
			server := httptest.NewServer(handler)
			defer server.Close()

			// Prepare placement JSON for POST request
			By("Prepare placement JSON for POST request")
			placement := testinghelpers.NewPlacement(namespace, placementName).
				WithClusterSets(clusterSet1Name).
				WithNOC(2).
				Build()
			placementJSON, err := json.Marshal(placement)
			Expect(err).ToNot(HaveOccurred())

			// Send POST request
			By("Send POST request with placement JSON and user context")
			req, err := http.NewRequest("POST", server.URL+debugger.DebugPath, bytes.NewBuffer(placementJSON))
			Expect(err).ToNot(HaveOccurred())
			req.Header.Set("Content-Type", "application/json")

			resp, err := http.DefaultClient.Do(req)
			Expect(err).ToNot(HaveOccurred())
			defer resp.Body.Close()

			// Verify response
			var result debugger.DebugResult
			err = json.NewDecoder(resp.Body).Decode(&result)
			Expect(err).ToNot(HaveOccurred())

			By("Verify POST request was successful")
			Expect(result.Error).To(BeEmpty(), "Should not have error with valid user and RBAC")
			Expect(result.Placement).ToNot(BeNil())
			Expect(result.Placement.Name).To(Equal(placementName))
			Expect(result.Placement.Namespace).To(Equal(namespace))
			Expect(result.FilterResults).ToNot(BeEmpty(), "Should return filter results")
		})
	})
})
