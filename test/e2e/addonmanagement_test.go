package e2e

import (
	"context"
	"fmt"

	ginkgo "github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/rand"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/klog/v2"

	addonapiv1alpha1 "open-cluster-management.io/api/addon/v1alpha1"
	clusterv1apha1 "open-cluster-management.io/api/cluster/v1alpha1"
	operatorapiv1 "open-cluster-management.io/api/operator/v1"

	"open-cluster-management.io/ocm/test/e2e/manifests"
)

var _ = ginkgo.Describe("Enable addon management feature gate", ginkgo.Label("addon-manager"), func() {
	var klusterletName, clusterName, agentNamespace, addonInstallNamespace string

	s := runtime.NewScheme()
	_ = scheme.AddToScheme(s)
	_ = clusterv1apha1.Install(s)
	_ = addonapiv1alpha1.Install(s)

	templateResources := []string{
		"addon/addon_deployment_config.yaml",
		"addon/addon_template.yaml",
		"addon/cluster_management_addon.yaml",
		"addon/cluster_role.yaml",
	}

	ginkgo.BeforeEach(func() {
		surfix := rand.String(6)
		klusterletName = fmt.Sprintf("e2e-klusterlet-%s", surfix)
		clusterName = fmt.Sprintf("e2e-managedcluster-%s", surfix)
		agentNamespace = fmt.Sprintf("open-cluster-management-agent-%s", surfix)
		addonInstallNamespace = fmt.Sprintf("%s-addon", agentNamespace)
		// enable addon management feature gate
		gomega.Eventually(func() error {
			clusterManager, err := t.OperatorClient.OperatorV1().ClusterManagers().Get(context.TODO(), "cluster-manager", metav1.GetOptions{})
			if err != nil {
				return err
			}
			clusterManager.Spec.AddOnManagerConfiguration = &operatorapiv1.AddOnManagerConfiguration{
				FeatureGates: []operatorapiv1.FeatureGate{
					{
						Feature: "AddonManagement",
						Mode:    operatorapiv1.FeatureGateModeTypeEnable,
					},
				},
			}
			_, err = t.OperatorClient.OperatorV1().ClusterManagers().Update(context.TODO(), clusterManager, metav1.UpdateOptions{})
			return err
		}, t.EventuallyTimeout*5, t.EventuallyInterval*5).Should(gomega.Succeed())

		// the addon manager deployment should be running
		gomega.Eventually(t.CheckHubReady, t.EventuallyTimeout, t.EventuallyInterval).Should(gomega.Succeed())

		_, err := t.CreateApprovedKlusterlet(klusterletName, clusterName, agentNamespace, operatorapiv1.InstallModeDefault)
		gomega.Expect(err).ToNot(gomega.HaveOccurred())

		ginkgo.By(fmt.Sprintf("create addon template resources for cluster %v", clusterName))
		err = createResourcesFromYamlFiles(context.Background(), t.HubDynamicClient, t.hubRestMapper, s,
			defaultAddonTemplateReaderManifestsFunc(manifests.AddonManifestFiles, map[string]interface{}{
				"Namespace":             clusterName,
				"AddonInstallNamespace": addonInstallNamespace,
			}),
			templateResources,
		)
		gomega.Expect(err).ToNot(gomega.HaveOccurred())
	})
	ginkgo.AfterEach(func() {
		ginkgo.By(fmt.Sprintf("delete addon template resources for cluster %v", clusterName))
		err := deleteResourcesFromYamlFiles(context.Background(), t.HubDynamicClient, t.hubRestMapper, s,
			defaultAddonTemplateReaderManifestsFunc(manifests.AddonManifestFiles, map[string]interface{}{
				"Namespace":             clusterName,
				"AddonInstallNamespace": addonInstallNamespace,
			}),
			templateResources,
		)
		gomega.Expect(err).ToNot(gomega.HaveOccurred())

		ginkgo.By(fmt.Sprintf("clean klusterlet %v resources after the test case", klusterletName))
		gomega.Expect(t.cleanKlusterletResources(klusterletName, clusterName)).To(gomega.BeNil())

		// disable addon management feature gate
		gomega.Eventually(func() error {
			clusterManager, err := t.OperatorClient.OperatorV1().ClusterManagers().Get(context.TODO(), "cluster-manager", metav1.GetOptions{})
			if err != nil {
				return err
			}
			clusterManager.Spec.AddOnManagerConfiguration = &operatorapiv1.AddOnManagerConfiguration{}
			_, err = t.OperatorClient.OperatorV1().ClusterManagers().Update(context.TODO(), clusterManager, metav1.UpdateOptions{})
			return err
		}, t.EventuallyTimeout*5, t.EventuallyInterval*5).Should(gomega.Succeed())

	})

	ginkgo.It("Test addon template", func() {
		addOnName := "hello-template"
		ginkgo.By(fmt.Sprintf("create the addon %v on the managed cluster namespace %v", addOnName, clusterName))
		err := t.CreateManagedClusterAddOn(clusterName, addOnName, addonInstallNamespace)
		if err != nil {
			klog.Errorf("failed to create managed cluster addon %v on the managed cluster namespace %v: %v", addOnName, clusterName, err)
			gomega.Expect(errors.IsAlreadyExists(err)).To(gomega.BeTrue())
		}

		ginkgo.By(fmt.Sprintf("wait the addon %v available condition to be true", addOnName))
		gomega.Eventually(func() error {
			return t.CheckManagedClusterAddOnStatus(clusterName, addOnName)
		}, t.EventuallyTimeout*5, t.EventuallyInterval*5).Should(gomega.Succeed())

		ginkgo.By(fmt.Sprintf("delete the addon %v on the managed cluster namespace %v", addOnName, clusterName))
		err = t.AddOnClinet.AddonV1alpha1().ManagedClusterAddOns(clusterName).Delete(
			context.TODO(), addOnName, metav1.DeleteOptions{})
		gomega.Expect(err).ToNot(gomega.HaveOccurred())

		gomega.Eventually(func() error {
			_, err := t.AddOnClinet.AddonV1alpha1().ManagedClusterAddOns(clusterName).Get(
				context.TODO(), addOnName, metav1.GetOptions{})
			if err == nil {
				return fmt.Errorf("the addon %v is not deleted", addOnName)
			}
			if !errors.IsNotFound(err) {
				return err
			}
			return nil
		}, t.EventuallyTimeout*5, t.EventuallyInterval*5).Should(gomega.Succeed())
	})
})
