package integration

import (
	"context"
	"fmt"
	"time"

	"github.com/onsi/ginkgo"
	"github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	apiequality "k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/rand"
	"open-cluster-management.io/addon-framework/pkg/addonmanager/constants"
	"open-cluster-management.io/addon-framework/pkg/agent"
	"open-cluster-management.io/addon-framework/pkg/utils"
	addonapiv1alpha1 "open-cluster-management.io/api/addon/v1alpha1"
	clusterv1 "open-cluster-management.io/api/cluster/v1"
	workapiv1 "open-cluster-management.io/api/work/v1"
)

const (
	deploymentJson = `{
		"apiVersion": "apps/v1",
		"kind": "Deployment",
		"metadata": {
			"name": "nginx-deployment",
			"namespace": "default"
		},
		"spec": {
			"replicas": 1,
			"selector": {
				"matchLabels": {
					"app": "nginx"
				}
			},
			"template": {
				"metadata": {
					"creationTimestamp": null,
					"labels": {
						"app": "nginx"
					}
				},
				"spec": {
					"containers": [
						{
							"image": "nginx:1.14.2",
							"name": "nginx",
							"ports": [
								{
									"containerPort": 80,
									"protocol": "TCP"
								}
							]
						}
					]
				}
			}
		}
	}`
)

var _ = ginkgo.Describe("Agent deploy", func() {
	var managedClusterName string
	var err error
	var manifestWorkName string
	ginkgo.BeforeEach(func() {
		suffix := rand.String(5)
		managedClusterName = fmt.Sprintf("managedcluster-%s", suffix)
		manifestWorkName = constants.DeployWorkName(testAddonImpl.name)

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
	})

	ginkgo.It("Should deploy agent successfully", func() {
		obj := &unstructured.Unstructured{}
		err := obj.UnmarshalJSON([]byte(deploymentJson))
		gomega.Expect(err).ToNot(gomega.HaveOccurred())
		testAddonImpl.manifests[managedClusterName] = []runtime.Object{obj}
		testAddonImpl.prober = &agent.HealthProber{
			Type: agent.HealthProberTypeWork,
		}

		addon := &addonapiv1alpha1.ManagedClusterAddOn{
			ObjectMeta: metav1.ObjectMeta{
				Name: testAddonImpl.name,
			},
			Spec: addonapiv1alpha1.ManagedClusterAddOnSpec{
				InstallNamespace: "default",
			},
		}
		_, err = hubAddonClient.AddonV1alpha1().ManagedClusterAddOns(managedClusterName).Create(context.Background(), addon, metav1.CreateOptions{})
		gomega.Expect(err).ToNot(gomega.HaveOccurred())

		gomega.Eventually(func() error {
			work, err := hubWorkClient.WorkV1().ManifestWorks(managedClusterName).Get(context.Background(), manifestWorkName, metav1.GetOptions{})
			if err != nil {
				return err
			}

			if len(work.Spec.Workload.Manifests) != 1 {
				return fmt.Errorf("Unexpected number of work manifests")
			}

			if apiequality.Semantic.DeepEqual(work.Spec.Workload.Manifests[0].Raw, []byte(deploymentJson)) {
				return fmt.Errorf("expected manifest is no correct, get %v", work.Spec.Workload.Manifests[0].Raw)
			}
			return nil
		}, eventuallyTimeout, eventuallyInterval).ShouldNot(gomega.HaveOccurred())

		// Update work status to trigger addon status
		work, err := hubWorkClient.WorkV1().ManifestWorks(managedClusterName).Get(context.Background(), manifestWorkName, metav1.GetOptions{})
		gomega.Expect(err).ToNot(gomega.HaveOccurred())
		meta.SetStatusCondition(&work.Status.Conditions, metav1.Condition{Type: "Applied", Status: metav1.ConditionTrue, Reason: "WorkApplied"})
		_, err = hubWorkClient.WorkV1().ManifestWorks(managedClusterName).UpdateStatus(context.Background(), work, metav1.UpdateOptions{})
		gomega.Expect(err).ToNot(gomega.HaveOccurred())

		gomega.Eventually(func() error {
			addon, err := hubAddonClient.AddonV1alpha1().ManagedClusterAddOns(managedClusterName).Get(context.Background(), testAddonImpl.name, metav1.GetOptions{})
			if err != nil {
				return err
			}

			if !meta.IsStatusConditionTrue(addon.Status.Conditions, "ManifestApplied") {
				return fmt.Errorf("Unexpected addon applied condition, %v", addon.Status.Conditions)
			}
			return nil
		}, eventuallyTimeout, eventuallyInterval).ShouldNot(gomega.HaveOccurred())

		// update work to available so addon becomes available
		work, err = hubWorkClient.WorkV1().ManifestWorks(managedClusterName).Get(context.Background(), manifestWorkName, metav1.GetOptions{})
		gomega.Expect(err).ToNot(gomega.HaveOccurred())
		meta.SetStatusCondition(&work.Status.Conditions, metav1.Condition{Type: workapiv1.WorkAvailable, Status: metav1.ConditionTrue, Reason: "WorkAvailable"})
		_, err = hubWorkClient.WorkV1().ManifestWorks(managedClusterName).UpdateStatus(context.Background(), work, metav1.UpdateOptions{})
		gomega.Expect(err).ToNot(gomega.HaveOccurred())

		gomega.Eventually(func() error {
			addon, err := hubAddonClient.AddonV1alpha1().ManagedClusterAddOns(managedClusterName).Get(context.Background(), testAddonImpl.name, metav1.GetOptions{})
			if err != nil {
				return err
			}

			if !meta.IsStatusConditionTrue(addon.Status.Conditions, "Available") {
				return fmt.Errorf("Unexpected addon available condition, %v", addon.Status.Conditions)
			}
			return nil
		}, eventuallyTimeout, eventuallyInterval).ShouldNot(gomega.HaveOccurred())
	})

	ginkgo.It("Should deploy agent and get available with prober func", func() {
		obj := &unstructured.Unstructured{}
		err := obj.UnmarshalJSON([]byte(deploymentJson))
		gomega.Expect(err).ToNot(gomega.HaveOccurred())
		testAddonImpl.manifests[managedClusterName] = []runtime.Object{obj}
		testAddonImpl.prober = utils.NewDeploymentProber(types.NamespacedName{Name: "nginx-deployment", Namespace: "default"})

		addon := &addonapiv1alpha1.ManagedClusterAddOn{
			ObjectMeta: metav1.ObjectMeta{
				Name: testAddonImpl.name,
			},
			Spec: addonapiv1alpha1.ManagedClusterAddOnSpec{
				InstallNamespace: "default",
			},
		}
		_, err = hubAddonClient.AddonV1alpha1().ManagedClusterAddOns(managedClusterName).Create(context.Background(), addon, metav1.CreateOptions{})
		gomega.Expect(err).ToNot(gomega.HaveOccurred())

		gomega.Eventually(func() error {
			work, err := hubWorkClient.WorkV1().ManifestWorks(managedClusterName).Get(context.Background(), manifestWorkName, metav1.GetOptions{})
			if err != nil {
				return err
			}

			if len(work.Spec.Workload.Manifests) != 1 {
				return fmt.Errorf("Unexpected number of work manifests")
			}

			if len(work.Spec.ManifestConfigs) != 1 {
				return fmt.Errorf("Unexpected number of work manifests configuration")
			}

			if apiequality.Semantic.DeepEqual(work.Spec.Workload.Manifests[0].Raw, []byte(deploymentJson)) {
				return fmt.Errorf("expected manifest is no correct, get %v", work.Spec.Workload.Manifests[0].Raw)
			}
			return nil
		}, eventuallyTimeout, eventuallyInterval).ShouldNot(gomega.HaveOccurred())

		// Update work status to trigger addon status
		work, err := hubWorkClient.WorkV1().ManifestWorks(managedClusterName).Get(context.Background(), manifestWorkName, metav1.GetOptions{})
		gomega.Expect(err).ToNot(gomega.HaveOccurred())
		meta.SetStatusCondition(&work.Status.Conditions, metav1.Condition{Type: workapiv1.WorkAvailable, Status: metav1.ConditionTrue, Reason: "WorkAvailable"})

		replica := int64(1)

		// update work status to a wrong feedback status
		work.Status.ResourceStatus = workapiv1.ManifestResourceStatus{
			Manifests: []workapiv1.ManifestCondition{
				{
					ResourceMeta: workapiv1.ManifestResourceMeta{
						Ordinal:   0,
						Group:     "apps",
						Resource:  "deployments",
						Name:      "nginx-deployment",
						Namespace: "default",
					},
					StatusFeedbacks: workapiv1.StatusFeedbackResult{
						Values: []workapiv1.FeedbackValue{
							{
								Name: "Replicas",
								Value: workapiv1.FieldValue{
									Type:    workapiv1.Integer,
									Integer: &replica,
								},
							},
						},
					},
					Conditions: []metav1.Condition{
						{
							Type:               "Available",
							Status:             metav1.ConditionTrue,
							Reason:             "MinimumReplicasAvailable",
							Message:            "Deployment has minimum availability.",
							LastTransitionTime: metav1.NewTime(time.Now()),
						},
					},
				},
			},
		}
		_, err = hubWorkClient.WorkV1().ManifestWorks(managedClusterName).UpdateStatus(context.Background(), work, metav1.UpdateOptions{})
		gomega.Expect(err).ToNot(gomega.HaveOccurred())

		gomega.Eventually(func() error {
			addon, err := hubAddonClient.AddonV1alpha1().ManagedClusterAddOns(managedClusterName).Get(context.Background(), testAddonImpl.name, metav1.GetOptions{})
			if err != nil {
				return err
			}

			if !meta.IsStatusConditionFalse(addon.Status.Conditions, "Available") {
				return fmt.Errorf("Unexpected addon available condition, %v", addon.Status.Conditions)
			}
			return nil
		}, eventuallyTimeout, eventuallyInterval).ShouldNot(gomega.HaveOccurred())

		// update to the correct condition
		work, err = hubWorkClient.WorkV1().ManifestWorks(managedClusterName).Get(context.Background(), manifestWorkName, metav1.GetOptions{})
		gomega.Expect(err).ToNot(gomega.HaveOccurred())
		work.Status.ResourceStatus = workapiv1.ManifestResourceStatus{
			Manifests: []workapiv1.ManifestCondition{
				{
					ResourceMeta: workapiv1.ManifestResourceMeta{
						Ordinal:   0,
						Group:     "apps",
						Resource:  "deployments",
						Name:      "nginx-deployment",
						Namespace: "default",
					},
					StatusFeedbacks: workapiv1.StatusFeedbackResult{
						Values: []workapiv1.FeedbackValue{
							{
								Name: "ReadyReplicas",
								Value: workapiv1.FieldValue{
									Type:    workapiv1.Integer,
									Integer: &replica,
								},
							},
						},
					},
					Conditions: []metav1.Condition{
						{
							Type:               "Available",
							Status:             metav1.ConditionTrue,
							Reason:             "MinimumReplicasAvailable",
							Message:            "Deployment has minimum availability.",
							LastTransitionTime: metav1.NewTime(time.Now()),
						},
					},
				},
			},
		}
		_, err = hubWorkClient.WorkV1().ManifestWorks(managedClusterName).UpdateStatus(context.Background(), work, metav1.UpdateOptions{})
		gomega.Expect(err).ToNot(gomega.HaveOccurred())

		gomega.Eventually(func() error {
			addon, err := hubAddonClient.AddonV1alpha1().ManagedClusterAddOns(managedClusterName).Get(context.Background(), testAddonImpl.name, metav1.GetOptions{})
			if err != nil {
				return err
			}

			if !meta.IsStatusConditionTrue(addon.Status.Conditions, "Available") {
				return fmt.Errorf("Unexpected addon available condition, %v", addon.Status.Conditions)
			}
			return nil
		}, eventuallyTimeout, eventuallyInterval).ShouldNot(gomega.HaveOccurred())
	})

})
