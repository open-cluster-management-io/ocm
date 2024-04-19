package cloudevents

import (
	"context"
	"fmt"
	"time"

	"github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	workclientset "open-cluster-management.io/api/client/work/clientset/versioned"
	workapiv1 "open-cluster-management.io/api/work/v1"

	commonoptions "open-cluster-management.io/ocm/pkg/common/options"
	"open-cluster-management.io/ocm/pkg/work/spoke"
	"open-cluster-management.io/ocm/test/integration/util"
)

func runWorkTest(sourceInfoGetter sourceInfoGetter, clusterNameGetter clusterNameGetter) func() {
	return func() {
		var err error

		var cancel context.CancelFunc

		var sourceDriver string
		var sourceConfigPath string
		var sourceHash string
		var sourceClient workclientset.Interface

		var clusterName string

		var work *workapiv1.ManifestWork
		var manifests []workapiv1.Manifest

		ginkgo.BeforeEach(func() {
			sourceClient, sourceDriver, sourceConfigPath, sourceHash = sourceInfoGetter()
			gomega.Expect(sourceClient).ToNot(gomega.BeNil())

			var ctx context.Context
			ctx, cancel = context.WithCancel(context.Background())

			clusterName = clusterNameGetter()

			ns := &corev1.Namespace{}
			ns.Name = clusterName
			_, err := spokeKubeClient.CoreV1().Namespaces().Create(context.Background(), ns, metav1.CreateOptions{})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())

			o := spoke.NewWorkloadAgentOptions()
			o.StatusSyncInterval = 3 * time.Second
			o.AppliedManifestWorkEvictionGracePeriod = 5 * time.Second
			o.WorkloadSourceDriver = sourceDriver
			o.WorkloadSourceConfig = sourceConfigPath
			o.CloudEventsClientID = fmt.Sprintf("%s-work-agent", clusterName)
			o.CloudEventsClientCodecs = []string{"manifest", "manifestbundle"}

			commOptions := commonoptions.NewAgentOptions()
			commOptions.SpokeClusterName = clusterName

			go runWorkAgent(ctx, o, commOptions)

			// reset manifests
			manifests = nil
		})

		ginkgo.JustBeforeEach(func() {
			work = util.NewManifestWork(clusterName, "", manifests)
			work, err = sourceClient.WorkV1().ManifestWorks(clusterName).Create(context.Background(), work, metav1.CreateOptions{})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())
		})

		ginkgo.AfterEach(func() {
			err = sourceClient.WorkV1().ManifestWorks(clusterName).Delete(context.Background(), work.Name, metav1.DeleteOptions{})
			if !errors.IsNotFound(err) {
				gomega.Expect(err).ToNot(gomega.HaveOccurred())
			}

			gomega.Eventually(func() error {
				_, err := sourceClient.WorkV1().ManifestWorks(clusterName).Get(context.Background(), work.Name, metav1.GetOptions{})
				if errors.IsNotFound(err) {
					return nil
				}
				if err != nil {
					return err
				}
				return fmt.Errorf("work %s in namespace %s still exists", work.Name, clusterName)
			}, eventuallyTimeout, eventuallyInterval).Should(gomega.Succeed())

			err = spokeKubeClient.CoreV1().Namespaces().Delete(context.Background(), clusterName, metav1.DeleteOptions{})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())
			if cancel != nil {
				cancel()
			}
		})

		ginkgo.Context("With a single manifest", func() {
			ginkgo.BeforeEach(func() {
				manifests = []workapiv1.Manifest{
					util.ToManifest(util.NewConfigmap(clusterName, cm1, map[string]string{"a": "b"}, nil)),
				}
			})

			ginkgo.It("should create work and then apply it successfully", func() {
				assertWorkCreated(sourceClient, work, manifests)
			})

			ginkgo.It("should update work and then apply it successfully", func() {
				newManifests := []workapiv1.Manifest{
					util.ToManifest(util.NewConfigmap(clusterName, cm2, map[string]string{"x": "y"}, nil)),
				}
				assertWorkUpdated(sourceClient, clusterName, toAppliedManifestWorkName(sourceHash, work),
					work, manifests, newManifests, cm1)
			})

			ginkgo.It("should delete work successfully", func() {
				assertWorkDeleted(sourceClient, clusterName, toAppliedManifestWorkName(sourceHash, work),
					work, manifests)
			})
		})

		ginkgo.Context("With multiple manifests", func() {
			ginkgo.BeforeEach(func() {
				manifests = []workapiv1.Manifest{
					util.ToManifest(util.NewConfigmap(clusterName, cm1, map[string]string{"a": "b"}, nil)),
					util.ToManifest(util.NewConfigmap(clusterName, cm2, map[string]string{"c": "d"}, nil)),
				}
			})

			ginkgo.It("should create work and then apply it successfully", func() {
				assertWorkCreated(sourceClient, work, manifests)
			})

			ginkgo.It("should update work and then apply it successfully", func() {
				newManifests := []workapiv1.Manifest{
					util.ToManifest(util.NewConfigmap(clusterName, cm1, map[string]string{"a": "b"}, nil)),
					util.ToManifest(util.NewConfigmap(clusterName, cm3, map[string]string{"x": "y"}, nil)),
				}
				assertWorkUpdated(sourceClient, clusterName, toAppliedManifestWorkName(sourceHash, work),
					work, manifests, newManifests, cm2)
			})

			ginkgo.It("should delete work successfully", func() {
				assertWorkDeleted(sourceClient, clusterName, toAppliedManifestWorkName(sourceHash, work),
					work, manifests)
			})
		})
	}
}

func assertWorkCreated(sourceClient workclientset.Interface, work *workapiv1.ManifestWork, manifests []workapiv1.Manifest) {
	expectedStatus := []metav1.ConditionStatus{}
	for range manifests {
		expectedStatus = append(expectedStatus, metav1.ConditionTrue)
	}
	util.AssertExistenceOfConfigMaps(manifests, spokeKubeClient, eventuallyTimeout, eventuallyInterval)
	util.AssertWorkCondition(work.Namespace, work.Name, sourceClient, workapiv1.WorkApplied,
		metav1.ConditionTrue, expectedStatus, eventuallyTimeout, eventuallyInterval)
	util.AssertWorkCondition(work.Namespace, work.Name, sourceClient, workapiv1.WorkAvailable,
		metav1.ConditionTrue, expectedStatus, eventuallyTimeout, eventuallyInterval)
}

func assertWorkUpdated(sourceClient workclientset.Interface, clusterName, appliedManifestWorkName string,
	work *workapiv1.ManifestWork, manifests, newManifests []workapiv1.Manifest, removedCM string) {
	assertWorkCreated(sourceClient, work, manifests)
	work, err := sourceClient.WorkV1().ManifestWorks(clusterName).Get(context.Background(), work.Name, metav1.GetOptions{})
	gomega.Expect(err).ToNot(gomega.HaveOccurred())

	work.Spec.Workload.Manifests = newManifests

	_, err = sourceClient.WorkV1().ManifestWorks(clusterName).Update(context.Background(), work, metav1.UpdateOptions{})
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
			if appliedResource.Name == removedCM {
				return fmt.Errorf("found applied resource %s", removedCM)
			}
		}

		return nil
	}, eventuallyTimeout, eventuallyInterval).ShouldNot(gomega.HaveOccurred())

	_, err = spokeKubeClient.CoreV1().ConfigMaps(clusterName).Get(context.Background(), removedCM, metav1.GetOptions{})
	gomega.Expect(errors.IsNotFound(err)).To(gomega.BeTrue())
}

func assertWorkDeleted(sourceClient workclientset.Interface, clusterName, appliedManifestWorkName string,
	work *workapiv1.ManifestWork, manifests []workapiv1.Manifest) {
	util.AssertFinalizerAdded(work.Namespace, work.Name, sourceClient, eventuallyTimeout, eventuallyInterval)

	err := sourceClient.WorkV1().ManifestWorks(clusterName).Delete(context.Background(), work.Name, metav1.DeleteOptions{})
	gomega.Expect(err).ToNot(gomega.HaveOccurred())

	util.AssertWorkDeleted(work.Namespace, work.Name, appliedManifestWorkName, manifests,
		sourceClient, spokeWorkClient, spokeKubeClient, eventuallyTimeout, eventuallyInterval)
}
