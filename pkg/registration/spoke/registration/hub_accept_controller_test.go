package registration

import (
	"context"
	"testing"

	"github.com/openshift/library-go/pkg/operator/events/eventstesting"
	"github.com/stretchr/testify/assert"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	clienttesting "k8s.io/client-go/testing"

	fakeclusterclient "open-cluster-management.io/api/client/cluster/clientset/versioned/fake"
	clusterv1 "open-cluster-management.io/api/cluster/v1"
)

func TestHubAcceptControllerSync(t *testing.T) {
	clusterName := "test-cluster"

	tests := []struct {
		name                 string
		existingObjects      []runtime.Object
		hubAcceptsClient     bool
		expectedError        bool
		expectedHandleCalled bool
	}{
		{
			name: "HubAcceptsClient is true",
			existingObjects: []runtime.Object{
				&clusterv1.ManagedCluster{
					ObjectMeta: metav1.ObjectMeta{Name: clusterName},
					Spec: clusterv1.ManagedClusterSpec{
						HubAcceptsClient: true,
					},
				},
			},
			hubAcceptsClient:     true,
			expectedError:        false,
			expectedHandleCalled: false,
		},
		{
			name: "HubAcceptsClient is false",
			existingObjects: []runtime.Object{
				&clusterv1.ManagedCluster{
					ObjectMeta: metav1.ObjectMeta{Name: clusterName},
					Spec: clusterv1.ManagedClusterSpec{
						HubAcceptsClient: false,
					},
				},
			},
			hubAcceptsClient:     false,
			expectedError:        false,
			expectedHandleCalled: true,
		},
		{
			name:                 "ManagedCluster not found",
			existingObjects:      []runtime.Object{},
			expectedError:        true,
			expectedHandleCalled: false,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			// Setup
			fakeClusterClient := fakeclusterclient.NewSimpleClientset(test.existingObjects...)
			handleCalled := false
			handleAcceptFalse := func(ctx context.Context) error {
				handleCalled = true
				return nil
			}

			controller := &hubAcceptController{
				clusterName:       clusterName,
				hubClusterClient:  fakeClusterClient,
				handleAcceptFalse: handleAcceptFalse,
				recorder:          eventstesting.NewTestingEventRecorder(t),
			}

			// Execute
			err := controller.sync(context.TODO(), nil)

			// Verify
			if test.expectedError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}

			assert.Equal(t, test.expectedHandleCalled, handleCalled)
		})
	}
}

func TestHubAcceptControllerSyncForbidden(t *testing.T) {
	clusterName := "test-cluster"
	fakeClusterClient := fakeclusterclient.NewSimpleClientset()
	fakeClusterClient.PrependReactor("get", "managedclusters", func(action clienttesting.Action) (handled bool, ret runtime.Object, err error) {
		return true, nil, errors.NewForbidden(action.GetResource().GroupResource(), clusterName, nil)
	})

	handleCalled := false
	handleAcceptFalse := func(ctx context.Context) error {
		handleCalled = true
		return nil
	}

	controller := &hubAcceptController{
		clusterName:       clusterName,
		hubClusterClient:  fakeClusterClient,
		handleAcceptFalse: handleAcceptFalse,
		recorder:          eventstesting.NewTestingEventRecorder(t),
	}

	err := controller.sync(context.TODO(), nil)

	assert.NoError(t, err)
	assert.True(t, handleCalled)
}
