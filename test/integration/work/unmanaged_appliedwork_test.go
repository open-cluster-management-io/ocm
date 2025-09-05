package work

import (
	"context"
	"fmt"
	"os"
	"path"
	"time"

	"github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	utilrand "k8s.io/apimachinery/pkg/util/rand"
	"k8s.io/client-go/kubernetes"
	"sigs.k8s.io/controller-runtime/pkg/envtest"

	workclientset "open-cluster-management.io/api/client/work/clientset/versioned"
	workapiv1 "open-cluster-management.io/api/work/v1"

	commonoptions "open-cluster-management.io/ocm/pkg/common/options"
	"open-cluster-management.io/ocm/pkg/work/spoke"
	"open-cluster-management.io/ocm/test/integration/util"
)

var _ = ginkgo.Describe("Unmanaged ApplieManifestWork", func() {
	var cancel context.CancelFunc
	var work *workapiv1.ManifestWork
	var clusterName string
	var agentID string
	var manifests []workapiv1.Manifest
	var appliedManifestWorkName string
	var err error
	var ns *corev1.Namespace

	optionDecorator := func(agentID, sourceConfigFile string, gracePeriod time.Duration) agentOptionsDecorator {
		return func(opt *spoke.WorkloadAgentOptions, commonOpt *commonoptions.AgentOptions) (
			*spoke.WorkloadAgentOptions, *commonoptions.AgentOptions) {
			opt.AppliedManifestWorkEvictionGracePeriod = gracePeriod
			opt.WorkloadSourceConfig = sourceConfigFile
			commonOpt.AgentID = agentID
			return opt, commonOpt
		}
	}

	ginkgo.BeforeEach(func() {
		clusterName = utilrand.String(5)
		agentID = utilrand.String(5)

		ns = &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{Name: clusterName},
		}
		_, err := spokeKubeClient.CoreV1().Namespaces().Create(context.Background(), ns, metav1.CreateOptions{})
		gomega.Expect(err).ToNot(gomega.HaveOccurred())

		var ctx context.Context
		ctx, cancel = context.WithCancel(context.Background())
		go startWorkAgent(ctx, clusterName, optionDecorator(agentID, sourceConfigFileName, 10*time.Second))

		manifests = []workapiv1.Manifest{
			util.ToManifest(util.NewConfigmap(clusterName, cm1, map[string]string{"a": "b"}, nil)),
		}

		work = util.NewManifestWork(clusterName, "unmanaged-appliedwork", manifests)
		_, err = hubWorkClient.WorkV1().ManifestWorks(clusterName).Create(context.Background(), work, metav1.CreateOptions{})
		gomega.Expect(err).ToNot(gomega.HaveOccurred())

		appliedManifestWorkName = fmt.Sprintf("%s-%s", hubHash, work.Name)
	})

	ginkgo.AfterEach(func() {
		if cancel != nil {
			cancel()
		}
		err := spokeKubeClient.CoreV1().Namespaces().Delete(context.Background(), clusterName, metav1.DeleteOptions{})
		gomega.Expect(err).ToNot(gomega.HaveOccurred())
	})

	ginkgo.Context("Should delete unmanaged applied work when hub changed", func() {
		var newSourceConfigFile string
		var newKubeClient kubernetes.Interface
		var newWorkClient workclientset.Interface
		var newAgentID string
		var newHub *envtest.Environment
		var newHubTempDir string
		var grpcCtx context.Context
		var grpcCancel context.CancelFunc

		ginkgo.BeforeEach(func() {
			// start another hub
			newHub = &envtest.Environment{
				ErrorIfCRDPathMissing: true,
				CRDDirectoryPaths:     CRDPaths,
			}

			newAgentID = utilrand.String(5)

			newCfg, err := newHub.Start()
			gomega.Expect(err).ToNot(gomega.HaveOccurred())

			newHubTempDir, err = os.MkdirTemp("", "unmanaged_work_test")
			gomega.Expect(err).ToNot(gomega.HaveOccurred())

			gomega.Expect(sourceDriver).Should(gomega.Or(gomega.Equal("grpc"), gomega.Equal("kube")))

			grpcCtx, grpcCancel = context.WithCancel(context.Background())
			if sourceDriver == "grpc" {
				newSourceConfigFile, _ = startGRPCServer(grpcCtx, newHubTempDir, newCfg)
			} else if sourceDriver == "kube" {
				newSourceConfigFile = path.Join(newHubTempDir, "kubeconfig")
				err = util.CreateKubeconfigFile(newCfg, newSourceConfigFile)
				gomega.Expect(err).ToNot(gomega.HaveOccurred())
			}

			newKubeClient, err = kubernetes.NewForConfig(newCfg)
			gomega.Expect(err).ToNot(gomega.HaveOccurred())

			newWorkClient, err = workclientset.NewForConfig(newCfg)
			gomega.Expect(err).ToNot(gomega.HaveOccurred())

			_, err = newKubeClient.CoreV1().Namespaces().Create(context.Background(), ns, metav1.CreateOptions{})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())
		})

		ginkgo.AfterEach(func() {
			grpcCancel()
			if cancel != nil {
				cancel()
			}
			err := newHub.Stop()
			gomega.Expect(err).ToNot(gomega.HaveOccurred())
			if newHubTempDir != "" {
				err := os.RemoveAll(newHubTempDir)
				gomega.Expect(err).ToNot(gomega.HaveOccurred())
			}
		})

		ginkgo.It("should keep old appliemanifestwork with different agent id", func() {
			util.AssertExistenceOfConfigMaps(manifests, spokeKubeClient, eventuallyTimeout, eventuallyInterval)

			util.AssertWorkCondition(work.Namespace, work.Name, hubWorkClient, workapiv1.WorkApplied, metav1.ConditionTrue,
				[]metav1.ConditionStatus{metav1.ConditionTrue}, eventuallyTimeout, eventuallyInterval)
			util.AssertWorkCondition(work.Namespace, work.Name, hubWorkClient, workapiv1.WorkAvailable, metav1.ConditionTrue,
				[]metav1.ConditionStatus{metav1.ConditionTrue}, eventuallyTimeout, eventuallyInterval)

			// stop the agent and make it connect to the new hub
			if cancel != nil {
				cancel()
			}

			var ctx context.Context
			ctx, cancel = context.WithCancel(context.Background())
			go startWorkAgent(ctx, clusterName, optionDecorator(newAgentID, newSourceConfigFile, 5*time.Second))

			// Create the same manifestwork with the same name on new hub.
			work, err = newWorkClient.WorkV1().ManifestWorks(clusterName).Create(context.Background(), work, metav1.CreateOptions{})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())

			util.AssertWorkCondition(work.Namespace, work.Name, newWorkClient, workapiv1.WorkApplied, metav1.ConditionTrue,
				[]metav1.ConditionStatus{metav1.ConditionTrue}, eventuallyTimeout, eventuallyInterval)
			util.AssertWorkCondition(work.Namespace, work.Name, newWorkClient, workapiv1.WorkAvailable, metav1.ConditionTrue,
				[]metav1.ConditionStatus{metav1.ConditionTrue}, eventuallyTimeout, eventuallyInterval)

			// ensure the resource has two ownerrefs
			gomega.Eventually(func() error {
				cm, err := spokeKubeClient.CoreV1().ConfigMaps(clusterName).Get(context.TODO(), cm1, metav1.GetOptions{})
				if err != nil {
					return err
				}
				if len(cm.OwnerReferences) != 2 {
					return fmt.Errorf("should have two owners, but got %v", cm.OwnerReferences)
				}
				return nil
			}, eventuallyTimeout, eventuallyInterval).ShouldNot(gomega.HaveOccurred())
		})

		ginkgo.It("should remove old appliemanifestwork if applied again on new hub", func() {
			util.AssertExistenceOfConfigMaps(manifests, spokeKubeClient, eventuallyTimeout, eventuallyInterval)

			util.AssertWorkCondition(work.Namespace, work.Name, hubWorkClient, workapiv1.WorkApplied, metav1.ConditionTrue,
				[]metav1.ConditionStatus{metav1.ConditionTrue}, eventuallyTimeout, eventuallyInterval)
			util.AssertWorkCondition(work.Namespace, work.Name, hubWorkClient, workapiv1.WorkAvailable, metav1.ConditionTrue,
				[]metav1.ConditionStatus{metav1.ConditionTrue}, eventuallyTimeout, eventuallyInterval)

			// stop the agent and make it connect to the new hub
			if cancel != nil {
				cancel()
			}

			newOptDecorator := func(opt *spoke.WorkloadAgentOptions, commonOpt *commonoptions.AgentOptions) (
				*spoke.WorkloadAgentOptions, *commonoptions.AgentOptions) {
				opt.WorkloadSourceConfig = newSourceConfigFile
				opt.AppliedManifestWorkEvictionGracePeriod = 5 * time.Second
				commonOpt.SpokeClusterName = clusterName
				commonOpt.AgentID = agentID
				return opt, commonOpt
			}

			var ctx context.Context
			ctx, cancel = context.WithCancel(context.Background())
			go startWorkAgent(ctx, clusterName, newOptDecorator)

			// Create the same manifestwork with the same name.
			work, err = newWorkClient.WorkV1().ManifestWorks(clusterName).Create(context.Background(), work, metav1.CreateOptions{})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())

			util.AssertWorkCondition(work.Namespace, work.Name, newWorkClient, workapiv1.WorkApplied, metav1.ConditionTrue,
				[]metav1.ConditionStatus{metav1.ConditionTrue}, eventuallyTimeout, eventuallyInterval)
			util.AssertWorkCondition(work.Namespace, work.Name, newWorkClient, workapiv1.WorkAvailable, metav1.ConditionTrue,
				[]metav1.ConditionStatus{metav1.ConditionTrue}, eventuallyTimeout, eventuallyInterval)

			// ensure the old manifestwork is removed.
			gomega.Eventually(func() error {
				_, err := spokeWorkClient.WorkV1().AppliedManifestWorks().Get(context.TODO(), appliedManifestWorkName, metav1.GetOptions{})
				if errors.IsNotFound(err) {
					return nil
				}
				if err != nil {
					return err
				}
				return fmt.Errorf("appliedmanifestwork %s still exists", appliedManifestWorkName)
			}, eventuallyTimeout, eventuallyInterval).ShouldNot(gomega.HaveOccurred())

			// ensure the resource has only one ownerref
			gomega.Eventually(func() error {
				cm, err := spokeKubeClient.CoreV1().ConfigMaps(clusterName).Get(context.TODO(), cm1, metav1.GetOptions{})
				if err != nil {
					return err
				}
				if len(cm.OwnerReferences) != 1 {
					return fmt.Errorf("should only have one owners, but got %v", cm.OwnerReferences)
				}
				if cm.OwnerReferences[0].Name == appliedManifestWorkName {
					return fmt.Errorf("ownerref name is not correct")
				}
				return nil
			}, eventuallyTimeout, eventuallyInterval).ShouldNot(gomega.HaveOccurred())
		})
	})

	ginkgo.Context("Should evict applied work when its manifestwork is missing on the hub", func() {
		ginkgo.BeforeEach(func() {
			util.AssertExistenceOfConfigMaps(manifests, spokeKubeClient, eventuallyTimeout, eventuallyInterval)
			util.AssertWorkCondition(work.Namespace, work.Name, hubWorkClient, workapiv1.WorkApplied, metav1.ConditionTrue,
				[]metav1.ConditionStatus{metav1.ConditionTrue}, eventuallyTimeout, eventuallyInterval)
			util.AssertWorkCondition(work.Namespace, work.Name, hubWorkClient, workapiv1.WorkAvailable, metav1.ConditionTrue,
				[]metav1.ConditionStatus{metav1.ConditionTrue}, eventuallyTimeout, eventuallyInterval)

			// stop the agent
			if cancel != nil {
				cancel()
			}

			// force delete the work from hub
			err := forceDeleteManifestWork(context.TODO(), work)
			gomega.Expect(err).ToNot(gomega.HaveOccurred())
		})

		ginkgo.It("should evict appliemanifestwork after the work agent reconnected to the hub", func() {
			// restart the work agent
			var ctx context.Context
			ctx, cancel = context.WithCancel(context.Background())
			go startWorkAgent(ctx, clusterName, optionDecorator(agentID, sourceConfigFileName, 10*time.Second))

			// ensure the manifestwork is removed.
			gomega.Eventually(func() error {
				_, err := spokeWorkClient.WorkV1().AppliedManifestWorks().Get(context.TODO(), appliedManifestWorkName, metav1.GetOptions{})
				if errors.IsNotFound(err) {
					return nil
				}
				if err != nil {
					return err
				}
				return fmt.Errorf("appliedmanifestwork %s still exists", appliedManifestWorkName)
			}, eventuallyTimeout, eventuallyInterval).ShouldNot(gomega.HaveOccurred())
		})

		ginkgo.It("should keep appliemanifestwork if its manifestwork is recreated on new hub", func() {
			// restart the work agent
			var ctx context.Context
			ctx, cancel = context.WithCancel(context.Background())
			go startWorkAgent(ctx, clusterName, optionDecorator(agentID, sourceConfigFileName, 10*time.Second))

			// recreate the work on the hub
			_, err = hubWorkClient.WorkV1().ManifestWorks(clusterName).Create(context.Background(), work, metav1.CreateOptions{})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())

			// ensure the appliemanifestwork eviction is stopped
			gomega.Eventually(func() error {
				appliedWork, err := spokeWorkClient.WorkV1().AppliedManifestWorks().Get(context.TODO(), appliedManifestWorkName, metav1.GetOptions{})
				if err != nil {
					return err
				}

				if appliedWork.Status.EvictionStartTime != nil {
					return fmt.Errorf("the appliedmanifestwork %s evication is not stopped", appliedManifestWorkName)
				}
				return nil
			}, eventuallyTimeout, eventuallyInterval).ShouldNot(gomega.HaveOccurred())

		})
	})

	ginkgo.Context("Should disable the eviction if the grace period is really large", func() {
		ginkgo.BeforeEach(func() {
			util.AssertExistenceOfConfigMaps(manifests, spokeKubeClient, eventuallyTimeout, eventuallyInterval)
			util.AssertWorkCondition(work.Namespace, work.Name, hubWorkClient, workapiv1.WorkApplied, metav1.ConditionTrue,
				[]metav1.ConditionStatus{metav1.ConditionTrue}, eventuallyTimeout, eventuallyInterval)
			util.AssertWorkCondition(work.Namespace, work.Name, hubWorkClient, workapiv1.WorkAvailable, metav1.ConditionTrue,
				[]metav1.ConditionStatus{metav1.ConditionTrue}, eventuallyTimeout, eventuallyInterval)

			// stop the agent
			if cancel != nil {
				cancel()
			}

			// fore delete the work from hub
			err := forceDeleteManifestWork(context.TODO(), work)
			gomega.Expect(err).ToNot(gomega.HaveOccurred())
		})

		ginkgo.It("should stop eviction of the appliemanifestwork after the work agent reconnected to the hub", func() {
			// restart the work agent
			var ctx context.Context
			ctx, cancel = context.WithCancel(context.Background())
			go startWorkAgent(ctx, clusterName, optionDecorator(agentID, sourceConfigFileName, 5*time.Second))

			// ensure the eviction of the applied manifestwork starts
			gomega.Eventually(func() error {
				appliedManifestWork, err := spokeWorkClient.WorkV1().AppliedManifestWorks().Get(context.TODO(), appliedManifestWorkName, metav1.GetOptions{})
				if errors.IsNotFound(err) {
					return nil
				}
				if err != nil {
					return err
				}

				if appliedManifestWork.Status.EvictionStartTime == nil {
					return fmt.Errorf("eviction of the appliedmanifestwork %s does not start", appliedManifestWorkName)
				}
				return nil
			}, eventuallyTimeout, eventuallyInterval).ShouldNot(gomega.HaveOccurred())

			// stop the agent
			cancel()

			// restart the work agent with a really large eviction grace period
			ctx, cancel = context.WithCancel(context.Background())
			go startWorkAgent(ctx, clusterName, optionDecorator(agentID, sourceConfigFileName, 100*365*24*time.Hour))

			// ensure the eviction of the applied manifestwork stops
			gomega.Eventually(func() error {
				appliedManifestWork, err := spokeWorkClient.WorkV1().AppliedManifestWorks().Get(context.TODO(), appliedManifestWorkName, metav1.GetOptions{})
				if errors.IsNotFound(err) {
					return nil
				}
				if err != nil {
					return err
				}

				if appliedManifestWork.Status.EvictionStartTime != nil {
					return fmt.Errorf("eviction of the appliedmanifestwork %s does not stop", appliedManifestWorkName)
				}
				return nil
			}, eventuallyTimeout, eventuallyInterval).ShouldNot(gomega.HaveOccurred())
		})
	})
})

func forceDeleteManifestWork(ctx context.Context, work *workapiv1.ManifestWork) error {
	err := hubWorkClient.WorkV1().ManifestWorks(work.Namespace).Delete(ctx, work.Name, metav1.DeleteOptions{})
	if err != nil {
		return err
	}

	deleted, err := hubWorkClient.WorkV1().ManifestWorks(work.Namespace).Get(ctx, work.Name, metav1.GetOptions{})
	if errors.IsNotFound(err) {
		return nil
	}

	deleted.Finalizers = []string{}
	_, err = hubWorkClient.WorkV1().ManifestWorks(work.Namespace).Update(ctx, deleted, metav1.UpdateOptions{})
	return err
}
