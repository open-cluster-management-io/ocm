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

	clusterv1alpha1 "open-cluster-management.io/api/cluster/v1alpha1"
	clusterv1beta1 "open-cluster-management.io/api/cluster/v1beta1"
	workapiv1 "open-cluster-management.io/api/work/v1"
	workapiv1alpha1 "open-cluster-management.io/api/work/v1alpha1"

	commonoptions "open-cluster-management.io/ocm/pkg/common/options"
	"open-cluster-management.io/ocm/pkg/work/hub"
	"open-cluster-management.io/ocm/pkg/work/spoke"
	"open-cluster-management.io/ocm/test/integration/util"
)

const mwrsTestCM = "mwrs-test-cm"

var _ = ginkgo.Describe("ManifestWorkReplicaSet", func() {
	var err error
	var cancel context.CancelFunc

	var clusterAName, clusterBName string
	var namespace string
	var placement *clusterv1beta1.Placement
	var placementDecision *clusterv1beta1.PlacementDecision
	var manifestWorkReplicaSet *workapiv1alpha1.ManifestWorkReplicaSet

	ginkgo.BeforeEach(func() {
		var ctx context.Context
		ctx, cancel = context.WithCancel(context.Background())

		namespace = utilrand.String(5)
		ns := &corev1.Namespace{}
		ns.Name = namespace
		_, err = spokeKubeClient.CoreV1().Namespaces().Create(ctx, ns, metav1.CreateOptions{})
		gomega.Expect(err).ToNot(gomega.HaveOccurred())

		clusterAName = "cluster-" + utilrand.String(5)
		clusterNS := &corev1.Namespace{}
		clusterNS.Name = clusterAName
		_, err = spokeKubeClient.CoreV1().Namespaces().Create(ctx, clusterNS, metav1.CreateOptions{})
		gomega.Expect(err).NotTo(gomega.HaveOccurred())

		clusterBName = "cluster-" + utilrand.String(5)
		clusterNS = &corev1.Namespace{}
		clusterNS.Name = clusterBName
		_, err = spokeKubeClient.CoreV1().Namespaces().Create(ctx, clusterNS, metav1.CreateOptions{})
		gomega.Expect(err).NotTo(gomega.HaveOccurred())

		placement = &clusterv1beta1.Placement{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-placement",
				Namespace: namespace,
			},
		}
		_, err = hubClusterClient.ClusterV1beta1().Placements(namespace).Create(ctx, placement, metav1.CreateOptions{})
		gomega.Expect(err).NotTo(gomega.HaveOccurred())

		placementDecision = &clusterv1beta1.PlacementDecision{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-placement-decision",
				Namespace: namespace,
				Labels: map[string]string{
					clusterv1beta1.PlacementLabel:          placement.Name,
					clusterv1beta1.DecisionGroupIndexLabel: "0",
				},
			},
		}
		decision, err := hubClusterClient.ClusterV1beta1().PlacementDecisions(namespace).Create(ctx, placementDecision, metav1.CreateOptions{})
		gomega.Expect(err).NotTo(gomega.HaveOccurred())
		decision.Status.Decisions = []clusterv1beta1.ClusterDecision{
			{ClusterName: clusterAName},
			{ClusterName: clusterBName},
		}
		_, err = hubClusterClient.ClusterV1beta1().PlacementDecisions(namespace).UpdateStatus(ctx, decision, metav1.UpdateOptions{})
		gomega.Expect(err).NotTo(gomega.HaveOccurred())

		<-time.After(time.Second)

		startCtrl(ctx)

		// start work agents
		startAgent(ctx, clusterAName)
		startAgent(ctx, clusterBName)

		manifestWorkReplicaSet = &workapiv1alpha1.ManifestWorkReplicaSet{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-work",
				Namespace: namespace,
			},
			Spec: workapiv1alpha1.ManifestWorkReplicaSetSpec{
				ManifestWorkTemplate: workapiv1.ManifestWorkSpec{
					Workload: workapiv1.ManifestsTemplate{
						Manifests: []workapiv1.Manifest{
							util.ToManifest(util.NewConfigmap("default", mwrsTestCM, map[string]string{"a": "b"}, nil)),
						},
					},
				},
				PlacementRefs: []workapiv1alpha1.LocalPlacementReference{
					{
						Name:            placement.Name,
						RolloutStrategy: clusterv1alpha1.RolloutStrategy{Type: clusterv1alpha1.All},
					},
				},
			},
		}
		_, err = hubWorkClient.WorkV1alpha1().ManifestWorkReplicaSets(namespace).Create(context.TODO(), manifestWorkReplicaSet, metav1.CreateOptions{})
		gomega.Expect(err).ToNot(gomega.HaveOccurred())
	})

	ginkgo.AfterEach(func() {
		err := spokeKubeClient.CoreV1().Namespaces().Delete(context.Background(), namespace, metav1.DeleteOptions{})
		gomega.Expect(err).ToNot(gomega.HaveOccurred())

		if cancel != nil {
			cancel()
		}
	})

	ginkgo.Context("Create/Update/Delete a manifestWorkReplicaSet", func() {
		ginkgo.It("should create/update/delete successfully", func() {
			gomega.Eventually(func() error {
				return assertSummary(workapiv1alpha1.ManifestWorkReplicaSetSummary{Total: 2, Available: 2, Applied: 2}, manifestWorkReplicaSet)
			}, eventuallyTimeout, eventuallyInterval).Should(gomega.Succeed())

			ginkgo.By("Update decision so manifestworks should be updated")
			decision, err := hubClusterClient.ClusterV1beta1().PlacementDecisions(namespace).Get(context.TODO(), placementDecision.Name, metav1.GetOptions{})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())

			decision.Status.Decisions = decision.Status.Decisions[:1]
			_, err = hubClusterClient.ClusterV1beta1().PlacementDecisions(namespace).UpdateStatus(context.TODO(), decision, metav1.UpdateOptions{})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())

			gomega.Eventually(func() error {
				return assertSummary(workapiv1alpha1.ManifestWorkReplicaSetSummary{Total: 1, Available: 1, Applied: 1}, manifestWorkReplicaSet)
			}, eventuallyTimeout, eventuallyInterval).Should(gomega.Succeed())

			ginkgo.By("Delete manifestworkreplicaset")
			err = hubWorkClient.WorkV1alpha1().ManifestWorkReplicaSets(namespace).Delete(context.TODO(), manifestWorkReplicaSet.Name, metav1.DeleteOptions{})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())

			gomega.Eventually(func() error {
				_, err := hubWorkClient.WorkV1alpha1().ManifestWorkReplicaSets(namespace).Get(context.TODO(), manifestWorkReplicaSet.Name, metav1.GetOptions{})
				if errors.IsNotFound(err) {
					return nil
				}

				return fmt.Errorf("the mwrs is not deleted, %v", err)
			}, eventuallyTimeout, eventuallyInterval).Should(gomega.Succeed())
		})
	})
})

func startAgent(ctx context.Context, clusterName string) {
	o := spoke.NewWorkloadAgentOptions()
	o.StatusSyncInterval = 3 * time.Second
	o.AppliedManifestWorkEvictionGracePeriod = 5 * time.Second
	o.WorkloadSourceDriver = workSourceDriver
	o.WorkloadSourceConfig = mwrsConfigFileName
	o.CloudEventsClientID = fmt.Sprintf("%s-work-client", clusterName)
	o.CloudEventsClientCodecs = []string{"manifestbundle"}

	commOptions := commonoptions.NewAgentOptions()
	commOptions.SpokeClusterName = clusterName

	go runWorkAgent(ctx, o, commOptions)
}

func startCtrl(ctx context.Context) {
	opts := hub.NewWorkHubManagerOptions()
	opts.WorkDriver = workSourceDriver
	opts.WorkDriverConfig = mwrsConfigFileName
	opts.CloudEventsClientID = "mwrsctrl-client"
	hubConfig := hub.NewWorkHubManagerConfig(opts)

	// start hub controller
	go func() {
		err := hubConfig.RunWorkHubManager(ctx, &controllercmd.ControllerContext{
			KubeConfig:    hubRestConfig,
			EventRecorder: util.NewIntegrationTestEventRecorder("mwrsctrl"),
		})
		fmt.Println(err)
		gomega.Expect(err).NotTo(gomega.HaveOccurred())
	}()

}

func assertSummary(summary workapiv1alpha1.ManifestWorkReplicaSetSummary, mwrs *workapiv1alpha1.ManifestWorkReplicaSet) error {
	rs, err := hubWorkClient.WorkV1alpha1().ManifestWorkReplicaSets(mwrs.Namespace).Get(context.TODO(), mwrs.Name, metav1.GetOptions{})

	if err != nil {
		return err
	}

	if rs.Status.Summary != summary {
		return fmt.Errorf("unexpected summary expected: %v, got :%v", summary, rs.Status.Summary)
	}

	return nil
}
