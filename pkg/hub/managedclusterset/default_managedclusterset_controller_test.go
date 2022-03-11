package managedclusterset

import (
	"context"
	"testing"
	"time"

	"github.com/openshift/library-go/pkg/operator/events/eventstesting"
	"k8s.io/apimachinery/pkg/api/equality"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	clienttesting "k8s.io/client-go/testing"
	clusterfake "open-cluster-management.io/api/client/cluster/clientset/versioned/fake"
	clusterinformers "open-cluster-management.io/api/client/cluster/informers/externalversions"
	clusterv1beta1 "open-cluster-management.io/api/cluster/v1beta1"
	testinghelpers "open-cluster-management.io/registration/pkg/helpers/testing"
)

func TestSyncDefaultClusterSet(t *testing.T) {

	var editedDefaultManagedClusterSetSpec = clusterv1beta1.ManagedClusterSetSpec{
		ClusterSelector: clusterv1beta1.ManagedClusterSelector{
			SelectorType: "non-LegacyClusterSetLabel",
		},
	}

	cases := []struct {
		name               string
		existingClusterSet *clusterv1beta1.ManagedClusterSet
		validateActions    func(t *testing.T, actions []clienttesting.Action)
	}{
		{
			name:               "sync default cluster set",
			existingClusterSet: newDefaultManagedClusterSet(defaultManagedClusterSetName, defaultManagedClusterSetSpec, false),
			validateActions: func(t *testing.T, actions []clienttesting.Action) {
				testinghelpers.AssertNoActions(t, actions)
			},
		},
		{
			name:               "sync edited default cluster set",
			existingClusterSet: newDefaultManagedClusterSet(defaultManagedClusterSetName, editedDefaultManagedClusterSetSpec, false),
			validateActions: func(t *testing.T, actions []clienttesting.Action) {

				testinghelpers.AssertActions(t, actions, "update")
				clusterset := actions[0].(clienttesting.UpdateAction).GetObject().(*clusterv1beta1.ManagedClusterSet)
				// if spec not rollbacked, error
				if !equality.Semantic.DeepEqual(clusterset.Spec, defaultManagedClusterSetSpec) {
					t.Errorf("Failed to rollback default managed cluster set spec after it is edited")
				}
			},
		},
		{
			name:               "sync deleting default cluster set",
			existingClusterSet: newDefaultManagedClusterSet(defaultManagedClusterSetName, defaultManagedClusterSetSpec, true),
			validateActions: func(t *testing.T, actions []clienttesting.Action) {
				testinghelpers.AssertNoActions(t, actions)
			},
		},
		{
			name: "sync deleted default cluster set",
			// default cluster set should be created if it is deleted.
			validateActions: func(t *testing.T, actions []clienttesting.Action) {
				testinghelpers.AssertActions(t, actions, "create")
				clusterset := actions[0].(clienttesting.CreateAction).GetObject().(*clusterv1beta1.ManagedClusterSet)
				if clusterset.ObjectMeta.Name != defaultManagedClusterSetName {
					t.Errorf("Failed to create default managed cluster set")
				}
			},
		},
		{
			name:               "sync default cluster set with disabled annotation",
			existingClusterSet: newDefaultManagedClusterSetWithAnnotation(defaultManagedClusterSetName, autoUpdateAnnotation, "false", defaultManagedClusterSetSpec, false),
			validateActions: func(t *testing.T, actions []clienttesting.Action) {
				testinghelpers.AssertNoActions(t, actions)
			},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			objects := []runtime.Object{}

			if c.existingClusterSet != nil {
				objects = append(objects, c.existingClusterSet)
			}

			clusterSetClient := clusterfake.NewSimpleClientset(objects...)
			informerFactory := clusterinformers.NewSharedInformerFactory(clusterSetClient, 5*time.Minute)

			if c.existingClusterSet != nil {
				informerFactory.Cluster().V1beta1().ManagedClusterSets().Informer().GetStore().Add(c.existingClusterSet)
			}

			ctrl := defaultManagedClusterSetController{
				clusterSetClient: clusterSetClient.ClusterV1beta1(),
				clusterSetLister: informerFactory.Cluster().V1beta1().ManagedClusterSets().Lister(),
				eventRecorder:    eventstesting.NewTestingEventRecorder(t),
			}

			syncErr := ctrl.sync(context.TODO(), testinghelpers.NewFakeSyncContext(t, testinghelpers.TestManagedClusterName))
			if syncErr != nil {
				t.Errorf("unexpected err: %v", syncErr)
			}

			c.validateActions(t, clusterSetClient.Actions())

		})
	}
}

func newDefaultManagedClusterSet(name string, spec clusterv1beta1.ManagedClusterSetSpec, terminating bool) *clusterv1beta1.ManagedClusterSet {
	clusterSet := &clusterv1beta1.ManagedClusterSet{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
		Spec: spec,
	}
	if terminating {
		now := metav1.Now()
		clusterSet.DeletionTimestamp = &now
	}

	return clusterSet
}

func newDefaultManagedClusterSetWithAnnotation(name string, k, v string, spec clusterv1beta1.ManagedClusterSetSpec, terminating bool) *clusterv1beta1.ManagedClusterSet {
	clusterSet := &clusterv1beta1.ManagedClusterSet{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
			Annotations: map[string]string{
				k: v,
			},
		},
		Spec: spec,
	}
	if terminating {
		now := metav1.Now()
		clusterSet.DeletionTimestamp = &now
	}

	return clusterSet
}
