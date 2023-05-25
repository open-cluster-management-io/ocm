package integration

import (
	"context"
	"fmt"
	"time"

	"github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
	utilrand "k8s.io/apimachinery/pkg/util/rand"
	"k8s.io/utils/pointer"
	workapiv1 "open-cluster-management.io/api/work/v1"
	"open-cluster-management.io/work/pkg/spoke"
	"open-cluster-management.io/work/test/integration/util"
)

var _ = ginkgo.Describe("ManifestWork Update Strategy", func() {
	var o *spoke.WorkloadAgentOptions
	var cancel context.CancelFunc

	var work *workapiv1.ManifestWork
	var manifests []workapiv1.Manifest

	var err error

	ginkgo.BeforeEach(func() {
		o = spoke.NewWorkloadAgentOptions()
		o.HubKubeconfigFile = hubKubeconfigFileName
		o.SpokeClusterName = utilrand.String(5)
		o.StatusSyncInterval = 3 * time.Second

		ns := &corev1.Namespace{}
		ns.Name = o.SpokeClusterName
		_, err := spokeKubeClient.CoreV1().Namespaces().Create(context.Background(), ns, metav1.CreateOptions{})
		gomega.Expect(err).ToNot(gomega.HaveOccurred())

		var ctx context.Context
		ctx, cancel = context.WithCancel(context.Background())
		go startWorkAgent(ctx, o)

		// reset manifests
		manifests = nil
	})

	ginkgo.JustBeforeEach(func() {
		work = util.NewManifestWork(o.SpokeClusterName, "", manifests)
	})

	ginkgo.AfterEach(func() {
		if cancel != nil {
			cancel()
		}
		err := spokeKubeClient.CoreV1().Namespaces().Delete(context.Background(), o.SpokeClusterName, metav1.DeleteOptions{})
		gomega.Expect(err).ToNot(gomega.HaveOccurred())
	})

	ginkgo.Context("Create only strategy", func() {
		var object *unstructured.Unstructured

		ginkgo.BeforeEach(func() {
			object, _, err = util.NewDeployment(o.SpokeClusterName, "deploy1", "sa")
			gomega.Expect(err).ToNot(gomega.HaveOccurred())
			manifests = append(manifests, util.ToManifest(object))
		})

		ginkgo.It("deployed resource should not be updated when work is updated", func() {
			work.Spec.ManifestConfigs = []workapiv1.ManifestConfigOption{
				{
					ResourceIdentifier: workapiv1.ResourceIdentifier{
						Group:     "apps",
						Resource:  "deployments",
						Namespace: o.SpokeClusterName,
						Name:      "deploy1",
					},
					UpdateStrategy: &workapiv1.UpdateStrategy{
						Type: workapiv1.UpdateStrategyTypeCreateOnly,
					},
				},
			}

			work, err = hubWorkClient.WorkV1().ManifestWorks(o.SpokeClusterName).Create(context.Background(), work, metav1.CreateOptions{})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())

			util.AssertWorkCondition(work.Namespace, work.Name, hubWorkClient, string(workapiv1.WorkApplied), metav1.ConditionTrue,
				[]metav1.ConditionStatus{metav1.ConditionTrue}, eventuallyTimeout, eventuallyInterval)

			// update work
			err = unstructured.SetNestedField(object.Object, int64(3), "spec", "replicas")
			gomega.Expect(err).ToNot(gomega.HaveOccurred())
			gomega.Eventually(func() error {
				work, err = hubWorkClient.WorkV1().ManifestWorks(o.SpokeClusterName).Get(context.Background(), work.Name, metav1.GetOptions{})
				if err != nil {
					return err
				}

				work.Spec.Workload.Manifests[0] = util.ToManifest(object)
				_, err = hubWorkClient.WorkV1().ManifestWorks(o.SpokeClusterName).Update(context.Background(), work, metav1.UpdateOptions{})
				return err
			}, eventuallyTimeout, eventuallyInterval).ShouldNot(gomega.HaveOccurred())

			util.AssertWorkCondition(work.Namespace, work.Name, hubWorkClient, string(workapiv1.WorkApplied), metav1.ConditionTrue,
				[]metav1.ConditionStatus{metav1.ConditionTrue}, eventuallyTimeout, eventuallyInterval)

			gomega.Eventually(func() error {
				deploy, err := spokeKubeClient.AppsV1().Deployments(o.SpokeClusterName).Get(context.Background(), "deploy1", metav1.GetOptions{})
				if err != nil {
					return err
				}

				if *deploy.Spec.Replicas != 1 {
					return fmt.Errorf("Replicas should not be changed")
				}

				return nil
			}, eventuallyTimeout, eventuallyInterval).ShouldNot(gomega.HaveOccurred())
		})
	})

	ginkgo.Context("Server side apply strategy", func() {
		var object *unstructured.Unstructured

		ginkgo.BeforeEach(func() {
			object, _, err = util.NewDeployment(o.SpokeClusterName, "deploy1", "sa")
			gomega.Expect(err).ToNot(gomega.HaveOccurred())
			manifests = append(manifests, util.ToManifest(object))
		})

		ginkgo.It("deployed resource should be applied when work is updated", func() {
			work.Spec.ManifestConfigs = []workapiv1.ManifestConfigOption{
				{
					ResourceIdentifier: workapiv1.ResourceIdentifier{
						Group:     "apps",
						Resource:  "deployments",
						Namespace: o.SpokeClusterName,
						Name:      "deploy1",
					},
					UpdateStrategy: &workapiv1.UpdateStrategy{
						Type: workapiv1.UpdateStrategyTypeServerSideApply,
					},
				},
			}

			work, err = hubWorkClient.WorkV1().ManifestWorks(o.SpokeClusterName).Create(context.Background(), work, metav1.CreateOptions{})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())

			util.AssertWorkCondition(work.Namespace, work.Name, hubWorkClient, string(workapiv1.WorkApplied), metav1.ConditionTrue,
				[]metav1.ConditionStatus{metav1.ConditionTrue}, eventuallyTimeout, eventuallyInterval)

			// update work
			err = unstructured.SetNestedField(object.Object, int64(3), "spec", "replicas")
			gomega.Eventually(func() error {
				work, err = hubWorkClient.WorkV1().ManifestWorks(o.SpokeClusterName).Get(context.Background(), work.Name, metav1.GetOptions{})
				if err != nil {
					return err
				}

				work.Spec.Workload.Manifests[0] = util.ToManifest(object)
				_, err = hubWorkClient.WorkV1().ManifestWorks(o.SpokeClusterName).Update(context.Background(), work, metav1.UpdateOptions{})
				return err
			}, eventuallyTimeout, eventuallyInterval).ShouldNot(gomega.HaveOccurred())

			gomega.Eventually(func() error {
				deploy, err := spokeKubeClient.AppsV1().Deployments(o.SpokeClusterName).Get(context.Background(), "deploy1", metav1.GetOptions{})
				if err != nil {
					return err
				}

				if *deploy.Spec.Replicas != 3 {
					return fmt.Errorf("Replicas should be updated to 3 but got %d", *deploy.Spec.Replicas)
				}

				return nil
			}, eventuallyTimeout, eventuallyInterval).ShouldNot(gomega.HaveOccurred())
		})

		ginkgo.It("should get conflict if a field is taken by another manager", func() {
			work.Spec.ManifestConfigs = []workapiv1.ManifestConfigOption{
				{
					ResourceIdentifier: workapiv1.ResourceIdentifier{
						Group:     "apps",
						Resource:  "deployments",
						Namespace: o.SpokeClusterName,
						Name:      "deploy1",
					},
					UpdateStrategy: &workapiv1.UpdateStrategy{
						Type: workapiv1.UpdateStrategyTypeServerSideApply,
					},
				},
			}

			work, err = hubWorkClient.WorkV1().ManifestWorks(o.SpokeClusterName).Create(context.Background(), work, metav1.CreateOptions{})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())

			util.AssertWorkCondition(work.Namespace, work.Name, hubWorkClient, string(workapiv1.WorkApplied), metav1.ConditionTrue,
				[]metav1.ConditionStatus{metav1.ConditionTrue}, eventuallyTimeout, eventuallyInterval)

			// update deployment with another field manager
			err = unstructured.SetNestedField(object.Object, int64(2), "spec", "replicas")
			gomega.Expect(err).ToNot(gomega.HaveOccurred())
			patch, err := object.MarshalJSON()
			gomega.Expect(err).ToNot(gomega.HaveOccurred())
			_, err = spokeKubeClient.AppsV1().Deployments(o.SpokeClusterName).Patch(
				context.Background(), "deploy1", types.ApplyPatchType, []byte(patch), metav1.PatchOptions{Force: pointer.Bool(true), FieldManager: "test-integration"})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())

			// Update deployment by work
			err = unstructured.SetNestedField(object.Object, int64(3), "spec", "replicas")
			gomega.Expect(err).ToNot(gomega.HaveOccurred())
			gomega.Eventually(func() error {
				work, err = hubWorkClient.WorkV1().ManifestWorks(o.SpokeClusterName).Get(context.Background(), work.Name, metav1.GetOptions{})
				if err != nil {
					return err
				}

				work.Spec.Workload.Manifests[0] = util.ToManifest(object)
				_, err = hubWorkClient.WorkV1().ManifestWorks(o.SpokeClusterName).Update(context.Background(), work, metav1.UpdateOptions{})
				return err
			}, eventuallyTimeout, eventuallyInterval).ShouldNot(gomega.HaveOccurred())

			// Failed to apply due to conflict
			util.AssertWorkCondition(work.Namespace, work.Name, hubWorkClient, string(workapiv1.WorkApplied), metav1.ConditionFalse,
				[]metav1.ConditionStatus{metav1.ConditionFalse}, eventuallyTimeout, eventuallyInterval)

			// remove the replica field and the apply should work
			unstructured.RemoveNestedField(object.Object, "spec", "replicas")
			gomega.Eventually(func() error {
				work, err = hubWorkClient.WorkV1().ManifestWorks(o.SpokeClusterName).Get(context.Background(), work.Name, metav1.GetOptions{})
				if err != nil {
					return err
				}

				work.Spec.Workload.Manifests[0] = util.ToManifest(object)
				_, err = hubWorkClient.WorkV1().ManifestWorks(o.SpokeClusterName).Update(context.Background(), work, metav1.UpdateOptions{})
				return err
			}, eventuallyTimeout, eventuallyInterval).ShouldNot(gomega.HaveOccurred())

			util.AssertWorkCondition(work.Namespace, work.Name, hubWorkClient, string(workapiv1.WorkApplied), metav1.ConditionTrue,
				[]metav1.ConditionStatus{metav1.ConditionTrue}, eventuallyTimeout, eventuallyInterval)
		})

		ginkgo.It("two manifest works with different field manager", func() {
			work.Spec.ManifestConfigs = []workapiv1.ManifestConfigOption{
				{
					ResourceIdentifier: workapiv1.ResourceIdentifier{
						Group:     "apps",
						Resource:  "deployments",
						Namespace: o.SpokeClusterName,
						Name:      "deploy1",
					},
					UpdateStrategy: &workapiv1.UpdateStrategy{
						Type: workapiv1.UpdateStrategyTypeServerSideApply,
					},
				},
			}

			work, err = hubWorkClient.WorkV1().ManifestWorks(o.SpokeClusterName).Create(context.Background(), work, metav1.CreateOptions{})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())

			util.AssertWorkCondition(work.Namespace, work.Name, hubWorkClient, string(workapiv1.WorkApplied), metav1.ConditionTrue,
				[]metav1.ConditionStatus{metav1.ConditionTrue}, eventuallyTimeout, eventuallyInterval)

			// Create another work with different fieldmanager
			objCopy := object.DeepCopy()
			// work1 does not want to own replica field
			unstructured.RemoveNestedField(objCopy.Object, "spec", "replicas")
			work1 := util.NewManifestWork(o.SpokeClusterName, "another", []workapiv1.Manifest{util.ToManifest(objCopy)})
			work1.Spec.ManifestConfigs = []workapiv1.ManifestConfigOption{
				{
					ResourceIdentifier: workapiv1.ResourceIdentifier{
						Group:     "apps",
						Resource:  "deployments",
						Namespace: o.SpokeClusterName,
						Name:      "deploy1",
					},
					UpdateStrategy: &workapiv1.UpdateStrategy{
						Type: workapiv1.UpdateStrategyTypeServerSideApply,
						ServerSideApply: &workapiv1.ServerSideApplyConfig{
							Force:        true,
							FieldManager: "work-agent-another",
						},
					},
				},
			}

			_, err = hubWorkClient.WorkV1().ManifestWorks(o.SpokeClusterName).Create(context.Background(), work1, metav1.CreateOptions{})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())

			util.AssertWorkCondition(work1.Namespace, work1.Name, hubWorkClient, string(workapiv1.WorkApplied), metav1.ConditionTrue,
				[]metav1.ConditionStatus{metav1.ConditionTrue}, eventuallyTimeout, eventuallyInterval)

			// Update deployment replica by work should work since this work still owns the replicas field
			err = unstructured.SetNestedField(object.Object, int64(3), "spec", "replicas")
			gomega.Expect(err).ToNot(gomega.HaveOccurred())
			gomega.Eventually(func() error {
				work, err = hubWorkClient.WorkV1().ManifestWorks(o.SpokeClusterName).Get(context.Background(), work.Name, metav1.GetOptions{})
				if err != nil {
					return err
				}

				work.Spec.Workload.Manifests[0] = util.ToManifest(object)
				_, err = hubWorkClient.WorkV1().ManifestWorks(o.SpokeClusterName).Update(context.Background(), work, metav1.UpdateOptions{})
				return err
			}, eventuallyTimeout, eventuallyInterval).ShouldNot(gomega.HaveOccurred())

			// This should work since this work still own replicas
			util.AssertWorkCondition(work.Namespace, work.Name, hubWorkClient, string(workapiv1.WorkApplied), metav1.ConditionTrue,
				[]metav1.ConditionStatus{metav1.ConditionTrue}, eventuallyTimeout, eventuallyInterval)

			gomega.Eventually(func() error {
				deploy, err := spokeKubeClient.AppsV1().Deployments(o.SpokeClusterName).Get(context.Background(), "deploy1", metav1.GetOptions{})
				if err != nil {
					return err
				}

				if *deploy.Spec.Replicas != 3 {
					return fmt.Errorf("expected replica is not correct, got %d", *deploy.Spec.Replicas)
				}

				return nil
			}, eventuallyTimeout, eventuallyInterval).ShouldNot(gomega.HaveOccurred())

			// Update sa field will not work
			err = unstructured.SetNestedField(object.Object, "another-sa", "spec", "template", "spec", "serviceAccountName")
			gomega.Expect(err).ToNot(gomega.HaveOccurred())
			gomega.Eventually(func() error {
				work, err = hubWorkClient.WorkV1().ManifestWorks(o.SpokeClusterName).Get(context.Background(), work.Name, metav1.GetOptions{})
				if err != nil {
					return err
				}

				work.Spec.Workload.Manifests[0] = util.ToManifest(object)
				_, err = hubWorkClient.WorkV1().ManifestWorks(o.SpokeClusterName).Update(context.Background(), work, metav1.UpdateOptions{})
				return err
			}, eventuallyTimeout, eventuallyInterval).ShouldNot(gomega.HaveOccurred())

			// This should work since this work still own replicas
			util.AssertWorkCondition(work.Namespace, work.Name, hubWorkClient, string(workapiv1.WorkApplied), metav1.ConditionFalse,
				[]metav1.ConditionStatus{metav1.ConditionFalse}, eventuallyTimeout, eventuallyInterval)
		})

		ginkgo.It("with delete options", func() {
			work.Spec.ManifestConfigs = []workapiv1.ManifestConfigOption{
				{
					ResourceIdentifier: workapiv1.ResourceIdentifier{
						Group:     "apps",
						Resource:  "deployments",
						Namespace: o.SpokeClusterName,
						Name:      "deploy1",
					},
					UpdateStrategy: &workapiv1.UpdateStrategy{
						Type: workapiv1.UpdateStrategyTypeServerSideApply,
					},
				},
			}

			work, err = hubWorkClient.WorkV1().ManifestWorks(o.SpokeClusterName).Create(context.Background(), work, metav1.CreateOptions{})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())

			util.AssertWorkCondition(work.Namespace, work.Name, hubWorkClient, string(workapiv1.WorkApplied), metav1.ConditionTrue,
				[]metav1.ConditionStatus{metav1.ConditionTrue}, eventuallyTimeout, eventuallyInterval)

			// Create another work with different fieldmanager
			objCopy := object.DeepCopy()
			// work1 does not want to own replica field
			unstructured.RemoveNestedField(objCopy.Object, "spec", "replicas")
			work1 := util.NewManifestWork(o.SpokeClusterName, "another", []workapiv1.Manifest{util.ToManifest(objCopy)})
			work1.Spec.ManifestConfigs = []workapiv1.ManifestConfigOption{
				{
					ResourceIdentifier: workapiv1.ResourceIdentifier{
						Group:     "apps",
						Resource:  "deployments",
						Namespace: o.SpokeClusterName,
						Name:      "deploy1",
					},
					UpdateStrategy: &workapiv1.UpdateStrategy{
						Type: workapiv1.UpdateStrategyTypeServerSideApply,
						ServerSideApply: &workapiv1.ServerSideApplyConfig{
							Force:        true,
							FieldManager: "work-agent-another",
						},
					},
				},
			}

			_, err = hubWorkClient.WorkV1().ManifestWorks(o.SpokeClusterName).Create(context.Background(), work1, metav1.CreateOptions{})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())

			util.AssertWorkCondition(work1.Namespace, work1.Name, hubWorkClient, string(workapiv1.WorkApplied), metav1.ConditionTrue,
				[]metav1.ConditionStatus{metav1.ConditionTrue}, eventuallyTimeout, eventuallyInterval)

			gomega.Eventually(func() error {
				deploy, err := spokeKubeClient.AppsV1().Deployments(o.SpokeClusterName).Get(context.Background(), "deploy1", metav1.GetOptions{})
				if err != nil {
					return err
				}

				if len(deploy.OwnerReferences) != 2 {
					return fmt.Errorf("expected ownerrefs is not correct, got %v", deploy.OwnerReferences)
				}

				return nil
			}, eventuallyTimeout, eventuallyInterval).ShouldNot(gomega.HaveOccurred())

			// update deleteOption of the first work
			gomega.Eventually(func() error {
				work, err = hubWorkClient.WorkV1().ManifestWorks(o.SpokeClusterName).Get(context.Background(), work.Name, metav1.GetOptions{})
				if err != nil {
					return err
				}

				work.Spec.DeleteOption = &workapiv1.DeleteOption{PropagationPolicy: workapiv1.DeletePropagationPolicyTypeOrphan}
				_, err = hubWorkClient.WorkV1().ManifestWorks(o.SpokeClusterName).Update(context.Background(), work, metav1.UpdateOptions{})
				return err
			}, eventuallyTimeout, eventuallyInterval).ShouldNot(gomega.HaveOccurred())

			gomega.Eventually(func() error {
				deploy, err := spokeKubeClient.AppsV1().Deployments(o.SpokeClusterName).Get(context.Background(), "deploy1", metav1.GetOptions{})
				if err != nil {
					return err
				}

				if len(deploy.OwnerReferences) != 1 {
					return fmt.Errorf("expected ownerrefs is not correct, got %v", deploy.OwnerReferences)
				}

				return nil
			}, eventuallyTimeout, eventuallyInterval).ShouldNot(gomega.HaveOccurred())
		})
	})
})
