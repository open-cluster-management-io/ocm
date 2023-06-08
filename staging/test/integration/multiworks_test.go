package integration

import (
	"context"
	"fmt"
	"time"

	"github.com/onsi/ginkgo"
	"github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
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
	configmapJson = `{
    "apiVersion": "v1",
    "data": {
        "ca.crt": "test"
    },
    "kind": "ConfigMap",
    "metadata": {
        "name": "testconfigmap",
        "namespace": "default"
    }
}`

	mycrdJson = `{
    "apiVersion": "apiextensions.k8s.io/v1",
    "kind": "CustomResourceDefinition",
    "metadata": {
        "name": "mycrd.test.io"
    },
    "spec": {
        "conversion": {
            "strategy": "None"
        },
        "group": "test.io",
        "names": {
            "kind": "Mycrd",
            "listKind": "MycrdList",
            "plural": "mycrds",
            "singular": "mycrd"
        },
        "scope": "Cluster",
        "versions": [
            {
                "name": "v1",
                "schema": {},
                "served": true,
                "storage": true,
                "subresources": {
                    "status": {}
                }
            }
        ]
    }
}`

	mycrJson = `{
    "apiVersion": "mycrd.test.io/v1",
    "kind": "Mycrd",
    "metadata": {
        "name": "mycr"
    },
    "spec": {}
}`
)

func newFakeData(size int) string {
	if size <= 0 {
		return ""
	}

	s := make([]byte, size)
	for i := 0; i < size; i++ {
		s[i] = 'a'
	}
	return string(s)
}

var _ = ginkgo.Describe("Agent deploy multi works", func() {
	var managedClusterName string
	var err error
	var manifestWorkName0, manifestWorkName1 string
	ginkgo.BeforeEach(func() {
		suffix := rand.String(5)
		managedClusterName = fmt.Sprintf("managedcluster-%s", suffix)
		manifestWorkName0 = fmt.Sprintf("%s-0", constants.DeployWorkNamePrefix(testMultiWorksAddonImpl.name))
		manifestWorkName1 = fmt.Sprintf("%s-1", constants.DeployWorkNamePrefix(testMultiWorksAddonImpl.name))
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

		cma := newClusterManagementAddon(testMultiWorksAddonImpl.name)
		_, err = hubAddonClient.AddonV1alpha1().ClusterManagementAddOns().Create(context.Background(),
			cma, metav1.CreateOptions{})
		gomega.Expect(err).ToNot(gomega.HaveOccurred())
	})

	ginkgo.AfterEach(func() {
		err = hubKubeClient.CoreV1().Namespaces().Delete(context.Background(), managedClusterName, metav1.DeleteOptions{})
		gomega.Expect(err).ToNot(gomega.HaveOccurred())
		err = hubClusterClient.ClusterV1().ManagedClusters().Delete(context.Background(), managedClusterName, metav1.DeleteOptions{})
		gomega.Expect(err).ToNot(gomega.HaveOccurred())

		err = hubAddonClient.AddonV1alpha1().ClusterManagementAddOns().Delete(context.Background(),
			testMultiWorksAddonImpl.name, metav1.DeleteOptions{})
		gomega.Expect(err).ToNot(gomega.HaveOccurred())
	})

	ginkgo.It("Should deploy agent successfully", func() {
		deploymentObj := &unstructured.Unstructured{}
		err := deploymentObj.UnmarshalJSON([]byte(deploymentJson))
		gomega.Expect(err).ToNot(gomega.HaveOccurred())
		deploymentObj.SetAnnotations(map[string]string{"data": newFakeData(150 * 1024)})
		mycrdObj := &unstructured.Unstructured{}
		err = mycrdObj.UnmarshalJSON([]byte(mycrdJson))
		gomega.Expect(err).ToNot(gomega.HaveOccurred())
		mycrdObj.SetAnnotations(map[string]string{"data": newFakeData(200 * 1024)})
		mycrObj := &unstructured.Unstructured{}
		err = mycrObj.UnmarshalJSON([]byte(mycrJson))
		gomega.Expect(err).ToNot(gomega.HaveOccurred())
		mycrObj.SetAnnotations(map[string]string{"data": newFakeData(50 * 1024)})
		testMultiWorksAddonImpl.manifests[managedClusterName] = []runtime.Object{deploymentObj, mycrdObj, mycrObj}
		testMultiWorksAddonImpl.prober = &agent.HealthProber{
			Type: agent.HealthProberTypeWork,
		}

		// case 1: apply 2 works for the addon
		addon := &addonapiv1alpha1.ManagedClusterAddOn{
			ObjectMeta: metav1.ObjectMeta{
				Name: testMultiWorksAddonImpl.name,
			},
			Spec: addonapiv1alpha1.ManagedClusterAddOnSpec{
				InstallNamespace: "default",
			},
		}
		_, err = hubAddonClient.AddonV1alpha1().ManagedClusterAddOns(managedClusterName).Create(context.Background(), addon, metav1.CreateOptions{})
		gomega.Expect(err).ToNot(gomega.HaveOccurred())

		gomega.Eventually(func() error {
			works, err := hubWorkClient.WorkV1().ManifestWorks(managedClusterName).List(context.Background(), metav1.ListOptions{
				LabelSelector: fmt.Sprintf("%s=%s", addonapiv1alpha1.AddonLabelKey, testMultiWorksAddonImpl.name),
			})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())
			if len(works.Items) != 2 {
				return fmt.Errorf("failed to get 2 works")
			}

			for _, work := range works.Items {
				switch work.Name {
				case fmt.Sprintf("%s-0", constants.DeployWorkNamePrefix(testMultiWorksAddonImpl.name)):
					if len(work.Spec.Workload.Manifests) != 2 {
						return fmt.Errorf("unexpected number of work manifests")
					}
				case fmt.Sprintf("%s-1", constants.DeployWorkNamePrefix(testMultiWorksAddonImpl.name)):
					if len(work.Spec.Workload.Manifests) != 1 {
						return fmt.Errorf("unexpected number of work manifests")
					}
				default:
					gomega.Expect(work.Name).Should(gomega.ContainSubstring(constants.DeployWorkNamePrefix(testMultiWorksAddonImpl.name)))
				}
			}

			return nil
		}, eventuallyTimeout, eventuallyInterval).ShouldNot(gomega.HaveOccurred())

		// case 2: add a manifest, work-1 will be updated
		configmapObj := &unstructured.Unstructured{}
		err = configmapObj.UnmarshalJSON([]byte(configmapJson))
		gomega.Expect(err).ToNot(gomega.HaveOccurred())
		configmapObj.SetAnnotations(map[string]string{"data": newFakeData(80 * 1024)})
		testMultiWorksAddonImpl.manifests[managedClusterName] = []runtime.Object{deploymentObj, mycrdObj, mycrObj, configmapObj}

		// update addon to trigger reconcile
		addon, err = hubAddonClient.AddonV1alpha1().ManagedClusterAddOns(managedClusterName).Get(context.Background(), testMultiWorksAddonImpl.name, metav1.GetOptions{})
		gomega.Expect(err).ToNot(gomega.HaveOccurred())
		addon.SetLabels(map[string]string{"trigger": "2"})
		_, err = hubAddonClient.AddonV1alpha1().ManagedClusterAddOns(managedClusterName).Update(context.Background(), addon, metav1.UpdateOptions{})
		gomega.Expect(err).ToNot(gomega.HaveOccurred())

		gomega.Eventually(func() error {
			works, err := hubWorkClient.WorkV1().ManifestWorks(managedClusterName).List(context.Background(), metav1.ListOptions{
				LabelSelector: fmt.Sprintf("%s=%s", addonapiv1alpha1.AddonLabelKey, testMultiWorksAddonImpl.name),
			})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())
			if len(works.Items) != 2 {
				return fmt.Errorf("failed to get 2 works")
			}

			for _, work := range works.Items {
				switch work.Name {
				case fmt.Sprintf("%s-0", constants.DeployWorkNamePrefix(testMultiWorksAddonImpl.name)):
					if len(work.Spec.Workload.Manifests) != 2 {
						return fmt.Errorf("unexpected number of work manifests")
					}
				case fmt.Sprintf("%s-1", constants.DeployWorkNamePrefix(testMultiWorksAddonImpl.name)):
					if len(work.Spec.Workload.Manifests) != 2 {
						return fmt.Errorf("unexpected number of work manifests")
					}
				default:
					gomega.Expect(work.Name).Should(gomega.ContainSubstring(constants.DeployWorkNamePrefix(testMultiWorksAddonImpl.name)))
				}
			}

			return nil
		}, eventuallyTimeout, eventuallyInterval).ShouldNot(gomega.HaveOccurred())

		// case 3: delete a manifest, work-0 will be updated
		testMultiWorksAddonImpl.manifests[managedClusterName] = []runtime.Object{mycrdObj, mycrObj, configmapObj}

		// update addon to trigger reconcile
		addon, err = hubAddonClient.AddonV1alpha1().ManagedClusterAddOns(managedClusterName).Get(context.Background(), testMultiWorksAddonImpl.name, metav1.GetOptions{})
		gomega.Expect(err).ToNot(gomega.HaveOccurred())
		addon.SetLabels(map[string]string{"trigger": "3"})
		_, err = hubAddonClient.AddonV1alpha1().ManagedClusterAddOns(managedClusterName).Update(context.Background(), addon, metav1.UpdateOptions{})
		gomega.Expect(err).ToNot(gomega.HaveOccurred())

		gomega.Eventually(func() error {
			works, err := hubWorkClient.WorkV1().ManifestWorks(managedClusterName).List(context.Background(), metav1.ListOptions{
				LabelSelector: fmt.Sprintf("%s=%s", addonapiv1alpha1.AddonLabelKey, testMultiWorksAddonImpl.name),
			})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())
			if len(works.Items) != 2 {
				return fmt.Errorf("failed to get 2 works")
			}

			for _, work := range works.Items {
				switch work.Name {
				case fmt.Sprintf("%s-0", constants.DeployWorkNamePrefix(testMultiWorksAddonImpl.name)):
					if len(work.Spec.Workload.Manifests) != 1 {
						return fmt.Errorf("unexpected number of work manifests")
					}
				case fmt.Sprintf("%s-1", constants.DeployWorkNamePrefix(testMultiWorksAddonImpl.name)):
					if len(work.Spec.Workload.Manifests) != 2 {
						return fmt.Errorf("unexpected number of work manifests")
					}
				default:
					gomega.Expect(work.Name).Should(gomega.ContainSubstring(constants.DeployWorkNamePrefix(testMultiWorksAddonImpl.name)))
				}
			}

			return nil
		}, eventuallyTimeout, eventuallyInterval).ShouldNot(gomega.HaveOccurred())

		// case 4: Update works status to trigger addon status
		work, err := hubWorkClient.WorkV1().ManifestWorks(managedClusterName).Get(context.Background(), manifestWorkName0, metav1.GetOptions{})
		gomega.Expect(err).ToNot(gomega.HaveOccurred())
		meta.SetStatusCondition(&work.Status.Conditions, metav1.Condition{Type: "Applied", Status: metav1.ConditionTrue, Reason: "WorkApplied"})
		_, err = hubWorkClient.WorkV1().ManifestWorks(managedClusterName).UpdateStatus(context.Background(), work, metav1.UpdateOptions{})
		gomega.Expect(err).ToNot(gomega.HaveOccurred())

		work, err = hubWorkClient.WorkV1().ManifestWorks(managedClusterName).Get(context.Background(), manifestWorkName1, metav1.GetOptions{})
		gomega.Expect(err).ToNot(gomega.HaveOccurred())
		meta.SetStatusCondition(&work.Status.Conditions, metav1.Condition{Type: "Applied", Status: metav1.ConditionTrue, Reason: "WorkApplied"})
		_, err = hubWorkClient.WorkV1().ManifestWorks(managedClusterName).UpdateStatus(context.Background(), work, metav1.UpdateOptions{})
		gomega.Expect(err).ToNot(gomega.HaveOccurred())

		gomega.Eventually(func() error {
			addon, err := hubAddonClient.AddonV1alpha1().ManagedClusterAddOns(managedClusterName).Get(context.Background(), testMultiWorksAddonImpl.name, metav1.GetOptions{})
			if err != nil {
				return err
			}

			if !meta.IsStatusConditionTrue(addon.Status.Conditions, "ManifestApplied") {
				return fmt.Errorf("Unexpected addon applied condition, %v", addon.Status.Conditions)
			}
			return nil
		}, eventuallyTimeout, eventuallyInterval).ShouldNot(gomega.HaveOccurred())

		// update work to available so addon becomes available
		work, err = hubWorkClient.WorkV1().ManifestWorks(managedClusterName).Get(context.Background(), manifestWorkName0, metav1.GetOptions{})
		gomega.Expect(err).ToNot(gomega.HaveOccurred())
		meta.SetStatusCondition(&work.Status.Conditions, metav1.Condition{Type: workapiv1.WorkAvailable, Status: metav1.ConditionTrue, Reason: "WorkAvailable"})
		_, err = hubWorkClient.WorkV1().ManifestWorks(managedClusterName).UpdateStatus(context.Background(), work, metav1.UpdateOptions{})
		gomega.Expect(err).ToNot(gomega.HaveOccurred())

		work, err = hubWorkClient.WorkV1().ManifestWorks(managedClusterName).Get(context.Background(), manifestWorkName1, metav1.GetOptions{})
		gomega.Expect(err).ToNot(gomega.HaveOccurred())
		meta.SetStatusCondition(&work.Status.Conditions, metav1.Condition{Type: workapiv1.WorkAvailable, Status: metav1.ConditionTrue, Reason: "WorkAvailable"})
		_, err = hubWorkClient.WorkV1().ManifestWorks(managedClusterName).UpdateStatus(context.Background(), work, metav1.UpdateOptions{})
		gomega.Expect(err).ToNot(gomega.HaveOccurred())
		gomega.Eventually(func() error {
			addon, err := hubAddonClient.AddonV1alpha1().ManagedClusterAddOns(managedClusterName).Get(context.Background(), testMultiWorksAddonImpl.name, metav1.GetOptions{})
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
		deploymentObj0 := &unstructured.Unstructured{}
		err := deploymentObj0.UnmarshalJSON([]byte(deploymentJson))
		gomega.Expect(err).ToNot(gomega.HaveOccurred())
		deploymentObj0.SetName("nginx-deployment-0")
		deploymentObj0.SetAnnotations(map[string]string{"data": newFakeData(150 * 1024)})
		deploymentObj1 := &unstructured.Unstructured{}
		err = deploymentObj1.UnmarshalJSON([]byte(deploymentJson))
		gomega.Expect(err).ToNot(gomega.HaveOccurred())
		deploymentObj1.SetName("nginx-deployment-1")
		deploymentObj1.SetAnnotations(map[string]string{"data": newFakeData(150 * 1024)})
		mycrdObj := &unstructured.Unstructured{}
		err = mycrdObj.UnmarshalJSON([]byte(mycrdJson))
		gomega.Expect(err).ToNot(gomega.HaveOccurred())
		mycrdObj.SetAnnotations(map[string]string{"data": newFakeData(200 * 1024)})
		mycrObj := &unstructured.Unstructured{}
		err = mycrObj.UnmarshalJSON([]byte(mycrJson))
		gomega.Expect(err).ToNot(gomega.HaveOccurred())
		mycrObj.SetAnnotations(map[string]string{"data": newFakeData(50 * 1024)})
		testMultiWorksAddonImpl.manifests[managedClusterName] = []runtime.Object{deploymentObj0, deploymentObj1, mycrdObj, mycrObj}

		testMultiWorksAddonImpl.prober = utils.NewDeploymentProber(types.NamespacedName{Name: "nginx-deployment-0", Namespace: "default"},
			types.NamespacedName{Name: "nginx-deployment-1", Namespace: "default"})

		addon := &addonapiv1alpha1.ManagedClusterAddOn{
			ObjectMeta: metav1.ObjectMeta{
				Name: testMultiWorksAddonImpl.name,
			},
			Spec: addonapiv1alpha1.ManagedClusterAddOnSpec{
				InstallNamespace: "default",
			},
		}
		_, err = hubAddonClient.AddonV1alpha1().ManagedClusterAddOns(managedClusterName).Create(context.Background(), addon, metav1.CreateOptions{})
		gomega.Expect(err).ToNot(gomega.HaveOccurred())

		gomega.Eventually(func() error {
			works, err := hubWorkClient.WorkV1().ManifestWorks(managedClusterName).List(context.Background(), metav1.ListOptions{
				LabelSelector: fmt.Sprintf("%s=%s", addonapiv1alpha1.AddonLabelKey, testMultiWorksAddonImpl.name),
			})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())
			if len(works.Items) != 2 {
				return fmt.Errorf("failed to get 2 works")
			}

			for _, work := range works.Items {
				switch work.Name {
				case fmt.Sprintf("%s-0", constants.DeployWorkNamePrefix(testMultiWorksAddonImpl.name)):
					if len(work.Spec.Workload.Manifests) != 2 {
						return fmt.Errorf("unexpected number of work manifests")
					}
				case fmt.Sprintf("%s-1", constants.DeployWorkNamePrefix(testMultiWorksAddonImpl.name)):
					if len(work.Spec.Workload.Manifests) != 2 {
						return fmt.Errorf("unexpected number of work manifests")
					}
				default:
					gomega.Expect(work.Name).Should(gomega.ContainSubstring(constants.DeployWorkNamePrefix(testMultiWorksAddonImpl.name)))
				}
			}

			return nil
		}, eventuallyTimeout, eventuallyInterval).ShouldNot(gomega.HaveOccurred())

		// Update work0 and work1 status to trigger addon status
		replica := int64(1)
		for i := 0; i <= 1; i++ {
			workName := fmt.Sprintf("%s-%d", constants.DeployWorkNamePrefix(testMultiWorksAddonImpl.name), i)
			work, err := hubWorkClient.WorkV1().ManifestWorks(managedClusterName).Get(context.Background(), workName, metav1.GetOptions{})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())
			meta.SetStatusCondition(&work.Status.Conditions, metav1.Condition{Type: workapiv1.WorkAvailable, Status: metav1.ConditionTrue, Reason: "WorkAvailable"})
			meta.SetStatusCondition(&work.Status.Conditions, metav1.Condition{Type: workapiv1.WorkApplied, Status: metav1.ConditionTrue, Reason: "WorkApplied"})
			// update work0 status to a wrong feedback status
			work.Status.ResourceStatus = workapiv1.ManifestResourceStatus{
				Manifests: []workapiv1.ManifestCondition{
					{
						ResourceMeta: workapiv1.ManifestResourceMeta{
							Ordinal:   0,
							Group:     "apps",
							Resource:  "deployments",
							Name:      fmt.Sprintf("nginx-deployment-%d", i),
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
		}

		gomega.Eventually(func() error {
			addon, err := hubAddonClient.AddonV1alpha1().ManagedClusterAddOns(managedClusterName).Get(context.Background(), testMultiWorksAddonImpl.name, metav1.GetOptions{})
			if err != nil {
				return err
			}

			if !meta.IsStatusConditionFalse(addon.Status.Conditions, "Available") {
				return fmt.Errorf("unexpected addon available condition, %v", addon.Status.Conditions)
			}
			return nil
		}, eventuallyTimeout, eventuallyInterval).ShouldNot(gomega.HaveOccurred())

		// update work0 to the correct condition, the addon is not available
		work, err := hubWorkClient.WorkV1().ManifestWorks(managedClusterName).Get(context.Background(), manifestWorkName0, metav1.GetOptions{})
		gomega.Expect(err).ToNot(gomega.HaveOccurred())
		work.Status.ResourceStatus = workapiv1.ManifestResourceStatus{
			Manifests: []workapiv1.ManifestCondition{
				{
					ResourceMeta: workapiv1.ManifestResourceMeta{
						Ordinal:   0,
						Group:     "apps",
						Resource:  "deployments",
						Name:      "nginx-deployment-0",
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
			addon, err := hubAddonClient.AddonV1alpha1().ManagedClusterAddOns(managedClusterName).Get(context.Background(), testMultiWorksAddonImpl.name, metav1.GetOptions{})
			if err != nil {
				return err
			}

			if !meta.IsStatusConditionFalse(addon.Status.Conditions, "Available") {
				return fmt.Errorf("unexpected addon available condition, %v", addon.Status.Conditions)
			}
			return nil
		}, eventuallyTimeout, eventuallyInterval).ShouldNot(gomega.HaveOccurred())

		// update work1 to the correct condition and the work is available
		work, err = hubWorkClient.WorkV1().ManifestWorks(managedClusterName).Get(context.Background(), manifestWorkName1, metav1.GetOptions{})
		gomega.Expect(err).ToNot(gomega.HaveOccurred())
		work.Status.ResourceStatus = workapiv1.ManifestResourceStatus{
			Manifests: []workapiv1.ManifestCondition{
				{
					ResourceMeta: workapiv1.ManifestResourceMeta{
						Ordinal:   0,
						Group:     "apps",
						Resource:  "deployments",
						Name:      "nginx-deployment-1",
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
			addon, err := hubAddonClient.AddonV1alpha1().ManagedClusterAddOns(managedClusterName).Get(context.Background(), testMultiWorksAddonImpl.name, metav1.GetOptions{})
			if err != nil {
				return err
			}

			if !meta.IsStatusConditionTrue(addon.Status.Conditions, "Available") {
				return fmt.Errorf("unexpected addon available condition, %v", addon.Status.Conditions)
			}
			return nil
		}, eventuallyTimeout, eventuallyInterval).ShouldNot(gomega.HaveOccurred())
	})
})
