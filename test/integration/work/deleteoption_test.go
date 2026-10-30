package work

import (
	"context"
	"fmt"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/rand"

	workapiv1 "open-cluster-management.io/api/work/v1"

	"open-cluster-management.io/ocm/test/integration/util"
)

var _ = Describe("ManifestWork Delete Option", func() {
	var cancel context.CancelFunc

	var workName string
	var clusterName string
	var work *workapiv1.ManifestWork
	var appliedManifestWorkName string
	var manifests []workapiv1.Manifest

	var err error

	BeforeEach(func(ctx SpecContext) {
		clusterName = rand.String(5)
		workName = fmt.Sprintf("work-delete-option-%s", rand.String(5))

		ns := &corev1.Namespace{}
		ns.Name = clusterName
		_, err := spokeKubeClient.CoreV1().Namespaces().Create(ctx, ns, metav1.CreateOptions{})
		Expect(err).ToNot(HaveOccurred())

		var runCtx context.Context
		runCtx, cancel = context.WithCancel(context.Background())
		go startWorkAgent(runCtx, clusterName)

		// reset manifests
		manifests = nil
	})

	JustBeforeEach(func() {
		work = util.NewManifestWork(clusterName, workName, manifests)
	})

	AfterEach(func(ctx SpecContext) {
		if cancel != nil {
			cancel()
		}
		err := spokeKubeClient.CoreV1().Namespaces().Delete(ctx, clusterName, metav1.DeleteOptions{})
		Expect(err).ToNot(HaveOccurred())
	})

	Context("Resource sharing and adoption between manifestworks", func() {
		var anotherWork *workapiv1.ManifestWork
		var anotherAppliedManifestWorkName string
		BeforeEach(func() {
			manifests = []workapiv1.Manifest{
				util.ToManifest(util.NewConfigmap(clusterName, cm1, map[string]string{"a": "b"}, []string{})),
				util.ToManifest(util.NewConfigmap(clusterName, cm2, map[string]string{"c": "d"}, []string{})),
			}
			// Create another manifestworks with one shared resource.
			anotherWork = util.NewManifestWork(clusterName, "sharing-resource-work", []workapiv1.Manifest{manifests[0]})
		})

		JustBeforeEach(func(ctx SpecContext) {
			work, err = hubWorkClient.WorkV1().ManifestWorks(clusterName).Create(ctx, work, metav1.CreateOptions{})
			Expect(err).ToNot(HaveOccurred())

			appliedManifestWorkName = util.AppliedManifestWorkName(hubHash, work)

			util.AssertWorkCondition(work.Namespace, work.Name, hubWorkClient, workapiv1.WorkApplied, metav1.ConditionTrue,
				[]metav1.ConditionStatus{metav1.ConditionTrue, metav1.ConditionTrue}, eventuallyTimeout, eventuallyInterval)
			util.AssertWorkCondition(work.Namespace, work.Name, hubWorkClient, workapiv1.WorkAvailable, metav1.ConditionTrue,
				[]metav1.ConditionStatus{metav1.ConditionTrue, metav1.ConditionTrue}, eventuallyTimeout, eventuallyInterval)

			anotherWork, err = hubWorkClient.WorkV1().ManifestWorks(clusterName).Create(ctx, anotherWork, metav1.CreateOptions{})
			Expect(err).ToNot(HaveOccurred())
			util.AssertWorkCondition(anotherWork.Namespace, anotherWork.Name, hubWorkClient, workapiv1.WorkApplied, metav1.ConditionTrue,
				[]metav1.ConditionStatus{metav1.ConditionTrue}, eventuallyTimeout, eventuallyInterval)
			util.AssertWorkCondition(anotherWork.Namespace, anotherWork.Name, hubWorkClient, workapiv1.WorkAvailable, metav1.ConditionTrue,
				[]metav1.ConditionStatus{metav1.ConditionTrue}, eventuallyTimeout, eventuallyInterval)

			anotherAppliedManifestWorkName = util.AppliedManifestWorkName(hubHash, anotherWork)
		})

		It("shared resource between the manifestwork should be kept when one manifestwork is deleted", func(ctx SpecContext) {
			// ensure configmap exists and get its uid
			util.AssertExistenceOfConfigMaps(manifests, spokeKubeClient, eventuallyTimeout, eventuallyInterval)
			currentConfigMap, err := spokeKubeClient.CoreV1().ConfigMaps(clusterName).Get(ctx, cm1, metav1.GetOptions{})
			Expect(err).ToNot(HaveOccurred())
			currentUID := currentConfigMap.UID

			// Ensure that uid recorded in the appliedmanifestwork and anotherappliedmanifestwork is correct.
			Eventually(func() error {
				appliedManifestWork, err := spokeWorkClient.WorkV1().AppliedManifestWorks().Get(ctx, appliedManifestWorkName, metav1.GetOptions{})
				if err != nil {
					return err
				}

				for _, appliedResource := range appliedManifestWork.Status.AppliedResources {
					if appliedResource.Name == cm1 && appliedResource.UID == string(currentUID) {
						return nil
					}
				}

				return fmt.Errorf("resource name or uid in appliedmanifestwork does not match")
			}, eventuallyTimeout, eventuallyInterval).ShouldNot(HaveOccurred())

			Eventually(func() error {
				anotherAppliedManifestWork, err := spokeWorkClient.WorkV1().AppliedManifestWorks().Get(
					ctx, anotherAppliedManifestWorkName, metav1.GetOptions{})
				if err != nil {
					return err
				}

				for _, appliedResource := range anotherAppliedManifestWork.Status.AppliedResources {
					if appliedResource.Name == cm1 && appliedResource.UID == string(currentUID) {
						return nil
					}
				}

				return fmt.Errorf("resource name or uid in appliedmanifestwork does not match")
			}, eventuallyTimeout, eventuallyInterval).ShouldNot(HaveOccurred())

			// Delete one manifestwork
			err = hubWorkClient.WorkV1().ManifestWorks(clusterName).Delete(ctx, work.Name, metav1.DeleteOptions{})
			Expect(err).ToNot(HaveOccurred())

			// Ensure the appliedmanifestwork of deleted manifestwork is removed so it won't try to delete shared resource
			Eventually(func() error {
				appliedWork, err := spokeWorkClient.WorkV1().AppliedManifestWorks().Get(ctx, appliedManifestWorkName, metav1.GetOptions{})
				if errors.IsNotFound(err) {
					return nil
				}
				if err != nil {
					return err
				}
				return fmt.Errorf("appliedmanifestwork should not exist: %v", appliedWork.DeletionTimestamp)
			}, eventuallyTimeout, eventuallyInterval).Should(Succeed())

			// Ensure the configmap is kept and tracked by anotherappliedmanifestwork.
			Eventually(func() error {
				configMap, err := spokeKubeClient.CoreV1().ConfigMaps(clusterName).Get(ctx, cm1, metav1.GetOptions{})
				if err != nil {
					return err
				}

				if currentUID != configMap.UID {
					return fmt.Errorf("UID should be equal")
				}

				anotherAppliedManifestWork, err := spokeWorkClient.WorkV1().AppliedManifestWorks().Get(
					ctx, anotherAppliedManifestWorkName, metav1.GetOptions{})
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
			}, eventuallyTimeout, eventuallyInterval).ShouldNot(HaveOccurred())
		})

		It("shared resource between the manifestwork should be kept when the shared resource is removed from one manifestwork", func(ctx SpecContext) {
			// ensure configmap exists and get its uid
			util.AssertExistenceOfConfigMaps(manifests, spokeKubeClient, eventuallyTimeout, eventuallyInterval)
			curentConfigMap, err := spokeKubeClient.CoreV1().ConfigMaps(clusterName).Get(ctx, cm1, metav1.GetOptions{})
			Expect(err).ToNot(HaveOccurred())
			currentUID := curentConfigMap.UID

			// Ensure that uid recorded in the appliedmanifestwork and anotherappliedmanifestwork is correct.
			Eventually(func() error {
				appliedManifestWork, err := spokeWorkClient.WorkV1().AppliedManifestWorks().Get(ctx, appliedManifestWorkName, metav1.GetOptions{})
				if err != nil {
					return err
				}

				for _, appliedResource := range appliedManifestWork.Status.AppliedResources {
					if appliedResource.Name == cm1 && appliedResource.UID == string(currentUID) {
						return nil
					}
				}

				return fmt.Errorf("resource name or uid in appliedmanifestwork does not match")
			}, eventuallyTimeout, eventuallyInterval).ShouldNot(HaveOccurred())

			Eventually(func() error {
				anotherAppliedManifestWork, err := spokeWorkClient.WorkV1().AppliedManifestWorks().Get(
					ctx, anotherAppliedManifestWorkName, metav1.GetOptions{})
				if err != nil {
					return err
				}

				for _, appliedResource := range anotherAppliedManifestWork.Status.AppliedResources {
					if appliedResource.Name == cm1 && appliedResource.UID == string(currentUID) {
						return nil
					}
				}

				return fmt.Errorf("resource name or uid in appliedmanifestwork does not match")
			}, eventuallyTimeout, eventuallyInterval).ShouldNot(HaveOccurred())

			// Update one manifestwork to remove the shared resource
			updatedWork, err := hubWorkClient.WorkV1().ManifestWorks(clusterName).Get(ctx, work.Name, metav1.GetOptions{})
			Expect(err).ToNot(HaveOccurred())

			newWork := updatedWork.DeepCopy()
			newWork.Spec.Workload.Manifests = []workapiv1.Manifest{manifests[1]}

			pathBytes, err := util.NewWorkPatch(updatedWork, newWork)
			Expect(err).ToNot(HaveOccurred())

			_, err = hubWorkClient.WorkV1().ManifestWorks(clusterName).Patch(
				ctx, updatedWork.Name, types.MergePatchType, pathBytes, metav1.PatchOptions{})
			Expect(err).ToNot(HaveOccurred())

			// Ensure the resource is not tracked by the appliedmanifestwork.
			Eventually(func() error {
				appliedManifestWork, err := spokeWorkClient.WorkV1().AppliedManifestWorks().Get(ctx, appliedManifestWorkName, metav1.GetOptions{})
				if err != nil {
					return err
				}

				for _, appliedResource := range appliedManifestWork.Status.AppliedResources {
					if appliedResource.Name == cm1 {
						return fmt.Errorf("found applied resource name cm1")
					}
				}

				return nil
			}, eventuallyTimeout, eventuallyInterval).ShouldNot(HaveOccurred())

			// Ensure the configmap is kept and tracked by anotherappliedmanifestwork
			Consistently(func() error {
				configMap, err := spokeKubeClient.CoreV1().ConfigMaps(clusterName).Get(
					ctx, cm1, metav1.GetOptions{})
				if err != nil {
					return err
				}

				if currentUID != configMap.UID {
					return fmt.Errorf("UID should not be changed in configmap")
				}

				anotherAppliedManifestWork, err := spokeWorkClient.WorkV1().AppliedManifestWorks().Get(
					ctx, anotherAppliedManifestWorkName, metav1.GetOptions{})
				if err != nil {
					return err
				}

				for _, appliedResource := range anotherAppliedManifestWork.Status.AppliedResources {
					if appliedResource.Name != cm1 {
						return fmt.Errorf("resource Name should be cm1")
					}

					if appliedResource.UID != string(currentUID) {
						return fmt.Errorf("UID should not be changed in appliedmanifestwork, got %v, should be %s", appliedResource.UID, currentUID)
					}
				}

				return nil
			}, eventuallyTimeout, eventuallyInterval).ShouldNot(HaveOccurred())
		})

	})

	Context("Delete options", func() {
		BeforeEach(func() {
			manifests = []workapiv1.Manifest{
				util.ToManifest(util.NewConfigmap(clusterName, cm1, map[string]string{"a": "b"}, []string{})),
				util.ToManifest(util.NewConfigmap(clusterName, cm2, map[string]string{"c": "d"}, []string{})),
			}
		})

		It("Orphan deletion of the whole manifestwork", func(ctx SpecContext) {
			work.Spec.DeleteOption = &workapiv1.DeleteOption{
				PropagationPolicy: workapiv1.DeletePropagationPolicyTypeOrphan,
			}

			_, err = hubWorkClient.WorkV1().ManifestWorks(clusterName).Create(ctx, work, metav1.CreateOptions{})
			Expect(err).ToNot(HaveOccurred())

			util.AssertWorkCondition(work.Namespace, work.Name, hubWorkClient, workapiv1.WorkApplied, metav1.ConditionTrue,
				[]metav1.ConditionStatus{metav1.ConditionTrue, metav1.ConditionTrue}, eventuallyTimeout, eventuallyInterval)
			util.AssertWorkCondition(work.Namespace, work.Name, hubWorkClient, workapiv1.WorkAvailable, metav1.ConditionTrue,
				[]metav1.ConditionStatus{metav1.ConditionTrue, metav1.ConditionTrue}, eventuallyTimeout, eventuallyInterval)

			// Ensure configmap exists
			util.AssertExistenceOfConfigMaps(manifests, spokeKubeClient, eventuallyTimeout, eventuallyInterval)

			// Ensure ownership of configmap is updated
			Eventually(func() error {
				cm, err := spokeKubeClient.CoreV1().ConfigMaps(clusterName).Get(ctx, cm1, metav1.GetOptions{})
				if err != nil {
					return err
				}

				if len(cm.OwnerReferences) != 0 {
					return fmt.Errorf("owner reference are not correctly updated, current ownerrefs are %v", cm.OwnerReferences)
				}

				return nil
			}, eventuallyTimeout, eventuallyInterval).ShouldNot(HaveOccurred())

			Eventually(func() error {
				cm, err := spokeKubeClient.CoreV1().ConfigMaps(clusterName).Get(ctx, cm2, metav1.GetOptions{})
				if err != nil {
					return err
				}

				if len(cm.OwnerReferences) != 0 {
					return fmt.Errorf("owner reference are not correctly updated, current ownerrefs are %v", cm.OwnerReferences)
				}

				return nil
			}, eventuallyTimeout, eventuallyInterval).ShouldNot(HaveOccurred())

			// Delete the work
			err = hubWorkClient.WorkV1().ManifestWorks(clusterName).Delete(ctx, work.Name, metav1.DeleteOptions{})
			Expect(err).ToNot(HaveOccurred())

			// Wait for deletion of manifest work
			Eventually(func() bool {
				_, err := hubWorkClient.WorkV1().ManifestWorks(clusterName).Get(ctx, work.Name, metav1.GetOptions{})
				return errors.IsNotFound(err)
			}, eventuallyTimeout, eventuallyInterval).Should(BeTrue())

			// Ensure configmap exists
			util.AssertExistenceOfConfigMaps(manifests, spokeKubeClient, eventuallyTimeout, eventuallyInterval)
		})

		It("Selectively Orphan deletion of the manifestwork", func(ctx SpecContext) {
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

			work, err = hubWorkClient.WorkV1().ManifestWorks(clusterName).Create(ctx, work, metav1.CreateOptions{})
			Expect(err).ToNot(HaveOccurred())

			util.AssertWorkCondition(work.Namespace, work.Name, hubWorkClient, workapiv1.WorkApplied, metav1.ConditionTrue,
				[]metav1.ConditionStatus{metav1.ConditionTrue, metav1.ConditionTrue}, eventuallyTimeout, eventuallyInterval)
			util.AssertWorkCondition(work.Namespace, work.Name, hubWorkClient, workapiv1.WorkAvailable, metav1.ConditionTrue,
				[]metav1.ConditionStatus{metav1.ConditionTrue, metav1.ConditionTrue}, eventuallyTimeout, eventuallyInterval)

			// Ensure configmap exists
			util.AssertExistenceOfConfigMaps(manifests, spokeKubeClient, eventuallyTimeout, eventuallyInterval)

			// Ensure ownership of configmap is updated
			Eventually(func() error {
				cm, err := spokeKubeClient.CoreV1().ConfigMaps(clusterName).Get(ctx, cm1, metav1.GetOptions{})
				if err != nil {
					return err
				}

				if len(cm.OwnerReferences) != 0 {
					return fmt.Errorf("owner reference are not correctly updated, current ownerrefs are %v", cm.OwnerReferences)
				}

				return nil
			}, eventuallyTimeout, eventuallyInterval).ShouldNot(HaveOccurred())

			// Delete the work
			err = hubWorkClient.WorkV1().ManifestWorks(clusterName).Delete(ctx, work.Name, metav1.DeleteOptions{})
			Expect(err).ToNot(HaveOccurred())

			// Wait for deletion of manifest work
			Eventually(func() bool {
				_, err := hubWorkClient.WorkV1().ManifestWorks(clusterName).Get(ctx, work.Name, metav1.GetOptions{})
				return errors.IsNotFound(err)
			}, eventuallyTimeout, eventuallyInterval).Should(BeTrue())

			// One of the resource should be deleted.
			_, err = spokeKubeClient.CoreV1().ConfigMaps(clusterName).Get(ctx, cm2, metav1.GetOptions{})
			Expect(errors.IsNotFound(err)).To(BeTrue())

			// One of the resource should be kept
			_, err = spokeKubeClient.CoreV1().ConfigMaps(clusterName).Get(ctx, cm1, metav1.GetOptions{})
			Expect(err).ToNot(HaveOccurred())
		})

		It("Keep the resource when remove it from manifests with orphan deletion option", func(ctx SpecContext) {
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

			work, err = hubWorkClient.WorkV1().ManifestWorks(clusterName).Create(ctx, work, metav1.CreateOptions{})
			Expect(err).ToNot(HaveOccurred())

			util.AssertWorkCondition(work.Namespace, work.Name, hubWorkClient, workapiv1.WorkApplied, metav1.ConditionTrue,
				[]metav1.ConditionStatus{metav1.ConditionTrue, metav1.ConditionTrue}, eventuallyTimeout, eventuallyInterval)
			util.AssertWorkCondition(work.Namespace, work.Name, hubWorkClient, workapiv1.WorkAvailable, metav1.ConditionTrue,
				[]metav1.ConditionStatus{metav1.ConditionTrue, metav1.ConditionTrue}, eventuallyTimeout, eventuallyInterval)

			// Ensure configmap exists
			util.AssertExistenceOfConfigMaps(manifests, spokeKubeClient, eventuallyTimeout, eventuallyInterval)

			// Ensure ownership of configmap is updated
			Eventually(func() error {
				cm, err := spokeKubeClient.CoreV1().ConfigMaps(clusterName).Get(ctx, cm1, metav1.GetOptions{})
				if err != nil {
					return err
				}

				if len(cm.OwnerReferences) != 0 {
					return fmt.Errorf("owner reference are not correctly updated, current ownerrefs are %v", cm.OwnerReferences)
				}

				return nil
			}, eventuallyTimeout, eventuallyInterval).ShouldNot(HaveOccurred())

			// Remove the resource from the manifests
			Eventually(func() error {
				updatedWork, err := hubWorkClient.WorkV1().ManifestWorks(clusterName).Get(ctx, work.Name, metav1.GetOptions{})
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
					ctx, updatedWork.Name, types.MergePatchType, pathBytes, metav1.PatchOptions{})
				return err
			}, eventuallyTimeout, eventuallyInterval).ShouldNot(HaveOccurred())

			util.AssertWorkCondition(work.Namespace, work.Name, hubWorkClient, workapiv1.WorkApplied, metav1.ConditionTrue,
				[]metav1.ConditionStatus{metav1.ConditionTrue}, eventuallyTimeout, eventuallyInterval)
			util.AssertWorkCondition(work.Namespace, work.Name, hubWorkClient, workapiv1.WorkAvailable, metav1.ConditionTrue,
				[]metav1.ConditionStatus{metav1.ConditionTrue}, eventuallyTimeout, eventuallyInterval)

			// Wait for the resource to be observed
			Eventually(func() error {
				_, err := spokeKubeClient.CoreV1().ConfigMaps(clusterName).Get(ctx, cm1, metav1.GetOptions{})
				return err
			}, eventuallyTimeout, eventuallyInterval).ShouldNot(HaveOccurred())
		})

		It("Clean the resource when orphan deletion option is removed", func(ctx SpecContext) {
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

			work, err = hubWorkClient.WorkV1().ManifestWorks(clusterName).Create(ctx, work, metav1.CreateOptions{})
			Expect(err).ToNot(HaveOccurred())

			util.AssertWorkCondition(work.Namespace, work.Name, hubWorkClient, workapiv1.WorkApplied, metav1.ConditionTrue,
				[]metav1.ConditionStatus{metav1.ConditionTrue, metav1.ConditionTrue}, eventuallyTimeout, eventuallyInterval)
			util.AssertWorkCondition(work.Namespace, work.Name, hubWorkClient, workapiv1.WorkAvailable, metav1.ConditionTrue,
				[]metav1.ConditionStatus{metav1.ConditionTrue, metav1.ConditionTrue}, eventuallyTimeout, eventuallyInterval)

			// Ensure configmap exists
			util.AssertExistenceOfConfigMaps(manifests, spokeKubeClient, eventuallyTimeout, eventuallyInterval)

			// Ensure ownership of configmap is updated
			Eventually(func() error {
				cm, err := spokeKubeClient.CoreV1().ConfigMaps(clusterName).Get(ctx, cm1, metav1.GetOptions{})
				if err != nil {
					return err
				}

				if len(cm.OwnerReferences) != 0 {
					return fmt.Errorf("owner reference are not correctly updated, current ownerrefs are %v", cm.OwnerReferences)
				}

				return nil
			}, eventuallyTimeout, eventuallyInterval).ShouldNot(HaveOccurred())

			// Remove the delete option
			Eventually(func() error {
				updatedWork, err := hubWorkClient.WorkV1().ManifestWorks(clusterName).Get(ctx, work.Name, metav1.GetOptions{})
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
					ctx, updatedWork.Name, types.MergePatchType, pathBytes, metav1.PatchOptions{})
				return err
			}, eventuallyTimeout, eventuallyInterval).ShouldNot(HaveOccurred())

			// Ensure ownership of configmap is updated
			Eventually(func() error {
				cm, err := spokeKubeClient.CoreV1().ConfigMaps(clusterName).Get(ctx, cm1, metav1.GetOptions{})
				if err != nil {
					return err
				}

				if len(cm.OwnerReferences) != 1 {
					return fmt.Errorf("owner reference are not correctly updated, current ownerrefs are %v", cm.OwnerReferences)
				}

				return nil
			}, eventuallyTimeout, eventuallyInterval).ShouldNot(HaveOccurred())

			// Delete the work
			err = hubWorkClient.WorkV1().ManifestWorks(clusterName).Delete(ctx, work.Name, metav1.DeleteOptions{})
			Expect(err).ToNot(HaveOccurred())

			// Wait for deletion of manifest work
			Eventually(func() bool {
				_, err := hubWorkClient.WorkV1().ManifestWorks(clusterName).Get(ctx, work.Name, metav1.GetOptions{})
				return errors.IsNotFound(err)
			}, eventuallyTimeout, eventuallyInterval).Should(BeTrue())

			// All the resources should be deleted.
			_, err = spokeKubeClient.CoreV1().ConfigMaps(clusterName).Get(ctx, cm2, metav1.GetOptions{})
			Expect(errors.IsNotFound(err)).To(BeTrue())
			_, err = spokeKubeClient.CoreV1().ConfigMaps(clusterName).Get(ctx, cm1, metav1.GetOptions{})
			Expect(errors.IsNotFound(err)).To(BeTrue())
		})
	})
})
