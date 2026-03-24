package placement

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"

	"github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"
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

var _ = ginkgo.Describe("DebugService", func() {
	var namespace string
	var placementName string
	var clusterSet1Name string
	var suffix string

	ginkgo.BeforeEach(func() {
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
		_, err := kubeClient.CoreV1().Namespaces().Create(context.Background(), ns, metav1.CreateOptions{})
		gomega.Expect(err).ToNot(gomega.HaveOccurred())
	})

	ginkgo.AfterEach(func() {
		err := kubeClient.CoreV1().Namespaces().Delete(context.Background(), namespace, metav1.DeleteOptions{})
		gomega.Expect(err).ToNot(gomega.HaveOccurred())

		assertCleanupClusters()
	})

	ginkgo.Context("Debug Endpoint Permission Check with Real RBAC", func() {
		var serviceAccountName string
		var roleName string
		var roleBindingName string

		ginkgo.BeforeEach(func() {
			serviceAccountName = fmt.Sprintf("test-sa-%s", suffix)
			roleName = fmt.Sprintf("test-role-%s", suffix)
			roleBindingName = fmt.Sprintf("test-rb-%s", suffix)
		})

		ginkgo.AfterEach(func() {
			ginkgo.By("Cleanup RBAC resources")
			_ = kubeClient.CoreV1().ServiceAccounts(namespace).Delete(context.Background(), serviceAccountName, metav1.DeleteOptions{})
			_ = kubeClient.RbacV1().Roles(namespace).Delete(context.Background(), roleName, metav1.DeleteOptions{})
			_ = kubeClient.RbacV1().RoleBindings(namespace).Delete(context.Background(), roleBindingName, metav1.DeleteOptions{})
			_ = clusterClient.ClusterV1beta1().Placements(namespace).Delete(context.Background(), placementName, metav1.DeleteOptions{})
		})

		ginkgo.It("Should allow request with valid ServiceAccount token and proper RBAC", func() {
			// Create ClusterSet, binding and clusters
			assertBindingClusterSet(clusterSet1Name, namespace)
			assertCreatingClusters(clusterSet1Name, 2)

			// Create placement
			placement := testinghelpers.NewPlacement(namespace, placementName).
				WithClusterSets(clusterSet1Name).
				Build()
			assertCreatingPlacement(placement)

			// Create ServiceAccount
			sa := &corev1.ServiceAccount{
				ObjectMeta: metav1.ObjectMeta{
					Name:      serviceAccountName,
					Namespace: namespace,
				},
			}
			_, err := kubeClient.CoreV1().ServiceAccounts(namespace).Create(context.Background(), sa, metav1.CreateOptions{})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())

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
			_, err = kubeClient.RbacV1().Roles(namespace).Create(context.Background(), role, metav1.CreateOptions{})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())

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
			_, err = kubeClient.RbacV1().RoleBindings(namespace).Create(context.Background(), roleBinding, metav1.CreateOptions{})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())

			// Create debugger using helper function
			ctx, stopFunc := context.WithCancel(context.Background())
			defer stopFunc()

			debug, clusterInformers, err := controllers.NewDebuggerWithInformers(ctx, restConfig)
			gomega.Expect(err).ToNot(gomega.HaveOccurred())

			// Start informers and wait for cache sync
			clusterInformers.Start(ctx.Done())
			gomega.Expect(cache.WaitForCacheSync(ctx.Done(),
				clusterInformers.Cluster().V1beta1().Placements().Informer().HasSynced,
				clusterInformers.Cluster().V1beta1().PlacementDecisions().Informer().HasSynced,
				clusterInformers.Cluster().V1().ManagedClusters().Informer().HasSynced,
				clusterInformers.Cluster().V1beta2().ManagedClusterSets().Informer().HasSynced,
				clusterInformers.Cluster().V1beta2().ManagedClusterSetBindings().Informer().HasSynced,
				clusterInformers.Cluster().V1alpha1().AddOnPlacementScores().Informer().HasSynced,
			)).To(gomega.BeTrue())

			// Create httptest server with handler that injects user info into context
			ginkgo.By("Create test server with user context injection")
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
			ginkgo.By("Send GET request with user context")
			req, err := http.NewRequest("GET", fmt.Sprintf("%s%s%s/%s", server.URL, debugger.DebugPath, namespace, placementName), nil)
			gomega.Expect(err).ToNot(gomega.HaveOccurred())

			resp, err := http.DefaultClient.Do(req)
			gomega.Expect(err).ToNot(gomega.HaveOccurred())
			defer resp.Body.Close()

			// Verify response - should succeed
			var result debugger.DebugResult
			err = json.NewDecoder(resp.Body).Decode(&result)
			gomega.Expect(err).ToNot(gomega.HaveOccurred())

			ginkgo.By("Verify request was authorized")
			gomega.Expect(result.Error).To(gomega.BeEmpty(), "Should not have permission error with valid user and RBAC")
			gomega.Expect(result.Placement).ToNot(gomega.BeNil())
		})

		ginkgo.It("Should reject request when ServiceAccount lacks RBAC permission", func() {
			// Create ClusterSet, binding and clusters
			assertBindingClusterSet(clusterSet1Name, namespace)
			assertCreatingClusters(clusterSet1Name, 2)

			// Create placement
			placement := testinghelpers.NewPlacement(namespace, placementName).
				WithClusterSets(clusterSet1Name).
				Build()
			assertCreatingPlacement(placement)

			// Create ServiceAccount WITHOUT any RBAC permissions
			sa := &corev1.ServiceAccount{
				ObjectMeta: metav1.ObjectMeta{
					Name:      serviceAccountName,
					Namespace: namespace,
				},
			}
			_, err := kubeClient.CoreV1().ServiceAccounts(namespace).Create(context.Background(), sa, metav1.CreateOptions{})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())

			// Create debugger using helper function
			ctx, stopFunc := context.WithCancel(context.Background())
			defer stopFunc()

			debug, clusterInformers, err := controllers.NewDebuggerWithInformers(ctx, restConfig)
			gomega.Expect(err).ToNot(gomega.HaveOccurred())

			// Start informers and wait for cache sync
			clusterInformers.Start(ctx.Done())
			gomega.Expect(cache.WaitForCacheSync(ctx.Done(),
				clusterInformers.Cluster().V1beta1().Placements().Informer().HasSynced,
				clusterInformers.Cluster().V1beta1().PlacementDecisions().Informer().HasSynced,
				clusterInformers.Cluster().V1().ManagedClusters().Informer().HasSynced,
				clusterInformers.Cluster().V1beta2().ManagedClusterSets().Informer().HasSynced,
				clusterInformers.Cluster().V1beta2().ManagedClusterSetBindings().Informer().HasSynced,
				clusterInformers.Cluster().V1alpha1().AddOnPlacementScores().Informer().HasSynced,
			)).To(gomega.BeTrue())

			// Create httptest server with handler that injects user info into context
			ginkgo.By("Create test server with user context injection")
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
			ginkgo.By("Send GET request with user lacking RBAC permission")
			req, err := http.NewRequest("GET", fmt.Sprintf("%s%s%s/%s", server.URL, debugger.DebugPath, namespace, placementName), nil)
			gomega.Expect(err).ToNot(gomega.HaveOccurred())

			resp, err := http.DefaultClient.Do(req)
			gomega.Expect(err).ToNot(gomega.HaveOccurred())
			defer resp.Body.Close()

			// Verify response - should be rejected
			var result debugger.DebugResult
			err = json.NewDecoder(resp.Body).Decode(&result)
			gomega.Expect(err).ToNot(gomega.HaveOccurred())

			ginkgo.By("Verify request was rejected due to lack of permission")
			gomega.Expect(result.Error).ToNot(gomega.BeEmpty(), "Should have permission error")
			gomega.Expect(result.Error).To(gomega.ContainSubstring("does not have permission"))
		})

		ginkgo.It("Should accept POST request with placement JSON and valid token", func() {
			// Create ClusterSet, binding and clusters (no placement in cluster)
			assertBindingClusterSet(clusterSet1Name, namespace)
			assertCreatingClusters(clusterSet1Name, 3)

			// Create ServiceAccount
			sa := &corev1.ServiceAccount{
				ObjectMeta: metav1.ObjectMeta{
					Name:      serviceAccountName,
					Namespace: namespace,
				},
			}
			_, err := kubeClient.CoreV1().ServiceAccounts(namespace).Create(context.Background(), sa, metav1.CreateOptions{})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())

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
			_, err = kubeClient.RbacV1().Roles(namespace).Create(context.Background(), role, metav1.CreateOptions{})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())

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
			_, err = kubeClient.RbacV1().RoleBindings(namespace).Create(context.Background(), roleBinding, metav1.CreateOptions{})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())

			// Create debugger using helper function
			ctx, stopFunc := context.WithCancel(context.Background())
			defer stopFunc()

			debug, clusterInformers, err := controllers.NewDebuggerWithInformers(ctx, restConfig)
			gomega.Expect(err).ToNot(gomega.HaveOccurred())

			// Start informers and wait for cache sync
			clusterInformers.Start(ctx.Done())
			gomega.Expect(cache.WaitForCacheSync(ctx.Done(),
				clusterInformers.Cluster().V1beta1().Placements().Informer().HasSynced,
				clusterInformers.Cluster().V1beta1().PlacementDecisions().Informer().HasSynced,
				clusterInformers.Cluster().V1().ManagedClusters().Informer().HasSynced,
				clusterInformers.Cluster().V1beta2().ManagedClusterSets().Informer().HasSynced,
				clusterInformers.Cluster().V1beta2().ManagedClusterSetBindings().Informer().HasSynced,
				clusterInformers.Cluster().V1alpha1().AddOnPlacementScores().Informer().HasSynced,
			)).To(gomega.BeTrue())

			// Create httptest server with handler that injects user info into context
			ginkgo.By("Create test server with user context injection")
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
			ginkgo.By("Prepare placement JSON for POST request")
			placement := testinghelpers.NewPlacement(namespace, placementName).
				WithClusterSets(clusterSet1Name).
				WithNOC(2).
				Build()
			placementJSON, err := json.Marshal(placement)
			gomega.Expect(err).ToNot(gomega.HaveOccurred())

			// Send POST request
			ginkgo.By("Send POST request with placement JSON and user context")
			req, err := http.NewRequest("POST", server.URL+debugger.DebugPath, bytes.NewBuffer(placementJSON))
			gomega.Expect(err).ToNot(gomega.HaveOccurred())
			req.Header.Set("Content-Type", "application/json")

			resp, err := http.DefaultClient.Do(req)
			gomega.Expect(err).ToNot(gomega.HaveOccurred())
			defer resp.Body.Close()

			// Verify response
			var result debugger.DebugResult
			err = json.NewDecoder(resp.Body).Decode(&result)
			gomega.Expect(err).ToNot(gomega.HaveOccurred())

			ginkgo.By("Verify POST request was successful")
			gomega.Expect(result.Error).To(gomega.BeEmpty(), "Should not have error with valid user and RBAC")
			gomega.Expect(result.Placement).ToNot(gomega.BeNil())
			gomega.Expect(result.Placement.Name).To(gomega.Equal(placementName))
			gomega.Expect(result.Placement.Namespace).To(gomega.Equal(namespace))
			gomega.Expect(result.FilterResults).ToNot(gomega.BeEmpty(), "Should return filter results")
		})
	})
})
