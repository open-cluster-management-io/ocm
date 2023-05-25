package integration

import (
	"context"
	"fmt"
	"time"

	"github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"
	"github.com/openshift/library-go/pkg/controller/controllercmd"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/rand"
	clusterapiv1 "open-cluster-management.io/api/cluster/v1"
	clusterapiv1beta1 "open-cluster-management.io/api/cluster/v1beta1"
	controllers "open-cluster-management.io/placement/pkg/controllers"
	"open-cluster-management.io/placement/pkg/controllers/scheduling"
	"open-cluster-management.io/placement/test/integration/util"
)

var _ = ginkgo.Describe("TaintToleration", func() {
	var cancel context.CancelFunc
	var namespace string
	var placementName string
	var clusterSet1Name string
	var suffix string

	ginkgo.BeforeEach(func() {
		suffix = rand.String(5)
		namespace = fmt.Sprintf("ns-%s", suffix)
		placementName = fmt.Sprintf("placement-%s", suffix)
		clusterSet1Name = fmt.Sprintf("clusterset-%s", suffix)

		// create testing namespace
		ns := &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name: namespace,
			},
		}
		_, err := kubeClient.CoreV1().Namespaces().Create(context.Background(), ns, metav1.CreateOptions{})
		gomega.Expect(err).ToNot(gomega.HaveOccurred())

		// start controller manager
		var ctx context.Context
		ctx, cancel = context.WithCancel(context.Background())
		scheduling.ResyncInterval = time.Second * 5
		go controllers.RunControllerManager(ctx, &controllercmd.ControllerContext{
			KubeConfig:    restConfig,
			EventRecorder: util.NewIntegrationTestEventRecorder("integration"),
		})
	})

	ginkgo.AfterEach(func() {
		if cancel != nil {
			cancel()
		}
		err := kubeClient.CoreV1().Namespaces().Delete(context.Background(), namespace, metav1.DeleteOptions{})
		gomega.Expect(err).ToNot(gomega.HaveOccurred())
	})

	ginkgo.Context("TaintToleration", func() {
		ginkgo.It("Should schedule successfully when taint/toleration matches", func() {
			//Cluster settings
			assertBindingClusterSet(clusterSet1Name, namespace)
			clusterNames := assertCreatingClusters(clusterSet1Name, 3)

			assertUpdatingClusterWithClusterTaint(clusterNames[0], &clusterapiv1.Taint{
				Key:    "key1",
				Value:  "value1",
				Effect: clusterapiv1.TaintEffectNoSelect,
			})
			assertUpdatingClusterWithClusterTaint(clusterNames[1], &clusterapiv1.Taint{
				Key:    "key2",
				Value:  "value2",
				Effect: clusterapiv1.TaintEffectNoSelect,
			})
			assertUpdatingClusterWithClusterTaint(clusterNames[2], &clusterapiv1.Taint{
				Key:    "key2",
				Value:  "value3",
				Effect: clusterapiv1.TaintEffectNoSelect,
			})

			//Checking the result of the placement
			assertCreatingPlacementWithDecision(placementName, namespace, noc(3), 2, clusterapiv1beta1.PrioritizerPolicy{}, []clusterapiv1beta1.Toleration{
				{
					Key:      "key1",
					Operator: clusterapiv1beta1.TolerationOpExists,
				},
				{
					Key:      "key2",
					Operator: clusterapiv1beta1.TolerationOpEqual,
					Value:    "value2",
				},
			})
			assertClusterNamesOfDecisions(placementName, namespace, []string{clusterNames[0], clusterNames[1]})
		})

		ginkgo.It("Should schedule when taint/toleration effect matches", func() {
			//Cluster settings
			assertBindingClusterSet(clusterSet1Name, namespace)
			clusterNames := assertCreatingClusters(clusterSet1Name, 4)

			assertUpdatingClusterWithClusterTaint(clusterNames[0], &clusterapiv1.Taint{
				Key:    "key1",
				Value:  "value1",
				Effect: clusterapiv1.TaintEffectNoSelect,
			})
			assertUpdatingClusterWithClusterTaint(clusterNames[1], &clusterapiv1.Taint{
				Key:    "key2",
				Value:  "value2",
				Effect: clusterapiv1.TaintEffectNoSelectIfNew,
			})
			assertUpdatingClusterWithClusterTaint(clusterNames[2], &clusterapiv1.Taint{
				Key:    "key3",
				Value:  "value3",
				Effect: clusterapiv1.TaintEffectPreferNoSelect,
			})

			//Taint/toleration matches, effect not match
			assertCreatingPlacementWithDecision(placementName, namespace, noc(4), 2, clusterapiv1beta1.PrioritizerPolicy{}, []clusterapiv1beta1.Toleration{
				{
					Key:      "key1",
					Operator: clusterapiv1beta1.TolerationOpExists,
					Effect:   clusterapiv1.TaintEffectPreferNoSelect,
				},
				{
					Key:      "key2",
					Operator: clusterapiv1beta1.TolerationOpExists,
					Effect:   clusterapiv1.TaintEffectNoSelect,
				},
				{
					Key:      "key3",
					Operator: clusterapiv1beta1.TolerationOpExists,
					Effect:   clusterapiv1.TaintEffectNoSelect,
				},
				{
					Key:      "key4",
					Operator: clusterapiv1beta1.TolerationOpExists,
					Effect:   clusterapiv1.TaintEffectNoSelect,
				},
			})
			//Checking the result of the placement
			assertClusterNamesOfDecisions(placementName, namespace, []string{clusterNames[2], clusterNames[3]})

			//Taint effect is NoSelectIfNew, tolerations doesn't match, cluster is in decision
			assertUpdatingClusterWithClusterTaint(clusterNames[3], &clusterapiv1.Taint{
				Key:    "key4",
				Value:  "value4",
				Effect: clusterapiv1.TaintEffectNoSelectIfNew,
			})
			assertClusterNamesOfDecisions(placementName, namespace, []string{clusterNames[2], clusterNames[3]})
		})

		ginkgo.It("Should reschedule when expire TolerationSeconds", func() {
			addedTime := metav1.Now()
			addedTime_60 := addedTime.Add(-60 * time.Second)
			tolerationSeconds_10 := int64(10)
			tolerationSeconds_20 := int64(20)

			//Cluster settings
			assertBindingClusterSet(clusterSet1Name, namespace)
			clusterNames := assertCreatingClusters(clusterSet1Name, 4)

			assertUpdatingClusterWithClusterTaint(clusterNames[0], &clusterapiv1.Taint{
				Key:       "key1",
				Value:     "value1",
				Effect:    clusterapiv1.TaintEffectNoSelect,
				TimeAdded: addedTime,
			})
			assertUpdatingClusterWithClusterTaint(clusterNames[1], &clusterapiv1.Taint{
				Key:       "key2",
				Value:     "value2",
				Effect:    clusterapiv1.TaintEffectNoSelect,
				TimeAdded: addedTime,
			})
			assertUpdatingClusterWithClusterTaint(clusterNames[2], &clusterapiv1.Taint{
				Key:       "key2",
				Value:     "value2",
				Effect:    clusterapiv1.TaintEffectNoSelect,
				TimeAdded: metav1.NewTime(addedTime_60),
			})

			//Checking the result of the placement
			assertCreatingPlacementWithDecision(placementName, namespace, noc(4), 3, clusterapiv1beta1.PrioritizerPolicy{}, []clusterapiv1beta1.Toleration{
				{
					Key:               "key1",
					Operator:          clusterapiv1beta1.TolerationOpExists,
					TolerationSeconds: &tolerationSeconds_10,
				},
				{
					Key:               "key2",
					Operator:          clusterapiv1beta1.TolerationOpExists,
					TolerationSeconds: &tolerationSeconds_20,
				},
			})
			assertClusterNamesOfDecisions(placementName, namespace, []string{clusterNames[0], clusterNames[1], clusterNames[3]})

			//Check placement requeue, clusterNames[0] should be removed when TolerationSeconds expired.
			assertClusterNamesOfDecisions(placementName, namespace, []string{clusterNames[1], clusterNames[3]})
			//Check placement requeue, clusterNames[1] should be removed when TolerationSeconds expired.
			assertClusterNamesOfDecisions(placementName, namespace, []string{clusterNames[3]})
			//Check placement update, clusterNames[3] should be removed when new taint added.
			assertUpdatingClusterWithClusterTaint(clusterNames[3], &clusterapiv1.Taint{
				Key:       "key3",
				Value:     "value3",
				Effect:    clusterapiv1.TaintEffectNoSelect,
				TimeAdded: metav1.NewTime(addedTime_60),
			})
			assertClusterNamesOfDecisions(placementName, namespace, []string{})
		})
	})
})
