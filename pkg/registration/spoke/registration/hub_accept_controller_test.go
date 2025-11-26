package registration

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	clusterv1lister "open-cluster-management.io/api/client/cluster/listers/cluster/v1"
	clusterv1 "open-cluster-management.io/api/cluster/v1"
)

func TestHubAcceptController_sync(t *testing.T) {
	var err error
	clusterName := "testCluster"
	handled := false
	mockHubClusterLister := &MockManagedClusterLister{}
	hacontroller := &hubAcceptController{
		clusterName:      clusterName,
		hubClusterLister: mockHubClusterLister,
		handleAcceptFalse: func(ctx context.Context) error {
			handled = true
			return nil
		},
	}

	// Create a mock hub cluster with HubAcceptsClient set to false
	mockHubClusterLister.mockHubCluster = &clusterv1.ManagedCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name: clusterName,
		},
		Spec: clusterv1.ManagedClusterSpec{
			HubAcceptsClient: true,
		},
	}

	// Call the sync method
	err = hacontroller.sync(context.TODO(), nil, "")
	assert.NoError(t, err, "Expected no error")

	// Expect handled to be false
	assert.False(t, handled, "Expected handled to be false")

	// Create a mock hub cluster with HubAcceptsClient set to true
	mockHubClusterLister.mockHubCluster = &clusterv1.ManagedCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name: clusterName,
		},
		Spec: clusterv1.ManagedClusterSpec{
			HubAcceptsClient: false,
		},
	}

	// Call the sync method again
	err = hacontroller.sync(context.TODO(), nil, "")
	assert.NoError(t, err, "Expected no error")

	// Expect handled to be true
	assert.True(t, handled, "Expected handled to be true")
}

type MockManagedClusterLister struct {
	clusterv1lister.ManagedClusterLister
	mockHubCluster *clusterv1.ManagedCluster
}

func (m *MockManagedClusterLister) Get(name string) (*clusterv1.ManagedCluster, error) {
	// Return a dummy ManagedCluster or an error based on your test case.
	return m.mockHubCluster, nil
}
