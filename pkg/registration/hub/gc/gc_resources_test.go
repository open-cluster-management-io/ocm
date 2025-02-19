package gc

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	fakemetadataclient "k8s.io/client-go/metadata/fake"
	clienttesting "k8s.io/client-go/testing"

	addonv1alpha1 "open-cluster-management.io/api/addon/v1alpha1"
	clusterv1 "open-cluster-management.io/api/cluster/v1"
	workv1 "open-cluster-management.io/api/work/v1"

	testingcommon "open-cluster-management.io/ocm/pkg/common/testing"
	testinghelpers "open-cluster-management.io/ocm/pkg/registration/helpers/testing"
)

func TestGCResourcesController(t *testing.T) {
	cases := []struct {
		name             string
		cluster          *clusterv1.ManagedCluster
		clusterNamespace string
		objs             []runtime.Object
		expectedError    error
		validateActions  func(t *testing.T, kubeActions []clienttesting.Action)
	}{
		{
			name:             "delete addon",
			cluster:          testinghelpers.NewDeletingManagedCluster(),
			clusterNamespace: testinghelpers.TestManagedClusterName,
			objs:             []runtime.Object{newAddonMetadata(testinghelpers.TestManagedClusterName, "test", nil)},
			expectedError:    requeueError,
			validateActions: func(t *testing.T, kubeActions []clienttesting.Action) {
				testingcommon.AssertActions(t, kubeActions, "list", "delete")
			},
		},
		{
			name:             "delete work",
			cluster:          testinghelpers.NewDeletingManagedCluster(),
			clusterNamespace: testinghelpers.TestManagedClusterName,
			objs:             []runtime.Object{newWorkMetadata(testinghelpers.TestManagedClusterName, "test", nil)},
			expectedError:    requeueError,
			validateActions: func(t *testing.T, kubeActions []clienttesting.Action) {
				testingcommon.AssertActions(t, kubeActions, "list", "list", "delete")
			},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			scheme := fakemetadataclient.NewTestScheme()
			_ = addonv1alpha1.Install(scheme)
			_ = workv1.Install(scheme)
			_ = metav1.AddMetaToScheme(scheme)
			metadataClient := fakemetadataclient.NewSimpleMetadataClient(scheme, c.objs...)
			_ = newGCResourcesController(metadataClient, []schema.GroupVersionResource{addonGvr, workGvr})

			ctrl := &gcResourcesController{
				metadataClient:  metadataClient,
				resourceGVRList: []schema.GroupVersionResource{addonGvr, workGvr},
			}

			err := ctrl.reconcile(context.TODO(), c.cluster, c.clusterNamespace)
			assert.Equal(t, c.expectedError, err)
			c.validateActions(t, metadataClient.Actions())
		})
	}
}

func TestGetFirstDeletePriority(t *testing.T) {
	cases := []struct {
		name                        string
		objs                        []metav1.PartialObjectMetadata
		expectedFirstDeletePriority cleanupPriority
		expectedFirstDeletedCount   int
	}{
		{
			name: "no priority resource",
			objs: []metav1.PartialObjectMetadata{
				*newWorkMetadata(testinghelpers.TestManagedClusterName, "test1", nil),
				*newWorkMetadata(testinghelpers.TestManagedClusterName, "test2", nil),
				*newWorkMetadata(testinghelpers.TestManagedClusterName, "test3", nil),
				*newWorkMetadata(testinghelpers.TestManagedClusterName, "test4", nil),
			},
			expectedFirstDeletePriority: minCleanupPriority,
			expectedFirstDeletedCount:   4,
		},
		{
			name: "invalid priority resource",
			objs: []metav1.PartialObjectMetadata{
				*newWorkMetadata(testinghelpers.TestManagedClusterName, "test1", map[string]string{clusterv1.CleanupPriorityAnnotationKey: "abc"}),
				*newWorkMetadata(testinghelpers.TestManagedClusterName, "test2", map[string]string{clusterv1.CleanupPriorityAnnotationKey: "300"}),
				*newWorkMetadata(testinghelpers.TestManagedClusterName, "test3", map[string]string{clusterv1.CleanupPriorityAnnotationKey: "-1"}),
				*newWorkMetadata(testinghelpers.TestManagedClusterName, "test4", nil),
			},
			expectedFirstDeletePriority: minCleanupPriority,
			expectedFirstDeletedCount:   4,
		},
		{
			name: "multi priority resources",
			objs: []metav1.PartialObjectMetadata{
				*newWorkMetadata(testinghelpers.TestManagedClusterName, "test1", map[string]string{clusterv1.CleanupPriorityAnnotationKey: "100"}),
				*newWorkMetadata(testinghelpers.TestManagedClusterName, "test2", map[string]string{clusterv1.CleanupPriorityAnnotationKey: "300"}),
				*newWorkMetadata(testinghelpers.TestManagedClusterName, "test3", map[string]string{clusterv1.CleanupPriorityAnnotationKey: "10"}),
				*newWorkMetadata(testinghelpers.TestManagedClusterName, "test4", map[string]string{clusterv1.CleanupPriorityAnnotationKey: "abc"}),
				*newWorkMetadata(testinghelpers.TestManagedClusterName, "test5", map[string]string{clusterv1.CleanupPriorityAnnotationKey: "0"}),
				*newWorkMetadata(testinghelpers.TestManagedClusterName, "test6", map[string]string{clusterv1.CleanupPriorityAnnotationKey: "-1"}),
				*newWorkMetadata(testinghelpers.TestManagedClusterName, "test7", nil),
			},
			expectedFirstDeletePriority: minCleanupPriority,
			expectedFirstDeletedCount:   5,
		},
		{
			name: "valid priority resources ",
			objs: []metav1.PartialObjectMetadata{
				*newWorkMetadata(testinghelpers.TestManagedClusterName, "test1", map[string]string{clusterv1.CleanupPriorityAnnotationKey: "100"}),
				*newWorkMetadata(testinghelpers.TestManagedClusterName, "test2", map[string]string{clusterv1.CleanupPriorityAnnotationKey: "10"}),
				*newWorkMetadata(testinghelpers.TestManagedClusterName, "test3", map[string]string{clusterv1.CleanupPriorityAnnotationKey: "10"}),
				*newWorkMetadata(testinghelpers.TestManagedClusterName, "test4", map[string]string{clusterv1.CleanupPriorityAnnotationKey: "40"}),
				*newWorkMetadata(testinghelpers.TestManagedClusterName, "test5", map[string]string{clusterv1.CleanupPriorityAnnotationKey: "65"}),
				*newWorkMetadata(testinghelpers.TestManagedClusterName, "test6", map[string]string{clusterv1.CleanupPriorityAnnotationKey: "90"}),
			},
			expectedFirstDeletePriority: cleanupPriority(10),
			expectedFirstDeletedCount:   2,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			resourceList := &metav1.PartialObjectMetadataList{
				Items: c.objs,
			}
			priorityResourceMap := mapPriorityResource(resourceList)
			firstDeletePriority := getFirstDeletePriority(priorityResourceMap)
			assert.Equal(t, c.expectedFirstDeletePriority, firstDeletePriority)
			assert.Equal(t, c.expectedFirstDeletedCount, len(priorityResourceMap[firstDeletePriority]))
		})
	}
}

func newAddonMetadata(namespace, name string, annotations map[string]string) *metav1.PartialObjectMetadata {
	return &metav1.PartialObjectMetadata{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "addon.open-cluster-management.io/v1alpha1",
			Kind:       "ManagedClusterAddOn",
		},
		ObjectMeta: metav1.ObjectMeta{
			Namespace:   namespace,
			Name:        name,
			Annotations: annotations,
		},
	}
}
func newWorkMetadata(namespace, name string, annotations map[string]string) *metav1.PartialObjectMetadata {
	return &metav1.PartialObjectMetadata{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "work.open-cluster-management.io/v1",
			Kind:       "ManifestWork",
		},
		ObjectMeta: metav1.ObjectMeta{
			Namespace:   namespace,
			Name:        name,
			Annotations: annotations,
		},
	}
}
