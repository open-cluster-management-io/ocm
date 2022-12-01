package integration

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
	utilrand "k8s.io/apimachinery/pkg/util/rand"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/util/retry"

	workapiv1 "open-cluster-management.io/api/work/v1"
	"open-cluster-management.io/work/pkg/spoke"
	"open-cluster-management.io/work/test/integration/util"
)

func startWorkAgent(ctx context.Context, o *spoke.WorkloadAgentOptions) {
	err := o.RunWorkloadAgent(ctx, &controllercmd.ControllerContext{
		KubeConfig:    spokeRestConfig,
		EventRecorder: util.NewIntegrationTestEventRecorder("integration"),
	})
	gomega.Expect(err).NotTo(gomega.HaveOccurred())
}

var _ = ginkgo.Describe("ManifestWork", func() {
	var o *spoke.WorkloadAgentOptions
	var cancel context.CancelFunc

	var work *workapiv1.ManifestWork
	var manifests []workapiv1.Manifest
	var appliedManifestWorkName string

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
		work, err = hubWorkClient.WorkV1().ManifestWorks(o.SpokeClusterName).Create(context.Background(), work, metav1.CreateOptions{})
		appliedManifestWorkName = fmt.Sprintf("%s-%s", hubHash, work.Name)
		gomega.Expect(err).ToNot(gomega.HaveOccurred())
	})

	ginkgo.AfterEach(func() {
		if cancel != nil {
			cancel()
		}
		err := spokeKubeClient.CoreV1().Namespaces().Delete(context.Background(), o.SpokeClusterName, metav1.DeleteOptions{})
		gomega.Expect(err).ToNot(gomega.HaveOccurred())
	})

	ginkgo.Context("With a single manifest", func() {
		ginkgo.BeforeEach(func() {
			manifests = []workapiv1.Manifest{
				util.ToManifest(util.NewConfigmap(o.SpokeClusterName, "cm1", map[string]string{"a": "b"}, nil)),
			}
		})

		ginkgo.It("should create work and then apply it successfully", func() {
			util.AssertExistenceOfConfigMaps(manifests, spokeKubeClient, eventuallyTimeout, eventuallyInterval)

			util.AssertWorkCondition(work.Namespace, work.Name, hubWorkClient, string(workapiv1.WorkApplied), metav1.ConditionTrue,
				[]metav1.ConditionStatus{metav1.ConditionTrue}, eventuallyTimeout, eventuallyInterval)
			util.AssertWorkCondition(work.Namespace, work.Name, hubWorkClient, string(workapiv1.WorkAvailable), metav1.ConditionTrue,
				[]metav1.ConditionStatus{metav1.ConditionTrue}, eventuallyTimeout, eventuallyInterval)
		})

		ginkgo.It("should update work and then apply it successfully", func() {
			util.AssertWorkCondition(work.Namespace, work.Name, hubWorkClient, string(workapiv1.WorkApplied), metav1.ConditionTrue,
				[]metav1.ConditionStatus{metav1.ConditionTrue}, eventuallyTimeout, eventuallyInterval)
			util.AssertWorkCondition(work.Namespace, work.Name, hubWorkClient, string(workapiv1.WorkAvailable), metav1.ConditionTrue,
				[]metav1.ConditionStatus{metav1.ConditionTrue}, eventuallyTimeout, eventuallyInterval)

			newManifests := []workapiv1.Manifest{
				util.ToManifest(util.NewConfigmap(o.SpokeClusterName, "cm2", map[string]string{"x": "y"}, nil)),
			}
			work, err = hubWorkClient.WorkV1().ManifestWorks(o.SpokeClusterName).Get(context.Background(), work.Name, metav1.GetOptions{})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())
			work.Spec.Workload.Manifests = newManifests

			work, err = hubWorkClient.WorkV1().ManifestWorks(o.SpokeClusterName).Update(context.Background(), work, metav1.UpdateOptions{})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())

			util.AssertExistenceOfConfigMaps(newManifests, spokeKubeClient, eventuallyTimeout, eventuallyInterval)

			// check if resource created by stale manifest is deleted once it is removed from applied resource list
			gomega.Eventually(func() error {
				appliedManifestWork, err := hubWorkClient.WorkV1().AppliedManifestWorks().Get(
					context.Background(), appliedManifestWorkName, metav1.GetOptions{})
				if err != nil {
					return err
				}

				for _, appliedResource := range appliedManifestWork.Status.AppliedResources {
					if appliedResource.Name == "cm1" {
						return fmt.Errorf("found applied resource cm1")
					}
				}

				return nil
			}, eventuallyTimeout, eventuallyInterval).ShouldNot(gomega.HaveOccurred())

			_, err = spokeKubeClient.CoreV1().ConfigMaps(o.SpokeClusterName).Get(context.Background(), "cm1", metav1.GetOptions{})
			gomega.Expect(errors.IsNotFound(err)).To(gomega.BeTrue())
		})

		ginkgo.It("should delete work successfully", func() {
			util.AssertFinalizerAdded(work.Namespace, work.Name, hubWorkClient, eventuallyTimeout, eventuallyInterval)

			err = hubWorkClient.WorkV1().ManifestWorks(o.SpokeClusterName).Delete(context.Background(), work.Name, metav1.DeleteOptions{})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())

			util.AssertWorkDeleted(work.Namespace, work.Name, hubHash, manifests, hubWorkClient, spokeKubeClient, eventuallyTimeout, eventuallyInterval)
		})
	})

	ginkgo.Context("With multiple manifests", func() {
		ginkgo.BeforeEach(func() {
			manifests = []workapiv1.Manifest{
				util.ToManifest(util.NewConfigmap("non-existent-namespace", "cm1", map[string]string{"a": "b"}, nil)),
				util.ToManifest(util.NewConfigmap(o.SpokeClusterName, "cm2", map[string]string{"c": "d"}, nil)),
				util.ToManifest(util.NewConfigmap(o.SpokeClusterName, "cm3", map[string]string{"e": "f"}, nil)),
			}
		})

		ginkgo.It("should create work and then apply it successfully", func() {
			util.AssertExistenceOfConfigMaps(manifests[1:], spokeKubeClient, eventuallyTimeout, eventuallyInterval)

			util.AssertWorkCondition(work.Namespace, work.Name, hubWorkClient, string(workapiv1.WorkApplied), metav1.ConditionFalse,
				[]metav1.ConditionStatus{metav1.ConditionFalse, metav1.ConditionTrue, metav1.ConditionTrue}, eventuallyTimeout, eventuallyInterval)
			util.AssertWorkCondition(work.Namespace, work.Name, hubWorkClient, string(workapiv1.WorkAvailable), metav1.ConditionFalse,
				[]metav1.ConditionStatus{metav1.ConditionFalse, metav1.ConditionTrue, metav1.ConditionTrue}, eventuallyTimeout, eventuallyInterval)
		})

		ginkgo.It("should update work and then apply it successfully", func() {
			util.AssertExistenceOfConfigMaps(manifests[1:], spokeKubeClient, eventuallyTimeout, eventuallyInterval)

			util.AssertWorkCondition(work.Namespace, work.Name, hubWorkClient, string(workapiv1.WorkApplied), metav1.ConditionFalse,
				[]metav1.ConditionStatus{metav1.ConditionFalse, metav1.ConditionTrue, metav1.ConditionTrue}, eventuallyTimeout, eventuallyInterval)
			util.AssertWorkCondition(work.Namespace, work.Name, hubWorkClient, string(workapiv1.WorkAvailable), metav1.ConditionFalse,
				[]metav1.ConditionStatus{metav1.ConditionFalse, metav1.ConditionTrue, metav1.ConditionTrue}, eventuallyTimeout, eventuallyInterval)

			newManifests := []workapiv1.Manifest{
				util.ToManifest(util.NewConfigmap(o.SpokeClusterName, "cm1", map[string]string{"a": "b"}, nil)),
				util.ToManifest(util.NewConfigmap(o.SpokeClusterName, "cm2", map[string]string{"x": "y"}, nil)),
				util.ToManifest(util.NewConfigmap(o.SpokeClusterName, "cm4", map[string]string{"e": "f"}, nil)),
			}

			work, err = hubWorkClient.WorkV1().ManifestWorks(o.SpokeClusterName).Get(context.Background(), work.Name, metav1.GetOptions{})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())
			work.Spec.Workload.Manifests = newManifests
			work, err = hubWorkClient.WorkV1().ManifestWorks(o.SpokeClusterName).Update(context.Background(), work, metav1.UpdateOptions{})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())

			util.AssertExistenceOfConfigMaps(newManifests, spokeKubeClient, eventuallyTimeout, eventuallyInterval)

			// check if Available status is updated or not
			util.AssertWorkCondition(work.Namespace, work.Name, hubWorkClient, string(workapiv1.WorkAvailable), metav1.ConditionTrue,
				[]metav1.ConditionStatus{metav1.ConditionTrue, metav1.ConditionTrue, metav1.ConditionTrue}, eventuallyTimeout, eventuallyInterval)

			// check if resource created by stale manifest is deleted once it is removed from applied resource list
			gomega.Eventually(func() error {
				appliedManifestWork, err := spokeWorkClient.WorkV1().AppliedManifestWorks().Get(context.Background(), appliedManifestWorkName, metav1.GetOptions{})
				if err != nil {
					return err
				}

				for _, appliedResource := range appliedManifestWork.Status.AppliedResources {
					if appliedResource.Name == "cm3" {
						return fmt.Errorf("found appled resource cm3")
					}
				}

				return nil
			}, eventuallyTimeout, eventuallyInterval).ShouldNot(gomega.HaveOccurred())

			_, err = spokeKubeClient.CoreV1().ConfigMaps(o.SpokeClusterName).Get(context.Background(), "cm3", metav1.GetOptions{})
			gomega.Expect(errors.IsNotFound(err)).To(gomega.BeTrue())
		})

		ginkgo.It("should delete work successfully", func() {
			util.AssertFinalizerAdded(work.Namespace, work.Name, hubWorkClient, eventuallyTimeout, eventuallyInterval)

			err = hubWorkClient.WorkV1().ManifestWorks(o.SpokeClusterName).Delete(context.Background(), work.Name, metav1.DeleteOptions{})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())

			util.AssertWorkDeleted(work.Namespace, work.Name, hubHash, manifests, hubWorkClient, spokeKubeClient, eventuallyTimeout, eventuallyInterval)
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
			obj, gvr, err = util.GuestbookCr(o.SpokeClusterName, "guestbook1")
			gomega.Expect(err).ToNot(gomega.HaveOccurred())
			gvrs = append(gvrs, gvr)
			objects = append(objects, obj)

			for _, obj := range objects {
				manifests = append(manifests, util.ToManifest(obj))
			}
		})

		ginkgo.It("should create CRD and CR successfully", func() {
			util.AssertWorkCondition(work.Namespace, work.Name, hubWorkClient, string(workapiv1.WorkApplied), metav1.ConditionTrue,
				[]metav1.ConditionStatus{metav1.ConditionTrue, metav1.ConditionTrue}, eventuallyTimeout, eventuallyInterval)
			util.AssertWorkCondition(work.Namespace, work.Name, hubWorkClient, string(workapiv1.WorkAvailable), metav1.ConditionTrue,
				[]metav1.ConditionStatus{metav1.ConditionTrue, metav1.ConditionTrue}, eventuallyTimeout, eventuallyInterval)

			var namespaces, names []string
			for _, obj := range objects {
				namespaces = append(namespaces, obj.GetNamespace())
				names = append(names, obj.GetName())
			}

			util.AssertExistenceOfResources(gvrs, namespaces, names, spokeDynamicClient, eventuallyTimeout, eventuallyInterval)
			util.AssertAppliedResources(hubHash, work.Name, gvrs, namespaces, names, hubWorkClient, eventuallyTimeout, eventuallyInterval)
		})

		ginkgo.It("should merge annotation of existing CR", func() {
			util.AssertWorkCondition(work.Namespace, work.Name, hubWorkClient, string(workapiv1.WorkApplied), metav1.ConditionTrue,
				[]metav1.ConditionStatus{metav1.ConditionTrue, metav1.ConditionTrue}, eventuallyTimeout, eventuallyInterval)
			util.AssertWorkCondition(work.Namespace, work.Name, hubWorkClient, string(workapiv1.WorkAvailable), metav1.ConditionTrue,
				[]metav1.ConditionStatus{metav1.ConditionTrue, metav1.ConditionTrue}, eventuallyTimeout, eventuallyInterval)

			var namespaces, names []string
			for _, obj := range objects {
				namespaces = append(namespaces, obj.GetNamespace())
				names = append(names, obj.GetName())
			}

			util.AssertExistenceOfResources(gvrs, namespaces, names, spokeDynamicClient, eventuallyTimeout, eventuallyInterval)
			util.AssertAppliedResources(hubHash, work.Name, gvrs, namespaces, names, hubWorkClient, eventuallyTimeout, eventuallyInterval)

			// update object label
			obj, gvr, err := util.GuestbookCr(o.SpokeClusterName, "guestbook1")
			gomega.Expect(err).ToNot(gomega.HaveOccurred())

			cr, err := util.GetResource(obj.GetNamespace(), obj.GetName(), gvr, spokeDynamicClient)
			gomega.Expect(err).ToNot(gomega.HaveOccurred())

			cr.SetAnnotations(map[string]string{"foo": "bar"})
			_, err = spokeDynamicClient.Resource(gvr).Namespace(obj.GetNamespace()).Update(context.TODO(), cr, metav1.UpdateOptions{})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())

			// Update manifestwork
			obj.SetAnnotations(map[string]string{"foo1": "bar1"})
			updatework, err := hubWorkClient.WorkV1().ManifestWorks(work.Namespace).Get(context.TODO(), work.Name, metav1.GetOptions{})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())
			updatework.Spec.Workload.Manifests[1] = util.ToManifest(obj)
			_, err = hubWorkClient.WorkV1().ManifestWorks(work.Namespace).Update(context.TODO(), updatework, metav1.UpdateOptions{})
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
			util.AssertWorkCondition(work.Namespace, work.Name, hubWorkClient, string(workapiv1.WorkApplied), metav1.ConditionTrue,
				[]metav1.ConditionStatus{metav1.ConditionTrue, metav1.ConditionTrue}, eventuallyTimeout, eventuallyInterval)
			util.AssertWorkCondition(work.Namespace, work.Name, hubWorkClient, string(workapiv1.WorkAvailable), metav1.ConditionTrue,
				[]metav1.ConditionStatus{metav1.ConditionTrue, metav1.ConditionTrue}, eventuallyTimeout, eventuallyInterval)

			var namespaces, names []string
			for _, obj := range objects {
				namespaces = append(namespaces, obj.GetNamespace())
				names = append(names, obj.GetName())
			}

			util.AssertExistenceOfResources(gvrs, namespaces, names, spokeDynamicClient, eventuallyTimeout, eventuallyInterval)
			util.AssertAppliedResources(hubHash, work.Name, gvrs, namespaces, names, hubWorkClient, eventuallyTimeout, eventuallyInterval)

			// update object finalizer
			obj, gvr, err := util.GuestbookCr(o.SpokeClusterName, "guestbook1")
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
			updatework, err := hubWorkClient.WorkV1().ManifestWorks(work.Namespace).Get(context.TODO(), work.Name, metav1.GetOptions{})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())
			updatework.Spec.Workload.Manifests[1] = util.ToManifest(obj)
			_, err = hubWorkClient.WorkV1().ManifestWorks(work.Namespace).Update(context.TODO(), updatework, metav1.UpdateOptions{})
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
			util.AssertWorkCondition(work.Namespace, work.Name, hubWorkClient, string(workapiv1.WorkApplied), metav1.ConditionTrue,
				[]metav1.ConditionStatus{metav1.ConditionTrue, metav1.ConditionTrue}, eventuallyTimeout, eventuallyInterval)
			util.AssertWorkCondition(work.Namespace, work.Name, hubWorkClient, string(workapiv1.WorkAvailable), metav1.ConditionTrue,
				[]metav1.ConditionStatus{metav1.ConditionTrue, metav1.ConditionTrue}, eventuallyTimeout, eventuallyInterval)

			var namespaces, names []string
			for _, obj := range objects {
				namespaces = append(namespaces, obj.GetNamespace())
				names = append(names, obj.GetName())
			}

			util.AssertExistenceOfResources(gvrs, namespaces, names, spokeDynamicClient, eventuallyTimeout, eventuallyInterval)
			util.AssertAppliedResources(hubHash, work.Name, gvrs, namespaces, names, hubWorkClient, eventuallyTimeout, eventuallyInterval)

			// delete manifest work
			err = hubWorkClient.WorkV1().ManifestWorks(o.SpokeClusterName).Delete(context.Background(), work.Name, metav1.DeleteOptions{})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())

			// wait for deletion of manifest work
			gomega.Eventually(func() bool {
				_, err := hubWorkClient.WorkV1().ManifestWorks(o.SpokeClusterName).Get(context.Background(), work.Name, metav1.GetOptions{})
				return errors.IsNotFound(err)
			}, eventuallyTimeout, eventuallyInterval).Should(gomega.BeTrue())

			// Once manifest work is deleted, all CRs/CRD should have already been deleted too
			for i := range gvrs {
				_, err := util.GetResource(namespaces[i], names[i], gvrs[i], spokeDynamicClient)
				gomega.Expect(errors.IsNotFound(err)).To(gomega.BeTrue())
			}
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

			u, gvr := util.NewServiceAccount(o.SpokeClusterName, "sa")
			gvrs = append(gvrs, gvr)
			objects = append(objects, u)

			u, gvr = util.NewRole(o.SpokeClusterName, "role1")
			gvrs = append(gvrs, gvr)
			objects = append(objects, u)

			u, gvr = util.NewRoleBinding(o.SpokeClusterName, "rolebinding1", "sa", "role1")
			gvrs = append(gvrs, gvr)
			objects = append(objects, u)

			u, gvr, err = util.NewDeployment(o.SpokeClusterName, "deploy1", "sa")
			gomega.Expect(err).ToNot(gomega.HaveOccurred())
			gvrs = append(gvrs, gvr)
			objects = append(objects, u)

			for _, obj := range objects {
				manifests = append(manifests, util.ToManifest(obj))
			}
		})

		ginkgo.It("should create Service Account, Role, RoleBinding and Deployment successfully", func() {
			util.AssertWorkCondition(work.Namespace, work.Name, hubWorkClient, string(workapiv1.WorkApplied), metav1.ConditionTrue,
				[]metav1.ConditionStatus{metav1.ConditionTrue, metav1.ConditionTrue, metav1.ConditionTrue, metav1.ConditionTrue},
				eventuallyTimeout, eventuallyInterval)
			util.AssertWorkCondition(work.Namespace, work.Name, hubWorkClient, string(workapiv1.WorkAvailable), metav1.ConditionTrue,
				[]metav1.ConditionStatus{metav1.ConditionTrue, metav1.ConditionTrue, metav1.ConditionTrue, metav1.ConditionTrue},
				eventuallyTimeout, eventuallyInterval)

			var namespaces, names []string
			for _, obj := range objects {
				namespaces = append(namespaces, obj.GetNamespace())
				names = append(names, obj.GetName())
			}

			util.AssertExistenceOfResources(gvrs, namespaces, names, spokeDynamicClient, eventuallyTimeout, eventuallyInterval)
			util.AssertAppliedResources(hubHash, work.Name, gvrs, namespaces, names, hubWorkClient, eventuallyTimeout, eventuallyInterval)
		})

		ginkgo.It("should update Service Account and Deployment successfully", func() {
			ginkgo.By("check condition status in work status")
			util.AssertWorkCondition(work.Namespace, work.Name, hubWorkClient, string(workapiv1.WorkApplied), metav1.ConditionTrue,
				[]metav1.ConditionStatus{metav1.ConditionTrue, metav1.ConditionTrue, metav1.ConditionTrue, metav1.ConditionTrue},
				eventuallyTimeout, eventuallyInterval)
			util.AssertWorkCondition(work.Namespace, work.Name, hubWorkClient, string(workapiv1.WorkAvailable), metav1.ConditionTrue,
				[]metav1.ConditionStatus{metav1.ConditionTrue, metav1.ConditionTrue, metav1.ConditionTrue, metav1.ConditionTrue},
				eventuallyTimeout, eventuallyInterval)

			util.AssertWorkGeneration(work.Namespace, work.Name, hubWorkClient, string(workapiv1.WorkApplied), eventuallyTimeout, eventuallyInterval)
			util.AssertWorkGeneration(work.Namespace, work.Name, hubWorkClient, string(workapiv1.WorkAvailable), eventuallyTimeout, eventuallyInterval)

			ginkgo.By("check existence of all maintained resources")
			var namespaces, names []string
			for _, obj := range objects {
				namespaces = append(namespaces, obj.GetNamespace())
				names = append(names, obj.GetName())
			}
			util.AssertExistenceOfResources(gvrs, namespaces, names, spokeDynamicClient, eventuallyTimeout, eventuallyInterval)

			ginkgo.By("check if applied resources in status are updated")
			util.AssertAppliedResources(hubHash, work.Name, gvrs, namespaces, names, hubWorkClient, eventuallyTimeout, eventuallyInterval)

			// update manifests in work: 1) swap service account and deployment; 2) rename service account; 3) update deployment
			ginkgo.By("update manifests in work")
			oldServiceAccount := objects[0]
			gvrs[0], gvrs[3] = gvrs[3], gvrs[0]
			u, _ := util.NewServiceAccount(o.SpokeClusterName, "admin")
			objects[3] = u
			u, _, err = util.NewDeployment(o.SpokeClusterName, "deploy1", "admin")
			gomega.Expect(err).ToNot(gomega.HaveOccurred())
			objects[0] = u

			newManifests := []workapiv1.Manifest{}
			for _, obj := range objects {
				newManifests = append(newManifests, util.ToManifest(obj))
			}

			// slow down to make the difference between LastTransitionTime and updateTime large enough for measurement
			time.Sleep(1 * time.Second)
			updateTime := metav1.Now()
			time.Sleep(1 * time.Second)

			work, err = hubWorkClient.WorkV1().ManifestWorks(o.SpokeClusterName).Get(context.Background(), work.Name, metav1.GetOptions{})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())
			work.Spec.Workload.Manifests = newManifests
			work, err = hubWorkClient.WorkV1().ManifestWorks(o.SpokeClusterName).Update(context.Background(), work, metav1.UpdateOptions{})
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
				u, err := util.GetResource(o.SpokeClusterName, objects[0].GetName(), gvrs[0], spokeDynamicClient)
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
				work, err = hubWorkClient.WorkV1().ManifestWorks(o.SpokeClusterName).Get(context.Background(), work.Name, metav1.GetOptions{})
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

			util.AssertWorkGeneration(work.Namespace, work.Name, hubWorkClient, string(workapiv1.WorkApplied), eventuallyTimeout, eventuallyInterval)
			util.AssertWorkGeneration(work.Namespace, work.Name, hubWorkClient, string(workapiv1.WorkAvailable), eventuallyTimeout, eventuallyInterval)

			ginkgo.By("check if applied resources in status are updated")
			util.AssertAppliedResources(hubHash, work.Name, gvrs, namespaces, names, hubWorkClient, eventuallyTimeout, eventuallyInterval)

			ginkgo.By("check if resources which are no longer maintained have been deleted")
			util.AssertNonexistenceOfResources([]schema.GroupVersionResource{gvrs[3]}, []string{oldServiceAccount.GetNamespace()}, []string{oldServiceAccount.GetName()}, spokeDynamicClient, eventuallyTimeout, eventuallyInterval)
		})
	})

	ginkgo.Context("Foreground deletion", func() {
		var finalizer = "cluster.open-cluster-management.io/testing"
		ginkgo.BeforeEach(func() {
			manifests = []workapiv1.Manifest{
				util.ToManifest(util.NewConfigmap(o.SpokeClusterName, "cm1", map[string]string{"a": "b"}, []string{finalizer})),
				util.ToManifest(util.NewConfigmap(o.SpokeClusterName, "cm2", map[string]string{"c": "d"}, []string{finalizer})),
				util.ToManifest(util.NewConfigmap(o.SpokeClusterName, "cm3", map[string]string{"e": "f"}, []string{finalizer})),
			}
		})

		ginkgo.It("should remove applied resource for stale manifest from list once the resource is gone", func() {
			util.AssertExistenceOfConfigMaps(manifests, spokeKubeClient, eventuallyTimeout, eventuallyInterval)

			util.AssertWorkCondition(work.Namespace, work.Name, hubWorkClient, string(workapiv1.WorkApplied), metav1.ConditionTrue,
				[]metav1.ConditionStatus{metav1.ConditionTrue, metav1.ConditionTrue, metav1.ConditionTrue}, eventuallyTimeout, eventuallyInterval)
			util.AssertWorkCondition(work.Namespace, work.Name, hubWorkClient, string(workapiv1.WorkAvailable), metav1.ConditionTrue,
				[]metav1.ConditionStatus{metav1.ConditionTrue, metav1.ConditionTrue, metav1.ConditionTrue}, eventuallyTimeout, eventuallyInterval)

			work, err = hubWorkClient.WorkV1().ManifestWorks(o.SpokeClusterName).Get(context.Background(), work.Name, metav1.GetOptions{})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())
			work.Spec.Workload.Manifests = manifests[1:]
			work, err = hubWorkClient.WorkV1().ManifestWorks(o.SpokeClusterName).Update(context.Background(), work, metav1.UpdateOptions{})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())

			util.AssertExistenceOfConfigMaps(manifests[1:], spokeKubeClient, eventuallyTimeout, eventuallyInterval)

			err := hubWorkClient.WorkV1().ManifestWorks(work.Namespace).Delete(context.Background(), work.Name, metav1.DeleteOptions{})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())

			// remove finalizer from the applied resources for stale manifest after 2 seconds
			go func() {
				time.Sleep(2 * time.Second)
				cm := manifests[0].Object.(*corev1.ConfigMap)
				cm, err := spokeKubeClient.CoreV1().ConfigMaps(cm.Namespace).Get(context.Background(), cm.Name, metav1.GetOptions{})
				if err == nil {
					cm.Finalizers = nil
					_, _ = spokeKubeClient.CoreV1().ConfigMaps(cm.Namespace).Update(context.Background(), cm, metav1.UpdateOptions{})
				}
			}()

			// check if resource created by stale manifest is deleted once it is removed from applied resource list
			gomega.Eventually(func() error {
				appliedManifestWork, err := spokeWorkClient.WorkV1().AppliedManifestWorks().Get(context.Background(), appliedManifestWorkName, metav1.GetOptions{})
				if err != nil {
					return err
				}

				for _, appliedResource := range appliedManifestWork.Status.AppliedResources {
					if appliedResource.Name == "cm1" {
						return fmt.Errorf("found resource cm1")
					}
				}

				return nil
			}, eventuallyTimeout, eventuallyInterval).ShouldNot(gomega.HaveOccurred())

			_, err = spokeKubeClient.CoreV1().ConfigMaps(o.SpokeClusterName).Get(context.Background(), "cm1", metav1.GetOptions{})
			gomega.Expect(errors.IsNotFound(err)).To(gomega.BeTrue())
		})

		ginkgo.It("should delete manifest work eventually after all applied resources are gone", func() {
			util.AssertExistenceOfConfigMaps(manifests, spokeKubeClient, eventuallyTimeout, eventuallyInterval)

			util.AssertWorkCondition(work.Namespace, work.Name, hubWorkClient, string(workapiv1.WorkApplied), metav1.ConditionTrue,
				[]metav1.ConditionStatus{metav1.ConditionTrue, metav1.ConditionTrue, metav1.ConditionTrue}, eventuallyTimeout, eventuallyInterval)
			util.AssertWorkCondition(work.Namespace, work.Name, hubWorkClient, string(workapiv1.WorkAvailable), metav1.ConditionTrue,
				[]metav1.ConditionStatus{metav1.ConditionTrue, metav1.ConditionTrue, metav1.ConditionTrue}, eventuallyTimeout, eventuallyInterval)

			err := hubWorkClient.WorkV1().ManifestWorks(work.Namespace).Delete(context.Background(), work.Name, metav1.DeleteOptions{})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())

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

			util.AssertWorkDeleted(work.Namespace, work.Name, hubHash, manifests, hubWorkClient, spokeKubeClient, eventuallyTimeout, eventuallyInterval)
		})

		ginkgo.It("should delete applied manifest work if it is orphan", func() {
			appliedManifestWork := &workapiv1.AppliedManifestWork{
				ObjectMeta: metav1.ObjectMeta{
					Name: fmt.Sprintf("%s-fakework", hubHash),
				},
				Spec: workapiv1.AppliedManifestWorkSpec{
					HubHash:          hubHash,
					ManifestWorkName: "fakework",
				},
			}
			_, err := spokeWorkClient.WorkV1().AppliedManifestWorks().Create(context.Background(), appliedManifestWork, metav1.CreateOptions{})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())

			util.AssertAppliedManifestWorkDeleted(appliedManifestWork.Name, spokeWorkClient, eventuallyTimeout, eventuallyInterval)
		})
	})
})
