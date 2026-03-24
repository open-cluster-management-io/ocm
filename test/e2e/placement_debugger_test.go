package e2e

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"time"

	"github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"
	authv1 "k8s.io/api/authentication/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/rand"

	clusterapiv1 "open-cluster-management.io/api/cluster/v1"
	clusterapiv1beta1 "open-cluster-management.io/api/cluster/v1beta1"
	clusterapiv1beta2 "open-cluster-management.io/api/cluster/v1beta2"

	"open-cluster-management.io/ocm/test/framework"
)

const (
	placementDebuggerTestLabel       = "placement-debugger-e2e-test"
	placementDebuggerServiceName     = "cluster-manager-placement"
	placementDebuggerServicePort     = 9443
	placementDebuggerHubNamespace    = "open-cluster-management-hub"
	placementDebuggerTimeout         = 60 * time.Second
	placementDebuggerPollingInterval = 1 * time.Second
)

type DebugResponse struct {
	Placement               *clusterapiv1beta1.Placement `json:"placement,omitempty"`
	FilteredPipelineResults []interface{}                `json:"filteredPipelineResults,omitempty"`
	PrioritizeResults       []interface{}                `json:"prioritizeResults,omitempty"`
	AggregatedScores        []interface{}                `json:"aggregatedScores,omitempty"`
	Error                   string                       `json:"error,omitempty"`
}

var _ = ginkgo.Describe("PlacementDebugger", ginkgo.Label("placement-debugger", "sanity-check"), func() {
	var namespace string
	var placementName string
	var clusterSetName string
	var clusterName string
	var serviceAccountName string
	var suffix string
	var token string

	ginkgo.BeforeEach(func() {
		suffix = rand.String(5)
		namespace = fmt.Sprintf("debugger-test-%s", suffix)
		placementName = fmt.Sprintf("test-placement-%s", suffix)
		clusterSetName = fmt.Sprintf("test-clusterset-%s", suffix)
		clusterName = fmt.Sprintf("test-cluster-%s", suffix)
		serviceAccountName = fmt.Sprintf("test-sa-%s", suffix)

		ginkgo.By(fmt.Sprintf("Creating test namespace: %s", namespace))
		ns := &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name: namespace,
				Labels: map[string]string{
					e2eTestLabel: placementDebuggerTestLabel,
				},
			},
		}
		_, err := hub.KubeClient.CoreV1().Namespaces().Create(context.Background(), ns, metav1.CreateOptions{})
		gomega.Expect(err).ToNot(gomega.HaveOccurred())

		ginkgo.By(fmt.Sprintf("Creating ManagedClusterSet: %s", clusterSetName))
		clusterSet := &clusterapiv1beta2.ManagedClusterSet{
			ObjectMeta: metav1.ObjectMeta{
				Name: clusterSetName,
				Labels: map[string]string{
					e2eTestLabel: placementDebuggerTestLabel,
				},
			},
		}
		_, err = hub.ClusterClient.ClusterV1beta2().ManagedClusterSets().Create(context.Background(), clusterSet, metav1.CreateOptions{})
		gomega.Expect(err).ToNot(gomega.HaveOccurred())

		ginkgo.By(fmt.Sprintf("Creating ManagedClusterSetBinding in namespace: %s", namespace))
		binding := &clusterapiv1beta2.ManagedClusterSetBinding{
			ObjectMeta: metav1.ObjectMeta{
				Name:      clusterSetName,
				Namespace: namespace,
			},
			Spec: clusterapiv1beta2.ManagedClusterSetBindingSpec{
				ClusterSet: clusterSetName,
			},
		}
		_, err = hub.ClusterClient.ClusterV1beta2().ManagedClusterSetBindings(namespace).Create(context.Background(), binding, metav1.CreateOptions{})
		gomega.Expect(err).ToNot(gomega.HaveOccurred())

		ginkgo.By(fmt.Sprintf("Creating ManagedCluster: %s", clusterName))
		cluster := &clusterapiv1.ManagedCluster{
			ObjectMeta: metav1.ObjectMeta{
				Name: clusterName,
				Labels: map[string]string{
					e2eTestLabel: placementDebuggerTestLabel,
					"cluster.open-cluster-management.io/clusterset": clusterSetName,
					"env": "test",
				},
			},
			Spec: clusterapiv1.ManagedClusterSpec{
				HubAcceptsClient: true,
			},
		}
		_, err = hub.ClusterClient.ClusterV1().ManagedClusters().Create(context.Background(), cluster, metav1.CreateOptions{})
		gomega.Expect(err).ToNot(gomega.HaveOccurred())

		ginkgo.By(fmt.Sprintf("Creating Placement: %s", placementName))
		placement := &clusterapiv1beta1.Placement{
			ObjectMeta: metav1.ObjectMeta{
				Name:      placementName,
				Namespace: namespace,
			},
			Spec: clusterapiv1beta1.PlacementSpec{
				ClusterSets: []string{clusterSetName},
				Tolerations: []clusterapiv1beta1.Toleration{
					{
						Key:      "cluster.open-cluster-management.io/unreachable",
						Operator: clusterapiv1beta1.TolerationOpExists,
					},
					{
						Key:      "cluster.open-cluster-management.io/unavailable",
						Operator: clusterapiv1beta1.TolerationOpExists,
					},
				},
			},
		}
		_, err = hub.ClusterClient.ClusterV1beta1().Placements(namespace).Create(context.Background(), placement, metav1.CreateOptions{})
		gomega.Expect(err).ToNot(gomega.HaveOccurred())

		ginkgo.By(fmt.Sprintf("Creating ServiceAccount: %s", serviceAccountName))
		sa := &corev1.ServiceAccount{
			ObjectMeta: metav1.ObjectMeta{
				Name:      serviceAccountName,
				Namespace: namespace,
			},
		}
		_, err = hub.KubeClient.CoreV1().ServiceAccounts(namespace).Create(context.Background(), sa, metav1.CreateOptions{})
		gomega.Expect(err).ToNot(gomega.HaveOccurred())

		ginkgo.By("Creating Role with placement permissions")
		role := &rbacv1.Role{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "placement-reader",
				Namespace: namespace,
			},
			Rules: []rbacv1.PolicyRule{
				{
					APIGroups: []string{"cluster.open-cluster-management.io"},
					Resources: []string{"placements"},
					Verbs:     []string{"create", "get"},
				},
			},
		}
		_, err = hub.KubeClient.RbacV1().Roles(namespace).Create(context.Background(), role, metav1.CreateOptions{})
		gomega.Expect(err).ToNot(gomega.HaveOccurred())

		ginkgo.By("Creating RoleBinding")
		roleBinding := &rbacv1.RoleBinding{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "placement-reader-binding",
				Namespace: namespace,
			},
			RoleRef: rbacv1.RoleRef{
				APIGroup: "rbac.authorization.k8s.io",
				Kind:     "Role",
				Name:     "placement-reader",
			},
			Subjects: []rbacv1.Subject{
				{
					Kind:      "ServiceAccount",
					Name:      serviceAccountName,
					Namespace: namespace,
				},
			},
		}
		_, err = hub.KubeClient.RbacV1().RoleBindings(namespace).Create(context.Background(), roleBinding, metav1.CreateOptions{})
		gomega.Expect(err).ToNot(gomega.HaveOccurred())

		ginkgo.By("Creating ClusterRole for debug path access")
		clusterRoleName := fmt.Sprintf("debug-access-%s", namespace)
		clusterRole := &rbacv1.ClusterRole{
			ObjectMeta: metav1.ObjectMeta{
				Name: clusterRoleName,
				Labels: map[string]string{
					e2eTestLabel: placementDebuggerTestLabel,
				},
			},
			Rules: []rbacv1.PolicyRule{
				{
					NonResourceURLs: []string{"/debug/placements/*"},
					Verbs:           []string{"get", "post"},
				},
			},
		}
		_, err = hub.KubeClient.RbacV1().ClusterRoles().Create(context.Background(), clusterRole, metav1.CreateOptions{})
		gomega.Expect(err).ToNot(gomega.HaveOccurred())

		ginkgo.By("Creating ClusterRoleBinding")
		clusterRoleBinding := &rbacv1.ClusterRoleBinding{
			ObjectMeta: metav1.ObjectMeta{
				Name: fmt.Sprintf("debug-access-%s-binding", namespace),
				Labels: map[string]string{
					e2eTestLabel: placementDebuggerTestLabel,
				},
			},
			RoleRef: rbacv1.RoleRef{
				APIGroup: "rbac.authorization.k8s.io",
				Kind:     "ClusterRole",
				Name:     clusterRoleName,
			},
			Subjects: []rbacv1.Subject{
				{
					Kind:      "ServiceAccount",
					Name:      serviceAccountName,
					Namespace: namespace,
				},
			},
		}
		_, err = hub.KubeClient.RbacV1().ClusterRoleBindings().Create(context.Background(), clusterRoleBinding, metav1.CreateOptions{})
		gomega.Expect(err).ToNot(gomega.HaveOccurred())

		ginkgo.By("Creating ServiceAccount token")
		tokenRequest := &authv1.TokenRequest{
			Spec: authv1.TokenRequestSpec{
				ExpirationSeconds: func(i int64) *int64 { return &i }(3600),
			},
		}
		tokenResp, err := hub.KubeClient.CoreV1().ServiceAccounts(namespace).CreateToken(
			context.Background(),
			serviceAccountName,
			tokenRequest,
			metav1.CreateOptions{},
		)
		gomega.Expect(err).ToNot(gomega.HaveOccurred())
		token = tokenResp.Status.Token
		gomega.Expect(token).ToNot(gomega.BeEmpty())

		// Wait a bit for RBAC to propagate
		time.Sleep(5 * time.Second)
	})

	ginkgo.AfterEach(func() {
		ginkgo.By("Cleaning up test resources")

		// Delete namespace
		err := hub.KubeClient.CoreV1().Namespaces().Delete(context.Background(), namespace, metav1.DeleteOptions{})
		if err != nil && !errors.IsNotFound(err) {
			ginkgo.GinkgoLogr.Error(err, "Failed to delete namespace", "namespace", namespace)
		}

		// Delete ManagedCluster
		err = hub.ClusterClient.ClusterV1().ManagedClusters().Delete(context.Background(), clusterName, metav1.DeleteOptions{})
		if err != nil && !errors.IsNotFound(err) {
			ginkgo.GinkgoLogr.Error(err, "Failed to delete ManagedCluster", "cluster", clusterName)
		}

		// Delete ManagedClusterSet
		err = hub.ClusterClient.ClusterV1beta2().ManagedClusterSets().Delete(context.Background(), clusterSetName, metav1.DeleteOptions{})
		if err != nil && !errors.IsNotFound(err) {
			ginkgo.GinkgoLogr.Error(err, "Failed to delete ManagedClusterSet", "clusterSet", clusterSetName)
		}

		// Delete ClusterRole and ClusterRoleBinding
		clusterRoleName := fmt.Sprintf("debug-access-%s", namespace)
		err = hub.KubeClient.RbacV1().ClusterRoles().Delete(context.Background(), clusterRoleName, metav1.DeleteOptions{})
		if err != nil && !errors.IsNotFound(err) {
			ginkgo.GinkgoLogr.Error(err, "Failed to delete ClusterRole", "clusterRole", clusterRoleName)
		}

		clusterRoleBindingName := fmt.Sprintf("debug-access-%s-binding", namespace)
		err = hub.KubeClient.RbacV1().ClusterRoleBindings().Delete(context.Background(), clusterRoleBindingName, metav1.DeleteOptions{})
		if err != nil && !errors.IsNotFound(err) {
			ginkgo.GinkgoLogr.Error(err, "Failed to delete ClusterRoleBinding", "clusterRoleBinding", clusterRoleBindingName)
		}
	})

	ginkgo.It("Should debug placement with GET method using curl pod", func() {
		ginkgo.By("Creating a curl pod to test the debugger service")

		debugPath := fmt.Sprintf("/debug/placements/%s/%s", namespace, placementName)
		response := callDebugServiceWithPod(hub, namespace, "GET", debugPath, token, nil)

		ginkgo.By("Validating GET response")
		gomega.Expect(response.Placement).ToNot(gomega.BeNil())
		gomega.Expect(response.Placement.Name).To(gomega.Equal(placementName))
		gomega.Expect(response.Placement.Namespace).To(gomega.Equal(namespace))
		gomega.Expect(response.FilteredPipelineResults).ToNot(gomega.BeNil())
		gomega.Expect(response.Error).To(gomega.BeEmpty())
	})

	ginkgo.It("Should debug placement with POST method using curl pod", func() {
		ginkgo.By("Creating a curl pod to test the debugger service")

		placementPayload := &clusterapiv1beta1.Placement{
			TypeMeta: metav1.TypeMeta{
				APIVersion: "cluster.open-cluster-management.io/v1beta1",
				Kind:       "Placement",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-post-placement",
				Namespace: namespace,
			},
			Spec: clusterapiv1beta1.PlacementSpec{
				ClusterSets: []string{clusterSetName},
				Tolerations: []clusterapiv1beta1.Toleration{
					{
						Key:      "cluster.open-cluster-management.io/unreachable",
						Operator: clusterapiv1beta1.TolerationOpExists,
					},
					{
						Key:      "cluster.open-cluster-management.io/unavailable",
						Operator: clusterapiv1beta1.TolerationOpExists,
					},
				},
			},
		}

		debugPath := "/debug/placements/"
		response := callDebugServiceWithPod(hub, namespace, "POST", debugPath, token, placementPayload)

		ginkgo.By("Validating POST response")
		gomega.Expect(response.Placement).ToNot(gomega.BeNil())
		gomega.Expect(response.Placement.Name).To(gomega.Equal("test-post-placement"))
		gomega.Expect(response.Placement.Namespace).To(gomega.Equal(namespace))
		gomega.Expect(response.FilteredPipelineResults).ToNot(gomega.BeNil())
		gomega.Expect(response.Error).To(gomega.BeEmpty())
	})
})

// callDebugServiceWithPod calls the placement debugger service using a curl pod
func callDebugServiceWithPod(hub *framework.Hub, namespace, method, path, token string, payload interface{}) *DebugResponse {
	serviceURL := fmt.Sprintf("https://%s.%s.svc.cluster.local:%d%s",
		placementDebuggerServiceName,
		placementDebuggerHubNamespace,
		placementDebuggerServicePort,
		path,
	)

	podName := fmt.Sprintf("curl-test-%s", rand.String(5))

	// Build curl command
	curlCmd := []string{
		"curl", "-sk",
		"-H", fmt.Sprintf("Authorization: Bearer %s", token),
	}

	if method == "POST" && payload != nil {
		jsonData, err := json.Marshal(payload)
		gomega.Expect(err).ToNot(gomega.HaveOccurred())
		curlCmd = append(curlCmd,
			"-X", "POST",
			"-H", "Content-Type: application/json",
			"-d", string(jsonData),
		)
	}

	curlCmd = append(curlCmd, serviceURL)

	// Create a pod with curl
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      podName,
			Namespace: namespace,
		},
		Spec: corev1.PodSpec{
			RestartPolicy: corev1.RestartPolicyNever,
			Containers: []corev1.Container{
				{
					Name:    "curl",
					Image:   "curlimages/curl:latest",
					Command: curlCmd,
				},
			},
		},
	}

	ginkgo.By(fmt.Sprintf("Creating curl pod %s in namespace %s", podName, namespace))
	_, err := hub.KubeClient.CoreV1().Pods(namespace).Create(context.Background(), pod, metav1.CreateOptions{})
	gomega.Expect(err).ToNot(gomega.HaveOccurred())

	// Wait for pod to complete
	ginkgo.By("Waiting for curl pod to complete")
	gomega.Eventually(func() corev1.PodPhase {
		p, err := hub.KubeClient.CoreV1().Pods(namespace).Get(context.Background(), podName, metav1.GetOptions{})
		if err != nil {
			return corev1.PodUnknown
		}
		return p.Status.Phase
	}, placementDebuggerTimeout, placementDebuggerPollingInterval).Should(gomega.Or(
		gomega.Equal(corev1.PodSucceeded),
		gomega.Equal(corev1.PodFailed),
	))

	// Get pod logs
	ginkgo.By("Retrieving curl pod logs")
	req := hub.KubeClient.CoreV1().Pods(namespace).GetLogs(podName, &corev1.PodLogOptions{})
	logs, err := req.Stream(context.Background())
	gomega.Expect(err).ToNot(gomega.HaveOccurred())
	defer logs.Close()

	respBody, err := io.ReadAll(logs)
	gomega.Expect(err).ToNot(gomega.HaveOccurred())

	ginkgo.GinkgoLogr.Info("Received response from debugger", "response", string(respBody))

	// Clean up the pod
	err = hub.KubeClient.CoreV1().Pods(namespace).Delete(context.Background(), podName, metav1.DeleteOptions{})
	if err != nil && !errors.IsNotFound(err) {
		ginkgo.GinkgoLogr.Error(err, "Failed to delete curl pod", "pod", podName)
	}

	var debugResp DebugResponse
	err = json.Unmarshal(respBody, &debugResp)
	gomega.Expect(err).ToNot(gomega.HaveOccurred())

	return &debugResp
}
