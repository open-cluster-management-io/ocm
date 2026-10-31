package placement

import (
	"context"
	"fmt"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/openshift/library-go/pkg/controller/controllercmd"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/rand"

	clusterapiv1 "open-cluster-management.io/api/cluster/v1"
	clusterapiv1beta1 "open-cluster-management.io/api/cluster/v1beta1"

	controllers "open-cluster-management.io/ocm/pkg/placement/controllers"
	testinghelpers "open-cluster-management.io/ocm/pkg/placement/helpers/testing"
	"open-cluster-management.io/ocm/test/integration/util"
)

var _ = Describe("TaintToleration", func() {
	var cancel context.CancelFunc
	var namespace string
	var placementName string
	var clusterSet1Name string
	var suffix string

	BeforeEach(func(ctx SpecContext) {
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
		_, err := kubeClient.CoreV1().Namespaces().Create(ctx, ns, metav1.CreateOptions{})
		Expect(err).ToNot(HaveOccurred())

		// start controller manager
		var runCtx context.Context
		runCtx, cancel = context.WithCancel(context.Background())
		go func() {
			defer GinkgoRecover()
			err := controllers.RunControllerManager(runCtx, &controllercmd.ControllerContext{
				KubeConfig:    restConfig,
				EventRecorder: util.NewIntegrationTestEventRecorder("integration"),
			})
			Expect(err).NotTo(HaveOccurred())
		}()
	})

	AfterEach(func(ctx SpecContext) {
		if cancel != nil {
			cancel()
		}
		err := kubeClient.CoreV1().Namespaces().Delete(ctx, namespace, metav1.DeleteOptions{})
		Expect(err).ToNot(HaveOccurred())
	})

	Context("TaintToleration", func() {
		It("Should schedule successfully when taint/toleration matches", func(ctx SpecContext) {
			// Cluster settings
			assertBindingClusterSet(ctx, clusterSet1Name, namespace)
			clusterNames := assertCreatingClusters(ctx, clusterSet1Name, 3)

			assertPatchingClusterSpecWithTaint(ctx, clusterNames[0], &clusterapiv1.Taint{
				Key:    "key1",
				Value:  "value1",
				Effect: clusterapiv1.TaintEffectNoSelect,
			})
			assertPatchingClusterSpecWithTaint(ctx, clusterNames[1], &clusterapiv1.Taint{
				Key:    "key2",
				Value:  "value2",
				Effect: clusterapiv1.TaintEffectNoSelect,
			})
			assertPatchingClusterSpecWithTaint(ctx, clusterNames[2], &clusterapiv1.Taint{
				Key:    "key2",
				Value:  "value3",
				Effect: clusterapiv1.TaintEffectNoSelect,
			})

			// Checking the result of the placement
			placement := testinghelpers.NewPlacement(namespace, placementName).WithNOC(3).AddToleration(&clusterapiv1beta1.Toleration{
				Key:      "key1",
				Operator: clusterapiv1beta1.TolerationOpExists,
			}).AddToleration(&clusterapiv1beta1.Toleration{
				Key:      "key2",
				Operator: clusterapiv1beta1.TolerationOpEqual,
				Value:    "value2",
			}).Build()
			assertCreatingPlacementWithDecision(ctx, placement, 2, 1)
			assertPlacementDecisionClusterNames(ctx, placementName, namespace, []string{clusterNames[0], clusterNames[1]})
		})

		It("Should schedule when taint/toleration effect matches", func(ctx SpecContext) {
			// Cluster settings
			assertBindingClusterSet(ctx, clusterSet1Name, namespace)
			clusterNames := assertCreatingClusters(ctx, clusterSet1Name, 4)

			assertPatchingClusterSpecWithTaint(ctx, clusterNames[0], &clusterapiv1.Taint{
				Key:    "key1",
				Value:  "value1",
				Effect: clusterapiv1.TaintEffectNoSelect,
			})
			assertPatchingClusterSpecWithTaint(ctx, clusterNames[1], &clusterapiv1.Taint{
				Key:    "key2",
				Value:  "value2",
				Effect: clusterapiv1.TaintEffectNoSelectIfNew,
			})
			assertPatchingClusterSpecWithTaint(ctx, clusterNames[2], &clusterapiv1.Taint{
				Key:    "key3",
				Value:  "value3",
				Effect: clusterapiv1.TaintEffectPreferNoSelect,
			})

			// Taint/toleration matches, effect not match
			placement := testinghelpers.NewPlacement(namespace, placementName).WithNOC(4).AddToleration(&clusterapiv1beta1.Toleration{
				Key:      "key1",
				Operator: clusterapiv1beta1.TolerationOpExists,
				Effect:   clusterapiv1.TaintEffectPreferNoSelect,
			}).AddToleration(&clusterapiv1beta1.Toleration{
				Key:      "key2",
				Operator: clusterapiv1beta1.TolerationOpExists,
				Effect:   clusterapiv1.TaintEffectNoSelect,
			}).AddToleration(&clusterapiv1beta1.Toleration{
				Key:      "key3",
				Operator: clusterapiv1beta1.TolerationOpExists,
				Effect:   clusterapiv1.TaintEffectNoSelect,
			}).AddToleration(&clusterapiv1beta1.Toleration{
				Key:      "key4",
				Operator: clusterapiv1beta1.TolerationOpExists,
				Effect:   clusterapiv1.TaintEffectNoSelect,
			}).Build()
			assertCreatingPlacementWithDecision(ctx, placement, 2, 1)
			// Checking the result of the placement
			assertPlacementDecisionClusterNames(ctx, placementName, namespace, []string{clusterNames[2], clusterNames[3]})

			// Taint effect is NoSelectIfNew, tolerations doesn't match, cluster is in decision
			assertPatchingClusterSpecWithTaint(ctx, clusterNames[3], &clusterapiv1.Taint{
				Key:    "key4",
				Value:  "value4",
				Effect: clusterapiv1.TaintEffectNoSelectIfNew,
			})
			assertPlacementDecisionClusterNames(ctx, placementName, namespace, []string{clusterNames[2], clusterNames[3]})
		})

		It("Should reschedule when expire TolerationSeconds", func(ctx SpecContext) {
			addedTime := metav1.Now()
			addedTime_60 := addedTime.Add(-60 * time.Second)
			tolerationSeconds_10 := int64(10)
			tolerationSeconds_20 := int64(20)

			// Cluster settings
			assertBindingClusterSet(ctx, clusterSet1Name, namespace)
			clusterNames := assertCreatingClusters(ctx, clusterSet1Name, 4)

			assertPatchingClusterSpecWithTaint(ctx, clusterNames[0], &clusterapiv1.Taint{
				Key:       "key1",
				Value:     "value1",
				Effect:    clusterapiv1.TaintEffectNoSelect,
				TimeAdded: addedTime,
			})
			assertPatchingClusterSpecWithTaint(ctx, clusterNames[1], &clusterapiv1.Taint{
				Key:       "key2",
				Value:     "value2",
				Effect:    clusterapiv1.TaintEffectNoSelect,
				TimeAdded: addedTime,
			})
			assertPatchingClusterSpecWithTaint(ctx, clusterNames[2], &clusterapiv1.Taint{
				Key:       "key2",
				Value:     "value2",
				Effect:    clusterapiv1.TaintEffectNoSelect,
				TimeAdded: metav1.NewTime(addedTime_60),
			})

			// Checking the result of the placement
			placement := testinghelpers.NewPlacement(namespace, placementName).WithNOC(4).AddToleration(&clusterapiv1beta1.Toleration{
				Key:               "key1",
				Operator:          clusterapiv1beta1.TolerationOpExists,
				TolerationSeconds: &tolerationSeconds_10,
			}).AddToleration(&clusterapiv1beta1.Toleration{
				Key:               "key2",
				Operator:          clusterapiv1beta1.TolerationOpExists,
				TolerationSeconds: &tolerationSeconds_20,
			}).Build()
			assertCreatingPlacementWithDecision(ctx, placement, 3, 1)
			assertPlacementDecisionClusterNames(ctx, placementName, namespace, []string{clusterNames[0], clusterNames[1], clusterNames[3]})

			// Check placement requeue, clusterNames[0] should be removed when TolerationSeconds expired.
			assertPlacementDecisionClusterNames(ctx, placementName, namespace, []string{clusterNames[1], clusterNames[3]})
			// Check placement requeue, clusterNames[1] should be removed when TolerationSeconds expired.
			assertPlacementDecisionClusterNames(ctx, placementName, namespace, []string{clusterNames[3]})
			// Check placement update, clusterNames[3] should be removed when new taint added.
			assertPatchingClusterSpecWithTaint(ctx, clusterNames[3], &clusterapiv1.Taint{
				Key:       "key3",
				Value:     "value3",
				Effect:    clusterapiv1.TaintEffectNoSelect,
				TimeAdded: metav1.NewTime(addedTime_60),
			})
			assertPlacementDecisionClusterNames(ctx, placementName, namespace, []string{})
		})
	})
})
