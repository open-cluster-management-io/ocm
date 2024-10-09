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
	addonapiv1alpha1 "open-cluster-management.io/api/addon/v1alpha1"
	clusterv1alpha1 "open-cluster-management.io/api/cluster/v1alpha1"
	clusterv1beta1 "open-cluster-management.io/api/cluster/v1beta1"
)

var _ = ginkgo.Describe("Agent deploy", func() {
	var clusterNames []string
	var err error
	var addonName string
	var addonTemplateName string
	var addonDeployConfigName string
	var addonDeployConfigNamespace string
	var placementName string
	var placementNamespace string
	var manifestWorkName string

	ginkgo.BeforeEach(func() {
		suffix := rand.String(5)
		addonName = fmt.Sprintf("addon-%s", suffix)
		addonTemplateName = "hello-template"
		addonDeployConfigName = "hello-config"
		addonDeployConfigNamespace = "default"
		placementName = fmt.Sprintf("ns-%s", suffix)
		placementNamespace = fmt.Sprintf("ns-%s", suffix)
		manifestWorkName = fmt.Sprintf("%s-0", constants.DeployWorkNamePrefix(addonName))

		s := runtime.NewScheme()
		_ = scheme.AddToScheme(s)
		_ = addonapiv1alpha1.Install(s)
		decoder := serializer.NewCodecFactory(s).UniversalDeserializer()

		// prepare cluster
		for i := 0; i < 2; i++ {
			managedClusterName := fmt.Sprintf("managedcluster-%s-%d", suffix, i)
			clusterNames = append(clusterNames, managedClusterName)
			err = createManagedCluster(hubClusterClient, managedClusterName)
			gomega.Expect(err).ToNot(gomega.HaveOccurred())
		}

		// prepare cma
		cma := &addonapiv1alpha1.ClusterManagementAddOn{
			ObjectMeta: metav1.ObjectMeta{
				Name: addonName,
			},
			Spec: addonapiv1alpha1.ClusterManagementAddOnSpec{
				SupportedConfigs: []addonapiv1alpha1.ConfigMeta{
					{
						ConfigGroupResource: addonapiv1alpha1.ConfigGroupResource{
							Group:    utils.AddOnTemplateGVR.Group,
							Resource: utils.AddOnTemplateGVR.Resource,
						},
					},
					{
						ConfigGroupResource: addonapiv1alpha1.ConfigGroupResource{
							Group:    utils.AddOnDeploymentConfigGVR.Group,
							Resource: utils.AddOnDeploymentConfigGVR.Resource,
						},
					},
				},
				InstallStrategy: addonapiv1alpha1.InstallStrategy{
					Type: addonapiv1alpha1.AddonInstallStrategyPlacements,
					Placements: []addonapiv1alpha1.PlacementStrategy{
						{
							PlacementRef: addonapiv1alpha1.PlacementRef{Name: placementName, Namespace: placementNamespace},
							RolloutStrategy: clusterv1alpha1.RolloutStrategy{
								Type: clusterv1alpha1.Progressive,
								Progressive: &clusterv1alpha1.RolloutProgressive{
									MaxConcurrency: intstr.FromInt(1),
								},
							},
							Configs: []addonapiv1alpha1.AddOnConfig{
								{
									ConfigGroupResource: addonapiv1alpha1.ConfigGroupResource{
										Group:    utils.AddOnTemplateGVR.Group,
										Resource: utils.AddOnTemplateGVR.Resource,
									},
									ConfigReferent: addonapiv1alpha1.ConfigReferent{
										Name: addonTemplateName,
									},
								},
								{
									ConfigGroupResource: addonapiv1alpha1.ConfigGroupResource{
										Group:    utils.AddOnDeploymentConfigGVR.Group,
										Resource: utils.AddOnDeploymentConfigGVR.Resource,
									},
									ConfigReferent: addonapiv1alpha1.ConfigReferent{
										Name:      addonDeployConfigName,
										Namespace: addonDeployConfigNamespace,
									},
								},
							},
						},
					},
				},
			},
		}
		_, err := hubAddonClient.AddonV1alpha1().ClusterManagementAddOns().Create(context.Background(), cma, metav1.CreateOptions{})
		gomega.Expect(err).ToNot(gomega.HaveOccurred())

		assertClusterManagementAddOnAnnotations(addonName)

		// prepare addon template
		var addonTemplate *addonapiv1alpha1.AddOnTemplate
		data, err := os.ReadFile("./test/integration/addon/testmanifests/addontemplate.yaml")
		gomega.Expect(err).ToNot(gomega.HaveOccurred())

		addonTemplate = &addonapiv1alpha1.AddOnTemplate{
			ObjectMeta: metav1.ObjectMeta{
				Name: addonTemplateName,
			},
		}
		_, _, err = decoder.Decode(data, nil, addonTemplate)
		gomega.Expect(err).ToNot(gomega.HaveOccurred())

		_, err = hubAddonClient.AddonV1alpha1().AddOnTemplates().Create(context.Background(), addonTemplate, metav1.CreateOptions{})
		gomega.Expect(err).ToNot(gomega.HaveOccurred())

		// prepare addon deployment config
		addonDeploymentConfig := &addonapiv1alpha1.AddOnDeploymentConfig{
			ObjectMeta: metav1.ObjectMeta{
				Name:      addonDeployConfigName,
				Namespace: addonDeployConfigNamespace,
			},
			Spec: addonapiv1alpha1.AddOnDeploymentConfigSpec{
				AgentInstallNamespace: "test-install-namespace",
				CustomizedVariables: []addonapiv1alpha1.CustomizedVariable{
					{
						Name:  "LOG_LEVEL",
						Value: "4",
					},
				},
				NodePlacement: &addonapiv1alpha1.NodePlacement{
					NodeSelector: map[string]string{
						"host": "ssd",
					},
					Tolerations: []corev1.Toleration{
						{
							Key:      "foo",
							Operator: corev1.TolerationOpExists,
							Effect:   corev1.TaintEffectNoExecute,
						},
					},
				},
				Registries: []addonapiv1alpha1.ImageMirror{
					{
						Source: "quay.io/open-cluster-management",
						Mirror: "quay.io/ocm",
					},
				},
			},
		}
		_, err = hubAddonClient.AddonV1alpha1().AddOnDeploymentConfigs(addonDeployConfigNamespace).Create(
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
		err = createPlacementDecision(hubClusterClient, placementNamespace, placementName, "0", clusterNames[0], clusterNames[1])
		gomega.Expect(err).ToNot(gomega.HaveOccurred())
	})

	ginkgo.AfterEach(func() {
		for _, managedClusterName := range clusterNames {
			err = hubKubeClient.CoreV1().Namespaces().Delete(context.Background(), managedClusterName, metav1.DeleteOptions{})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())
			err = hubClusterClient.ClusterV1().ManagedClusters().Delete(context.Background(), managedClusterName, metav1.DeleteOptions{})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())
			delete(testAddOnConfigsImpl.registrations, managedClusterName)
		}

		err = hubAddonClient.AddonV1alpha1().ClusterManagementAddOns().Delete(context.Background(),
			addonName, metav1.DeleteOptions{})
		gomega.Expect(err).ToNot(gomega.HaveOccurred())
	})

	ginkgo.It("Should deploy agent for addon template", func() {
		ginkgo.By("check mca condition")
		assertManagedClusterAddOnConditions(addonName, clusterNames[0], metav1.Condition{
			Type:    addonapiv1alpha1.ManagedClusterAddOnConditionConfigured,
			Status:  metav1.ConditionTrue,
			Reason:  "ConfigurationsConfigured",
			Message: "Configurations configured",
		})
		assertManagedClusterAddOnConditions(addonName, clusterNames[1], metav1.Condition{
			Type:    addonapiv1alpha1.ManagedClusterAddOnConditionConfigured,
			Status:  metav1.ConditionFalse,
			Reason:  "ConfigurationsNotConfigured",
			Message: "Configurations updated and not configured yet",
		})

		ginkgo.By("check only 1 work rendered")
		gomega.Eventually(func() error {
			work, err := hubWorkClient.WorkV1().ManifestWorks(clusterNames[0]).List(
				context.Background(), metav1.ListOptions{})
			if err != nil {
				return err
			}

			if len(work.Items) != 1 {
				return fmt.Errorf("Expect 1 work but get %v", work.Items)
			}

			return nil
		}, eventuallyTimeout, eventuallyInterval).ShouldNot(gomega.HaveOccurred())

		gomega.Eventually(func() error {
			work, err := hubWorkClient.WorkV1().ManifestWorks(clusterNames[1]).List(
				context.Background(), metav1.ListOptions{})
			if err != nil {
				return err
			}

			if len(work.Items) != 0 {
				return fmt.Errorf("Expect 0 work but get %v", work.Items)
			}

			return nil
		}, eventuallyTimeout, eventuallyInterval).ShouldNot(gomega.HaveOccurred())

		ginkgo.By("update work status to trigger addon status")
		updateManifestWorkStatus(hubWorkClient, clusterNames[0], manifestWorkName, metav1.ConditionTrue)

		ginkgo.By("check mca condition")
		assertManagedClusterAddOnConditions(addonName, clusterNames[1], metav1.Condition{
			Type:    addonapiv1alpha1.ManagedClusterAddOnConditionConfigured,
			Status:  metav1.ConditionTrue,
			Reason:  "ConfigurationsConfigured",
			Message: "Configurations configured",
		})

		ginkgo.By("check rendered work")
		gomega.Eventually(func() error {
			work, err := hubWorkClient.WorkV1().ManifestWorks(clusterNames[1]).List(
				context.Background(), metav1.ListOptions{})
			if err != nil {
				return err
			}

			if len(work.Items) != 1 {
				return fmt.Errorf("Expect 1 work but get %v", work.Items)
			}

			return nil
		}, eventuallyTimeout, eventuallyInterval).ShouldNot(gomega.HaveOccurred())
	})

})
