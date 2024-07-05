package e2e

import (
	"context"
	"fmt"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/rand"
)

var _ = Describe("Manage the managed cluster addons", func() {
	var addOnName string
	BeforeEach(func() {
		addOnName = fmt.Sprintf("e2e-addon-%s", rand.String(6))
	})

	AfterEach(func() {
		err := t.AddOnClinet.AddonV1alpha1().ManagedClusterAddOns(universalClusterName).Delete(context.TODO(), addOnName, metav1.DeleteOptions{})
		Expect(err).ToNot(HaveOccurred())
	})

	It("Create one managed cluster addon and make sure it is available", func() {
		By(fmt.Sprintf("create the addon %v on the managed cluster namespace %v", addOnName, universalClusterName))
		err := t.CreateManagedClusterAddOn(universalClusterName, addOnName, addOnName)
		Expect(err).ToNot(HaveOccurred())

		By(fmt.Sprintf("create the addon lease %v on addon install namespace %v", addOnName, addOnName))
		err = t.CreateManagedClusterAddOnLease(addOnName, addOnName)
		Expect(err).ToNot(HaveOccurred())

		By(fmt.Sprintf("wait the addon %v available condition to be true", addOnName))
		Eventually(func() error {
			return t.CheckManagedClusterAddOnStatus(universalClusterName, addOnName)
		}, t.EventuallyTimeout*5, t.EventuallyInterval*5).Should(Succeed())
	})

	It("Create one managed cluster addon and make sure it is available in Hosted mode", func() {
		By(fmt.Sprintf("create the addon %v on the managed cluster namespace %v", addOnName, universalClusterName))
		err := t.CreateManagedClusterAddOn(universalClusterName, addOnName, addOnName)
		Expect(err).ToNot(HaveOccurred())

		By(fmt.Sprintf("create the addon lease %v on addon install namespace %v", addOnName, addOnName))
		err = t.CreateManagedClusterAddOnLease(addOnName, addOnName)
		Expect(err).ToNot(HaveOccurred())

		By(fmt.Sprintf("wait the addon %v available condition to be true", addOnName))
		Eventually(func() error {
			return t.CheckManagedClusterAddOnStatus(universalClusterName, addOnName)
		}, t.EventuallyTimeout*5, t.EventuallyInterval*5).Should(Succeed())
	})
})
