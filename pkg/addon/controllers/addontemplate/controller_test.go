package addontemplate

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/openshift/library-go/pkg/operator/events/eventstesting"
	"github.com/stretchr/testify/assert"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/dynamic/dynamicinformer"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	fakekube "k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/rest"

	"open-cluster-management.io/addon-framework/pkg/addonmanager/addontesting"
	"open-cluster-management.io/addon-framework/pkg/utils"
	addonv1alpha1 "open-cluster-management.io/api/addon/v1alpha1"
	fakeaddon "open-cluster-management.io/api/client/addon/clientset/versioned/fake"
	addoninformers "open-cluster-management.io/api/client/addon/informers/externalversions"
	fakecluster "open-cluster-management.io/api/client/cluster/clientset/versioned/fake"
	clusterv1informers "open-cluster-management.io/api/client/cluster/informers/externalversions"
	fakework "open-cluster-management.io/api/client/work/clientset/versioned/fake"
	workinformers "open-cluster-management.io/api/client/work/informers/externalversions"

	testingcommon "open-cluster-management.io/ocm/pkg/common/testing"
)

func TestReconcile(t *testing.T) {
	cases := []struct {
		name                   string
		syncKeys               []string
		managedClusteraddon    []runtime.Object
		clusterManagementAddon []runtime.Object
		expectedCount          int
		expectedTimeout        bool
	}{
		{
			name:                   "no clustermanagementaddon",
			syncKeys:               []string{"test"},
			managedClusteraddon:    []runtime.Object{},
			clusterManagementAddon: []runtime.Object{},
			expectedCount:          0,
			expectedTimeout:        true,
		},
		{
			name:                "not template type clustermanagementaddon",
			syncKeys:            []string{"test"},
			managedClusteraddon: []runtime.Object{},
			clusterManagementAddon: []runtime.Object{
				addontesting.NewClusterManagementAddon("test", "", "").Build()},
			expectedCount:   0,
			expectedTimeout: true,
		},
		{
			name:                "one template type clustermanagementaddon",
			syncKeys:            []string{"test"},
			managedClusteraddon: []runtime.Object{},
			clusterManagementAddon: []runtime.Object{
				addontesting.NewClusterManagementAddon("test", "", "").WithSupportedConfigs(
					addonv1alpha1.ConfigMeta{
						ConfigGroupResource: addonv1alpha1.ConfigGroupResource{
							Group:    utils.AddOnTemplateGVR.Group,
							Resource: utils.AddOnTemplateGVR.Resource,
						},
						DefaultConfig: &addonv1alpha1.ConfigReferent{Name: "test"},
					}).Build()},
			expectedCount:   1,
			expectedTimeout: false,
		},
		{
			name:                "two template type clustermanagementaddon",
			syncKeys:            []string{"test", "test1"},
			managedClusteraddon: []runtime.Object{},
			clusterManagementAddon: []runtime.Object{
				addontesting.NewClusterManagementAddon("test", "", "").WithSupportedConfigs(
					addonv1alpha1.ConfigMeta{
						ConfigGroupResource: addonv1alpha1.ConfigGroupResource{
							Group:    utils.AddOnTemplateGVR.Group,
							Resource: utils.AddOnTemplateGVR.Resource,
						},
						DefaultConfig: &addonv1alpha1.ConfigReferent{Name: "test"},
					}).Build(),
				addontesting.NewClusterManagementAddon("test1", "", "").WithSupportedConfigs(
					addonv1alpha1.ConfigMeta{
						ConfigGroupResource: addonv1alpha1.ConfigGroupResource{
							Group:    utils.AddOnTemplateGVR.Group,
							Resource: utils.AddOnTemplateGVR.Resource,
						},
						DefaultConfig: &addonv1alpha1.ConfigReferent{Name: "test"},
					}).Build(),
			},
			expectedCount:   2,
			expectedTimeout: false,
		},
		{
			name:                "two template type and one not template type clustermanagementaddon",
			syncKeys:            []string{"test", "test1", "test2"},
			managedClusteraddon: []runtime.Object{},
			clusterManagementAddon: []runtime.Object{
				addontesting.NewClusterManagementAddon("test", "", "").WithSupportedConfigs(
					addonv1alpha1.ConfigMeta{
						ConfigGroupResource: addonv1alpha1.ConfigGroupResource{
							Group:    utils.AddOnTemplateGVR.Group,
							Resource: utils.AddOnTemplateGVR.Resource,
						},
						DefaultConfig: &addonv1alpha1.ConfigReferent{Name: "test"},
					}).Build(),
				addontesting.NewClusterManagementAddon("test1", "", "").WithSupportedConfigs(
					addonv1alpha1.ConfigMeta{
						ConfigGroupResource: addonv1alpha1.ConfigGroupResource{
							Group:    utils.AddOnTemplateGVR.Group,
							Resource: utils.AddOnTemplateGVR.Resource,
						},
						DefaultConfig: &addonv1alpha1.ConfigReferent{Name: "test"},
					}).Build(),
				addontesting.NewClusterManagementAddon("test2", "", "").Build(),
			},
			expectedCount:   2,
			expectedTimeout: true,
		},
	}

	for _, c := range cases {
		count := 0
		var wg sync.WaitGroup
		lock := &sync.Mutex{}
		rederCount := func() int {
			lock.Lock()
			defer lock.Unlock()
			return count
		}
		increaseCount := func() {
			lock.Lock()
			defer lock.Unlock()
			count++
		}

		for range c.syncKeys {
			wg.Add(1)
		}
		runController := func(ctx context.Context, addonName string) error {
			defer wg.Done()
			increaseCount()
			return nil
		}
		obj := append(c.clusterManagementAddon, c.managedClusteraddon...) //nolint:gocritic
		fakeAddonClient := fakeaddon.NewSimpleClientset(obj...)

		addonInformers := addoninformers.NewSharedInformerFactory(fakeAddonClient, 10*time.Minute)

		for _, obj := range c.managedClusteraddon {
			if err := addonInformers.Addon().V1alpha1().ManagedClusterAddOns().Informer().GetStore().Add(obj); err != nil {
				t.Fatal(err)
			}
		}
		for _, obj := range c.clusterManagementAddon {
			if err := addonInformers.Addon().V1alpha1().ClusterManagementAddOns().Informer().GetStore().Add(obj); err != nil {
				t.Fatal(err)
			}
		}

		fakeDynamicClient := dynamicfake.NewSimpleDynamicClient(runtime.NewScheme())
		dynamicInformerFactory := dynamicinformer.NewDynamicSharedInformerFactory(fakeDynamicClient, 0)

		fakeClusterClient := fakecluster.NewSimpleClientset()
		clusterInformers := clusterv1informers.NewSharedInformerFactory(fakeClusterClient, 10*time.Minute)

		fakeWorkClient := fakework.NewSimpleClientset()
		workInformers := workinformers.NewSharedInformerFactory(fakeWorkClient, 10*time.Minute)

		hubKubeClient := fakekube.NewSimpleClientset()

		controller := NewAddonTemplateController(
			nil,
			hubKubeClient,
			fakeAddonClient,
			fakeWorkClient,
			addonInformers,
			clusterInformers,
			dynamicInformerFactory,
			workInformers,
			eventstesting.NewTestingEventRecorder(t),
			runController,
		)
		ctx := context.TODO()
		for _, syncKey := range c.syncKeys {
			syncContext := testingcommon.NewFakeSyncContext(t, syncKey)
			err := controller.Sync(ctx, syncContext)
			if err != nil {
				t.Errorf("expected no error when sync: %v", err)
			}
		}

		ch := make(chan struct{})
		go func() {
			defer close(ch)
			wg.Wait()
		}()

		select {
		case <-ch:
			actualCount := rederCount()
			if actualCount != c.expectedCount {
				t.Errorf("name : %s, expected runControllerFunc to be called %d, but was called %d times",
					c.name, c.expectedCount, actualCount)
			}
		case <-time.After(1 * time.Second):
			if !c.expectedTimeout {
				t.Errorf("name : %s, expected not timeout, but timeout", c.name)
			}
			actualCount := rederCount()
			if actualCount != c.expectedCount {
				t.Errorf("name : %s, expected runControllerFunc to be called %d, but was called %d times",
					c.name, c.expectedCount, actualCount)
			}
		}
	}
}

func TestRunController(t *testing.T) {
	cases := []struct {
		name        string
		addonName   string
		expectedErr string
	}{
		{
			name:        "addon name empty",
			addonName:   "",
			expectedErr: "addon name should be set",
		},
		{
			name:        "fake kubeconfig",
			addonName:   "test",
			expectedErr: `connect: connection refused`,
		},
	}

	for _, c := range cases {
		fakeAddonClient := fakeaddon.NewSimpleClientset()
		addonInformers := addoninformers.NewSharedInformerFactory(fakeAddonClient, 10*time.Minute)
		fakeDynamicClient := dynamicfake.NewSimpleDynamicClient(runtime.NewScheme())
		dynamicInformerFactory := dynamicinformer.NewDynamicSharedInformerFactory(fakeDynamicClient, 0)
		fakeClusterClient := fakecluster.NewSimpleClientset()
		clusterInformers := clusterv1informers.NewSharedInformerFactory(fakeClusterClient, 10*time.Minute)
		fakeWorkClient := fakework.NewSimpleClientset()
		workInformers := workinformers.NewSharedInformerFactory(fakeWorkClient, 10*time.Minute)
		hubKubeClient := fakekube.NewSimpleClientset()
		controller := &addonTemplateController{
			kubeConfig:       &rest.Config{},
			kubeClient:       hubKubeClient,
			addonClient:      fakeAddonClient,
			cmaLister:        addonInformers.Addon().V1alpha1().ClusterManagementAddOns().Lister(),
			addonManagers:    make(map[string]context.CancelFunc),
			addonInformers:   addonInformers,
			clusterInformers: clusterInformers,
			dynamicInformers: dynamicInformerFactory,
			workInformers:    workInformers,
		}
		ctx := context.TODO()

		err := controller.runController(ctx, c.addonName)
		if err == nil {
			assert.Empty(t, c.expectedErr)
		} else {
			assert.Contains(t, err.Error(), c.expectedErr, "name : %s, expected error %v, but got %v", c.name, c.expectedErr, err)
		}
	}
}
