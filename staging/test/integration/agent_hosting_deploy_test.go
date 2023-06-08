package integration

import (
	"context"
	"fmt"
	"time"

	"github.com/onsi/ginkgo"
	"github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	apiequality "k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/api/errors"
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
	deploymentHostingJson = `{
		"apiVersion": "apps/v1",
		"kind": "Deployment",
		"metadata": {
			"name": "nginx-deployment",
			"namespace": "default",
			"annotations": {
				"addon.open-cluster-management.io/hosted-manifest-location": "hosting"
			}
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
	var managedClusterName, hostingClusterName string
	var err error
	var hostingManifestWorkName string
	ginkgo.BeforeEach(func() {
		suffix := rand.String(5)
		managedClusterName = fmt.Sprintf("managedcluster-%s", suffix)
		hostingClusterName = fmt.Sprintf("hostingcluster-%s", suffix)
		hostingManifestWorkName = fmt.Sprintf("%s-0", constants.DeployHostingWorkNamePrefix(managedClusterName, testHostedAddonImpl.name))

		managedCluster := &clusterv1.ManagedCluster{
			ObjectMeta: metav1.ObjectMeta{
				Name: managedClusterName,
			},
			Spec: clusterv1.ManagedClusterSpec{
				HubAcceptsClient: true,
			},
		}
		hostingCluster := &clusterv1.ManagedCluster{
			ObjectMeta: metav1.ObjectMeta{
				Name: hostingClusterName,
			},
			Spec: clusterv1.ManagedClusterSpec{
				HubAcceptsClient: true,
			},
		}
		_, err = hubClusterClient.ClusterV1().ManagedClusters().Create(
			context.Background(), managedCluster, metav1.CreateOptions{})
		gomega.Expect(err).ToNot(gomega.HaveOccurred())

		ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: managedClusterName}}
		_, err = hubKubeClient.CoreV1().Namespaces().Create(context.Background(), ns, metav1.CreateOptions{})
		gomega.Expect(err).ToNot(gomega.HaveOccurred())

		_, err = hubClusterClient.ClusterV1().ManagedClusters().Create(
			context.Background(), hostingCluster, metav1.CreateOptions{})
		gomega.Expect(err).ToNot(gomega.HaveOccurred())

		ns = &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: hostingClusterName}}
		_, err = hubKubeClient.CoreV1().Namespaces().Create(context.Background(), ns, metav1.CreateOptions{})
		gomega.Expect(err).ToNot(gomega.HaveOccurred())

		cma := newClusterManagementAddon(testHostedAddonImpl.name)
		_, err = hubAddonClient.AddonV1alpha1().ClusterManagementAddOns().Create(context.Background(),
			cma, metav1.CreateOptions{})
		gomega.Expect(err).ToNot(gomega.HaveOccurred())
	})

	ginkgo.AfterEach(func() {
		err = hubKubeClient.CoreV1().Namespaces().Delete(
			context.Background(), managedClusterName, metav1.DeleteOptions{})
		gomega.Expect(err).ToNot(gomega.HaveOccurred())
		err = hubClusterClient.ClusterV1().ManagedClusters().Delete(
			context.Background(), managedClusterName, metav1.DeleteOptions{})
		gomega.Expect(err).ToNot(gomega.HaveOccurred())

		err = hubKubeClient.CoreV1().Namespaces().Delete(
			context.Background(), hostingClusterName, metav1.DeleteOptions{})
		gomega.Expect(err).ToNot(gomega.HaveOccurred())
		err = hubClusterClient.ClusterV1().ManagedClusters().Delete(
			context.Background(), hostingClusterName, metav1.DeleteOptions{})
		gomega.Expect(err).ToNot(gomega.HaveOccurred())

		err = hubAddonClient.AddonV1alpha1().ClusterManagementAddOns().Delete(context.Background(),
			testHostedAddonImpl.name, metav1.DeleteOptions{})
		gomega.Expect(err).ToNot(gomega.HaveOccurred())
	})

	ginkgo.It("Should deploy and delete agent on hosting cluster successfully", func() {
		obj := &unstructured.Unstructured{}
		err := obj.UnmarshalJSON([]byte(deploymentHostingJson))
		gomega.Expect(err).ToNot(gomega.HaveOccurred())
		testHostedAddonImpl.manifests[managedClusterName] = []runtime.Object{obj}
		testHostedAddonImpl.prober = &agent.HealthProber{
			Type: agent.HealthProberTypeWork,
		}

		addon := &addonapiv1alpha1.ManagedClusterAddOn{
			ObjectMeta: metav1.ObjectMeta{
				Name: testHostedAddonImpl.name,
				Annotations: map[string]string{
					addonapiv1alpha1.HostingClusterNameAnnotationKey: hostingClusterName,
				},
			},
			Spec: addonapiv1alpha1.ManagedClusterAddOnSpec{
				InstallNamespace: "default",
			},
		}
		_, err = hubAddonClient.AddonV1alpha1().ManagedClusterAddOns(managedClusterName).Create(
			context.Background(), addon, metav1.CreateOptions{})
		gomega.Expect(err).ToNot(gomega.HaveOccurred())

		gomega.Eventually(func() error {
			work, err := hubWorkClient.WorkV1().ManifestWorks(hostingClusterName).Get(context.Background(),
				hostingManifestWorkName, metav1.GetOptions{})
			if err != nil {
				return err
			}

			if len(work.Spec.Workload.Manifests) != 1 {
				return fmt.Errorf("Unexpected number of work manifests")
			}

			if apiequality.Semantic.DeepEqual(work.Spec.Workload.Manifests[0].Raw, []byte(deploymentHostingJson)) {
				return fmt.Errorf("expected manifest is no correct, get %v", work.Spec.Workload.Manifests[0].Raw)
			}

			return nil
		}, eventuallyTimeout, eventuallyInterval).ShouldNot(gomega.HaveOccurred())

		// Update work status to trigger addon status
		work, err := hubWorkClient.WorkV1().ManifestWorks(hostingClusterName).Get(context.Background(),
			hostingManifestWorkName, metav1.GetOptions{})
		gomega.Expect(err).ToNot(gomega.HaveOccurred())
		meta.SetStatusCondition(&work.Status.Conditions,
			metav1.Condition{Type: "Applied", Status: metav1.ConditionTrue, Reason: "WorkApplied"})
		_, err = hubWorkClient.WorkV1().ManifestWorks(hostingClusterName).UpdateStatus(
			context.Background(), work, metav1.UpdateOptions{})
		gomega.Expect(err).ToNot(gomega.HaveOccurred())

		gomega.Eventually(func() error {
			addon, err := hubAddonClient.AddonV1alpha1().ManagedClusterAddOns(managedClusterName).Get(
				context.Background(), testHostedAddonImpl.name, metav1.GetOptions{})
			if err != nil {
				return err
			}

			if !meta.IsStatusConditionTrue(addon.Status.Conditions, addonapiv1alpha1.ManagedClusterAddOnHostingManifestApplied) {
				return fmt.Errorf("Unexpected addon applied condition, %v", addon.Status.Conditions)
			}
			return nil
		}, eventuallyTimeout, eventuallyInterval).ShouldNot(gomega.HaveOccurred())

		// update work to available so addon becomes available
		work, err = hubWorkClient.WorkV1().ManifestWorks(hostingClusterName).Get(context.Background(),
			hostingManifestWorkName, metav1.GetOptions{})
		gomega.Expect(err).ToNot(gomega.HaveOccurred())
		meta.SetStatusCondition(&work.Status.Conditions,
			metav1.Condition{Type: workapiv1.WorkAvailable, Status: metav1.ConditionTrue, Reason: "WorkAvailable"})
		_, err = hubWorkClient.WorkV1().ManifestWorks(hostingClusterName).UpdateStatus(
			context.Background(), work, metav1.UpdateOptions{})
		gomega.Expect(err).ToNot(gomega.HaveOccurred())

		gomega.Eventually(func() error {
			addon, err = hubAddonClient.AddonV1alpha1().ManagedClusterAddOns(managedClusterName).Get(
				context.Background(), testHostedAddonImpl.name, metav1.GetOptions{})
			if err != nil {
				return err
			}

			// HealthProberTypeWork for hosting manifestwork is not supported by now
			// TODO: consider to support it
			// if !meta.IsStatusConditionTrue(addon.Status.Conditions, "Available") {
			// 	return fmt.Errorf("Unexpected addon available condition, %v", addon.Status.Conditions)
			// }
			return nil
		}, eventuallyTimeout, eventuallyInterval).ShouldNot(gomega.HaveOccurred())

		// do nothing if cluster is deleting and addon is not deleted
		cluster, err := hubClusterClient.ClusterV1().ManagedClusters().Get(context.Background(), managedClusterName, metav1.GetOptions{})
		gomega.Expect(err).ToNot(gomega.HaveOccurred())
		cluster.SetFinalizers([]string{"cluster.open-cluster-management.io/api-resource-cleanup"})
		_, err = hubClusterClient.ClusterV1().ManagedClusters().Update(context.Background(), cluster, metav1.UpdateOptions{})
		gomega.Expect(err).ToNot(gomega.HaveOccurred())
		err = hubClusterClient.ClusterV1().ManagedClusters().Delete(context.Background(), managedClusterName, metav1.DeleteOptions{})
		gomega.Expect(err).ToNot(gomega.HaveOccurred())

		time.Sleep(5 * time.Second) // wait 5 seconds to sync
		gomega.Eventually(func() error {
			work, err = hubWorkClient.WorkV1().ManifestWorks(hostingClusterName).Get(context.Background(),
				hostingManifestWorkName, metav1.GetOptions{})
			return err
		}, eventuallyTimeout, eventuallyInterval).ShouldNot(gomega.HaveOccurred())

		// delete managedclusteraddon
		err = hubAddonClient.AddonV1alpha1().ManagedClusterAddOns(managedClusterName).Delete(
			context.Background(), testHostedAddonImpl.name, metav1.DeleteOptions{})
		gomega.Expect(err).ToNot(gomega.HaveOccurred())
		gomega.Eventually(func() bool {
			_, err := hubWorkClient.WorkV1().ManifestWorks(hostingClusterName).Get(context.Background(),
				hostingManifestWorkName, metav1.GetOptions{})
			return errors.IsNotFound(err)
		}, eventuallyTimeout, eventuallyInterval).Should(gomega.BeTrue())

		_, err = hubAddonClient.AddonV1alpha1().ManagedClusterAddOns(managedClusterName).Get(
			context.Background(), testHostedAddonImpl.name, metav1.GetOptions{})
		if err != nil {
			gomega.Expect(errors.IsNotFound(err)).To(gomega.BeTrue())
		}
	})

	ginkgo.It("Should deploy agent on hosting cluster and get available with prober func", func() {
		obj := &unstructured.Unstructured{}
		err := obj.UnmarshalJSON([]byte(deploymentHostingJson))
		gomega.Expect(err).ToNot(gomega.HaveOccurred())
		testHostedAddonImpl.manifests[managedClusterName] = []runtime.Object{obj}
		testHostedAddonImpl.prober = utils.NewDeploymentProber(
			types.NamespacedName{Name: "nginx-deployment", Namespace: "default"})

		addon := &addonapiv1alpha1.ManagedClusterAddOn{
			ObjectMeta: metav1.ObjectMeta{
				Name: testHostedAddonImpl.name,
				Annotations: map[string]string{
					addonapiv1alpha1.HostingClusterNameAnnotationKey: hostingClusterName,
				},
			},
			Spec: addonapiv1alpha1.ManagedClusterAddOnSpec{
				InstallNamespace: "default",
			},
		}
		_, err = hubAddonClient.AddonV1alpha1().ManagedClusterAddOns(managedClusterName).Create(
			context.Background(), addon, metav1.CreateOptions{})
		gomega.Expect(err).ToNot(gomega.HaveOccurred())

		gomega.Eventually(func() error {
			work, err := hubWorkClient.WorkV1().ManifestWorks(hostingClusterName).Get(context.Background(),
				hostingManifestWorkName, metav1.GetOptions{})
			if err != nil {
				return err
			}

			if len(work.Spec.Workload.Manifests) != 1 {
				return fmt.Errorf("Unexpected number of work manifests")
			}

			if len(work.Spec.ManifestConfigs) != 1 {
				return fmt.Errorf("Unexpected number of work manifests configuration")
			}

			if apiequality.Semantic.DeepEqual(work.Spec.Workload.Manifests[0].Raw, []byte(deploymentHostingJson)) {
				return fmt.Errorf("expected manifest is no correct, get %v", work.Spec.Workload.Manifests[0].Raw)
			}
			return nil
		}, eventuallyTimeout, eventuallyInterval).ShouldNot(gomega.HaveOccurred())

		// Update work status to trigger addon status
		work, err := hubWorkClient.WorkV1().ManifestWorks(hostingClusterName).Get(context.Background(),
			hostingManifestWorkName, metav1.GetOptions{})
		gomega.Expect(err).ToNot(gomega.HaveOccurred())
		meta.SetStatusCondition(&work.Status.Conditions,
			metav1.Condition{Type: workapiv1.WorkAvailable, Status: metav1.ConditionTrue, Reason: "WorkAvailable"})

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
		_, err = hubWorkClient.WorkV1().ManifestWorks(hostingClusterName).UpdateStatus(
			context.Background(), work, metav1.UpdateOptions{})
		gomega.Expect(err).ToNot(gomega.HaveOccurred())

		gomega.Eventually(func() error {
			addon, err = hubAddonClient.AddonV1alpha1().ManagedClusterAddOns(managedClusterName).Get(
				context.Background(), testHostedAddonImpl.name, metav1.GetOptions{})
			if err != nil {
				return err
			}

			// Health prober func for hosting cluster is not supported by now
			// TODO: consider to support it
			// if !meta.IsStatusConditionFalse(addon.Status.Conditions, "Available") {
			// 	return fmt.Errorf("Unexpected addon available condition, %v", addon.Status.Conditions)
			// }
			return nil
		}, eventuallyTimeout, eventuallyInterval).ShouldNot(gomega.HaveOccurred())

		// update to the correct condition
		work, err = hubWorkClient.WorkV1().ManifestWorks(hostingClusterName).Get(context.Background(),
			hostingManifestWorkName, metav1.GetOptions{})
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
		_, err = hubWorkClient.WorkV1().ManifestWorks(hostingClusterName).UpdateStatus(
			context.Background(), work, metav1.UpdateOptions{})
		gomega.Expect(err).ToNot(gomega.HaveOccurred())

		gomega.Eventually(func() error {
			addon, err = hubAddonClient.AddonV1alpha1().ManagedClusterAddOns(managedClusterName).Get(
				context.Background(), testHostedAddonImpl.name, metav1.GetOptions{})
			if err != nil {
				return err
			}

			// Health prober func for hosting cluster is not supported by now
			// TODO: consider to support it
			// if !meta.IsStatusConditionTrue(addon.Status.Conditions, "Available") {
			// 	return fmt.Errorf("Unexpected addon available condition, %v", addon.Status.Conditions)
			// }
			return nil
		}, eventuallyTimeout, eventuallyInterval).ShouldNot(gomega.HaveOccurred())
	})

})
