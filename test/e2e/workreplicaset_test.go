package e2e

import (
	"context"
	"fmt"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/util/rand"

	operatorapiv1 "open-cluster-management.io/api/operator/v1"
)

var _ = Describe("Enable ManifestWorkReplicaSet feature gate", func() {
	var klusterletName, clusterName string

	BeforeEach(func() {
		klusterletName = fmt.Sprintf("e2e-klusterlet-%s", rand.String(6))
		clusterName = fmt.Sprintf("e2e-managedcluster-%s", rand.String(6))

		// enable addon management feature gate
		Eventually(func() error {
			clusterManager, err := t.OperatorClient.OperatorV1().ClusterManagers().Get(context.TODO(), "cluster-manager", metav1.GetOptions{})
			if err != nil {
				return err
			}
			clusterManager.Spec.WorkConfiguration = &operatorapiv1.WorkConfiguration{
				FeatureGates: []operatorapiv1.FeatureGate{
					{
						Feature: "ManifestWorkReplicaSet",
						Mode:    operatorapiv1.FeatureGateModeTypeEnable,
					},
				},
			}
			_, err = t.OperatorClient.OperatorV1().ClusterManagers().Update(context.TODO(), clusterManager, metav1.UpdateOptions{})
			return err
		}, t.EventuallyTimeout*5, t.EventuallyInterval*5).Should(Succeed())

		// the work controller deployment should be running
		Eventually(func() error {
			hubWorkControllerDeployment, err := t.KubeClient.AppsV1().Deployments(t.clusterManagerNamespace).
				Get(context.TODO(), t.hubWorkControllerDeployment, metav1.GetOptions{})
			if err != nil {
				return err
			}
			replicas := *hubWorkControllerDeployment.Spec.Replicas
			readyReplicas := hubWorkControllerDeployment.Status.ReadyReplicas
			if readyReplicas != replicas {
				return fmt.Errorf("deployment %s should have %d but got %d ready replicas", t.hubWorkControllerDeployment, replicas, readyReplicas)
			}
			return nil
		}, t.EventuallyTimeout*5, t.EventuallyInterval*5).Should(BeNil())

		// feature gate status should be valid
		Eventually(func() error {
			clusterManager, err := t.OperatorClient.OperatorV1().ClusterManagers().Get(context.TODO(), "cluster-manager", metav1.GetOptions{})
			if err != nil {
				return err
			}

			if meta.IsStatusConditionFalse(clusterManager.Status.Conditions, "Applied") {
				return fmt.Errorf("components of cluster manager are not all applied")
			}
			if meta.IsStatusConditionFalse(clusterManager.Status.Conditions, "ValidFeatureGates") {
				return fmt.Errorf("feature gates are not all valid")
			}
			return nil
		}, t.EventuallyTimeout*5, t.EventuallyInterval*5).Should(BeNil())
	})
	AfterEach(func() {
		By(fmt.Sprintf("clean klusterlet %v resources after the test case", klusterletName))
		Expect(t.cleanKlusterletResources(klusterletName, clusterName)).To(BeNil())

		// disable addon management feature gate
		Eventually(func() error {
			clusterManager, err := t.OperatorClient.OperatorV1().ClusterManagers().Get(context.TODO(), "cluster-manager", metav1.GetOptions{})
			if err != nil {
				return err
			}
			clusterManager.Spec.WorkConfiguration = &operatorapiv1.WorkConfiguration{}
			_, err = t.OperatorClient.OperatorV1().ClusterManagers().Update(context.TODO(), clusterManager, metav1.UpdateOptions{})
			return err
		}, t.EventuallyTimeout*5, t.EventuallyInterval*5).Should(Succeed())

	})

	It("Test the work controller", func() {
		// TODO: add more test cases
		return
	})
})
