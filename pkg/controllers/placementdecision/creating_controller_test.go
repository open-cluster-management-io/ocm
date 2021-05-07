package placementdecision

import (
	"context"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/rand"
	clienttesting "k8s.io/client-go/testing"

	clusterfake "github.com/open-cluster-management/api/client/cluster/clientset/versioned/fake"
	clusterinformers "github.com/open-cluster-management/api/client/cluster/informers/externalversions"
	clusterapiv1alpha1 "github.com/open-cluster-management/api/cluster/v1alpha1"
	testinghelpers "github.com/open-cluster-management/placement/pkg/helpers/testing"
)

func assertValidPlacementDecision(t *testing.T, placementNamespace, placementName string, placementDecision *clusterapiv1alpha1.PlacementDecision) {
	// check namespace
	if placementDecision.Namespace != placementNamespace {
		t.Errorf("expected PlacementDecision is created under namespace %q, but got %q",
			placementNamespace, placementDecision.Namespace)
	}

	// check label
	if _, ok := placementDecision.Labels[placementLabel]; !ok {
		t.Errorf("expected Placement label on Placement decision")
	}

	// check owner reference
	owner := metav1.GetControllerOf(&placementDecision.ObjectMeta)
	if owner == nil {
		t.Errorf("expected PlacementDecision with ownerreference")
	}

	if owner.Kind != "Placement" {
		t.Errorf("expected ownerreference with kind %q, but got %q", "Placement", owner.Kind)
	}

	if owner.Name != placementName {
		t.Errorf("expected ownerreference with name %q, but got %q", placementName, owner.Name)
	}
}

func TestPlacementDecisionCreatingControllerSync(t *testing.T) {
	placementNamespace := "ns1"
	placementName := "placement1"
	queueKey := placementNamespace + "/" + placementName
	placementUID := rand.String(16)

	cases := []struct {
		name            string
		queueKey        string
		initObjs        []runtime.Object
		validateActions func(t *testing.T, actions []clienttesting.Action)
	}{
		{
			name:     "placement not found",
			queueKey: queueKey,
			validateActions: func(t *testing.T, actions []clienttesting.Action) {
				if len(actions) != 0 {
					t.Errorf("expected no action but got: %v ", actions)
				}
			},
		},
		{
			name:     "placement is deleting",
			queueKey: queueKey,
			initObjs: []runtime.Object{
				testinghelpers.NewPlacement(placementNamespace, placementName).WithDeletionTimestamp().Build(),
			},
			validateActions: func(t *testing.T, actions []clienttesting.Action) {
				if len(actions) != 0 {
					t.Errorf("expected no action but got: %v ", actions)
				}
			},
		},
		{
			name:     "new placement",
			queueKey: queueKey,
			initObjs: []runtime.Object{
				testinghelpers.NewPlacement(placementNamespace, placementName).WithUID(placementUID).Build(),
			},
			validateActions: func(t *testing.T, actions []clienttesting.Action) {
				// check if PlacementDecision has been created
				testinghelpers.AssertActions(t, actions, "create")
				actual := actions[0].(clienttesting.CreateActionImpl).Object
				placementDecision, ok := actual.(*clusterapiv1alpha1.PlacementDecision)
				if !ok {
					t.Errorf("expected PlacementDecision was created")
				}
				assertValidPlacementDecision(t, placementNamespace, placementName, placementDecision)
			},
		},
		{
			name:     "placementdecision exits",
			queueKey: queueKey,
			initObjs: []runtime.Object{
				testinghelpers.NewPlacement(placementNamespace, placementName).WithUID(placementUID).Build(),
				testinghelpers.NewPlacementDecision(placementNamespace, "decison1").
					WithPlacementLabel(placementName).WithController(placementUID).Build(),
			},
			validateActions: func(t *testing.T, actions []clienttesting.Action) {
				if len(actions) != 0 {
					t.Errorf("expected no action but got %d: %v", len(actions), actions)
				}
			},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			clusterClient := clusterfake.NewSimpleClientset(c.initObjs...)
			clusterInformerFactory := clusterinformers.NewSharedInformerFactory(clusterClient, time.Minute*10)
			placementStore := clusterInformerFactory.Cluster().V1alpha1().Placements().Informer().GetStore()
			placementDecisionStore := clusterInformerFactory.Cluster().V1alpha1().PlacementDecisions().Informer().GetStore()
			for _, obj := range c.initObjs {
				switch obj.(type) {
				case *clusterapiv1alpha1.Placement:
					placementStore.Add(obj)
				case *clusterapiv1alpha1.PlacementDecision:
					placementDecisionStore.Add(obj)
				}
			}

			ctrl := placementDecisionCreatingController{
				clusterClient:           clusterClient,
				placementLister:         clusterInformerFactory.Cluster().V1alpha1().Placements().Lister(),
				placementDecisionLister: clusterInformerFactory.Cluster().V1alpha1().PlacementDecisions().Lister(),
			}
			syncErr := ctrl.sync(context.TODO(), testinghelpers.NewFakeSyncContext(t, c.queueKey))
			if syncErr != nil {
				t.Errorf("unexpected err: %v", syncErr)
			}

			c.validateActions(t, clusterClient.Actions())
		})
	}
}
