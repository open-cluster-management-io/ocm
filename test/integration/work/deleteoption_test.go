package work

import (
	"context"
	"fmt"

	"github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/rand"

	workapiv1 "open-cluster-management.io/api/work/v1"

	"open-cluster-management.io/ocm/test/integration/util"
)

var _ = ginkgo.Describe("ManifestWork Delete Option", func() {
	var cancel context.CancelFunc

	var workName string
	var clusterName string
	var work *workapiv1.ManifestWork
	var appliedManifestWorkName string
	var manifests []workapiv1.Manifest

	var err error

	ginkgo.BeforeEach(func() {
		clusterName = rand.String(5)
		workName = fmt.Sprintf("work-delete-option-%s", rand.String(5))

		ns := &corev1.Namespace{}
		ns.Name = clusterName
		_, err := spokeKubeClient.CoreV1().Namespaces().Create(context.Background(), ns, metav1.CreateOptions{})
		gomega.Expect(err).ToNot(gomega.HaveOccurred())

		var ctx context.Context
		ctx, cancel = context.WithCancel(context.Background())
		go startWorkAgent(ctx, clusterName)

		// reset manifests
		manifests = nil
	})

	ginkgo.JustBeforeEach(func() {
		work = util.NewManifestWork(clusterName, workName, manifests)
	})

	ginkgo.AfterEach(func() {
		if cancel != nil {
			cancel()
		}
		err := spokeKubeClient.CoreV1().Namespaces().Delete(context.Background(), clusterName, metav1.DeleteOptions{})
		gomega.Expect(err).ToNot(gomega.HaveOccurred())
	})

	ginkgo.Context("Resource sharing and adoption between manifestworks", func() {
		var anotherWork *workapiv1.ManifestWork
		var anotherAppliedManifestWorkName string
		ginkgo.BeforeEach(func() {
			manifests = []workapiv1.Manifest{
				util.ToManifest(util.NewConfigmap(clusterName, cm1, map[string]string{"a": "b"}, []string{})),
				util.ToManifest(util.NewConfigmap(clusterName, cm2, map[string]string{"c": "d"}, []string{})),
			}
			// Create another manifestworks with one shared resource.
			anotherWork = util.NewManifestWork(clusterName, "sharing-resource-work", []workapiv1.Manifest{manifests[0]})
		})

		ginkgo.JustBeforeEach(func() {
			work, err = hubWorkClient.WorkV1().ManifestWorks(clusterName).Create(context.Background(), work, metav1.CreateOptions{})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())

			appliedManifestWorkName = util.AppliedManifestWorkName(hubHash, work)

			util.AssertWorkCondition(work.Namespace, work.Name, hubWorkClient, workapiv1.WorkApplied, metav1.ConditionTrue,
				[]metav1.ConditionStatus{metav1.ConditionTrue, metav1.ConditionTrue}, eventuallyTimeout, eventuallyInterval)
			util.AssertWorkCondition(work.Namespace, work.Name, hubWorkClient, workapiv1.WorkAvailable, metav1.ConditionTrue,
				[]metav1.ConditionStatus{metav1.ConditionTrue, metav1.ConditionTrue}, eventuallyTimeout, eventuallyInterval)

			anotherWork, err = hubWorkClient.WorkV1().ManifestWorks(clusterName).Create(context.Background(), anotherWork, metav1.CreateOptions{})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())
			util.AssertWorkCondition(anotherWork.Namespace, anotherWork.Name, hubWorkClient, workapiv1.WorkApplied, metav1.ConditionTrue,
				[]metav1.ConditionStatus{metav1.ConditionTrue}, eventuallyTimeout, eventuallyInterval)
			util.AssertWorkCondition(anotherWork.Namespace, anotherWork.Name, hubWorkClient, workapiv1.WorkAvailable, metav1.ConditionTrue,
				[]metav1.ConditionStatus{metav1.ConditionTrue}, eventuallyTimeout, eventuallyInterval)

			anotherAppliedManifestWorkName = util.AppliedManifestWorkName(hubHash, anotherWork)
		})

		ginkgo.It("shared resource between the manifestwork should be kept when one manifestwork is deleted", func() {
			// ensure configmap exists and get its uid
			util.AssertExistenceOfConfigMaps(manifests, spokeKubeClient, eventuallyTimeout, eventuallyInterval)
			currentConfigMap, err := spokeKubeClient.CoreV1().ConfigMaps(clusterName).Get(context.Background(), cm1, metav1.GetOptions{})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())
			currentUID := currentConfigMap.UID

			// Ensure that uid recorded in the appliedmanifestwork and anotherappliedmanifestwork is correct.
			gomega.Eventually(func() error {
				appliedManifestWork, err := spokeWorkClient.WorkV1().AppliedManifestWorks().Get(context.Background(), appliedManifestWorkName, metav1.GetOptions{})
				if err != nil {
					return err
				}

				for _, appliedResource := range appliedManifestWork.Status.AppliedResources {
					if appliedResource.Name == cm1 && appliedResource.UID == string(currentUID) {
						return nil
					}
				}

				return fmt.Errorf("resource name or uid in appliedmanifestwork does not match")
			}, eventuallyTimeout, eventuallyInterval).ShouldNot(gomega.HaveOccurred())

			gomega.Eventually(func() error {
				anotherAppliedManifestWork, err := spokeWorkClient.WorkV1().AppliedManifestWorks().Get(
					context.Background(), anotherAppliedManifestWorkName, metav1.GetOptions{})
				if err != nil {
					return err
				}

				for _, appliedResource := range anotherAppliedManifestWork.Status.AppliedResources {
					if appliedResource.Name == cm1 && appliedResource.UID == string(currentUID) {
						return nil
					}
				}

				return fmt.Errorf("resource name or uid in appliedmanifestwork does not match")
			}, eventuallyTimeout, eventuallyInterval).ShouldNot(gomega.HaveOccurred())

			// Delete one manifestwork
			err = hubWorkClient.WorkV1().ManifestWorks(clusterName).Delete(context.Background(), work.Name, metav1.DeleteOptions{})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())

			// Ensure the appliedmanifestwork of deleted manifestwork is removed so it won't try to delete shared resource
			gomega.Eventually(func() error {
				appliedWork, err := spokeWorkClient.WorkV1().AppliedManifestWorks().Get(context.Background(), appliedManifestWorkName, metav1.GetOptions{})
				if errors.IsNotFound(err) {
					return nil
				}
				if err != nil {
					return err
				}
				return fmt.Errorf("appliedmanifestwork should not exist: %v", appliedWork.DeletionTimestamp)
			}, eventuallyTimeout, eventuallyInterval).Should(gomega.Succeed())

			// Ensure the configmap is kept and tracked by anotherappliedmanifestwork.
			gomega.Eventually(func() error {
				configMap, err := spokeKubeClient.CoreV1().ConfigMaps(clusterName).Get(context.Background(), cm1, metav1.GetOptions{})
				if err != nil {
					return err
				}

				if currentUID != configMap.UID {
					return fmt.Errorf("UID should be equal")
				}

				anotherAppliedManifestWork, err := spokeWorkClient.WorkV1().AppliedManifestWorks().Get(
					context.Background(), anotherAppliedManifestWorkName, metav1.GetOptions{})
				if err != nil {
					return err
				}

				for _, appliedResource := range anotherAppliedManifestWork.Status.AppliedResources {
					if appliedResource.Name != cm1 {
						return fmt.Errorf("resource Name should be cm1")
					}

					if appliedResource.UID != string(currentUID) {
						return fmt.Errorf("UID should be equal")
					}
				}

				return nil
			}, eventuallyTimeout, eventuallyInterval).ShouldNot(gomega.HaveOccurred())
		})

		ginkgo.It("shared resource between the manifestwork should be kept when the shared resource is removed from one manifestwork", func() {
			// ensure configmap exists and get its uid
			util.AssertExistenceOfConfigMaps(manifests, spokeKubeClient, eventuallyTimeout, eventuallyInterval)
			curentConfigMap, err := spokeKubeClient.CoreV1().ConfigMaps(clusterName).Get(context.Background(), cm1, metav1.GetOptions{})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())
			currentUID := curentConfigMap.UID

			// Ensure that uid recorded in the appliedmanifestwork and anotherappliedmanifestwork is correct.
			gomega.Eventually(func() error {
				appliedManifestWork, err := spokeWorkClient.WorkV1().AppliedManifestWorks().Get(context.Background(), appliedManifestWorkName, metav1.GetOptions{})
				if err != nil {
					return err
				}

				for _, appliedResource := range appliedManifestWork.Status.AppliedResources {
					if appliedResource.Name == cm1 && appliedResource.UID == string(currentUID) {
						return nil
					}
				}

				return fmt.Errorf("resource name or uid in appliedmanifestwork does not match")
			}, eventuallyTimeout, eventuallyInterval).ShouldNot(gomega.HaveOccurred())

			gomega.Eventually(func() error {
				anotherAppliedManifestWork, err := spokeWorkClient.WorkV1().AppliedManifestWorks().Get(
					context.Background(), anotherAppliedManifestWorkName, metav1.GetOptions{})
				if err != nil {
					return err
				}

				for _, appliedResource := range anotherAppliedManifestWork.Status.AppliedResources {
					if appliedResource.Name == cm1 && appliedResource.UID == string(currentUID) {
						return nil
					}
				}

				return fmt.Errorf("resource name or uid in appliedmanifestwork does not match")
			}, eventuallyTimeout, eventuallyInterval).ShouldNot(gomega.HaveOccurred())

			// Update one manifestwork to remove the shared resource
			updatedWork, err := hubWorkClient.WorkV1().ManifestWorks(clusterName).Get(context.Background(), work.Name, metav1.GetOptions{})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())

			newWork := updatedWork.DeepCopy()
			newWork.Spec.Workload.Manifests = []workapiv1.Manifest{manifests[1]}

			pathBytes, err := util.NewWorkPatch(updatedWork, newWork)
			gomega.Expect(err).ToNot(gomega.HaveOccurred())

			_, err = hubWorkClient.WorkV1().ManifestWorks(clusterName).Patch(
				context.Background(), updatedWork.Name, types.MergePatchType, pathBytes, metav1.PatchOptions{})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())

			// Ensure the resource is not tracked by the appliedmanifestwork.
			gomega.Eventually(func() error {
				appliedManifestWork, err := spokeWorkClient.WorkV1().AppliedManifestWorks().Get(context.Background(), appliedManifestWorkName, metav1.GetOptions{})
				if err != nil {
					return err
				}

				for _, appliedResource := range appliedManifestWork.Status.AppliedResources {
					if appliedResource.Name == cm1 {
						return fmt.Errorf("found applied resource name cm1")
					}
				}

				return nil
			}, eventuallyTimeout, eventuallyInterval).ShouldNot(gomega.HaveOccurred())

			// Ensure the configmap is kept and tracked by anotherappliedmanifestwork
			gomega.Eventually(func() error {
				configMap, err := spokeKubeClient.CoreV1().ConfigMaps(clusterName).Get(
					context.Background(), cm1, metav1.GetOptions{})
				if err != nil {
					return err
				}

				if currentUID != configMap.UID {
					return fmt.Errorf("UID should be equal")
				}

				anotherAppliedManifestWork, err := spokeWorkClient.WorkV1().AppliedManifestWorks().Get(
					context.Background(), anotherAppliedManifestWorkName, metav1.GetOptions{})
				if err != nil {
					return err
				}

				for _, appliedResource := range anotherAppliedManifestWork.Status.AppliedResources {
					if appliedResource.Name != cm1 {
						return fmt.Errorf("resource Name should be cm1")
					}

					if appliedResource.UID != string(currentUID) {
						return fmt.Errorf("UID should be equal")
					}
				}

				return nil
			}, eventuallyTimeout, eventuallyInterval).ShouldNot(gomega.HaveOccurred())
		})

	})

	ginkgo.Context("Delete options", func() {
		ginkgo.BeforeEach(func() {
			manifests = []workapiv1.Manifest{
				util.ToManifest(util.NewConfigmap(clusterName, cm1, map[string]string{"a": "b"}, []string{})),
				util.ToManifest(util.NewConfigmap(clusterName, cm2, map[string]string{"c": "d"}, []string{})),
			}
		})

		ginkgo.It("Orphan deletion of the whole manifestwork", func() {
			work.Spec.DeleteOption = &workapiv1.DeleteOption{
				PropagationPolicy: workapiv1.DeletePropagationPolicyTypeOrphan,
			}

			_, err = hubWorkClient.WorkV1().ManifestWorks(clusterName).Create(context.Background(), work, metav1.CreateOptions{})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())

			util.AssertWorkCondition(work.Namespace, work.Name, hubWorkClient, workapiv1.WorkApplied, metav1.ConditionTrue,
				[]metav1.ConditionStatus{metav1.ConditionTrue, metav1.ConditionTrue}, eventuallyTimeout, eventuallyInterval)
			util.AssertWorkCondition(work.Namespace, work.Name, hubWorkClient, workapiv1.WorkAvailable, metav1.ConditionTrue,
				[]metav1.ConditionStatus{metav1.ConditionTrue, metav1.ConditionTrue}, eventuallyTimeout, eventuallyInterval)

			// Ensure configmap exists
			util.AssertExistenceOfConfigMaps(manifests, spokeKubeClient, eventuallyTimeout, eventuallyInterval)

			// Ensure ownership of configmap is updated
			gomega.Eventually(func() error {
				cm, err := spokeKubeClient.CoreV1().ConfigMaps(clusterName).Get(context.Background(), cm1, metav1.GetOptions{})
				if err != nil {
					return err
				}

				if len(cm.OwnerReferences) != 0 {
					return fmt.Errorf("owner reference are not correctly updated, current ownerrefs are %v", cm.OwnerReferences)
				}

				return nil
			}, eventuallyTimeout, eventuallyInterval).ShouldNot(gomega.HaveOccurred())

			gomega.Eventually(func() error {
				cm, err := spokeKubeClient.CoreV1().ConfigMaps(clusterName).Get(context.Background(), cm2, metav1.GetOptions{})
				if err != nil {
					return err
				}

				if len(cm.OwnerReferences) != 0 {
					return fmt.Errorf("owner reference are not correctly updated, current ownerrefs are %v", cm.OwnerReferences)
				}

				return nil
			}, eventuallyTimeout, eventuallyInterval).ShouldNot(gomega.HaveOccurred())

			// Delete the work
			err = hubWorkClient.WorkV1().ManifestWorks(clusterName).Delete(context.Background(), work.Name, metav1.DeleteOptions{})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())

			// Wait for deletion of manifest work
			gomega.Eventually(func() bool {
				_, err := hubWorkClient.WorkV1().ManifestWorks(clusterName).Get(context.Background(), work.Name, metav1.GetOptions{})
				return errors.IsNotFound(err)
			}, eventuallyTimeout, eventuallyInterval).Should(gomega.BeTrue())

			// Ensure configmap exists
			util.AssertExistenceOfConfigMaps(manifests, spokeKubeClient, eventuallyTimeout, eventuallyInterval)
		})

		ginkgo.It("Selectively Orphan deletion of the manifestwork", func() {
			work.Spec.DeleteOption = &workapiv1.DeleteOption{
				PropagationPolicy: workapiv1.DeletePropagationPolicyTypeSelectivelyOrphan,
				SelectivelyOrphan: &workapiv1.SelectivelyOrphan{
					OrphaningRules: []workapiv1.OrphaningRule{
						{
							Group:     "",
							Resource:  "configmaps",
							Namespace: clusterName,
							Name:      cm1,
						},
					},
				},
			}

			work, err = hubWorkClient.WorkV1().ManifestWorks(clusterName).Create(context.Background(), work, metav1.CreateOptions{})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())

			util.AssertWorkCondition(work.Namespace, work.Name, hubWorkClient, workapiv1.WorkApplied, metav1.ConditionTrue,
				[]metav1.ConditionStatus{metav1.ConditionTrue, metav1.ConditionTrue}, eventuallyTimeout, eventuallyInterval)
			util.AssertWorkCondition(work.Namespace, work.Name, hubWorkClient, workapiv1.WorkAvailable, metav1.ConditionTrue,
				[]metav1.ConditionStatus{metav1.ConditionTrue, metav1.ConditionTrue}, eventuallyTimeout, eventuallyInterval)

			// Ensure configmap exists
			util.AssertExistenceOfConfigMaps(manifests, spokeKubeClient, eventuallyTimeout, eventuallyInterval)

			// Ensure ownership of configmap is updated
			gomega.Eventually(func() error {
				cm, err := spokeKubeClient.CoreV1().ConfigMaps(clusterName).Get(context.Background(), cm1, metav1.GetOptions{})
				if err != nil {
					return err
				}

				if len(cm.OwnerReferences) != 0 {
					return fmt.Errorf("owner reference are not correctly updated, current ownerrefs are %v", cm.OwnerReferences)
				}

				return nil
			}, eventuallyTimeout, eventuallyInterval).ShouldNot(gomega.HaveOccurred())

			// Delete the work
			err = hubWorkClient.WorkV1().ManifestWorks(clusterName).Delete(context.Background(), work.Name, metav1.DeleteOptions{})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())

			// Wait for deletion of manifest work
			gomega.Eventually(func() bool {
				_, err := hubWorkClient.WorkV1().ManifestWorks(clusterName).Get(context.Background(), work.Name, metav1.GetOptions{})
				return errors.IsNotFound(err)
			}, eventuallyTimeout, eventuallyInterval).Should(gomega.BeTrue())

			// One of the resource should be deleted.
			_, err = spokeKubeClient.CoreV1().ConfigMaps(clusterName).Get(context.Background(), cm2, metav1.GetOptions{})
			gomega.Expect(errors.IsNotFound(err)).To(gomega.BeTrue())

			// One of the resource should be kept
			_, err = spokeKubeClient.CoreV1().ConfigMaps(clusterName).Get(context.Background(), cm1, metav1.GetOptions{})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())
		})

		ginkgo.It("Keep the resource when remove it from manifests with orphan deletion option", func() {
			work.Spec.DeleteOption = &workapiv1.DeleteOption{
				PropagationPolicy: workapiv1.DeletePropagationPolicyTypeSelectivelyOrphan,
				SelectivelyOrphan: &workapiv1.SelectivelyOrphan{
					OrphaningRules: []workapiv1.OrphaningRule{
						{
							Group:     "",
							Resource:  "configmaps",
							Namespace: clusterName,
							Name:      cm1,
						},
					},
				},
			}

			work, err = hubWorkClient.WorkV1().ManifestWorks(clusterName).Create(context.Background(), work, metav1.CreateOptions{})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())

			util.AssertWorkCondition(work.Namespace, work.Name, hubWorkClient, workapiv1.WorkApplied, metav1.ConditionTrue,
				[]metav1.ConditionStatus{metav1.ConditionTrue, metav1.ConditionTrue}, eventuallyTimeout, eventuallyInterval)
			util.AssertWorkCondition(work.Namespace, work.Name, hubWorkClient, workapiv1.WorkAvailable, metav1.ConditionTrue,
				[]metav1.ConditionStatus{metav1.ConditionTrue, metav1.ConditionTrue}, eventuallyTimeout, eventuallyInterval)

			// Ensure configmap exists
			util.AssertExistenceOfConfigMaps(manifests, spokeKubeClient, eventuallyTimeout, eventuallyInterval)

			// Ensure ownership of configmap is updated
			gomega.Eventually(func() error {
				cm, err := spokeKubeClient.CoreV1().ConfigMaps(clusterName).Get(context.Background(), cm1, metav1.GetOptions{})
				if err != nil {
					return err
				}

				if len(cm.OwnerReferences) != 0 {
					return fmt.Errorf("owner reference are not correctly updated, current ownerrefs are %v", cm.OwnerReferences)
				}

				return nil
			}, eventuallyTimeout, eventuallyInterval).ShouldNot(gomega.HaveOccurred())

			// Remove the resource from the manifests
			gomega.Eventually(func() error {
				updatedWork, err := hubWorkClient.WorkV1().ManifestWorks(clusterName).Get(context.Background(), work.Name, metav1.GetOptions{})
				if err != nil {
					return err
				}

				newWork := updatedWork.DeepCopy()
				newWork.Spec.Workload.Manifests = []workapiv1.Manifest{
					util.ToManifest(util.NewConfigmap(clusterName, cm2, map[string]string{"c": "d"}, []string{})),
				}

				pathBytes, err := util.NewWorkPatch(updatedWork, newWork)
				if err != nil {
					return err
				}

				_, err = hubWorkClient.WorkV1().ManifestWorks(clusterName).Patch(
					context.Background(), updatedWork.Name, types.MergePatchType, pathBytes, metav1.PatchOptions{})
				return err
			}, eventuallyTimeout, eventuallyInterval).ShouldNot(gomega.HaveOccurred())

			util.AssertWorkCondition(work.Namespace, work.Name, hubWorkClient, workapiv1.WorkApplied, metav1.ConditionTrue,
				[]metav1.ConditionStatus{metav1.ConditionTrue}, eventuallyTimeout, eventuallyInterval)
			util.AssertWorkCondition(work.Namespace, work.Name, hubWorkClient, workapiv1.WorkAvailable, metav1.ConditionTrue,
				[]metav1.ConditionStatus{metav1.ConditionTrue}, eventuallyTimeout, eventuallyInterval)

			// Wait for the resource to be observed
			gomega.Eventually(func() error {
				_, err := spokeKubeClient.CoreV1().ConfigMaps(clusterName).Get(context.Background(), cm1, metav1.GetOptions{})
				return err
			}, eventuallyTimeout, eventuallyInterval).ShouldNot(gomega.HaveOccurred())
		})

		ginkgo.It("Clean the resource when orphan deletion option is removed", func() {
			work.Spec.DeleteOption = &workapiv1.DeleteOption{
				PropagationPolicy: workapiv1.DeletePropagationPolicyTypeSelectivelyOrphan,
				SelectivelyOrphan: &workapiv1.SelectivelyOrphan{
					OrphaningRules: []workapiv1.OrphaningRule{
						{
							Group:     "",
							Resource:  "configmaps",
							Namespace: clusterName,
							Name:      cm1,
						},
					},
				},
			}

			work, err = hubWorkClient.WorkV1().ManifestWorks(clusterName).Create(context.Background(), work, metav1.CreateOptions{})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())

			util.AssertWorkCondition(work.Namespace, work.Name, hubWorkClient, workapiv1.WorkApplied, metav1.ConditionTrue,
				[]metav1.ConditionStatus{metav1.ConditionTrue, metav1.ConditionTrue}, eventuallyTimeout, eventuallyInterval)
			util.AssertWorkCondition(work.Namespace, work.Name, hubWorkClient, workapiv1.WorkAvailable, metav1.ConditionTrue,
				[]metav1.ConditionStatus{metav1.ConditionTrue, metav1.ConditionTrue}, eventuallyTimeout, eventuallyInterval)

			// Ensure configmap exists
			util.AssertExistenceOfConfigMaps(manifests, spokeKubeClient, eventuallyTimeout, eventuallyInterval)

			// Ensure ownership of configmap is updated
			gomega.Eventually(func() error {
				cm, err := spokeKubeClient.CoreV1().ConfigMaps(clusterName).Get(context.Background(), cm1, metav1.GetOptions{})
				if err != nil {
					return err
				}

				if len(cm.OwnerReferences) != 0 {
					return fmt.Errorf("owner reference are not correctly updated, current ownerrefs are %v", cm.OwnerReferences)
				}

				return nil
			}, eventuallyTimeout, eventuallyInterval).ShouldNot(gomega.HaveOccurred())

			// Remove the delete option
			gomega.Eventually(func() error {
				updatedWork, err := hubWorkClient.WorkV1().ManifestWorks(clusterName).Get(context.Background(), work.Name, metav1.GetOptions{})
				if err != nil {
					return err
				}

				newWork := updatedWork.DeepCopy()
				newWork.Spec.DeleteOption = nil

				pathBytes, err := util.NewWorkPatch(updatedWork, newWork)
				if err != nil {
					return err
				}

				_, err = hubWorkClient.WorkV1().ManifestWorks(clusterName).Patch(
					context.Background(), updatedWork.Name, types.MergePatchType, pathBytes, metav1.PatchOptions{})
				return err
			}, eventuallyTimeout, eventuallyInterval).ShouldNot(gomega.HaveOccurred())

			// Ensure ownership of configmap is updated
			gomega.Eventually(func() error {
				cm, err := spokeKubeClient.CoreV1().ConfigMaps(clusterName).Get(context.Background(), cm1, metav1.GetOptions{})
				if err != nil {
					return err
				}

				if len(cm.OwnerReferences) != 1 {
					return fmt.Errorf("owner reference are not correctly updated, current ownerrefs are %v", cm.OwnerReferences)
				}

				return nil
			}, eventuallyTimeout, eventuallyInterval).ShouldNot(gomega.HaveOccurred())

			// Delete the work
			err = hubWorkClient.WorkV1().ManifestWorks(clusterName).Delete(context.Background(), work.Name, metav1.DeleteOptions{})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())

			// Wait for deletion of manifest work
			gomega.Eventually(func() bool {
				_, err := hubWorkClient.WorkV1().ManifestWorks(clusterName).Get(context.Background(), work.Name, metav1.GetOptions{})
				return errors.IsNotFound(err)
			}, eventuallyTimeout, eventuallyInterval).Should(gomega.BeTrue())

			// All the resources should be deleted.
			_, err = spokeKubeClient.CoreV1().ConfigMaps(clusterName).Get(context.Background(), cm2, metav1.GetOptions{})
			gomega.Expect(errors.IsNotFound(err)).To(gomega.BeTrue())
			_, err = spokeKubeClient.CoreV1().ConfigMaps(clusterName).Get(context.Background(), cm1, metav1.GetOptions{})
			gomega.Expect(errors.IsNotFound(err)).To(gomega.BeTrue())
		})
	})
})
