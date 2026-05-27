package e2e

import (
	"context"
	"fmt"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/rand"
)

var _ = Describe("Manage the managed cluster addons (v1alpha1)", Label("addon"), func() {
	var addOnName string
	BeforeEach(func() {
		addOnName = fmt.Sprintf("e2e-addon-%s", rand.String(6))
	})

	AfterEach(func() {
		err := hub.AddonClient.AddonV1alpha1().ManagedClusterAddOns(universalClusterName).Delete(context.TODO(), addOnName, metav1.DeleteOptions{})
		Expect(err).ToNot(HaveOccurred())
	})

	It("Create one managed cluster addon and make sure it is available", func() {
		By(fmt.Sprintf("create the addon %v on the managed cluster namespace %v", addOnName, universalClusterName))
		err := hub.CreateManagedClusterAddOnV1Alpha1(universalClusterName, addOnName, addOnName)
		Expect(err).ToNot(HaveOccurred())

		By(fmt.Sprintf("create the addon lease %v on addon install namespace %v", addOnName, addOnName))
		err = hub.CreateManagedClusterAddOnLease(addOnName, addOnName)
		Expect(err).ToNot(HaveOccurred())

		By(fmt.Sprintf("wait the addon %v available condition to be true", addOnName))
		Eventually(func() error {
			return hub.CheckManagedClusterAddOnStatusV1Alpha1(universalClusterName, addOnName)
		}).Should(Succeed())
	})

	It("Create one managed cluster addon and make sure it is available in Hosted mode", func() {
		By(fmt.Sprintf("create the addon %v on the managed cluster namespace %v", addOnName, universalClusterName))
		err := hub.CreateManagedClusterAddOnV1Alpha1(universalClusterName, addOnName, addOnName)
		Expect(err).ToNot(HaveOccurred())

		By(fmt.Sprintf("create the addon lease %v on addon install namespace %v", addOnName, addOnName))
		err = hub.CreateManagedClusterAddOnLease(addOnName, addOnName)
		Expect(err).ToNot(HaveOccurred())

		By(fmt.Sprintf("wait the addon %v available condition to be true", addOnName))
		Eventually(func() error {
			return hub.CheckManagedClusterAddOnStatusV1Alpha1(universalClusterName, addOnName)
		}).Should(Succeed())
	})
})
