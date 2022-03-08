package addonhealthcheck

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/openshift/library-go/pkg/operator/events/eventstesting"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	clienttesting "k8s.io/client-go/testing"
	"k8s.io/client-go/tools/cache"
	"open-cluster-management.io/addon-framework/pkg/addonmanager/addontesting"
	"open-cluster-management.io/addon-framework/pkg/addonmanager/constants"
	"open-cluster-management.io/addon-framework/pkg/agent"
	addonapiv1alpha1 "open-cluster-management.io/api/addon/v1alpha1"
	fakeaddon "open-cluster-management.io/api/client/addon/clientset/versioned/fake"
	addoninformers "open-cluster-management.io/api/client/addon/informers/externalversions"
	fakework "open-cluster-management.io/api/client/work/clientset/versioned/fake"
	workinformers "open-cluster-management.io/api/client/work/informers/externalversions"
	clusterv1 "open-cluster-management.io/api/cluster/v1"
	workapiv1 "open-cluster-management.io/api/work/v1"
)

type testAgent struct {
	name   string
	health *agent.HealthProber
}

func (t *testAgent) Manifests(cluster *clusterv1.ManagedCluster, addon *addonapiv1alpha1.ManagedClusterAddOn) ([]runtime.Object, error) {
	return nil, nil
}

func (t *testAgent) GetAgentAddonOptions() agent.AgentAddonOptions {
	return agent.AgentAddonOptions{
		AddonName:    t.name,
		HealthProber: t.health,
	}
}

func TestReconcile(t *testing.T) {
	cases := []struct {
		name                 string
		addon                []runtime.Object
		testaddon            *testAgent
		validateAddonActions func(t *testing.T, actions []clienttesting.Action)
	}{
		{
			name:  "no op if health checker is nil",
			addon: []runtime.Object{addontesting.NewAddon("test", "cluster1")},
			validateAddonActions: func(t *testing.T, actions []clienttesting.Action) {
				addontesting.AssertNoActions(t, actions)
			},
			testaddon: &testAgent{
				name:   "test",
				health: nil,
			},
		},
		{
			name:  "update addon health check mode",
			addon: []runtime.Object{addontesting.NewAddon("test", "cluster1")},
			validateAddonActions: func(t *testing.T, actions []clienttesting.Action) {
				addontesting.AssertActions(t, actions, "update")
				actual := actions[0].(clienttesting.UpdateActionImpl).Object
				addOn := actual.(*addonapiv1alpha1.ManagedClusterAddOn)
				if addOn.Status.HealthCheck.Mode != addonapiv1alpha1.HealthCheckModeCustomized {
					t.Errorf("Health check mode is not correct, expected %s but got %s",
						addonapiv1alpha1.HealthCheckModeCustomized, addOn.Status.HealthCheck.Mode)
				}
			},
			testaddon: &testAgent{
				name: "test",
				health: &agent.HealthProber{
					Type: agent.HealthProberTypeNone,
				},
			},
		},
		{
			name:  "no op if health checker mode is identical (None)",
			addon: []runtime.Object{NewAddonWithHealthCheck("test", "cluster1", addonapiv1alpha1.HealthCheckModeCustomized)},
			validateAddonActions: func(t *testing.T, actions []clienttesting.Action) {
				addontesting.AssertNoActions(t, actions)
			},
			testaddon: &testAgent{
				name: "test",
				health: &agent.HealthProber{
					Type: agent.HealthProberTypeNone,
				},
			},
		},
		{
			name:  "no op if health checker mode is identical (Lease)",
			addon: []runtime.Object{NewAddonWithHealthCheck("test", "cluster1", addonapiv1alpha1.HealthCheckModeLease)},
			validateAddonActions: func(t *testing.T, actions []clienttesting.Action) {
				addontesting.AssertNoActions(t, actions)
			},
			testaddon: &testAgent{
				name: "test",
				health: &agent.HealthProber{
					Type: agent.HealthProberTypeLease,
				},
			},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			fakeAddonClient := fakeaddon.NewSimpleClientset(c.addon...)

			addonInformers := addoninformers.NewSharedInformerFactory(fakeAddonClient, 10*time.Minute)

			for _, obj := range c.addon {
				addonInformers.Addon().V1alpha1().ManagedClusterAddOns().Informer().GetStore().Add(obj)
			}

			controller := addonHealthCheckController{
				addonClient:               fakeAddonClient,
				managedClusterAddonLister: addonInformers.Addon().V1alpha1().ManagedClusterAddOns().Lister(),
				agentAddons:               map[string]agent.AgentAddon{c.testaddon.name: c.testaddon},
				eventRecorder:             eventstesting.NewTestingEventRecorder(t),
			}

			for _, addon := range c.addon {
				key, _ := cache.MetaNamespaceKeyFunc(addon)
				syncContext := addontesting.NewFakeSyncContext(t, key)
				err := controller.sync(context.TODO(), syncContext)
				if err != nil {
					t.Errorf("expected no error when sync: %v", err)
				}
				c.validateAddonActions(t, fakeAddonClient.Actions())
			}

		})
	}
}

func NewAddonWithHealthCheck(name, namespace string, mode addonapiv1alpha1.HealthCheckMode) *addonapiv1alpha1.ManagedClusterAddOn {
	addon := addontesting.NewAddon(name, namespace)
	addon.Status.HealthCheck = addonapiv1alpha1.HealthCheck{Mode: mode}
	return addon
}

func TestReconcileWithWork(t *testing.T) {
	addon := NewAddonWithHealthCheck("test", "cluster1", addonapiv1alpha1.HealthCheckModeCustomized)

	fakeAddonClient := fakeaddon.NewSimpleClientset(addon)
	fakeWorkClient := fakework.NewSimpleClientset()

	addonInformers := addoninformers.NewSharedInformerFactory(fakeAddonClient, 10*time.Minute)
	workInformers := workinformers.NewSharedInformerFactory(fakeWorkClient, 10*time.Minute)

	addonInformers.Addon().V1alpha1().ManagedClusterAddOns().Informer().GetStore().Add(addon)

	testaddon := &testAgent{
		name: "test",
		health: &agent.HealthProber{
			Type: agent.HealthProberTypeWork,
		},
	}

	controller := addonHealthCheckController{
		addonClient:               fakeAddonClient,
		managedClusterAddonLister: addonInformers.Addon().V1alpha1().ManagedClusterAddOns().Lister(),
		workLister:                workInformers.Work().V1().ManifestWorks().Lister(),
		agentAddons:               map[string]agent.AgentAddon{testaddon.name: testaddon},
		eventRecorder:             eventstesting.NewTestingEventRecorder(t),
	}

	key, _ := cache.MetaNamespaceKeyFunc(addon)
	syncContext := addontesting.NewFakeSyncContext(t, key)
	err := controller.sync(context.TODO(), syncContext)
	if err != nil {
		t.Errorf("expected no error when sync: %v", err)
	}

	addontesting.AssertActions(t, fakeAddonClient.Actions(), "update")
	actual := fakeAddonClient.Actions()[0].(clienttesting.UpdateActionImpl).Object
	addOn := actual.(*addonapiv1alpha1.ManagedClusterAddOn)
	if meta.IsStatusConditionTrue(addOn.Status.Conditions, "Available") {
		t.Errorf("addon condition should be unavailable: %v", addOn.Status.Conditions)
	}

	fakeAddonClient.ClearActions()
	work := &workapiv1.ManifestWork{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: addon.Namespace,
			Name:      constants.DeployWorkName(addon.Name),
		},
	}

	workInformers.Work().V1().ManifestWorks().Informer().GetStore().Add(work)

	err = controller.sync(context.TODO(), syncContext)
	if err != nil {
		t.Errorf("expected no error when sync: %v", err)
	}

	addontesting.AssertActions(t, fakeAddonClient.Actions(), "update")
	actual = fakeAddonClient.Actions()[0].(clienttesting.UpdateActionImpl).Object
	addOn = actual.(*addonapiv1alpha1.ManagedClusterAddOn)

	cond := meta.FindStatusCondition(addOn.Status.Conditions, "Available")
	if cond == nil && cond.Status != metav1.ConditionUnknown {
		t.Errorf("addon condition should be unknown: %v", addOn.Status.Conditions)
	}

	work.Status = workapiv1.ManifestWorkStatus{
		Conditions: []metav1.Condition{
			{
				Type:   workapiv1.WorkAvailable,
				Status: metav1.ConditionTrue,
			},
		},
	}

	fakeAddonClient.ClearActions()
	workInformers.Work().V1().ManifestWorks().Informer().GetStore().Update(work)

	err = controller.sync(context.TODO(), syncContext)
	if err != nil {
		t.Errorf("expected no error when sync: %v", err)
	}

	addontesting.AssertActions(t, fakeAddonClient.Actions(), "update")
	actual = fakeAddonClient.Actions()[0].(clienttesting.UpdateActionImpl).Object
	addOn = actual.(*addonapiv1alpha1.ManagedClusterAddOn)

	cond = meta.FindStatusCondition(addOn.Status.Conditions, "Available")
	if cond == nil && cond.Status != metav1.ConditionTrue {
		t.Errorf("addon condition should be available: %v", addOn.Status.Conditions)
	}
}

type testProbe struct {
	checkError error
}

func (p *testProbe) ProbeFields() []agent.ProbeField {
	return []agent.ProbeField{
		{
			ResourceIdentifier: workapiv1.ResourceIdentifier{
				Resource:  "tests",
				Name:      "test",
				Namespace: "testns",
			},
			ProbeRules: []workapiv1.FeedbackRule{
				{
					Type: workapiv1.WellKnownStatusType,
				},
			},
		},
	}
}

// HealthCheck check status of the addon based on probe result.
func (p *testProbe) HealthCheck(workapiv1.ResourceIdentifier, workapiv1.StatusFeedbackResult) error {
	return p.checkError
}

func TestReconcileWithProbe(t *testing.T) {
	addon := NewAddonWithHealthCheck("test", "cluster1", addonapiv1alpha1.HealthCheckModeCustomized)
	work := &workapiv1.ManifestWork{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: addon.Namespace,
			Name:      constants.DeployWorkName(addon.Name),
		},
		Status: workapiv1.ManifestWorkStatus{
			Conditions: []metav1.Condition{
				{
					Type:   workapiv1.WorkAvailable,
					Status: metav1.ConditionTrue,
				},
			},
		},
	}

	fakeAddonClient := fakeaddon.NewSimpleClientset(addon)
	fakeWorkClient := fakework.NewSimpleClientset(work)

	addonInformers := addoninformers.NewSharedInformerFactory(fakeAddonClient, 10*time.Minute)
	workInformers := workinformers.NewSharedInformerFactory(fakeWorkClient, 10*time.Minute)

	addonInformers.Addon().V1alpha1().ManagedClusterAddOns().Informer().GetStore().Add(addon)
	workInformers.Work().V1().ManifestWorks().Informer().GetStore().Add(work)

	prober := &testProbe{
		checkError: fmt.Errorf("health check fails"),
	}

	testaddon := &testAgent{
		name: "test",
		health: &agent.HealthProber{
			Type: agent.HealthProberTypeWork,
			WorkProber: &agent.WorkHealthProber{
				ProbeFields: prober.ProbeFields(),
				HealthCheck: prober.HealthCheck,
			},
		},
	}

	controller := addonHealthCheckController{
		addonClient:               fakeAddonClient,
		managedClusterAddonLister: addonInformers.Addon().V1alpha1().ManagedClusterAddOns().Lister(),
		workLister:                workInformers.Work().V1().ManifestWorks().Lister(),
		agentAddons:               map[string]agent.AgentAddon{testaddon.name: testaddon},
		eventRecorder:             eventstesting.NewTestingEventRecorder(t),
	}

	key, _ := cache.MetaNamespaceKeyFunc(addon)
	syncContext := addontesting.NewFakeSyncContext(t, key)
	err := controller.sync(context.TODO(), syncContext)
	if err != nil {
		t.Errorf("expected no error when sync: %v", err)
	}

	// return unknown if no status are found
	addontesting.AssertActions(t, fakeAddonClient.Actions(), "update")
	actual := fakeAddonClient.Actions()[0].(clienttesting.UpdateActionImpl).Object
	addOn := actual.(*addonapiv1alpha1.ManagedClusterAddOn)
	cond := meta.FindStatusCondition(addOn.Status.Conditions, "Available")
	if cond == nil && cond.Status != metav1.ConditionUnknown {
		t.Errorf("addon condition should be unknown: %v", addOn.Status.Conditions)
	}

	work.Status.ResourceStatus = workapiv1.ManifestResourceStatus{
		Manifests: []workapiv1.ManifestCondition{
			{
				ResourceMeta: workapiv1.ManifestResourceMeta{
					Resource:  "tests",
					Name:      "test",
					Namespace: "testns",
				},
				StatusFeedbacks: workapiv1.StatusFeedbackResult{
					Values: []workapiv1.FeedbackValue{
						{
							Name: "noop",
						},
					},
				},
			},
		},
	}

	fakeAddonClient.ClearActions()
	workInformers.Work().V1().ManifestWorks().Informer().GetStore().Update(work)

	err = controller.sync(context.TODO(), syncContext)
	if err != nil {
		t.Errorf("expected no error when sync: %v", err)
	}

	// return unavailable if check returns err
	addontesting.AssertActions(t, fakeAddonClient.Actions(), "update")
	actual = fakeAddonClient.Actions()[0].(clienttesting.UpdateActionImpl).Object
	addOn = actual.(*addonapiv1alpha1.ManagedClusterAddOn)

	if !meta.IsStatusConditionFalse(addOn.Status.Conditions, "Available") {
		t.Errorf("addon condition should be unavailable: %v", addOn.Status.Conditions)
	}

	prober.checkError = nil
	fakeAddonClient.ClearActions()
	err = controller.sync(context.TODO(), syncContext)
	if err != nil {
		t.Errorf("expected no error when sync: %v", err)
	}

	// return available if check returns nil
	addontesting.AssertActions(t, fakeAddonClient.Actions(), "update")
	actual = fakeAddonClient.Actions()[0].(clienttesting.UpdateActionImpl).Object
	addOn = actual.(*addonapiv1alpha1.ManagedClusterAddOn)
	if !meta.IsStatusConditionTrue(addOn.Status.Conditions, "Available") {
		t.Errorf("addon condition should be available: %v", addOn.Status.Conditions)
	}
}
