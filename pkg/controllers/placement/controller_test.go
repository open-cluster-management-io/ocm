package placement

import (
	"context"
	"testing"
	"time"

	"k8s.io/apimachinery/pkg/runtime"
	clienttesting "k8s.io/client-go/testing"

	clusterfake "github.com/open-cluster-management/api/client/cluster/clientset/versioned/fake"
	clusterinformers "github.com/open-cluster-management/api/client/cluster/informers/externalversions"
	testinghelpers "github.com/open-cluster-management/placement/pkg/helpers/testing"
)

func TestSync(t *testing.T) {
	cases := []struct {
		name            string
		queueKey        string
		initObjs        []runtime.Object
		validateActions func(t *testing.T, hubActions, agentActions []clienttesting.Action)
	}{}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			clusterClient := clusterfake.NewSimpleClientset(c.initObjs...)
			clusterInformerFactory := clusterinformers.NewSharedInformerFactory(clusterClient, time.Minute*10)
			clusterStore := clusterInformerFactory.Cluster().V1().ManagedClusters().Informer().GetStore()
			for _, cluster := range c.initObjs {
				clusterStore.Add(cluster)
			}

			ctrl := placementController{
				clusterLister: clusterInformerFactory.Cluster().V1().ManagedClusters().Lister(),
			}
			syncErr := ctrl.sync(context.TODO(), testinghelpers.NewFakeSyncContext(t, c.queueKey))
			if syncErr != nil {
				t.Errorf("unexpected err: %v", syncErr)
			}

			//c.validateActions(t, nil)
		})
	}
}
