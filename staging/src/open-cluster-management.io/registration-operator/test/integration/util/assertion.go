package util

import (
	"context"
	"fmt"

	"github.com/onsi/gomega"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	operatorclientset "open-cluster-management.io/api/client/operator/clientset/versioned"
)

const (
	eventuallyTimeout  = 60 // seconds
	eventuallyInterval = 1  // seconds
)

func AssertKlusterletCondition(
	name string, operatorClient operatorclientset.Interface, expectedType, expectedReason string, expectedWorkStatus metav1.ConditionStatus) {
	gomega.Eventually(func() error {
		klusterlet, err := operatorClient.OperatorV1().Klusterlets().Get(context.Background(), name, metav1.GetOptions{})
		if err != nil {
			return err
		}

		// check work status condition
		if !HasCondition(klusterlet.Status.Conditions, expectedType, expectedReason, expectedWorkStatus) {
			return fmt.Errorf("expect have type %s with reason %s and status %s, but got %v",
				expectedType, expectedReason, expectedWorkStatus, klusterlet.Status.Conditions)
		}
		return nil
	}, eventuallyTimeout, eventuallyInterval).ShouldNot(gomega.HaveOccurred())
}

func AssertClusterManagerCondition(
	name string, operatorClient operatorclientset.Interface, expectedType, expectedReason string, expectedWorkStatus metav1.ConditionStatus) {
	gomega.Eventually(func() error {
		clusterManager, err := operatorClient.OperatorV1().ClusterManagers().Get(context.Background(), name, metav1.GetOptions{})
		if err != nil {
			return err
		}

		// check work status condition
		if !HasCondition(clusterManager.Status.Conditions, expectedType, expectedReason, expectedWorkStatus) {
			return fmt.Errorf("expect have type %s with reason %s and status %s, but got %v",
				expectedType, expectedReason, expectedWorkStatus, clusterManager.Status.Conditions)
		}
		return nil
	}, eventuallyTimeout, eventuallyInterval).ShouldNot(gomega.HaveOccurred())
}
