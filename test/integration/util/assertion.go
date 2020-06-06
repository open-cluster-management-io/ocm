package util

import (
	"context"

	"github.com/onsi/gomega"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	operatorclientset "github.com/open-cluster-management/api/client/operator/clientset/versioned"
)

func AssertKlusterletCondition(
	name string, operatorClient operatorclientset.Interface, expectedType string, expectedWorkStatus metav1.ConditionStatus, eventuallyTimeout, eventuallyInterval int) {
	gomega.Eventually(func() bool {
		klusterlet, err := operatorClient.OperatorV1().Klusterlets().Get(context.Background(), name, metav1.GetOptions{})
		if err != nil {
			return false
		}

		// check work status condition
		return HasCondition(klusterlet.Status.Conditions, expectedType, expectedWorkStatus)
	}, eventuallyTimeout, eventuallyInterval).Should(gomega.BeTrue())
}

func AssertClusterManagerCondition(
	name string, operatorClient operatorclientset.Interface, expectedType string, expectedWorkStatus metav1.ConditionStatus, eventuallyTimeout, eventuallyInterval int) {
	gomega.Eventually(func() bool {
		klusterlet, err := operatorClient.OperatorV1().ClusterManagers().Get(context.Background(), name, metav1.GetOptions{})
		if err != nil {
			return false
		}

		// check work status condition
		return HasCondition(klusterlet.Status.Conditions, expectedType, expectedWorkStatus)
	}, eventuallyTimeout, eventuallyInterval).Should(gomega.BeTrue())
}
