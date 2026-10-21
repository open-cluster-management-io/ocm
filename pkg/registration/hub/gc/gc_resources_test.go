package gc

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	fakemetadataclient "k8s.io/client-go/metadata/fake"
	clienttesting "k8s.io/client-go/testing"

	addonv1beta1 "open-cluster-management.io/api/addon/v1beta1"
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
			_ = addonv1beta1.Install(scheme)
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

func TestCollectGCTargets(t *testing.T) {
	cases := []struct {
		name                          string
		objs                          []runtime.Object
		expectedFirstDeletePriority   cleanupPriority
		expectedFirstDeletedNames     []string
		expectedTotalCount            int
		expectedFinalizerPendingCount int
	}{
		{
			name: "no priority resource",
			objs: []runtime.Object{
				newWorkMetadata(testinghelpers.TestManagedClusterName, "test1", nil),
				newWorkMetadata(testinghelpers.TestManagedClusterName, "test2", nil),
				newWorkMetadata(testinghelpers.TestManagedClusterName, "test3", nil),
				newWorkMetadata(testinghelpers.TestManagedClusterName, "test4", nil),
			},
			expectedFirstDeletePriority: minCleanupPriority,
			expectedFirstDeletedNames:   []string{"test1", "test2", "test3", "test4"},
			expectedTotalCount:          4,
		},
		{
			name: "resources pending finalizers",
			objs: []runtime.Object{
				newWorkMetadataWithFinalizers(testinghelpers.TestManagedClusterName, "test1", nil, "work.open-cluster-management.io/hold"),
				newWorkMetadata(testinghelpers.TestManagedClusterName, "test2", nil),
				newWorkMetadataWithFinalizers(testinghelpers.TestManagedClusterName, "test3", nil, "work.open-cluster-management.io/hold"),
				newWorkMetadata(testinghelpers.TestManagedClusterName, "test4", nil),
			},
			expectedFirstDeletePriority:   minCleanupPriority,
			expectedFirstDeletedNames:     []string{"test1", "test2", "test3", "test4"},
			expectedTotalCount:            4,
			expectedFinalizerPendingCount: 2,
		},
		{
			name: "invalid priority resource",
			objs: []runtime.Object{
				newWorkMetadata(testinghelpers.TestManagedClusterName, "test1", map[string]string{clusterv1.CleanupPriorityAnnotationKey: "abc"}),
				newWorkMetadata(testinghelpers.TestManagedClusterName, "test2", map[string]string{clusterv1.CleanupPriorityAnnotationKey: "300"}),
				newWorkMetadata(testinghelpers.TestManagedClusterName, "test3", map[string]string{clusterv1.CleanupPriorityAnnotationKey: "-1"}),
				newWorkMetadata(testinghelpers.TestManagedClusterName, "test4", nil),
			},
			expectedFirstDeletePriority: minCleanupPriority,
			expectedFirstDeletedNames:   []string{"test1", "test2", "test3", "test4"},
			expectedTotalCount:          4,
		},
		{
			name: "multi priority resources",
			objs: []runtime.Object{
				newWorkMetadata(testinghelpers.TestManagedClusterName, "test1", map[string]string{clusterv1.CleanupPriorityAnnotationKey: "100"}),
				newWorkMetadata(testinghelpers.TestManagedClusterName, "test2", map[string]string{clusterv1.CleanupPriorityAnnotationKey: "300"}),
				newWorkMetadata(testinghelpers.TestManagedClusterName, "test3", map[string]string{clusterv1.CleanupPriorityAnnotationKey: "10"}),
				newWorkMetadata(testinghelpers.TestManagedClusterName, "test4", map[string]string{clusterv1.CleanupPriorityAnnotationKey: "abc"}),
				newWorkMetadata(testinghelpers.TestManagedClusterName, "test5", map[string]string{clusterv1.CleanupPriorityAnnotationKey: "0"}),
				newWorkMetadata(testinghelpers.TestManagedClusterName, "test6", map[string]string{clusterv1.CleanupPriorityAnnotationKey: "-1"}),
				newWorkMetadata(testinghelpers.TestManagedClusterName, "test7", nil),
			},
			expectedFirstDeletePriority: minCleanupPriority,
			expectedFirstDeletedNames:   []string{"test2", "test4", "test5", "test6", "test7"},
			expectedTotalCount:          7,
		},
		{
			name: "valid priority resources",
			objs: []runtime.Object{
				newWorkMetadata(testinghelpers.TestManagedClusterName, "test1", map[string]string{clusterv1.CleanupPriorityAnnotationKey: "100"}),
				newWorkMetadata(testinghelpers.TestManagedClusterName, "test2", map[string]string{clusterv1.CleanupPriorityAnnotationKey: "10"}),
				newWorkMetadata(testinghelpers.TestManagedClusterName, "test3", map[string]string{clusterv1.CleanupPriorityAnnotationKey: "10"}),
				newWorkMetadata(testinghelpers.TestManagedClusterName, "test4", map[string]string{clusterv1.CleanupPriorityAnnotationKey: "40"}),
				newWorkMetadata(testinghelpers.TestManagedClusterName, "test5", map[string]string{clusterv1.CleanupPriorityAnnotationKey: "65"}),
				newWorkMetadata(testinghelpers.TestManagedClusterName, "test6", map[string]string{clusterv1.CleanupPriorityAnnotationKey: "90"}),
			},
			expectedFirstDeletePriority: cleanupPriority(10),
			expectedFirstDeletedNames:   []string{"test2", "test3"},
			expectedTotalCount:          6,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			scheme := fakemetadataclient.NewTestScheme()
			_ = workv1.Install(scheme)
			_ = metav1.AddMetaToScheme(scheme)
			metadataClient := fakemetadataclient.NewSimpleMetadataClient(scheme, c.objs...)

			ctrl := &gcResourcesController{
				metadataClient: metadataClient,
			}
			result, err := ctrl.collectGCTargets(context.TODO(), workGvr, testinghelpers.TestManagedClusterName)
			assert.NoError(t, err)
			assert.Equal(t, c.expectedTotalCount, result.totalCount)
			assert.Equal(t, c.expectedFinalizerPendingCount, result.finalizerPendingCount)
			assert.Equal(t, c.expectedFirstDeletePriority, result.lowestPriority)
			assert.ElementsMatch(t, c.expectedFirstDeletedNames, result.lowestPriorityNames)
		})
	}
}

// TestCollectGCTargetsPagination drives collectGCTargets across several pages. The
// object tracker behind NewSimpleMetadataClient ignores ListOptions.Limit/Continue,
// so pages are served by a stateful list reactor instead: each call returns the next
// chunk and a non-empty Continue token until the last one.
func TestCollectGCTargetsPagination(t *testing.T) {
	// 1200 works: the lowest priority (10) only appears on the last page, so the
	// names collected from the earlier pages must be discarded.
	var objs []metav1.PartialObjectMetadata
	for i := 0; i < 1200; i++ {
		priority := "50"
		switch {
		case i >= 1150:
			priority = "10"
		case i%2 == 0:
			priority = "90"
		}
		objs = append(objs, *newWorkMetadata(testinghelpers.TestManagedClusterName,
			fmt.Sprintf("test-%04d", i),
			map[string]string{clusterv1.CleanupPriorityAnnotationKey: priority}))
	}
	// every 100th work is pending a finalizer
	for i := range objs {
		if i%100 == 0 {
			objs[i].Finalizers = []string{"work.open-cluster-management.io/hold"}
		}
	}

	const pageSize = 500
	scheme := fakemetadataclient.NewTestScheme()
	_ = workv1.Install(scheme)
	_ = metav1.AddMetaToScheme(scheme)
	metadataClient := fakemetadataclient.NewSimpleMetadataClient(scheme)

	served := 0
	metadataClient.PrependReactor("list", workGvr.Resource,
		func(action clienttesting.Action) (bool, runtime.Object, error) {
			if served >= len(objs) {
				return true, &metav1.List{}, nil
			}
			end := min(served+pageSize, len(objs))
			page := &metav1.List{}
			for i := served; i < end; i++ {
				page.Items = append(page.Items, runtime.RawExtension{Object: &objs[i]})
			}
			served = end
			if served < len(objs) {
				page.Continue = fmt.Sprintf("continue-%d", served)
			}
			return true, page, nil
		})

	ctrl := &gcResourcesController{metadataClient: metadataClient}
	result, err := ctrl.collectGCTargets(context.TODO(), workGvr, testinghelpers.TestManagedClusterName)
	assert.NoError(t, err)

	// 1200 objects at 500 per page is 3 list calls, the last one with an empty Continue.
	assert.Len(t, metadataClient.Actions(), 3)
	assert.Equal(t, 1200, result.totalCount)
	assert.Equal(t, 12, result.finalizerPendingCount)
	assert.Equal(t, cleanupPriority(10), result.lowestPriority)

	var expectedNames []string
	for i := 1150; i < 1200; i++ {
		expectedNames = append(expectedNames, fmt.Sprintf("test-%04d", i))
	}
	assert.ElementsMatch(t, expectedNames, result.lowestPriorityNames)
}

func newWorkMetadataWithFinalizers(namespace, name string,
	annotations map[string]string, finalizers ...string) *metav1.PartialObjectMetadata {
	work := newWorkMetadata(namespace, name, annotations)
	work.Finalizers = finalizers
	return work
}

func newAddonMetadata(namespace, name string, annotations map[string]string) *metav1.PartialObjectMetadata {
	return &metav1.PartialObjectMetadata{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "addon.open-cluster-management.io/v1beta1",
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
