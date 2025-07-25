package work

import (
	"context"
	"fmt"
	"time"

	"github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"
	"github.com/openshift/library-go/pkg/controller/controllercmd"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/rand"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/util/retry"

	workapiv1 "open-cluster-management.io/api/work/v1"
	"open-cluster-management.io/sdk-go/pkg/cloudevents/clients/common"

	commonoptions "open-cluster-management.io/ocm/pkg/common/options"
	"open-cluster-management.io/ocm/pkg/work/spoke"
	"open-cluster-management.io/ocm/test/integration/util"
)

func startWorkAgent(ctx context.Context, o *spoke.WorkloadAgentOptions, commOption *commonoptions.AgentOptions) {
	agentConfig := spoke.NewWorkAgentConfig(commOption, o)
	err := agentConfig.RunWorkloadAgent(ctx, &controllercmd.ControllerContext{
		KubeConfig:    spokeRestConfig,
		EventRecorder: util.NewIntegrationTestEventRecorder("integration"),
	})
	gomega.Expect(err).NotTo(gomega.HaveOccurred())
}

var _ = ginkgo.Describe("ManifestWork", func() {
	var o *spoke.WorkloadAgentOptions
	var commOptions *commonoptions.AgentOptions
	var cancel context.CancelFunc

	var workName string
	var work *workapiv1.ManifestWork
	var expectedFinalizer string
	var manifests []workapiv1.Manifest
	var workOpts []func(work *workapiv1.ManifestWork)
	var appliedManifestWorkName string

	var err error

	ginkgo.BeforeEach(func() {
		expectedFinalizer = workapiv1.ManifestWorkFinalizer
		workName = fmt.Sprintf("work-%s", rand.String(5))
		clusterName := rand.String(5)

		o = spoke.NewWorkloadAgentOptions()
		o.StatusSyncInterval = 3 * time.Second
		o.AppliedManifestWorkEvictionGracePeriod = 5 * time.Second
		o.WorkloadSourceDriver = sourceDriver
		o.WorkloadSourceConfig = sourceConfigFileName
		if sourceDriver != util.KubeDriver {
			expectedFinalizer = common.ResourceFinalizer
			o.CloudEventsClientID = fmt.Sprintf("%s-work-agent", clusterName)
			o.CloudEventsClientCodecs = []string{"manifestbundle"}
		}

		commOptions = commonoptions.NewAgentOptions()
		commOptions.SpokeClusterName = clusterName

		ns := &corev1.Namespace{}
		ns.Name = commOptions.SpokeClusterName
		_, err := spokeKubeClient.CoreV1().Namespaces().Create(context.Background(), ns, metav1.CreateOptions{})
		gomega.Expect(err).ToNot(gomega.HaveOccurred())

		var ctx context.Context
		ctx, cancel = context.WithCancel(context.Background())
		go startWorkAgent(ctx, o, commOptions)

		// reset manifests and workOpts
		manifests = nil
		workOpts = nil
	})

	ginkgo.JustBeforeEach(func() {
		work = util.NewManifestWork(commOptions.SpokeClusterName, workName, manifests)
		for _, opt := range workOpts {
			opt(work)
		}
		work, err = hubWorkClient.WorkV1().ManifestWorks(commOptions.SpokeClusterName).Create(context.Background(), work, metav1.CreateOptions{})
		appliedManifestWorkName = util.AppliedManifestWorkName(sourceDriver, hubHash, work)
		gomega.Expect(err).ToNot(gomega.HaveOccurred())
	})

	ginkgo.AfterEach(func() {
		err = hubWorkClient.WorkV1().ManifestWorks(commOptions.SpokeClusterName).Delete(context.Background(), work.Name, metav1.DeleteOptions{})
		if !errors.IsNotFound(err) {
			gomega.Expect(err).ToNot(gomega.HaveOccurred())
		}

		gomega.Eventually(func() error {
			_, err := hubWorkClient.WorkV1().ManifestWorks(commOptions.SpokeClusterName).Get(context.Background(), work.Name, metav1.GetOptions{})
			if errors.IsNotFound(err) {
				return nil
			}
			if err != nil {
				return err
			}
			return fmt.Errorf("work %s in namespace %s still exists", work.Name, commOptions.SpokeClusterName)
		}, eventuallyTimeout, eventuallyInterval).Should(gomega.Succeed())

		err := spokeKubeClient.CoreV1().Namespaces().Delete(context.Background(), commOptions.SpokeClusterName, metav1.DeleteOptions{})
		gomega.Expect(err).ToNot(gomega.HaveOccurred())

		if cancel != nil {
			cancel()
		}
	})

	ginkgo.Context("With a single manifest", func() {
		ginkgo.BeforeEach(func() {
			manifests = []workapiv1.Manifest{
				util.ToManifest(util.NewConfigmap(commOptions.SpokeClusterName, cm1, map[string]string{"a": "b"}, nil)),
			}
		})

		ginkgo.It("should create work and then apply it successfully", func() {
			util.AssertExistenceOfConfigMaps(manifests, spokeKubeClient, eventuallyTimeout, eventuallyInterval)

			ginkgo.By("field manager should be work-agent")
			cm, err := spokeKubeClient.CoreV1().ConfigMaps(commOptions.SpokeClusterName).Get(context.Background(), cm1, metav1.GetOptions{})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())
			for _, entry := range cm.ManagedFields {
				gomega.Expect(entry.Manager).To(gomega.Equal("work-agent"))
			}

			util.AssertWorkCondition(work.Namespace, work.Name, hubWorkClient, workapiv1.WorkApplied, metav1.ConditionTrue,
				[]metav1.ConditionStatus{metav1.ConditionTrue}, eventuallyTimeout, eventuallyInterval)
			util.AssertWorkCondition(work.Namespace, work.Name, hubWorkClient, workapiv1.WorkAvailable, metav1.ConditionTrue,
				[]metav1.ConditionStatus{metav1.ConditionTrue}, eventuallyTimeout, eventuallyInterval)
		})

		ginkgo.It("should update work and then apply it successfully", func() {
			util.AssertWorkCondition(work.Namespace, work.Name, hubWorkClient, workapiv1.WorkApplied, metav1.ConditionTrue,
				[]metav1.ConditionStatus{metav1.ConditionTrue}, eventuallyTimeout, eventuallyInterval)
			util.AssertWorkCondition(work.Namespace, work.Name, hubWorkClient, workapiv1.WorkAvailable, metav1.ConditionTrue,
				[]metav1.ConditionStatus{metav1.ConditionTrue}, eventuallyTimeout, eventuallyInterval)

			newManifests := []workapiv1.Manifest{
				util.ToManifest(util.NewConfigmap(commOptions.SpokeClusterName, cm2, map[string]string{"x": "y"}, nil)),
			}
			updatedWork, err := hubWorkClient.WorkV1().ManifestWorks(commOptions.SpokeClusterName).Get(context.Background(), work.Name, metav1.GetOptions{})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())

			newWork := updatedWork.DeepCopy()
			newWork.Spec.Workload.Manifests = newManifests

			pathBytes, err := util.NewWorkPatch(updatedWork, newWork)
			gomega.Expect(err).ToNot(gomega.HaveOccurred())

			_, err = hubWorkClient.WorkV1().ManifestWorks(commOptions.SpokeClusterName).Patch(
				context.Background(), updatedWork.Name, types.MergePatchType, pathBytes, metav1.PatchOptions{})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())

			util.AssertExistenceOfConfigMaps(newManifests, spokeKubeClient, eventuallyTimeout, eventuallyInterval)

			// check if resource created by stale manifest is deleted once it is removed from applied resource list
			gomega.Eventually(func() error {
				appliedManifestWork, err := spokeWorkClient.WorkV1().AppliedManifestWorks().Get(
					context.Background(), appliedManifestWorkName, metav1.GetOptions{})
				if err != nil {
					return err
				}

				for _, appliedResource := range appliedManifestWork.Status.AppliedResources {
					if appliedResource.Name == cm1 {
						return fmt.Errorf("found applied resource cm1")
					}
				}

				return nil
			}, eventuallyTimeout, eventuallyInterval).ShouldNot(gomega.HaveOccurred())

			_, err = spokeKubeClient.CoreV1().ConfigMaps(commOptions.SpokeClusterName).Get(context.Background(), cm1, metav1.GetOptions{})
			gomega.Expect(errors.IsNotFound(err)).To(gomega.BeTrue())
		})

		ginkgo.It("should delete work successfully", func() {
			util.AssertFinalizerAdded(work.Namespace, work.Name, expectedFinalizer, hubWorkClient, eventuallyTimeout, eventuallyInterval)

			err = hubWorkClient.WorkV1().ManifestWorks(commOptions.SpokeClusterName).Delete(context.Background(), work.Name, metav1.DeleteOptions{})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())

			util.AssertWorkDeleted(work.Namespace, work.Name, fmt.Sprintf("%s-%s", hubHash, work.Name),
				manifests, hubWorkClient, spokeWorkClient, spokeKubeClient, eventuallyTimeout, eventuallyInterval)
		})
	})

	ginkgo.Context("With multiple manifests", func() {
		ginkgo.BeforeEach(func() {
			manifests = []workapiv1.Manifest{
				util.ToManifest(util.NewConfigmap("non-existent-namespace", cm1, map[string]string{"a": "b"}, nil)),
				util.ToManifest(util.NewConfigmap(commOptions.SpokeClusterName, cm2, map[string]string{"c": "d"}, nil)),
				util.ToManifest(util.NewConfigmap(commOptions.SpokeClusterName, "cm3", map[string]string{"e": "f"}, nil)),
			}
		})

		ginkgo.It("should create work and then apply it successfully", func() {
			util.AssertExistenceOfConfigMaps(manifests[1:], spokeKubeClient, eventuallyTimeout, eventuallyInterval)

			util.AssertWorkCondition(work.Namespace, work.Name, hubWorkClient, workapiv1.WorkApplied, metav1.ConditionFalse,
				[]metav1.ConditionStatus{metav1.ConditionFalse, metav1.ConditionTrue, metav1.ConditionTrue}, eventuallyTimeout, eventuallyInterval)
			util.AssertWorkCondition(work.Namespace, work.Name, hubWorkClient, workapiv1.WorkAvailable, metav1.ConditionFalse,
				[]metav1.ConditionStatus{metav1.ConditionFalse, metav1.ConditionTrue, metav1.ConditionTrue}, eventuallyTimeout, eventuallyInterval)
		})

		ginkgo.It("should update work and then apply it successfully", func() {
			util.AssertExistenceOfConfigMaps(manifests[1:], spokeKubeClient, eventuallyTimeout, eventuallyInterval)

			util.AssertWorkCondition(work.Namespace, work.Name, hubWorkClient, workapiv1.WorkApplied, metav1.ConditionFalse,
				[]metav1.ConditionStatus{metav1.ConditionFalse, metav1.ConditionTrue, metav1.ConditionTrue}, eventuallyTimeout, eventuallyInterval)
			util.AssertWorkCondition(work.Namespace, work.Name, hubWorkClient, workapiv1.WorkAvailable, metav1.ConditionFalse,
				[]metav1.ConditionStatus{metav1.ConditionFalse, metav1.ConditionTrue, metav1.ConditionTrue}, eventuallyTimeout, eventuallyInterval)

			newManifests := []workapiv1.Manifest{
				util.ToManifest(util.NewConfigmap(commOptions.SpokeClusterName, cm1, map[string]string{"a": "b"}, nil)),
				util.ToManifest(util.NewConfigmap(commOptions.SpokeClusterName, cm2, map[string]string{"x": "y"}, nil)),
				util.ToManifest(util.NewConfigmap(commOptions.SpokeClusterName, "cm4", map[string]string{"e": "f"}, nil)),
			}

			updatedWork, err := hubWorkClient.WorkV1().ManifestWorks(commOptions.SpokeClusterName).Get(context.Background(), work.Name, metav1.GetOptions{})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())

			newWork := updatedWork.DeepCopy()
			newWork.Spec.Workload.Manifests = newManifests

			pathBytes, err := util.NewWorkPatch(updatedWork, newWork)
			gomega.Expect(err).ToNot(gomega.HaveOccurred())

			_, err = hubWorkClient.WorkV1().ManifestWorks(commOptions.SpokeClusterName).Patch(
				context.Background(), updatedWork.Name, types.MergePatchType, pathBytes, metav1.PatchOptions{})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())

			util.AssertExistenceOfConfigMaps(newManifests, spokeKubeClient, eventuallyTimeout, eventuallyInterval)

			// check if Available status is updated or not
			util.AssertWorkCondition(work.Namespace, work.Name, hubWorkClient, workapiv1.WorkAvailable, metav1.ConditionTrue,
				[]metav1.ConditionStatus{metav1.ConditionTrue, metav1.ConditionTrue, metav1.ConditionTrue}, eventuallyTimeout, eventuallyInterval)

			// check if resource created by stale manifest is deleted once it is removed from applied resource list
			gomega.Eventually(func() error {
				appliedManifestWork, err := spokeWorkClient.WorkV1().AppliedManifestWorks().Get(context.Background(), appliedManifestWorkName, metav1.GetOptions{})
				if err != nil {
					return err
				}

				for _, appliedResource := range appliedManifestWork.Status.AppliedResources {
					if appliedResource.Name == "cm3" {
						return fmt.Errorf("found applied resource cm3")
					}
				}

				return nil
			}, eventuallyTimeout, eventuallyInterval).ShouldNot(gomega.HaveOccurred())

			_, err = spokeKubeClient.CoreV1().ConfigMaps(commOptions.SpokeClusterName).Get(context.Background(), "cm3", metav1.GetOptions{})
			gomega.Expect(errors.IsNotFound(err)).To(gomega.BeTrue())
		})

		ginkgo.It("should delete work successfully", func() {
			util.AssertFinalizerAdded(work.Namespace, work.Name, expectedFinalizer, hubWorkClient, eventuallyTimeout, eventuallyInterval)

			err = hubWorkClient.WorkV1().ManifestWorks(commOptions.SpokeClusterName).Delete(context.Background(), work.Name, metav1.DeleteOptions{})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())

			util.AssertWorkDeleted(work.Namespace, work.Name, fmt.Sprintf("%s-%s", hubHash, work.Name),
				manifests, hubWorkClient, spokeWorkClient, spokeKubeClient, eventuallyTimeout, eventuallyInterval)
		})
	})

	ginkgo.Context("With CRD and CR in manifests", func() {
		var spokeDynamicClient dynamic.Interface
		var gvrs []schema.GroupVersionResource
		var objects []*unstructured.Unstructured

		ginkgo.BeforeEach(func() {
			spokeDynamicClient, err = dynamic.NewForConfig(spokeRestConfig)
			gvrs = nil
			objects = nil

			// crd
			obj, gvr, err := util.GuestbookCrd()
			gomega.Expect(err).ToNot(gomega.HaveOccurred())
			gvrs = append(gvrs, gvr)
			objects = append(objects, obj)

			// cr
			obj, gvr, err = util.GuestbookCr(commOptions.SpokeClusterName, "guestbook1")
			gomega.Expect(err).ToNot(gomega.HaveOccurred())
			gvrs = append(gvrs, gvr)
			objects = append(objects, obj)

			for _, obj := range objects {
				manifests = append(manifests, util.ToManifest(obj))
			}
		})

		ginkgo.It("should create CRD and CR successfully", func() {
			util.AssertWorkCondition(work.Namespace, work.Name, hubWorkClient, workapiv1.WorkApplied, metav1.ConditionTrue,
				[]metav1.ConditionStatus{metav1.ConditionTrue, metav1.ConditionTrue}, eventuallyTimeout, eventuallyInterval)
			util.AssertWorkCondition(work.Namespace, work.Name, hubWorkClient, workapiv1.WorkAvailable, metav1.ConditionTrue,
				[]metav1.ConditionStatus{metav1.ConditionTrue, metav1.ConditionTrue}, eventuallyTimeout, eventuallyInterval)

			var namespaces, names []string
			for _, obj := range objects {
				namespaces = append(namespaces, obj.GetNamespace())
				names = append(names, obj.GetName())
			}

			util.AssertExistenceOfResources(gvrs, namespaces, names, spokeDynamicClient, eventuallyTimeout, eventuallyInterval)
			util.AssertAppliedResources(appliedManifestWorkName, gvrs, namespaces, names, spokeWorkClient, eventuallyTimeout, eventuallyInterval)
		})

		ginkgo.It("should merge annotation of existing CR", func() {
			util.AssertWorkCondition(work.Namespace, work.Name, hubWorkClient, workapiv1.WorkApplied, metav1.ConditionTrue,
				[]metav1.ConditionStatus{metav1.ConditionTrue, metav1.ConditionTrue}, eventuallyTimeout, eventuallyInterval)
			util.AssertWorkCondition(work.Namespace, work.Name, hubWorkClient, workapiv1.WorkAvailable, metav1.ConditionTrue,
				[]metav1.ConditionStatus{metav1.ConditionTrue, metav1.ConditionTrue}, eventuallyTimeout, eventuallyInterval)

			var namespaces, names []string
			for _, obj := range objects {
				namespaces = append(namespaces, obj.GetNamespace())
				names = append(names, obj.GetName())
			}

			util.AssertExistenceOfResources(gvrs, namespaces, names, spokeDynamicClient, eventuallyTimeout, eventuallyInterval)
			util.AssertAppliedResources(appliedManifestWorkName, gvrs, namespaces, names, spokeWorkClient, eventuallyTimeout, eventuallyInterval)

			// update object label
			obj, gvr, err := util.GuestbookCr(commOptions.SpokeClusterName, "guestbook1")
			gomega.Expect(err).ToNot(gomega.HaveOccurred())

			cr, err := util.GetResource(obj.GetNamespace(), obj.GetName(), gvr, spokeDynamicClient)
			gomega.Expect(err).ToNot(gomega.HaveOccurred())

			cr.SetAnnotations(map[string]string{"foo": "bar"})
			_, err = spokeDynamicClient.Resource(gvr).Namespace(obj.GetNamespace()).Update(context.TODO(), cr, metav1.UpdateOptions{})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())

			// Update manifestwork
			obj.SetAnnotations(map[string]string{"foo1": "bar1"})
			work, err := hubWorkClient.WorkV1().ManifestWorks(work.Namespace).Get(context.TODO(), work.Name, metav1.GetOptions{})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())

			newWork := work.DeepCopy()
			newWork.Spec.Workload.Manifests[1] = util.ToManifest(obj)

			pathBytes, err := util.NewWorkPatch(work, newWork)
			gomega.Expect(err).ToNot(gomega.HaveOccurred())

			_, err = hubWorkClient.WorkV1().ManifestWorks(commOptions.SpokeClusterName).Patch(
				context.Background(), work.Name, types.MergePatchType, pathBytes, metav1.PatchOptions{})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())

			// wait for annotation merge
			gomega.Eventually(func() error {
				cr, err := util.GetResource(obj.GetNamespace(), obj.GetName(), gvr, spokeDynamicClient)
				if err != nil {
					return err
				}

				if len(cr.GetAnnotations()) != 2 {
					return fmt.Errorf("expect 2 annotation but get %v", cr.GetAnnotations())
				}

				return nil
			}, eventuallyTimeout, eventuallyInterval).Should(gomega.Succeed())

		})

		ginkgo.It("should keep the finalizer unchanged of existing CR", func() {
			util.AssertWorkCondition(work.Namespace, work.Name, hubWorkClient, workapiv1.WorkApplied, metav1.ConditionTrue,
				[]metav1.ConditionStatus{metav1.ConditionTrue, metav1.ConditionTrue}, eventuallyTimeout, eventuallyInterval)
			util.AssertWorkCondition(work.Namespace, work.Name, hubWorkClient, workapiv1.WorkAvailable, metav1.ConditionTrue,
				[]metav1.ConditionStatus{metav1.ConditionTrue, metav1.ConditionTrue}, eventuallyTimeout, eventuallyInterval)

			var namespaces, names []string
			for _, obj := range objects {
				namespaces = append(namespaces, obj.GetNamespace())
				names = append(names, obj.GetName())
			}

			util.AssertExistenceOfResources(gvrs, namespaces, names, spokeDynamicClient, eventuallyTimeout, eventuallyInterval)
			util.AssertAppliedResources(appliedManifestWorkName, gvrs, namespaces, names, spokeWorkClient, eventuallyTimeout, eventuallyInterval)

			// update object finalizer
			obj, gvr, err := util.GuestbookCr(commOptions.SpokeClusterName, "guestbook1")
			gomega.Expect(err).ToNot(gomega.HaveOccurred())

			cr, err := util.GetResource(obj.GetNamespace(), obj.GetName(), gvr, spokeDynamicClient)
			gomega.Expect(err).ToNot(gomega.HaveOccurred())

			cr.SetFinalizers([]string{"foo"})
			updated, err := spokeDynamicClient.Resource(gvr).Namespace(obj.GetNamespace()).Update(context.TODO(), cr, metav1.UpdateOptions{})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())
			rsvBefore := updated.GetResourceVersion()

			// Update manifestwork
			obj.SetFinalizers(nil)
			// set an annotation to make sure the cr will be updated, so that we can check whether the finalizer changest.
			obj.SetAnnotations(map[string]string{"foo": "bar"})

			updatedWork, err := hubWorkClient.WorkV1().ManifestWorks(work.Namespace).Get(context.TODO(), work.Name, metav1.GetOptions{})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())

			newWork := updatedWork.DeepCopy()
			newWork.Spec.Workload.Manifests[1] = util.ToManifest(obj)

			pathBytes, err := util.NewWorkPatch(updatedWork, newWork)
			gomega.Expect(err).ToNot(gomega.HaveOccurred())

			_, err = hubWorkClient.WorkV1().ManifestWorks(commOptions.SpokeClusterName).Patch(
				context.Background(), updatedWork.Name, types.MergePatchType, pathBytes, metav1.PatchOptions{})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())

			// wait for annotation merge
			gomega.Eventually(func() error {
				cr, err := util.GetResource(obj.GetNamespace(), obj.GetName(), gvr, spokeDynamicClient)
				if err != nil {
					return err
				}

				if len(cr.GetAnnotations()) != 1 {
					return fmt.Errorf("expect 1 annotation but get %v", cr.GetAnnotations())
				}

				return nil
			}, eventuallyTimeout, eventuallyInterval).Should(gomega.Succeed())

			// check if finalizer exists
			cr, err = util.GetResource(obj.GetNamespace(), obj.GetName(), gvr, spokeDynamicClient)
			gomega.Expect(err).ToNot(gomega.HaveOccurred())
			gomega.Expect(cr.GetFinalizers()).NotTo(gomega.BeNil())
			gomega.Expect(cr.GetFinalizers()[0]).To(gomega.Equal("foo"))
			gomega.Expect(cr.GetResourceVersion()).NotTo(gomega.Equal(rsvBefore))

			// remove the finalizer
			cr.SetFinalizers(nil)
			_, err = spokeDynamicClient.Resource(gvr).Namespace(obj.GetNamespace()).Update(context.TODO(), cr, metav1.UpdateOptions{})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())
		})

		ginkgo.It("should delete CRD and CR successfully", func() {
			util.AssertWorkCondition(work.Namespace, work.Name, hubWorkClient, workapiv1.WorkApplied, metav1.ConditionTrue,
				[]metav1.ConditionStatus{metav1.ConditionTrue, metav1.ConditionTrue}, eventuallyTimeout, eventuallyInterval)
			util.AssertWorkCondition(work.Namespace, work.Name, hubWorkClient, workapiv1.WorkAvailable, metav1.ConditionTrue,
				[]metav1.ConditionStatus{metav1.ConditionTrue, metav1.ConditionTrue}, eventuallyTimeout, eventuallyInterval)

			var namespaces, names []string
			for _, obj := range objects {
				namespaces = append(namespaces, obj.GetNamespace())
				names = append(names, obj.GetName())
			}

			util.AssertExistenceOfResources(gvrs, namespaces, names, spokeDynamicClient, eventuallyTimeout, eventuallyInterval)
			util.AssertAppliedResources(appliedManifestWorkName, gvrs, namespaces, names, spokeWorkClient, eventuallyTimeout, eventuallyInterval)

			// delete manifest work
			err = hubWorkClient.WorkV1().ManifestWorks(commOptions.SpokeClusterName).Delete(context.Background(), work.Name, metav1.DeleteOptions{})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())

			// wait for deletion of manifest work
			gomega.Eventually(func() bool {
				_, err := hubWorkClient.WorkV1().ManifestWorks(commOptions.SpokeClusterName).Get(context.Background(), work.Name, metav1.GetOptions{})
				return errors.IsNotFound(err)
			}, eventuallyTimeout, eventuallyInterval).Should(gomega.BeTrue())

			// Once manifest work is not found, its relating appliedmanifestwork will be evicted, and finally,
			// all CRs/CRD should be deleted too
			gomega.Eventually(func() error {
				for i := range gvrs {
					_, err := util.GetResource(namespaces[i], names[i], gvrs[i], spokeDynamicClient)
					if errors.IsNotFound(err) {
						continue
					}
					if err != nil {
						return err
					}

					return fmt.Errorf("the resource %s/%s still exists", namespaces[i], names[i])
				}

				return nil
			}, eventuallyTimeout, eventuallyInterval).ShouldNot(gomega.HaveOccurred())
		})
	})

	ginkgo.Context("With cluster scoped resource in manifests", func() {
		var spokeDynamicClient dynamic.Interface
		var gvrs []schema.GroupVersionResource
		var objects []*unstructured.Unstructured

		ginkgo.BeforeEach(func() {
			spokeDynamicClient, err = dynamic.NewForConfig(spokeRestConfig)
			gvrs = nil
			objects = nil

			// Create a clusterrole with namespace in metadata
			u, gvr := util.NewClusterRole(commOptions.SpokeClusterName, "work-clusterrole")
			gvrs = append(gvrs, gvr)
			objects = append(objects, u)

			for _, obj := range objects {
				manifests = append(manifests, util.ToManifest(obj))
			}
		})

		ginkgo.It("should create Clusterrole successfully", func() {
			util.AssertWorkCondition(work.Namespace, work.Name, hubWorkClient, workapiv1.WorkApplied, metav1.ConditionTrue,
				[]metav1.ConditionStatus{metav1.ConditionTrue},
				eventuallyTimeout, eventuallyInterval)
			util.AssertWorkCondition(work.Namespace, work.Name, hubWorkClient, workapiv1.WorkAvailable, metav1.ConditionTrue,
				[]metav1.ConditionStatus{metav1.ConditionTrue},
				eventuallyTimeout, eventuallyInterval)

			var namespaces, names []string
			for _, obj := range objects {
				// the namespace should be empty for cluster scoped resource
				namespaces = append(namespaces, "")
				names = append(names, obj.GetName())
			}

			util.AssertExistenceOfResources(gvrs, namespaces, names, spokeDynamicClient, eventuallyTimeout, eventuallyInterval)
			util.AssertAppliedResources(appliedManifestWorkName, gvrs, namespaces, names, spokeWorkClient, eventuallyTimeout, eventuallyInterval)
		})

	})

	ginkgo.Context("With Service Account, Role, RoleBinding and Deployment in manifests", func() {
		var spokeDynamicClient dynamic.Interface
		var gvrs []schema.GroupVersionResource
		var objects []*unstructured.Unstructured

		ginkgo.BeforeEach(func() {
			spokeDynamicClient, err = dynamic.NewForConfig(spokeRestConfig)
			gvrs = nil
			objects = nil

			u, gvr := util.NewServiceAccount(commOptions.SpokeClusterName, "sa")
			gvrs = append(gvrs, gvr)
			objects = append(objects, u)

			u, gvr = util.NewRole(commOptions.SpokeClusterName, "role1")
			gvrs = append(gvrs, gvr)
			objects = append(objects, u)

			u, gvr = util.NewRoleBinding(commOptions.SpokeClusterName, "rolebinding1", "sa", "role1")
			gvrs = append(gvrs, gvr)
			objects = append(objects, u)

			u, gvr, err = util.NewDeployment(commOptions.SpokeClusterName, "deploy1", "sa")
			gomega.Expect(err).ToNot(gomega.HaveOccurred())
			gvrs = append(gvrs, gvr)
			objects = append(objects, u)

			for _, obj := range objects {
				manifests = append(manifests, util.ToManifest(obj))
			}
		})

		ginkgo.It("should create Service Account, Role, RoleBinding and Deployment successfully", func() {
			util.AssertWorkCondition(work.Namespace, work.Name, hubWorkClient, workapiv1.WorkApplied, metav1.ConditionTrue,
				[]metav1.ConditionStatus{metav1.ConditionTrue, metav1.ConditionTrue, metav1.ConditionTrue, metav1.ConditionTrue},
				eventuallyTimeout, eventuallyInterval)
			util.AssertWorkCondition(work.Namespace, work.Name, hubWorkClient, workapiv1.WorkAvailable, metav1.ConditionTrue,
				[]metav1.ConditionStatus{metav1.ConditionTrue, metav1.ConditionTrue, metav1.ConditionTrue, metav1.ConditionTrue},
				eventuallyTimeout, eventuallyInterval)

			var namespaces, names []string
			for _, obj := range objects {
				namespaces = append(namespaces, obj.GetNamespace())
				names = append(names, obj.GetName())
			}

			util.AssertExistenceOfResources(gvrs, namespaces, names, spokeDynamicClient, eventuallyTimeout, eventuallyInterval)
			util.AssertAppliedResources(appliedManifestWorkName, gvrs, namespaces, names, spokeWorkClient, eventuallyTimeout, eventuallyInterval)
		})

		ginkgo.It("should update Service Account and Deployment successfully", func() {
			ginkgo.By("check condition status in work status")
			util.AssertWorkCondition(work.Namespace, work.Name, hubWorkClient, workapiv1.WorkApplied, metav1.ConditionTrue,
				[]metav1.ConditionStatus{metav1.ConditionTrue, metav1.ConditionTrue, metav1.ConditionTrue, metav1.ConditionTrue},
				eventuallyTimeout, eventuallyInterval)
			util.AssertWorkCondition(work.Namespace, work.Name, hubWorkClient, workapiv1.WorkAvailable, metav1.ConditionTrue,
				[]metav1.ConditionStatus{metav1.ConditionTrue, metav1.ConditionTrue, metav1.ConditionTrue, metav1.ConditionTrue},
				eventuallyTimeout, eventuallyInterval)

			util.AssertWorkGeneration(work.Namespace, work.Name, hubWorkClient, workapiv1.WorkApplied, eventuallyTimeout, eventuallyInterval)
			util.AssertWorkGeneration(work.Namespace, work.Name, hubWorkClient, workapiv1.WorkAvailable, eventuallyTimeout, eventuallyInterval)

			ginkgo.By("check existence of all maintained resources")
			var namespaces, names []string
			for _, obj := range objects {
				namespaces = append(namespaces, obj.GetNamespace())
				names = append(names, obj.GetName())
			}
			util.AssertExistenceOfResources(gvrs, namespaces, names, spokeDynamicClient, eventuallyTimeout, eventuallyInterval)

			ginkgo.By("check if applied resources in status are updated")
			util.AssertAppliedResources(appliedManifestWorkName, gvrs, namespaces, names, spokeWorkClient, eventuallyTimeout, eventuallyInterval)

			// update manifests in work: 1) swap service account and deployment; 2) rename service account; 3) update deployment
			ginkgo.By("update manifests in work")
			oldServiceAccount := objects[0]
			gvrs[0], gvrs[3] = gvrs[3], gvrs[0]
			u, _ := util.NewServiceAccount(commOptions.SpokeClusterName, "admin")
			objects[3] = u
			u, _, err = util.NewDeployment(commOptions.SpokeClusterName, "deploy1", "admin")
			gomega.Expect(err).ToNot(gomega.HaveOccurred())
			objects[0] = u

			var newManifests []workapiv1.Manifest
			for _, obj := range objects {
				newManifests = append(newManifests, util.ToManifest(obj))
			}

			// slow down to make the difference between LastTransitionTime and updateTime large enough for measurement
			time.Sleep(1 * time.Second)
			updateTime := metav1.Now()
			time.Sleep(1 * time.Second)

			updatedWork, err := hubWorkClient.WorkV1().ManifestWorks(commOptions.SpokeClusterName).Get(context.Background(), work.Name, metav1.GetOptions{})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())

			newWork := updatedWork.DeepCopy()
			newWork.Spec.Workload.Manifests = newManifests

			pathBytes, err := util.NewWorkPatch(updatedWork, newWork)
			gomega.Expect(err).ToNot(gomega.HaveOccurred())

			_, err = hubWorkClient.WorkV1().ManifestWorks(commOptions.SpokeClusterName).Patch(
				context.Background(), updatedWork.Name, types.MergePatchType, pathBytes, metav1.PatchOptions{})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())

			ginkgo.By("check existence of all maintained resources")
			namespaces = nil
			names = nil
			for _, obj := range objects {
				namespaces = append(namespaces, obj.GetNamespace())
				names = append(names, obj.GetName())
			}
			util.AssertExistenceOfResources(gvrs, namespaces, names, spokeDynamicClient, eventuallyTimeout, eventuallyInterval)

			ginkgo.By("check if deployment is updated")
			gomega.Eventually(func() error {
				u, err := util.GetResource(commOptions.SpokeClusterName, objects[0].GetName(), gvrs[0], spokeDynamicClient)
				if err != nil {
					return err
				}

				sa, _, _ := unstructured.NestedString(u.Object, "spec", "template", "spec", "serviceAccountName")
				if sa != "admin" {
					return fmt.Errorf("sa is not admin")
				}
				return nil
			}, eventuallyTimeout, eventuallyInterval).ShouldNot(gomega.HaveOccurred())

			ginkgo.By("check if LastTransitionTime is updated")
			gomega.Eventually(func() error {
				work, err = hubWorkClient.WorkV1().ManifestWorks(commOptions.SpokeClusterName).Get(context.Background(), work.Name, metav1.GetOptions{})
				if err != nil {
					return err
				}
				if len(work.Status.ResourceStatus.Manifests) != len(objects) {
					return fmt.Errorf("there are %d objects, but got %d in status",
						len(objects), len(work.Status.ResourceStatus.Manifests))
				}

				for i := range work.Status.ResourceStatus.Manifests {
					if len(work.Status.ResourceStatus.Manifests[i].Conditions) != 3 {
						return fmt.Errorf("expect 3 conditions, but got %d",
							len(work.Status.ResourceStatus.Manifests[i].Conditions))
					}
				}

				// the LastTransitionTime of deployment should not change because of resource update
				if updateTime.Before(&work.Status.ResourceStatus.Manifests[0].Conditions[0].LastTransitionTime) {
					return fmt.Errorf("condition time of deployment changed")
				}

				// the LastTransitionTime of service account changes because of resource re-creation
				if work.Status.ResourceStatus.Manifests[3].Conditions[0].LastTransitionTime.Before(&updateTime) {
					return fmt.Errorf("condition time of service account did not change")
				}

				return nil
			}, eventuallyTimeout, eventuallyInterval).ShouldNot(gomega.HaveOccurred())

			util.AssertWorkGeneration(work.Namespace, work.Name, hubWorkClient, workapiv1.WorkApplied, eventuallyTimeout, eventuallyInterval)
			util.AssertWorkGeneration(work.Namespace, work.Name, hubWorkClient, workapiv1.WorkAvailable, eventuallyTimeout, eventuallyInterval)

			ginkgo.By("check if applied resources in status are updated")
			util.AssertAppliedResources(appliedManifestWorkName, gvrs, namespaces, names, spokeWorkClient, eventuallyTimeout, eventuallyInterval)

			ginkgo.By("check if resources which are no longer maintained have been deleted")
			util.AssertNonexistenceOfResources(
				[]schema.GroupVersionResource{gvrs[3]}, []string{oldServiceAccount.GetNamespace()}, []string{oldServiceAccount.GetName()},
				spokeDynamicClient, eventuallyTimeout, eventuallyInterval)
		})
	})

	ginkgo.Context("Foreground deletion", func() {
		var finalizer = "cluster.open-cluster-management.io/testing"
		ginkgo.BeforeEach(func() {
			manifests = []workapiv1.Manifest{
				util.ToManifest(util.NewConfigmap(commOptions.SpokeClusterName, cm1, map[string]string{"a": "b"}, []string{finalizer})),
				util.ToManifest(util.NewConfigmap(commOptions.SpokeClusterName, cm2, map[string]string{"c": "d"}, []string{finalizer})),
				util.ToManifest(util.NewConfigmap(commOptions.SpokeClusterName, "cm3", map[string]string{"e": "f"}, []string{finalizer})),
			}
		})

		ginkgo.AfterEach(func() {
			err = util.RemoveConfigmapFinalizers(spokeKubeClient, commOptions.SpokeClusterName, cm1, cm2, "cm3")
			gomega.Expect(err).ToNot(gomega.HaveOccurred())
		})

		ginkgo.It("should remove applied resource immediately when work is updated", func() {
			updatedWork, err := hubWorkClient.WorkV1().ManifestWorks(commOptions.SpokeClusterName).Get(context.Background(), work.Name, metav1.GetOptions{})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())

			newWork := updatedWork.DeepCopy()
			newWork.Spec.Workload.Manifests = manifests[1:]

			pathBytes, err := util.NewWorkPatch(updatedWork, newWork)
			gomega.Expect(err).ToNot(gomega.HaveOccurred())

			_, err = hubWorkClient.WorkV1().ManifestWorks(commOptions.SpokeClusterName).Patch(
				context.Background(), updatedWork.Name, types.MergePatchType, pathBytes, metav1.PatchOptions{})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())

			// remove finalizer from the applied resources for stale manifest after 2 seconds
			go func() {
				time.Sleep(2 * time.Second)
				// remove finalizers of cm1
				_ = util.RemoveConfigmapFinalizers(spokeKubeClient, commOptions.SpokeClusterName, cm1)
			}()

			// check if resource created by stale manifest is deleted once it is removed from applied resource list
			gomega.Eventually(func() error {
				appliedManifestWork, err := spokeWorkClient.WorkV1().AppliedManifestWorks().Get(context.Background(), appliedManifestWorkName, metav1.GetOptions{})
				if err != nil {
					return err
				}

				for _, appliedResource := range appliedManifestWork.Status.AppliedResources {
					if appliedResource.Name == cm1 {
						return fmt.Errorf("found resource cm1")
					}
				}

				return nil
			}, eventuallyTimeout, eventuallyInterval).ShouldNot(gomega.HaveOccurred())

			_, err = spokeKubeClient.CoreV1().ConfigMaps(commOptions.SpokeClusterName).Get(context.Background(), cm1, metav1.GetOptions{})
			gomega.Expect(errors.IsNotFound(err)).To(gomega.BeTrue())

			err = hubWorkClient.WorkV1().ManifestWorks(work.Namespace).Delete(context.Background(), work.Name, metav1.DeleteOptions{})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())
		})

		ginkgo.It("should remove applied resource for stale manifest from list once the resource is gone", func() {
			util.AssertExistenceOfConfigMaps(manifests, spokeKubeClient, eventuallyTimeout, eventuallyInterval)

			util.AssertWorkCondition(work.Namespace, work.Name, hubWorkClient, workapiv1.WorkApplied, metav1.ConditionTrue,
				[]metav1.ConditionStatus{metav1.ConditionTrue, metav1.ConditionTrue, metav1.ConditionTrue}, eventuallyTimeout, eventuallyInterval)
			util.AssertWorkCondition(work.Namespace, work.Name, hubWorkClient, workapiv1.WorkAvailable, metav1.ConditionTrue,
				[]metav1.ConditionStatus{metav1.ConditionTrue, metav1.ConditionTrue, metav1.ConditionTrue}, eventuallyTimeout, eventuallyInterval)

			updatedWork, err := hubWorkClient.WorkV1().ManifestWorks(commOptions.SpokeClusterName).Get(context.Background(), work.Name, metav1.GetOptions{})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())

			newWork := updatedWork.DeepCopy()
			newWork.Spec.Workload.Manifests = manifests[1:]

			pathBytes, err := util.NewWorkPatch(updatedWork, newWork)
			gomega.Expect(err).ToNot(gomega.HaveOccurred())

			_, err = hubWorkClient.WorkV1().ManifestWorks(commOptions.SpokeClusterName).Patch(
				context.Background(), updatedWork.Name, types.MergePatchType, pathBytes, metav1.PatchOptions{})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())

			util.AssertExistenceOfConfigMaps(manifests[1:], spokeKubeClient, eventuallyTimeout, eventuallyInterval)

			err = hubWorkClient.WorkV1().ManifestWorks(work.Namespace).Delete(context.Background(), work.Name, metav1.DeleteOptions{})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())

			// remove finalizer from the applied resources for stale manifest after 2 seconds
			go func() {
				time.Sleep(2 * time.Second)
				// remove finalizers of cm1
				_ = util.RemoveConfigmapFinalizers(spokeKubeClient, commOptions.SpokeClusterName, cm1)
			}()

			// check if resource created by stale manifest is deleted once it is removed from applied resource list
			gomega.Eventually(func() error {
				appliedManifestWork, err := spokeWorkClient.WorkV1().AppliedManifestWorks().Get(context.Background(), appliedManifestWorkName, metav1.GetOptions{})
				if err != nil {
					return err
				}

				for _, appliedResource := range appliedManifestWork.Status.AppliedResources {
					if appliedResource.Name == cm1 {
						return fmt.Errorf("found resource cm1")
					}
				}

				return nil
			}, eventuallyTimeout, eventuallyInterval).ShouldNot(gomega.HaveOccurred())

			_, err = spokeKubeClient.CoreV1().ConfigMaps(commOptions.SpokeClusterName).Get(context.Background(), cm1, metav1.GetOptions{})
			gomega.Expect(errors.IsNotFound(err)).To(gomega.BeTrue())
		})

		ginkgo.It("should delete manifest work eventually after all applied resources are gone", func() {
			util.AssertExistenceOfConfigMaps(manifests, spokeKubeClient, eventuallyTimeout, eventuallyInterval)

			util.AssertWorkCondition(work.Namespace, work.Name, hubWorkClient, workapiv1.WorkApplied, metav1.ConditionTrue,
				[]metav1.ConditionStatus{metav1.ConditionTrue, metav1.ConditionTrue, metav1.ConditionTrue}, eventuallyTimeout, eventuallyInterval)
			util.AssertWorkCondition(work.Namespace, work.Name, hubWorkClient, workapiv1.WorkAvailable, metav1.ConditionTrue,
				[]metav1.ConditionStatus{metav1.ConditionTrue, metav1.ConditionTrue, metav1.ConditionTrue}, eventuallyTimeout, eventuallyInterval)

			err := hubWorkClient.WorkV1().ManifestWorks(work.Namespace).Delete(context.Background(), work.Name, metav1.DeleteOptions{})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())

			util.AssertWorkCondition(work.Namespace, work.Name, hubWorkClient, workapiv1.WorkDeleting, metav1.ConditionTrue,
				nil, eventuallyTimeout, eventuallyInterval)

			// remove finalizer from one of applied resources every 2 seconds
			go func() {
				for _, manifest := range manifests {
					cm := manifest.Object.(*corev1.ConfigMap)
					err = retry.OnError(
						retry.DefaultBackoff,
						func(err error) bool {
							return err != nil
						},
						func() error {
							cm, err := spokeKubeClient.CoreV1().ConfigMaps(cm.Namespace).Get(context.Background(), cm.Name, metav1.GetOptions{})
							if err != nil {
								return err
							}

							cm.Finalizers = nil
							_, err = spokeKubeClient.CoreV1().ConfigMaps(cm.Namespace).Update(context.Background(), cm, metav1.UpdateOptions{})
							return err
						})
					gomega.Expect(err).ToNot(gomega.HaveOccurred())
					time.Sleep(2 * time.Second)
				}
			}()

			util.AssertWorkDeleted(work.Namespace, work.Name, fmt.Sprintf("%s-%s", hubHash, work.Name), manifests,
				hubWorkClient, spokeWorkClient, spokeKubeClient, eventuallyTimeout, eventuallyInterval)
		})

		ginkgo.It("should delete applied manifest work if it is orphan", func() {
			appliedManifestWork := &workapiv1.AppliedManifestWork{
				ObjectMeta: metav1.ObjectMeta{
					Name: fmt.Sprintf("%s-fakework", hubHash),
				},
				Spec: workapiv1.AppliedManifestWorkSpec{
					HubHash:          hubHash,
					AgentID:          hubHash,
					ManifestWorkName: "fakework",
				},
			}
			_, err := spokeWorkClient.WorkV1().AppliedManifestWorks().Create(context.Background(), appliedManifestWork, metav1.CreateOptions{})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())

			util.AssertAppliedManifestWorkDeleted(appliedManifestWork.Name, spokeWorkClient, eventuallyTimeout, eventuallyInterval)
		})
	})

	ginkgo.Context("Work completion", func() {
		ginkgo.BeforeEach(func() {
			manifests = []workapiv1.Manifest{
				util.ToManifest(util.NewConfigmap(commOptions.SpokeClusterName, cm1, map[string]string{"a": "b"}, nil)),
				util.ToManifest(util.NewConfigmap(commOptions.SpokeClusterName, cm2, map[string]string{"c": "d"}, nil)),
			}
			workOpts = append(workOpts, func(work *workapiv1.ManifestWork) {
				work.Spec.ManifestConfigs = []workapiv1.ManifestConfigOption{
					{
						ResourceIdentifier: workapiv1.ResourceIdentifier{
							Resource:  "configmaps",
							Name:      cm1,
							Namespace: commOptions.SpokeClusterName,
						},
						ConditionRules: []workapiv1.ConditionRule{
							{
								Type:      workapiv1.CelConditionExpressionsType,
								Condition: workapiv1.ManifestComplete,
								CelExpressions: []string{
									"has(object.data.complete) && object.data.complete == 'true'",
								},
							},
						},
					},
				}
			})
		})

		ginkgo.It("should update work and be completed", func() {
			util.AssertExistenceOfConfigMaps(manifests, spokeKubeClient, eventuallyTimeout, eventuallyInterval)

			util.AssertWorkCondition(work.Namespace, work.Name, hubWorkClient, workapiv1.WorkApplied, metav1.ConditionTrue,
				[]metav1.ConditionStatus{metav1.ConditionTrue, metav1.ConditionTrue}, eventuallyTimeout, eventuallyInterval)
			util.AssertWorkCondition(work.Namespace, work.Name, hubWorkClient, workapiv1.WorkAvailable, metav1.ConditionTrue,
				[]metav1.ConditionStatus{metav1.ConditionTrue, metav1.ConditionTrue}, eventuallyTimeout, eventuallyInterval)

			newManifests := []workapiv1.Manifest{
				util.ToManifest(util.NewConfigmap(commOptions.SpokeClusterName, cm1, map[string]string{"a": "b", "complete": "true"}, nil)),
				util.ToManifest(util.NewConfigmap(commOptions.SpokeClusterName, cm2, map[string]string{"c": "d"}, nil)),
			}

			updatedWork, err := hubWorkClient.WorkV1().ManifestWorks(commOptions.SpokeClusterName).Get(context.Background(), work.Name, metav1.GetOptions{})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())

			newWork := updatedWork.DeepCopy()
			newWork.Spec.Workload.Manifests = newManifests

			pathBytes, err := util.NewWorkPatch(updatedWork, newWork)
			gomega.Expect(err).ToNot(gomega.HaveOccurred())

			_, err = hubWorkClient.WorkV1().ManifestWorks(commOptions.SpokeClusterName).Patch(
				context.Background(), updatedWork.Name, types.MergePatchType, pathBytes, metav1.PatchOptions{})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())

			// ManifestWork should be marked completed
			gomega.Eventually(func() error {
				work, err := hubWorkClient.WorkV1().ManifestWorks(commOptions.SpokeClusterName).Get(context.Background(), work.Name, metav1.GetOptions{})
				if err != nil {
					return err
				}

				if err := util.CheckExpectedConditions(work.Status.Conditions, metav1.Condition{
					Type:   workapiv1.WorkComplete,
					Status: metav1.ConditionTrue,
					Reason: "ConditionRulesAggregated",
				}); err != nil {
					return fmt.Errorf("%s: %v", work.Name, err)
				}
				return nil
			}, eventuallyTimeout, eventuallyInterval).Should(gomega.Succeed())
		})
	})
})
