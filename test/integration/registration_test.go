package integration

import (
	"context"
	"fmt"

	"github.com/onsi/ginkgo"
	"github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/util/rand"

	addonapiv1alpha1 "github.com/open-cluster-management/api/addon/v1alpha1"
	clusterv1 "github.com/open-cluster-management/api/cluster/v1"
	certificatesv1 "k8s.io/api/certificates/v1"
	corev1 "k8s.io/api/core/v1"
	apiequality "k8s.io/apimachinery/pkg/api/equality"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var _ = ginkgo.Describe("Addon Registration", func() {
	var managedClusterName string
	var err error

	ginkgo.BeforeEach(func() {
		suffix := rand.String(5)
		managedClusterName = fmt.Sprintf("managedcluster-%s", suffix)

		managedCluster := &clusterv1.ManagedCluster{
			ObjectMeta: metav1.ObjectMeta{
				Name: managedClusterName,
			},
			Spec: clusterv1.ManagedClusterSpec{
				HubAcceptsClient: true,
			},
		}
		_, err = hubClusterClient.ClusterV1().ManagedClusters().Create(context.Background(), managedCluster, metav1.CreateOptions{})
		gomega.Expect(err).ToNot(gomega.HaveOccurred())

		ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: managedClusterName}}
		_, err = hubKubeClient.CoreV1().Namespaces().Create(context.Background(), ns, metav1.CreateOptions{})
		gomega.Expect(err).ToNot(gomega.HaveOccurred())
	})

	ginkgo.AfterEach(func() {
		err = hubKubeClient.CoreV1().Namespaces().Delete(context.Background(), managedClusterName, metav1.DeleteOptions{})
		gomega.Expect(err).ToNot(gomega.HaveOccurred())
		err = hubClusterClient.ClusterV1().ManagedClusters().Delete(context.Background(), managedClusterName, metav1.DeleteOptions{})
		gomega.Expect(err).ToNot(gomega.HaveOccurred())
		delete(testAddonImpl.registrations, managedClusterName)
	})

	ginkgo.It("Should setup registration successfully", func() {
		testAddonImpl.registrations[managedClusterName] = []addonapiv1alpha1.RegistrationConfig{
			{
				SignerName: certificatesv1.KubeAPIServerClientSignerName,
			},
		}

		addon := &addonapiv1alpha1.ManagedClusterAddOn{
			ObjectMeta: metav1.ObjectMeta{
				Name: testAddonImpl.name,
			},
			Spec: addonapiv1alpha1.ManagedClusterAddOnSpec{
				InstallNamespace: "test",
			},
		}
		_, err = hubAddonClient.AddonV1alpha1().ManagedClusterAddOns(managedClusterName).Create(context.Background(), addon, metav1.CreateOptions{})
		gomega.Expect(err).ToNot(gomega.HaveOccurred())

		gomega.Eventually(func() error {
			actual, err := hubAddonClient.AddonV1alpha1().ManagedClusterAddOns(managedClusterName).Get(context.Background(), testAddonImpl.name, metav1.GetOptions{})
			if err != nil {
				return err
			}
			if !apiequality.Semantic.DeepEqual(testAddonImpl.registrations[managedClusterName], actual.Status.Registrations) {
				return fmt.Errorf("Exected registration is not correct, actual: %v", actual.Status.Registrations)
			}
			return nil
		}, eventuallyTimeout, eventuallyInterval).ShouldNot(gomega.HaveOccurred())
	})

	ginkgo.It("Should update registration successfully", func() {
		testAddonImpl.registrations[managedClusterName] = []addonapiv1alpha1.RegistrationConfig{
			{
				SignerName: certificatesv1.KubeAPIServerClientSignerName,
			},
			{
				SignerName: "open-cluster-management.io/test-signer",
			},
		}

		addon := &addonapiv1alpha1.ManagedClusterAddOn{
			ObjectMeta: metav1.ObjectMeta{
				Name: testAddonImpl.name,
			},
			Spec: addonapiv1alpha1.ManagedClusterAddOnSpec{
				InstallNamespace: "test",
			},
		}
		_, err = hubAddonClient.AddonV1alpha1().ManagedClusterAddOns(managedClusterName).Create(context.Background(), addon, metav1.CreateOptions{})
		gomega.Expect(err).ToNot(gomega.HaveOccurred())

		gomega.Eventually(func() error {
			actual, err := hubAddonClient.AddonV1alpha1().ManagedClusterAddOns(managedClusterName).Get(context.Background(), testAddonImpl.name, metav1.GetOptions{})
			if err != nil {
				return err
			}
			actual.Status = addonapiv1alpha1.ManagedClusterAddOnStatus{
				Registrations: []addonapiv1alpha1.RegistrationConfig{
					{
						SignerName: certificatesv1.KubeAPIServerClientSignerName,
					},
				},
				Conditions: []metav1.Condition{},
			}
			_, err = hubAddonClient.AddonV1alpha1().ManagedClusterAddOns(managedClusterName).UpdateStatus(context.Background(), actual, metav1.UpdateOptions{})
			return err
		}, eventuallyTimeout, eventuallyInterval).ShouldNot(gomega.HaveOccurred())

		gomega.Eventually(func() error {
			actual, err := hubAddonClient.AddonV1alpha1().ManagedClusterAddOns(managedClusterName).Get(context.Background(), testAddonImpl.name, metav1.GetOptions{})
			if err != nil {
				return err
			}
			if !apiequality.Semantic.DeepEqual(testAddonImpl.registrations[managedClusterName], actual.Status.Registrations) {
				return fmt.Errorf("Exected registration is not correct, actual: %v", actual.Status.Registrations)
			}
			return nil
		}, eventuallyTimeout, eventuallyInterval).ShouldNot(gomega.HaveOccurred())
	})
})
