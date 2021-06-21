package util

import (
	"context"

	"github.com/onsi/gomega"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	operatorclientset "open-cluster-management.io/api/client/operator/clientset/versioned"
)

const (
	eventuallyTimeout  = 30 // seconds
	eventuallyInterval = 1  // seconds
)

func AssertKlusterletCondition(
	name string, operatorClient operatorclientset.Interface, expectedType, expectedReason string, expectedWorkStatus metav1.ConditionStatus) {
	gomega.Eventually(func() bool {
		klusterlet, err := operatorClient.OperatorV1().Klusterlets().Get(context.Background(), name, metav1.GetOptions{})
		if err != nil {
			return false
		}

		// check work status condition
		return HasCondition(klusterlet.Status.Conditions, expectedType, expectedReason, expectedWorkStatus)
	}, eventuallyTimeout, eventuallyInterval).Should(gomega.BeTrue())
}

func AssertClusterManagerCondition(
	name string, operatorClient operatorclientset.Interface, expectedType, expectedReason string, expectedWorkStatus metav1.ConditionStatus) {
	gomega.Eventually(func() bool {
		clusterManager, err := operatorClient.OperatorV1().ClusterManagers().Get(context.Background(), name, metav1.GetOptions{})
		if err != nil {
			return false
		}

		// check work status condition
		return HasCondition(clusterManager.Status.Conditions, expectedType, expectedReason, expectedWorkStatus)
	}, eventuallyTimeout, eventuallyInterval).Should(gomega.BeTrue())
}
