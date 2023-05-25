package benchmark

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/openshift/library-go/pkg/controller/controllercmd"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/klog/v2"
	clusterv1client "open-cluster-management.io/api/client/cluster/clientset/versioned"
	clusterapiv1 "open-cluster-management.io/api/cluster/v1"
	clusterapiv1alpha1 "open-cluster-management.io/api/cluster/v1alpha1"
	clusterapiv1beta1 "open-cluster-management.io/api/cluster/v1beta1"
	clusterapiv1beta2 "open-cluster-management.io/api/cluster/v1beta2"

	controllers "open-cluster-management.io/placement/pkg/controllers"
	scheduling "open-cluster-management.io/placement/pkg/controllers/scheduling"
	"open-cluster-management.io/placement/test/integration/util"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
)

const (
	clusterSetLabel = "cluster.open-cluster-management.io/clusterset"
)

var cfg *rest.Config
var kubeClient kubernetes.Interface
var clusterClient clusterv1client.Interface

var namespace, name = "benchmark", "benchmark"
var noc = int32(10)

var benchmarkPlacement = clusterapiv1beta1.Placement{
	ObjectMeta: metav1.ObjectMeta{
		Namespace: namespace,
		Name:      name,
	},
	Spec: clusterapiv1beta1.PlacementSpec{
		NumberOfClusters: &noc,

		PrioritizerPolicy: clusterapiv1beta1.PrioritizerPolicy{
			Mode: clusterapiv1beta1.PrioritizerPolicyModeExact,
			Configurations: []clusterapiv1beta1.PrioritizerConfig{
				{
					ScoreCoordinate: &clusterapiv1beta1.ScoreCoordinate{

						Type: clusterapiv1beta1.ScoreCoordinateTypeAddOn,
						AddOn: &clusterapiv1beta1.AddOnScore{
							ResourceName: "demo",
							ScoreName:    "demo",
						},
					},
					Weight: 1,
				},
				{
					ScoreCoordinate: &clusterapiv1beta1.ScoreCoordinate{

						Type:    clusterapiv1beta1.ScoreCoordinateTypeBuiltIn,
						BuiltIn: "ResourceAllocatableCPU",
					},
					Weight: 1,
				},
				{
					ScoreCoordinate: &clusterapiv1beta1.ScoreCoordinate{

						Type:    clusterapiv1beta1.ScoreCoordinateTypeBuiltIn,
						BuiltIn: "Balance",
					},
					Weight: 1,
				},
			},
		},
	},
}

func BenchmarkSchedulePlacements100(b *testing.B) {
	benchmarkSchedulePlacements(b, 100, 1)
}

func BenchmarkSchedulePlacements1000(b *testing.B) {
	benchmarkSchedulePlacements(b, 1000, 1000)
}

func BenchmarkSchedulePlacements10000(b *testing.B) {
	benchmarkSchedulePlacements(b, 10000, 1000)
}

func benchmarkSchedulePlacements(b *testing.B, pnum, cnum int) {
	var err error
	ctx, cancel := context.WithCancel(context.Background())
	scheduling.ResyncInterval = time.Second * 5

	// start a kube-apiserver
	testEnv := &envtest.Environment{
		ErrorIfCRDPathMissing: true,
		CRDDirectoryPaths: []string{
			filepath.Join("../../", "deploy", "hub"),
		},
	}

	// prepare client
	if cfg, err = testEnv.Start(); err != nil {
		klog.Fatalf("%v", err)
	}
	if kubeClient, err = kubernetes.NewForConfig(cfg); err != nil {
		klog.Fatalf("%v", err)
	}
	if clusterClient, err = clusterv1client.NewForConfig(cfg); err != nil {
		klog.Fatalf("%v", err)
	}

	// prepare namespace
	createNamespace(namespace)
	createClusters(namespace, name, cnum)
	createAddOnPlacementScores("demo", cnum)

	b.ResetTimer()
	go controllers.RunControllerManager(ctx, &controllercmd.ControllerContext{
		KubeConfig:    cfg,
		EventRecorder: util.NewIntegrationTestEventRecorder("integration"),
	})

	go createPlacements(pnum)
	assertPlacementDecisions(pnum, cancel)

}

func createNamespace(namespace string) {
	ns := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: namespace,
		},
	}
	_, err := kubeClient.CoreV1().Namespaces().Create(context.Background(), ns, metav1.CreateOptions{})
	if err != nil {
		klog.Fatalf("%v", err)
	}
}

func createClusters(namespace, name string, num int) {
	// Create ManagedClusterSet
	clusterset := &clusterapiv1beta2.ManagedClusterSet{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
	}
	_, err := clusterClient.ClusterV1beta2().ManagedClusterSets().Create(context.Background(), clusterset, metav1.CreateOptions{})
	if err != nil {
		klog.Fatalf("%v", err)
	}

	// Create ManagedClusterSetBinding
	csb := &clusterapiv1beta2.ManagedClusterSetBinding{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: namespace,
			Name:      name,
		},
		Spec: clusterapiv1beta2.ManagedClusterSetBindingSpec{
			ClusterSet: name,
		},
	}
	_, err = clusterClient.ClusterV1beta2().ManagedClusterSetBindings(namespace).Create(context.Background(), csb, metav1.CreateOptions{})
	if err != nil {
		klog.Fatalf("%v", err)
	}

	// Create ManagedCluster
	for i := 0; i < num; i++ {
		clusterName := fmt.Sprintf("cluster%d", i)
		cluster := &clusterapiv1.ManagedCluster{
			ObjectMeta: metav1.ObjectMeta{
				Name: clusterName,
				Labels: map[string]string{
					clusterSetLabel: name,
				},
			},
		}
		_, err := clusterClient.ClusterV1().ManagedClusters().Create(context.Background(), cluster, metav1.CreateOptions{})
		if err != nil {
			klog.Fatalf("%v", err)
		}
		// create cluster namespace
		createNamespace(clusterName)
	}

	// Check ManagedCluster
	clusters, err := clusterClient.ClusterV1().ManagedClusters().List(context.Background(), metav1.ListOptions{})
	if err != nil {
		klog.Fatalf("%v", err)
	} else if len(clusters.Items) != num {
		klog.Fatalf("Expect %d clusters created, but actually %d", num, len(clusters.Items))
	}
}

func createPlacements(num int) {
	for i := 0; i < num; i++ {
		benchmarkPlacement.Name = fmt.Sprintf("%s-%d", name, i)
		_, err := clusterClient.ClusterV1beta1().Placements(namespace).Create(context.Background(), &benchmarkPlacement, metav1.CreateOptions{})
		if err != nil {
			klog.Fatalf("%v", err)
		}
	}
}

func assertPlacementDecisions(num int, cancel context.CancelFunc) {
	for {
		decisions, _ := clusterClient.ClusterV1beta1().PlacementDecisions(namespace).List(context.Background(), metav1.ListOptions{})
		if len(decisions.Items) == num {
			if cancel != nil {
				cancel()
			}
			return
		}
		time.Sleep(1 * time.Second)
	}
}

func createAddOnPlacementScores(name string, num int) {
	// Create AddOnPlacementScore
	for i := 0; i < num; i++ {
		clusterName := fmt.Sprintf("cluster%d", i)
		addOn := &clusterapiv1alpha1.AddOnPlacementScore{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: clusterName,
				Name:      name,
			},
		}
		addOn, err := clusterClient.ClusterV1alpha1().AddOnPlacementScores(clusterName).Create(context.Background(), addOn, metav1.CreateOptions{})
		if err != nil {
			klog.Fatalf("%v", err)
		}

		vu := metav1.NewTime(time.Now().Add(1000 * time.Minute))
		addOn.Status = clusterapiv1alpha1.AddOnPlacementScoreStatus{
			Scores: []clusterapiv1alpha1.AddOnPlacementScoreItem{
				{
					Name:  name,
					Value: 80,
				},
			},
			ValidUntil: &vu,
		}

		_, err = clusterClient.ClusterV1alpha1().AddOnPlacementScores(clusterName).UpdateStatus(context.Background(), addOn, metav1.UpdateOptions{})
		if err != nil {
			klog.Fatalf("%v", err)
		}
	}

	// Check AddOnPlacementScore
	addons, err := clusterClient.ClusterV1alpha1().AddOnPlacementScores("").List(context.Background(), metav1.ListOptions{})
	if err != nil {
		klog.Fatalf("%v", err)
	} else if len(addons.Items) != num {
		klog.Fatalf("Expect %d clusters created, but actually %d", num, len(addons.Items))
	}
}
