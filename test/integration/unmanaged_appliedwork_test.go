package integration

import (
	"context"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"time"

	"github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	utilrand "k8s.io/apimachinery/pkg/util/rand"
	"k8s.io/client-go/kubernetes"
	workclientset "open-cluster-management.io/api/client/work/clientset/versioned"
	workapiv1 "open-cluster-management.io/api/work/v1"
	"open-cluster-management.io/work/pkg/spoke"
	"open-cluster-management.io/work/test/integration/util"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
)

var _ = ginkgo.Describe("Unmanaged ApplieManifestWork", func() {
	var o *spoke.WorkloadAgentOptions
	var cancel context.CancelFunc
	var work *workapiv1.ManifestWork
	var manifests []workapiv1.Manifest
	var appliedManifestWorkName string
	var err error

	var newHubKubeConfigFile string
	var newKubeClient kubernetes.Interface
	var newWorkClient workclientset.Interface
	var newHub *envtest.Environment
	var newHubTempDir string

	ginkgo.BeforeEach(func() {
		o = spoke.NewWorkloadAgentOptions()
		o.HubKubeconfigFile = hubKubeconfigFileName
		o.SpokeClusterName = utilrand.String(5)
		o.StatusSyncInterval = 3 * time.Second
		o.AgentID = utilrand.String(5)

		ns := &corev1.Namespace{}
		ns.Name = o.SpokeClusterName
		_, err := spokeKubeClient.CoreV1().Namespaces().Create(context.Background(), ns, metav1.CreateOptions{})
		gomega.Expect(err).ToNot(gomega.HaveOccurred())

		var ctx context.Context
		ctx, cancel = context.WithCancel(context.Background())
		go startWorkAgent(ctx, o)

		// start another hub
		newHub = &envtest.Environment{
			ErrorIfCRDPathMissing: true,
			CRDDirectoryPaths: []string{
				filepath.Join(".", "deploy", "hub"),
				filepath.Join(".", "deploy", "spoke"),
			},
		}

		newCfg, err := newHub.Start()
		gomega.Expect(err).ToNot(gomega.HaveOccurred())

		newHubTempDir, err = os.MkdirTemp("", "unmanaged_work_test")
		gomega.Expect(err).ToNot(gomega.HaveOccurred())

		newHubKubeConfigFile = path.Join(newHubTempDir, "kubeconfig")
		err = util.CreateKubeconfigFile(newCfg, newHubKubeConfigFile)
		gomega.Expect(err).ToNot(gomega.HaveOccurred())

		newKubeClient, err = kubernetes.NewForConfig(newCfg)
		gomega.Expect(err).ToNot(gomega.HaveOccurred())

		newWorkClient, err = workclientset.NewForConfig(newCfg)
		gomega.Expect(err).ToNot(gomega.HaveOccurred())

		_, err = newKubeClient.CoreV1().Namespaces().Create(context.Background(), ns, metav1.CreateOptions{})
		gomega.Expect(err).ToNot(gomega.HaveOccurred())

		// reset manifests
		manifests = nil
	})

	ginkgo.JustBeforeEach(func() {
		work = util.NewManifestWork(o.SpokeClusterName, "unmanaged-appliedwork", manifests)
		_, err = hubWorkClient.WorkV1().ManifestWorks(o.SpokeClusterName).Create(context.Background(), work, metav1.CreateOptions{})
		appliedManifestWorkName = fmt.Sprintf("%s-%s", hubHash, work.Name)
		gomega.Expect(err).ToNot(gomega.HaveOccurred())
	})

	ginkgo.AfterEach(func() {
		if cancel != nil {
			cancel()
		}
		err := spokeKubeClient.CoreV1().Namespaces().Delete(context.Background(), o.SpokeClusterName, metav1.DeleteOptions{})
		gomega.Expect(err).ToNot(gomega.HaveOccurred())
		err = newHub.Stop()
		gomega.Expect(err).ToNot(gomega.HaveOccurred())
		if newHubTempDir != "" {
			os.RemoveAll(newHubTempDir)
		}
	})

	ginkgo.Context("Should delete unmanaged applied work", func() {
		ginkgo.BeforeEach(func() {
			manifests = []workapiv1.Manifest{
				util.ToManifest(util.NewConfigmap(o.SpokeClusterName, "cm1", map[string]string{"a": "b"}, nil)),
			}
		})

		ginkgo.It("should keep old appliemanifestwork with different agent id", func() {
			util.AssertExistenceOfConfigMaps(manifests, spokeKubeClient, eventuallyTimeout, eventuallyInterval)

			util.AssertWorkCondition(work.Namespace, work.Name, hubWorkClient, string(workapiv1.WorkApplied), metav1.ConditionTrue,
				[]metav1.ConditionStatus{metav1.ConditionTrue}, eventuallyTimeout, eventuallyInterval)
			util.AssertWorkCondition(work.Namespace, work.Name, hubWorkClient, string(workapiv1.WorkAvailable), metav1.ConditionTrue,
				[]metav1.ConditionStatus{metav1.ConditionTrue}, eventuallyTimeout, eventuallyInterval)

			// stop the agent and make it connect to the new hub
			if cancel != nil {
				cancel()
			}

			newOption := spoke.NewWorkloadAgentOptions()
			newOption.HubKubeconfigFile = newHubKubeConfigFile
			newOption.SpokeClusterName = o.SpokeClusterName
			newOption.AgentID = utilrand.String(5)

			var ctx context.Context
			ctx, cancel = context.WithCancel(context.Background())
			go startWorkAgent(ctx, newOption)

			// Create the same manifestwork with the same name on new hub.
			work, err = newWorkClient.WorkV1().ManifestWorks(o.SpokeClusterName).Create(context.Background(), work, metav1.CreateOptions{})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())

			util.AssertWorkCondition(work.Namespace, work.Name, newWorkClient, string(workapiv1.WorkApplied), metav1.ConditionTrue,
				[]metav1.ConditionStatus{metav1.ConditionTrue}, eventuallyTimeout, eventuallyInterval)
			util.AssertWorkCondition(work.Namespace, work.Name, newWorkClient, string(workapiv1.WorkAvailable), metav1.ConditionTrue,
				[]metav1.ConditionStatus{metav1.ConditionTrue}, eventuallyTimeout, eventuallyInterval)

			// ensure the resource has two ownerrefs
			gomega.Eventually(func() error {
				cm, err := spokeKubeClient.CoreV1().ConfigMaps(o.SpokeClusterName).Get(context.TODO(), "cm1", metav1.GetOptions{})
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

			util.AssertWorkCondition(work.Namespace, work.Name, hubWorkClient, string(workapiv1.WorkApplied), metav1.ConditionTrue,
				[]metav1.ConditionStatus{metav1.ConditionTrue}, eventuallyTimeout, eventuallyInterval)
			util.AssertWorkCondition(work.Namespace, work.Name, hubWorkClient, string(workapiv1.WorkAvailable), metav1.ConditionTrue,
				[]metav1.ConditionStatus{metav1.ConditionTrue}, eventuallyTimeout, eventuallyInterval)

			// stop the agent and make it connect to the new hub
			if cancel != nil {
				cancel()
			}

			newOption := spoke.NewWorkloadAgentOptions()
			newOption.HubKubeconfigFile = newHubKubeConfigFile
			newOption.SpokeClusterName = o.SpokeClusterName
			newOption.AgentID = o.AgentID

			var ctx context.Context
			ctx, cancel = context.WithCancel(context.Background())
			go startWorkAgent(ctx, newOption)

			// Create the same manifestwork with the same name.
			work, err = newWorkClient.WorkV1().ManifestWorks(o.SpokeClusterName).Create(context.Background(), work, metav1.CreateOptions{})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())

			util.AssertWorkCondition(work.Namespace, work.Name, newWorkClient, string(workapiv1.WorkApplied), metav1.ConditionTrue,
				[]metav1.ConditionStatus{metav1.ConditionTrue}, eventuallyTimeout, eventuallyInterval)
			util.AssertWorkCondition(work.Namespace, work.Name, newWorkClient, string(workapiv1.WorkAvailable), metav1.ConditionTrue,
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
				cm, err := spokeKubeClient.CoreV1().ConfigMaps(o.SpokeClusterName).Get(context.TODO(), "cm1", metav1.GetOptions{})
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

})
