package integration

import (
	"context"
	"fmt"
	"time"

	"github.com/onsi/ginkgo"
	"github.com/onsi/gomega"

	"github.com/openshift/library-go/pkg/controller/controllercmd"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	utilrand "k8s.io/apimachinery/pkg/util/rand"
	"k8s.io/client-go/dynamic"

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
			gomega.Eventually(func() bool {
				appliedManifestWork, err := hubWorkClient.WorkV1().AppliedManifestWorks().Get(context.Background(), appliedManifestWorkName, metav1.GetOptions{})
				if err != nil {
					return false
				}

				for _, appliedResource := range appliedManifestWork.Status.AppliedResources {
					if appliedResource.Name == "cm1" {
						return false
					}
				}

				return true
			}, eventuallyTimeout, eventuallyInterval).Should(gomega.BeTrue())

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
			gomega.Eventually(func() bool {
				appliedManifestWork, err := spokeWorkClient.WorkV1().AppliedManifestWorks().Get(context.Background(), appliedManifestWorkName, metav1.GetOptions{})
				if err != nil {
					return false
				}

				for _, appliedResource := range appliedManifestWork.Status.AppliedResources {
					if appliedResource.Name == "cm3" {
						return false
					}
				}

				return true
			}, eventuallyTimeout, eventuallyInterval).Should(gomega.BeTrue())

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
				if !errors.IsNotFound(err) {
					return false
				}

				return true
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
			gomega.Eventually(func() bool {
				u, err := util.GetResource(o.SpokeClusterName, objects[0].GetName(), gvrs[0], spokeDynamicClient)
				if err != nil {
					return false
				}

				sa, _, _ := unstructured.NestedString(u.Object, "spec", "template", "spec", "serviceAccountName")
				if "admin" != sa {
					return false
				}

				return true
			}, eventuallyTimeout, eventuallyInterval).Should(gomega.BeTrue())

			ginkgo.By("check if LastTransitionTime is updated")
			gomega.Eventually(func() bool {
				work, err = hubWorkClient.WorkV1().ManifestWorks(o.SpokeClusterName).Get(context.Background(), work.Name, metav1.GetOptions{})
				if err != nil {
					return false
				}
				if len(work.Status.ResourceStatus.Manifests) != len(objects) {
					return false
				}

				for i := range work.Status.ResourceStatus.Manifests {
					if len(work.Status.ResourceStatus.Manifests[i].Conditions) != 3 {
						return false
					}
				}

				// the LastTransitionTime of deployment should not change because of resouce update
				if updateTime.Before(&work.Status.ResourceStatus.Manifests[0].Conditions[0].LastTransitionTime) {
					return false
				}

				// the LastTransitionTime of service account changes because of resource re-creation
				if work.Status.ResourceStatus.Manifests[3].Conditions[0].LastTransitionTime.Before(&updateTime) {
					return false
				}

				return true
			}, eventuallyTimeout, eventuallyInterval).Should(gomega.BeTrue())

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
			gomega.Eventually(func() bool {
				appliedManifestWork, err := spokeWorkClient.WorkV1().AppliedManifestWorks().Get(context.Background(), appliedManifestWorkName, metav1.GetOptions{})
				if err != nil {
					return false
				}

				for _, appliedResource := range appliedManifestWork.Status.AppliedResources {
					if appliedResource.Name == "cm1" {
						return false
					}
				}

				return true
			}, eventuallyTimeout, eventuallyInterval).Should(gomega.BeTrue())

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
					cm, err := spokeKubeClient.CoreV1().ConfigMaps(cm.Namespace).Get(context.Background(), cm.Name, metav1.GetOptions{})
					if err == nil {
						cm.Finalizers = nil
						_, _ = spokeKubeClient.CoreV1().ConfigMaps(cm.Namespace).Update(context.Background(), cm, metav1.UpdateOptions{})
					}
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

	ginkgo.Context("Resource sharing and adoption between manifestworks", func() {
		var anotherWork *workapiv1.ManifestWork
		var anotherAppliedManifestWorkName string
		ginkgo.BeforeEach(func() {
			manifests = []workapiv1.Manifest{
				util.ToManifest(util.NewConfigmap(o.SpokeClusterName, "cm1", map[string]string{"a": "b"}, []string{})),
				util.ToManifest(util.NewConfigmap(o.SpokeClusterName, "cm2", map[string]string{"c": "d"}, []string{})),
			}
		})

		ginkgo.JustBeforeEach(func() {
			util.AssertWorkCondition(work.Namespace, work.Name, hubWorkClient, string(workapiv1.WorkApplied), metav1.ConditionTrue,
				[]metav1.ConditionStatus{metav1.ConditionTrue, metav1.ConditionTrue}, eventuallyTimeout, eventuallyInterval)
			util.AssertWorkCondition(work.Namespace, work.Name, hubWorkClient, string(workapiv1.WorkAvailable), metav1.ConditionTrue,
				[]metav1.ConditionStatus{metav1.ConditionTrue, metav1.ConditionTrue}, eventuallyTimeout, eventuallyInterval)

			// Create another manifestworks with one shared resource.
			anotherWork = util.NewManifestWork(o.SpokeClusterName, "sharing-resource-work", []workapiv1.Manifest{manifests[0]})
			anotherWork, err = hubWorkClient.WorkV1().ManifestWorks(o.SpokeClusterName).Create(context.Background(), anotherWork, metav1.CreateOptions{})
			anotherAppliedManifestWorkName = fmt.Sprintf("%s-%s", hubHash, anotherWork.Name)
			gomega.Expect(err).ToNot(gomega.HaveOccurred())
		})

		ginkgo.It("shared resource between the manifestwork should be kept when one manifestwork is deleted", func() {
			// Ensure two manifestworks are all applied
			util.AssertWorkCondition(anotherWork.Namespace, anotherWork.Name, hubWorkClient, string(workapiv1.WorkApplied), metav1.ConditionTrue,
				[]metav1.ConditionStatus{metav1.ConditionTrue}, eventuallyTimeout, eventuallyInterval)
			util.AssertWorkCondition(anotherWork.Namespace, anotherWork.Name, hubWorkClient, string(workapiv1.WorkAvailable), metav1.ConditionTrue,
				[]metav1.ConditionStatus{metav1.ConditionTrue}, eventuallyTimeout, eventuallyInterval)

			// ensure configmap exists and get its uid
			util.AssertExistenceOfConfigMaps(manifests, spokeKubeClient, eventuallyTimeout, eventuallyInterval)
			curentConfigMap, err := spokeKubeClient.CoreV1().ConfigMaps(o.SpokeClusterName).Get(context.Background(), "cm1", metav1.GetOptions{})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())
			currentUID := curentConfigMap.UID

			// Ensure that uid recorded in the appliedmanifestwork and anotherappliedmanifestwork is correct.
			gomega.Eventually(func() error {
				appliedManifestWork, err := spokeWorkClient.WorkV1().AppliedManifestWorks().Get(context.Background(), appliedManifestWorkName, metav1.GetOptions{})
				if err != nil {
					return err
				}

				for _, appliedResource := range appliedManifestWork.Status.AppliedResources {
					if appliedResource.Name == "cm1" && appliedResource.UID == string(currentUID) {
						return nil
					}
				}

				return fmt.Errorf("Resource name or uid in appliedmanifestwork does not match")
			}, eventuallyTimeout, eventuallyInterval).ShouldNot(gomega.HaveOccurred())

			gomega.Eventually(func() error {
				anotherAppliedManifestWork, err := spokeWorkClient.WorkV1().AppliedManifestWorks().Get(context.Background(), anotherAppliedManifestWorkName, metav1.GetOptions{})
				if err != nil {
					return err
				}

				for _, appliedResource := range anotherAppliedManifestWork.Status.AppliedResources {
					if appliedResource.Name == "cm1" && appliedResource.UID == string(currentUID) {
						return nil
					}
				}

				return fmt.Errorf("Resource name or uid in appliedmanifestwork does not match")
			}, eventuallyTimeout, eventuallyInterval).ShouldNot(gomega.HaveOccurred())

			// Delete one manifestwork
			err = hubWorkClient.WorkV1().ManifestWorks(o.SpokeClusterName).Delete(context.Background(), work.Name, metav1.DeleteOptions{})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())

			// Ensure the appliedmanifestwork of deleted manifestwork is removed so it won't try to delete shared resource
			gomega.Eventually(func() bool {
				_, err := spokeWorkClient.WorkV1().AppliedManifestWorks().Get(context.Background(), appliedManifestWorkName, metav1.GetOptions{})
				if errors.IsNotFound(err) {
					return true
				}
				return false
			}, eventuallyTimeout, eventuallyInterval).Should(gomega.BeTrue())

			// Ensure the configmap is kept and tracked by anotherappliedmanifestwork.
			gomega.Eventually(func() error {
				configMap, err := spokeKubeClient.CoreV1().ConfigMaps(o.SpokeClusterName).Get(context.Background(), "cm1", metav1.GetOptions{})
				if err != nil {
					return err
				}

				if currentUID != configMap.UID {
					return fmt.Errorf("UID should be equal")
				}

				anotherappliedmanifestwork, err := spokeWorkClient.WorkV1().AppliedManifestWorks().Get(context.Background(), anotherAppliedManifestWorkName, metav1.GetOptions{})
				if err != nil {
					return err
				}

				for _, appliedResource := range anotherappliedmanifestwork.Status.AppliedResources {
					if appliedResource.Name != "cm1" {
						return fmt.Errorf("Resource Name should be cm1")
					}

					if appliedResource.UID != string(currentUID) {
						return fmt.Errorf("UID should be equal")
					}
				}

				return nil
			}, eventuallyTimeout, eventuallyInterval).ShouldNot(gomega.HaveOccurred())
		})

		ginkgo.It("shared resource between the manifestwork should be kept when the shared resource is removed from one manifestwork", func() {
			util.AssertWorkCondition(anotherWork.Namespace, anotherWork.Name, hubWorkClient, string(workapiv1.WorkApplied), metav1.ConditionTrue,
				[]metav1.ConditionStatus{metav1.ConditionTrue}, eventuallyTimeout, eventuallyInterval)
			util.AssertWorkCondition(anotherWork.Namespace, anotherWork.Name, hubWorkClient, string(workapiv1.WorkAvailable), metav1.ConditionTrue,
				[]metav1.ConditionStatus{metav1.ConditionTrue}, eventuallyTimeout, eventuallyInterval)

			// ensure configmap exists and get its uid
			util.AssertExistenceOfConfigMaps(manifests, spokeKubeClient, eventuallyTimeout, eventuallyInterval)
			curentConfigMap, err := spokeKubeClient.CoreV1().ConfigMaps(o.SpokeClusterName).Get(context.Background(), "cm1", metav1.GetOptions{})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())
			currentUID := curentConfigMap.UID

			// Ensure that uid recorded in the appliedmanifestwork and anotherappliedmanifestwork is correct.
			gomega.Eventually(func() error {
				appliedManifestWork, err := spokeWorkClient.WorkV1().AppliedManifestWorks().Get(context.Background(), appliedManifestWorkName, metav1.GetOptions{})
				if err != nil {
					return err
				}

				for _, appliedResource := range appliedManifestWork.Status.AppliedResources {
					if appliedResource.Name == "cm1" && appliedResource.UID == string(currentUID) {
						return nil
					}
				}

				return fmt.Errorf("Resource name or uid in appliedmanifestwork does not match")
			}, eventuallyTimeout, eventuallyInterval).ShouldNot(gomega.HaveOccurred())

			gomega.Eventually(func() error {
				anotherAppliedManifestWork, err := spokeWorkClient.WorkV1().AppliedManifestWorks().Get(context.Background(), anotherAppliedManifestWorkName, metav1.GetOptions{})
				if err != nil {
					return err
				}

				for _, appliedResource := range anotherAppliedManifestWork.Status.AppliedResources {
					if appliedResource.Name == "cm1" && appliedResource.UID == string(currentUID) {
						return nil
					}
				}

				return fmt.Errorf("Resource name or uid in appliedmanifestwork does not match")
			}, eventuallyTimeout, eventuallyInterval).ShouldNot(gomega.HaveOccurred())

			// Update one manifestwork to remove the shared resource
			work, err = hubWorkClient.WorkV1().ManifestWorks(o.SpokeClusterName).Get(context.Background(), work.Name, metav1.GetOptions{})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())
			work.Spec.Workload.Manifests = []workapiv1.Manifest{manifests[1]}
			_, err = hubWorkClient.WorkV1().ManifestWorks(o.SpokeClusterName).Update(context.Background(), work, metav1.UpdateOptions{})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())

			// Ensure the resource is not tracked by the appliedmanifestwork.
			gomega.Eventually(func() bool {
				appliedManifestWork, err := spokeWorkClient.WorkV1().AppliedManifestWorks().Get(context.Background(), appliedManifestWorkName, metav1.GetOptions{})
				if err != nil {
					return false
				}

				for _, appliedResource := range appliedManifestWork.Status.AppliedResources {
					if appliedResource.Name == "cm1" {
						return false
					}
				}

				return true
			}, eventuallyTimeout, eventuallyInterval).Should(gomega.BeTrue())

			// Ensure the configmap is kept and tracked by anotherappliedmanifestwork
			gomega.Eventually(func() error {
				configMap, err := spokeKubeClient.CoreV1().ConfigMaps(o.SpokeClusterName).Get(context.Background(), "cm1", metav1.GetOptions{})
				if err != nil {
					return err
				}

				if currentUID != configMap.UID {
					return fmt.Errorf("UID should be equal")
				}

				anotherAppliedManifestWork, err := spokeWorkClient.WorkV1().AppliedManifestWorks().Get(context.Background(), anotherAppliedManifestWorkName, metav1.GetOptions{})
				if err != nil {
					return err
				}

				for _, appliedResource := range anotherAppliedManifestWork.Status.AppliedResources {
					if appliedResource.Name != "cm1" {
						return fmt.Errorf("Resource Name should be cm1")
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
				util.ToManifest(util.NewConfigmap(o.SpokeClusterName, "cm1", map[string]string{"a": "b"}, []string{})),
				util.ToManifest(util.NewConfigmap(o.SpokeClusterName, "cm2", map[string]string{"c": "d"}, []string{})),
			}
		})

		ginkgo.JustBeforeEach(func() {
			util.AssertWorkCondition(work.Namespace, work.Name, hubWorkClient, string(workapiv1.WorkApplied), metav1.ConditionTrue,
				[]metav1.ConditionStatus{metav1.ConditionTrue, metav1.ConditionTrue}, eventuallyTimeout, eventuallyInterval)
			util.AssertWorkCondition(work.Namespace, work.Name, hubWorkClient, string(workapiv1.WorkAvailable), metav1.ConditionTrue,
				[]metav1.ConditionStatus{metav1.ConditionTrue, metav1.ConditionTrue}, eventuallyTimeout, eventuallyInterval)
		})

		ginkgo.It("Orphan deletion of the whole manifestwork", func() {
			work, err = hubWorkClient.WorkV1().ManifestWorks(o.SpokeClusterName).Get(context.Background(), work.Name, metav1.GetOptions{})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())

			work.Spec.DeleteOption = &workapiv1.DeleteOption{
				PropagationPolicy: workapiv1.DeletePropagationPolicyTypeOrphan,
			}

			work, err = hubWorkClient.WorkV1().ManifestWorks(o.SpokeClusterName).Update(context.Background(), work, metav1.UpdateOptions{})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())

			// Ensure configmap exists
			util.AssertExistenceOfConfigMaps(manifests, spokeKubeClient, eventuallyTimeout, eventuallyInterval)

			// Ensure ownership of configmap is updated
			gomega.Eventually(func() error {
				cm, err := spokeKubeClient.CoreV1().ConfigMaps(o.SpokeClusterName).Get(context.Background(), "cm1", metav1.GetOptions{})
				if err != nil {
					return err
				}

				if len(cm.OwnerReferences) != 0 {
					return fmt.Errorf("Owner reference are not correctly updated, current ownerrefs are %v", cm.OwnerReferences)
				}

				return nil
			}, eventuallyTimeout, eventuallyInterval).ShouldNot(gomega.HaveOccurred())

			gomega.Eventually(func() error {
				cm, err := spokeKubeClient.CoreV1().ConfigMaps(o.SpokeClusterName).Get(context.Background(), "cm2", metav1.GetOptions{})
				if err != nil {
					return err
				}

				if len(cm.OwnerReferences) != 0 {
					return fmt.Errorf("Owner reference are not correctly updated, current ownerrefs are %v", cm.OwnerReferences)
				}

				return nil
			}, eventuallyTimeout, eventuallyInterval).ShouldNot(gomega.HaveOccurred())

			// Delete the work
			err = hubWorkClient.WorkV1().ManifestWorks(o.SpokeClusterName).Delete(context.Background(), work.Name, metav1.DeleteOptions{})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())

			// Wait for deletion of manifest work
			gomega.Eventually(func() bool {
				_, err := hubWorkClient.WorkV1().ManifestWorks(o.SpokeClusterName).Get(context.Background(), work.Name, metav1.GetOptions{})
				return errors.IsNotFound(err)
			}, eventuallyTimeout, eventuallyInterval).Should(gomega.BeTrue())

			// Ensure configmap exists
			util.AssertExistenceOfConfigMaps(manifests, spokeKubeClient, eventuallyTimeout, eventuallyInterval)
		})

		ginkgo.It("Selectively Orphan deletion of the manifestwork", func() {
			work, err = hubWorkClient.WorkV1().ManifestWorks(o.SpokeClusterName).Get(context.Background(), work.Name, metav1.GetOptions{})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())

			work.Spec.DeleteOption = &workapiv1.DeleteOption{
				PropagationPolicy: workapiv1.DeletePropagationPolicyTypeSelectivelyOrphan,
				SelectivelyOrphan: &workapiv1.SelectivelyOrphan{
					OrphaningRules: []workapiv1.OrphaningRule{
						{
							Group:     "",
							Resource:  "configmaps",
							Namespace: o.SpokeClusterName,
							Name:      "cm1",
						},
					},
				},
			}

			work, err = hubWorkClient.WorkV1().ManifestWorks(o.SpokeClusterName).Update(context.Background(), work, metav1.UpdateOptions{})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())

			// Ensure configmap exists
			util.AssertExistenceOfConfigMaps(manifests, spokeKubeClient, eventuallyTimeout, eventuallyInterval)

			// Ensure ownership of configmap is updated
			gomega.Eventually(func() error {
				cm, err := spokeKubeClient.CoreV1().ConfigMaps(o.SpokeClusterName).Get(context.Background(), "cm1", metav1.GetOptions{})
				if err != nil {
					return err
				}

				if len(cm.OwnerReferences) != 0 {
					return fmt.Errorf("Owner reference are not correctly updated, current ownerrefs are %v", cm.OwnerReferences)
				}

				return nil
			}, eventuallyTimeout, eventuallyInterval).ShouldNot(gomega.HaveOccurred())

			// Delete the work
			err = hubWorkClient.WorkV1().ManifestWorks(o.SpokeClusterName).Delete(context.Background(), work.Name, metav1.DeleteOptions{})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())

			// Wait for deletion of manifest work
			gomega.Eventually(func() bool {
				_, err := hubWorkClient.WorkV1().ManifestWorks(o.SpokeClusterName).Get(context.Background(), work.Name, metav1.GetOptions{})
				return errors.IsNotFound(err)
			}, eventuallyTimeout, eventuallyInterval).Should(gomega.BeTrue())

			// One of the resource should be deleted.
			_, err = spokeKubeClient.CoreV1().ConfigMaps(o.SpokeClusterName).Get(context.Background(), "cm2", metav1.GetOptions{})
			gomega.Expect(errors.IsNotFound(err)).To(gomega.BeTrue())

			// One of the resource should be kept
			_, err = spokeKubeClient.CoreV1().ConfigMaps(o.SpokeClusterName).Get(context.Background(), "cm1", metav1.GetOptions{})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())
		})

		ginkgo.It("Clean the resource when orphan deletion option is removed", func() {
			work, err = hubWorkClient.WorkV1().ManifestWorks(o.SpokeClusterName).Get(context.Background(), work.Name, metav1.GetOptions{})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())

			work.Spec.DeleteOption = &workapiv1.DeleteOption{
				PropagationPolicy: workapiv1.DeletePropagationPolicyTypeSelectivelyOrphan,
				SelectivelyOrphan: &workapiv1.SelectivelyOrphan{
					OrphaningRules: []workapiv1.OrphaningRule{
						{
							Group:     "",
							Resource:  "configmaps",
							Namespace: o.SpokeClusterName,
							Name:      "cm1",
						},
					},
				},
			}

			work, err = hubWorkClient.WorkV1().ManifestWorks(o.SpokeClusterName).Update(context.Background(), work, metav1.UpdateOptions{})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())

			// Ensure configmap exists
			util.AssertExistenceOfConfigMaps(manifests, spokeKubeClient, eventuallyTimeout, eventuallyInterval)

			// Ensure ownership of configmap is updated
			gomega.Eventually(func() error {
				cm, err := spokeKubeClient.CoreV1().ConfigMaps(o.SpokeClusterName).Get(context.Background(), "cm1", metav1.GetOptions{})
				if err != nil {
					return err
				}

				if len(cm.OwnerReferences) != 0 {
					return fmt.Errorf("Owner reference are not correctly updated, current ownerrefs are %v", cm.OwnerReferences)
				}

				return nil
			}, eventuallyTimeout, eventuallyInterval).ShouldNot(gomega.HaveOccurred())

			// Remove the delete option
			gomega.Eventually(func() error {
				work, err = hubWorkClient.WorkV1().ManifestWorks(o.SpokeClusterName).Get(context.Background(), work.Name, metav1.GetOptions{})
				if err != nil {
					return err
				}

				work.Spec.DeleteOption = nil
				_, err = hubWorkClient.WorkV1().ManifestWorks(o.SpokeClusterName).Update(context.Background(), work, metav1.UpdateOptions{})
				return err
			}, eventuallyTimeout, eventuallyInterval).ShouldNot(gomega.HaveOccurred())

			// Ensure ownership of configmap is updated
			gomega.Eventually(func() error {
				cm, err := spokeKubeClient.CoreV1().ConfigMaps(o.SpokeClusterName).Get(context.Background(), "cm1", metav1.GetOptions{})
				if err != nil {
					return err
				}

				if len(cm.OwnerReferences) != 1 {
					return fmt.Errorf("Owner reference are not correctly updated, current ownerrefs are %v", cm.OwnerReferences)
				}

				return nil
			}, eventuallyTimeout, eventuallyInterval).ShouldNot(gomega.HaveOccurred())

			// Delete the work
			err = hubWorkClient.WorkV1().ManifestWorks(o.SpokeClusterName).Delete(context.Background(), work.Name, metav1.DeleteOptions{})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())

			// Wait for deletion of manifest work
			gomega.Eventually(func() bool {
				_, err := hubWorkClient.WorkV1().ManifestWorks(o.SpokeClusterName).Get(context.Background(), work.Name, metav1.GetOptions{})
				return errors.IsNotFound(err)
			}, eventuallyTimeout, eventuallyInterval).Should(gomega.BeTrue())

			// All of the resource should be deleted.
			_, err = spokeKubeClient.CoreV1().ConfigMaps(o.SpokeClusterName).Get(context.Background(), "cm2", metav1.GetOptions{})
			gomega.Expect(errors.IsNotFound(err)).To(gomega.BeTrue())
			_, err = spokeKubeClient.CoreV1().ConfigMaps(o.SpokeClusterName).Get(context.Background(), "cm1", metav1.GetOptions{})
			gomega.Expect(errors.IsNotFound(err)).To(gomega.BeTrue())
		})
	})
})
