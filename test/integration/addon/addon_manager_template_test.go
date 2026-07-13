package integration

import (
	"context"
	"fmt"
	"os"

	"github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/apimachinery/pkg/util/rand"
	"k8s.io/client-go/kubernetes/scheme"

	"open-cluster-management.io/addon-framework/pkg/addonmanager/constants"
	"open-cluster-management.io/addon-framework/pkg/utils"
	addonapiv1beta1 "open-cluster-management.io/api/addon/v1beta1"
	clusterv1alpha1 "open-cluster-management.io/api/cluster/v1alpha1"
	clusterv1beta1 "open-cluster-management.io/api/cluster/v1beta1"
)

var _ = ginkgo.Describe("Template deploy Beta", func() {
	var (
		clusterNames               []string
		numberOfClusters           int
		addonName                  string
		addonTemplateName          string
		addonDeployConfigName      string
		addonDeployConfigNamespace string
		placementName              string
		placementNamespace         string
		manifestWorkName           string
		err                        error
	)

	ginkgo.BeforeEach(func() {
		suffix := rand.String(5)
		numberOfClusters = 5
		addonName = fmt.Sprintf("addon-%s", suffix)
		// AddOnTemplate is cluster-scoped, so use a distinct name from the alpha
		// counterpart (addon_manager_template_alpha_test.go) to avoid collisions.
		addonTemplateName = fmt.Sprintf("hello-template-beta-%s", suffix)
		// AddOnDeploymentConfig "hello-config" in "default" is left behind (never
		// deleted) by the alpha counterpart's AfterEach, so use a distinct name here too.
		addonDeployConfigName = fmt.Sprintf("hello-config-beta-%s", suffix)
		addonDeployConfigNamespace = "default"
		placementName = fmt.Sprintf("placement-%s", suffix)
		placementNamespace = fmt.Sprintf("ns-%s", suffix)
		manifestWorkName = fmt.Sprintf("%s-0", constants.DeployWorkNamePrefix(addonName))

		s := runtime.NewScheme()
		_ = scheme.AddToScheme(s)
		_ = addonapiv1beta1.Install(s)
		decoder := serializer.NewCodecFactory(s).UniversalDeserializer()

		// prepare cluster
		for i := 0; i < numberOfClusters; i++ {
			managedClusterName := fmt.Sprintf("managedcluster-%s-%d", suffix, i)
			clusterNames = append(clusterNames, managedClusterName)
			err = createManagedCluster(hubClusterClient, managedClusterName)
			gomega.Expect(err).ToNot(gomega.HaveOccurred())
		}

		// Create the cma without template first to ensure mca exist before starting the testing.
		// This ensure the code could check the mca by the rollout order.
		// If create the cma with template directly, the mca will be created in a random order and introduce
		// flaky testing result.
		cma := &addonapiv1beta1.ClusterManagementAddOn{
			ObjectMeta: metav1.ObjectMeta{Name: addonName},
			Spec: addonapiv1beta1.ClusterManagementAddOnSpec{
				DefaultConfigs: []addonapiv1beta1.AddOnConfig{
					{
						ConfigGroupResource: addonapiv1beta1.ConfigGroupResource{
							Group: utils.AddOnTemplateGVR.Group, Resource: utils.AddOnTemplateGVR.Resource},
						ConfigReferent: addonapiv1beta1.ConfigReferent{Name: addonapiv1beta1.ReservedNoDefaultConfigName},
					},
					{
						ConfigGroupResource: addonapiv1beta1.ConfigGroupResource{
							Group: utils.AddOnDeploymentConfigGVR.Group, Resource: utils.AddOnDeploymentConfigGVR.Resource},
						ConfigReferent: addonapiv1beta1.ConfigReferent{Name: addonapiv1beta1.ReservedNoDefaultConfigName},
					},
				},
				InstallStrategy: addonapiv1beta1.InstallStrategy{
					Type: addonapiv1beta1.AddonInstallStrategyPlacements,
					Placements: []addonapiv1beta1.PlacementStrategy{
						{PlacementRef: addonapiv1beta1.PlacementRef{Name: placementName, Namespace: placementNamespace}},
					},
				},
			},
		}
		_, err := hubAddonClient.AddonV1beta1().ClusterManagementAddOns().Create(context.Background(), cma, metav1.CreateOptions{})
		gomega.Expect(err).ToNot(gomega.HaveOccurred())

		assertClusterManagementAddOnAnnotationsBeta(addonName)

		// prepare addon template
		addonTemplateData, err := os.ReadFile("./test/integration/addon/testmanifests/addontemplate_beta.yaml")
		gomega.Expect(err).ToNot(gomega.HaveOccurred())

		addonTemplate := &addonapiv1beta1.AddOnTemplate{}
		_, _, err = decoder.Decode(addonTemplateData, nil, addonTemplate)
		gomega.Expect(err).ToNot(gomega.HaveOccurred())
		// The fixture YAML has its own metadata.name; force our unique, per-test name
		// since AddOnTemplate is cluster-scoped and must not collide with the alpha test.
		addonTemplate.ObjectMeta.Name = addonTemplateName

		_, err = hubAddonClient.AddonV1beta1().AddOnTemplates().Create(context.Background(), addonTemplate, metav1.CreateOptions{})
		gomega.Expect(err).ToNot(gomega.HaveOccurred())

		// prepare addon deployment config
		addonDeploymentConfig := &addonapiv1beta1.AddOnDeploymentConfig{
			ObjectMeta: metav1.ObjectMeta{
				Name:      addonDeployConfigName,
				Namespace: addonDeployConfigNamespace,
			},
			Spec: addonapiv1beta1.AddOnDeploymentConfigSpec{
				AgentInstallNamespace: "test-install-namespace",
				CustomizedVariables: []addonapiv1beta1.CustomizedVariable{
					{Name: "LOG_LEVEL", Value: "4"},
				},
				NodePlacement: &addonapiv1beta1.NodePlacement{
					NodeSelector: map[string]string{"host": "ssd"},
					Tolerations: []corev1.Toleration{
						{Key: "foo", Operator: corev1.TolerationOpExists, Effect: corev1.TaintEffectNoExecute},
					},
				},
				Registries: []addonapiv1beta1.ImageMirror{
					{Source: "quay.io/open-cluster-management", Mirror: "quay.io/ocm"},
				},
			},
		}
		_, err = hubAddonClient.AddonV1beta1().AddOnDeploymentConfigs(addonDeployConfigNamespace).Create(
			context.Background(), addonDeploymentConfig, metav1.CreateOptions{})
		gomega.Expect(err).ToNot(gomega.HaveOccurred())

		// prepare placement
		pns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: placementNamespace}}
		_, err = hubKubeClient.CoreV1().Namespaces().Create(context.Background(), pns, metav1.CreateOptions{})
		gomega.Expect(err).ToNot(gomega.HaveOccurred())

		placement := &clusterv1beta1.Placement{ObjectMeta: metav1.ObjectMeta{Name: placementName, Namespace: placementNamespace}}
		_, err = hubClusterClient.ClusterV1beta1().Placements(placementNamespace).Create(
			context.Background(), placement, metav1.CreateOptions{})
		gomega.Expect(err).ToNot(gomega.HaveOccurred())

		// prepare placement decisions
		err = createPlacementDecision(hubClusterClient, placementNamespace, placementName, "0", clusterNames...)
		gomega.Expect(err).ToNot(gomega.HaveOccurred())
	})

	ginkgo.AfterEach(func() {
		for _, managedClusterName := range clusterNames {
			err = hubKubeClient.CoreV1().Namespaces().Delete(context.Background(), managedClusterName, metav1.DeleteOptions{})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())
			err = hubClusterClient.ClusterV1().ManagedClusters().Delete(context.Background(), managedClusterName, metav1.DeleteOptions{})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())
		}

		err = hubAddonClient.AddonV1beta1().ClusterManagementAddOns().Delete(context.Background(),
			addonName, metav1.DeleteOptions{})
		gomega.Expect(err).ToNot(gomega.HaveOccurred())
	})

	ginkgo.It("Should deploy agent for addon template", func() {
		ginkgo.By("check mca created")
		gomega.Eventually(func() error {
			for i := 0; i < numberOfClusters; i++ {
				_, err := hubAddonClient.AddonV1beta1().ManagedClusterAddOns(clusterNames[i]).Get(context.Background(), addonName, metav1.GetOptions{})
				if err != nil {
					return err
				}
			}
			return nil
		}, eventuallyTimeout, eventuallyInterval).ShouldNot(gomega.HaveOccurred())

		ginkgo.By("check no work rendered")
		for i := 0; i < numberOfClusters; i++ {
			checkWorkRendered(clusterNames[i], 0)
		}

		ginkgo.By("update cma")
		clusterManagementAddon, err := hubAddonClient.AddonV1beta1().ClusterManagementAddOns().Get(context.Background(), addonName, metav1.GetOptions{})
		gomega.Expect(err).ToNot(gomega.HaveOccurred())

		clusterManagementAddon.Spec.InstallStrategy = addonapiv1beta1.InstallStrategy{
			Type: addonapiv1beta1.AddonInstallStrategyPlacements,
			Placements: []addonapiv1beta1.PlacementStrategy{{
				PlacementRef: addonapiv1beta1.PlacementRef{Name: placementName, Namespace: placementNamespace},
				RolloutStrategy: clusterv1alpha1.RolloutStrategy{
					Type:        clusterv1alpha1.Progressive,
					Progressive: &clusterv1alpha1.RolloutProgressive{MaxConcurrency: intstr.FromInt(1)}},
				Configs: []addonapiv1beta1.AddOnConfig{
					{
						ConfigGroupResource: addonapiv1beta1.ConfigGroupResource{
							Group:    utils.AddOnTemplateGVR.Group,
							Resource: utils.AddOnTemplateGVR.Resource,
						},
						ConfigReferent: addonapiv1beta1.ConfigReferent{
							Name: addonTemplateName,
						},
					},
					{
						ConfigGroupResource: addonapiv1beta1.ConfigGroupResource{
							Group:    utils.AddOnDeploymentConfigGVR.Group,
							Resource: utils.AddOnDeploymentConfigGVR.Resource,
						},
						ConfigReferent: addonapiv1beta1.ConfigReferent{
							Name:      addonDeployConfigName,
							Namespace: addonDeployConfigNamespace,
						},
					},
				},
			},
			},
		}

		_, err = hubAddonClient.AddonV1beta1().ClusterManagementAddOns().Update(context.Background(), clusterManagementAddon, metav1.UpdateOptions{})
		gomega.Expect(err).ToNot(gomega.HaveOccurred())

		ginkgo.By("check mca condition")
		assertManagedClusterAddOnConditionsBeta(addonName, clusterNames[0], metav1.Condition{
			Type:    addonapiv1beta1.ManagedClusterAddOnConditionConfigured,
			Status:  metav1.ConditionTrue,
			Reason:  "ConfigurationsConfigured",
			Message: "Configurations configured",
		})
		for i := 1; i < numberOfClusters; i++ {
			assertManagedClusterAddOnConditionsBeta(addonName, clusterNames[i], metav1.Condition{
				Type:    addonapiv1beta1.ManagedClusterAddOnConditionConfigured,
				Status:  metav1.ConditionFalse,
				Reason:  "ConfigurationsNotConfigured",
				Message: "Configurations updated and not configured yet",
			})
		}

		ginkgo.By("check work rendered on the first cluster")
		checkWorkRendered(clusterNames[0], 1)
		for i := 1; i < numberOfClusters; i++ {
			checkWorkRendered(clusterNames[i], 0)
		}

		ginkgo.By("update work status to trigger addon status")
		updateManifestWorkStatus(hubWorkClient, clusterNames[0], manifestWorkName, metav1.ConditionTrue)

		ginkgo.By("check mca condition")
		assertManagedClusterAddOnConditionsBeta(addonName, clusterNames[1], metav1.Condition{
			Type:    addonapiv1beta1.ManagedClusterAddOnConditionConfigured,
			Status:  metav1.ConditionTrue,
			Reason:  "ConfigurationsConfigured",
			Message: "Configurations configured",
		})
		for i := 2; i < numberOfClusters; i++ {
			assertManagedClusterAddOnConditionsBeta(addonName, clusterNames[i], metav1.Condition{
				Type:    addonapiv1beta1.ManagedClusterAddOnConditionConfigured,
				Status:  metav1.ConditionFalse,
				Reason:  "ConfigurationsNotConfigured",
				Message: "Configurations updated and not configured yet",
			})
		}

		ginkgo.By("check work rendered on the second cluster")
		checkWorkRendered(clusterNames[1], 1)
		for i := 2; i < numberOfClusters; i++ {
			checkWorkRendered(clusterNames[i], 0)
		}
	})
})
