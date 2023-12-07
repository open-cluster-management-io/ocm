package cloudevents

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
	utilrand "k8s.io/apimachinery/pkg/util/rand"

	workapiv1 "open-cluster-management.io/api/work/v1"

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

	var work *workapiv1.ManifestWork
	var manifests []workapiv1.Manifest
	var appliedManifestWorkName string

	var err error

	ginkgo.BeforeEach(func() {
		o = spoke.NewWorkloadAgentOptions()
		o.StatusSyncInterval = 3 * time.Second
		o.AppliedManifestWorkEvictionGracePeriod = 5 * time.Second
		o.WorkloadSourceDriver.Type = workSourceDriver
		o.WorkloadSourceDriver.Config = workSourceConfigFileName

		commOptions = commonoptions.NewAgentOptions()
		commOptions.SpokeClusterName = utilrand.String(5)

		ns := &corev1.Namespace{}
		ns.Name = commOptions.SpokeClusterName
		_, err := spokeKubeClient.CoreV1().Namespaces().Create(context.Background(), ns, metav1.CreateOptions{})
		gomega.Expect(err).ToNot(gomega.HaveOccurred())

		var ctx context.Context
		ctx, cancel = context.WithCancel(context.Background())
		go startWorkAgent(ctx, o, commOptions)

		// reset manifests
		manifests = nil
	})

	ginkgo.JustBeforeEach(func() {
		work = util.NewManifestWork(commOptions.SpokeClusterName, "", manifests)
		work, err = workSourceWorkClient.WorkV1().ManifestWorks(commOptions.SpokeClusterName).Create(context.Background(), work, metav1.CreateOptions{})
		// if the source is not kube, the uid will be used as the manifestwork name
		appliedManifestWorkName = fmt.Sprintf("%s-%s", workSourceHash, work.UID)
		gomega.Expect(err).ToNot(gomega.HaveOccurred())
	})

	ginkgo.AfterEach(func() {
		err = workSourceWorkClient.WorkV1().ManifestWorks(commOptions.SpokeClusterName).Delete(context.Background(), work.Name, metav1.DeleteOptions{})
		if !errors.IsNotFound(err) {
			gomega.Expect(err).ToNot(gomega.HaveOccurred())
		}

		gomega.Eventually(func() error {
			_, err := workSourceWorkClient.WorkV1().ManifestWorks(commOptions.SpokeClusterName).Get(context.Background(), work.Name, metav1.GetOptions{})
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

			util.AssertWorkCondition(work.Namespace, work.Name, workSourceWorkClient, workapiv1.WorkApplied, metav1.ConditionTrue,
				[]metav1.ConditionStatus{metav1.ConditionTrue}, eventuallyTimeout, eventuallyInterval)
			util.AssertWorkCondition(work.Namespace, work.Name, workSourceWorkClient, workapiv1.WorkAvailable, metav1.ConditionTrue,
				[]metav1.ConditionStatus{metav1.ConditionTrue}, eventuallyTimeout, eventuallyInterval)
		})

		ginkgo.It("should update work and then apply it successfully", func() {
			util.AssertWorkCondition(work.Namespace, work.Name, workSourceWorkClient, workapiv1.WorkApplied, metav1.ConditionTrue,
				[]metav1.ConditionStatus{metav1.ConditionTrue}, eventuallyTimeout, eventuallyInterval)
			util.AssertWorkCondition(work.Namespace, work.Name, workSourceWorkClient, workapiv1.WorkAvailable, metav1.ConditionTrue,
				[]metav1.ConditionStatus{metav1.ConditionTrue}, eventuallyTimeout, eventuallyInterval)

			newManifests := []workapiv1.Manifest{
				util.ToManifest(util.NewConfigmap(commOptions.SpokeClusterName, cm2, map[string]string{"x": "y"}, nil)),
			}
			work, err = workSourceWorkClient.WorkV1().ManifestWorks(commOptions.SpokeClusterName).Get(context.Background(), work.Name, metav1.GetOptions{})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())
			work.Spec.Workload.Manifests = newManifests

			work, err = workSourceWorkClient.WorkV1().ManifestWorks(commOptions.SpokeClusterName).Update(context.Background(), work, metav1.UpdateOptions{})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())

			util.AssertExistenceOfConfigMaps(newManifests, spokeKubeClient, eventuallyTimeout, eventuallyInterval)

			// check if resource created by stale manifest is deleted once it is removed from applied resource list
			gomega.Eventually(func() error {
				appliedManifestWork, err := spokeWorkClient.WorkV1().AppliedManifestWorks().Get(context.Background(), appliedManifestWorkName, metav1.GetOptions{})
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
			util.AssertFinalizerAdded(work.Namespace, work.Name, workSourceWorkClient, eventuallyTimeout, eventuallyInterval)

			err = workSourceWorkClient.WorkV1().ManifestWorks(commOptions.SpokeClusterName).Delete(context.Background(), work.Name, metav1.DeleteOptions{})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())

			util.AssertWorkDeleted(work.Namespace, work.Name, fmt.Sprintf("%s-%s", workSourceHash, work.UID), manifests,
				workSourceWorkClient, spokeWorkClient, spokeKubeClient, eventuallyTimeout, eventuallyInterval)
		})
	})

	// TODO test multiple manifests after the manifestbundles is enabled
})
