package clustermanagercontroller

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/openshift/library-go/pkg/operator/events"
	"github.com/openshift/library-go/pkg/operator/events/eventstesting"
	"github.com/openshift/library-go/pkg/operator/resource/resourceapply"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	apiextensionsclient "k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset"
	fakeapiextensions "k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset/fake"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	kubeinformers "k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	fakekube "k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/rest"
	clienttesting "k8s.io/client-go/testing"
	fakeoperatorlient "open-cluster-management.io/api/client/operator/clientset/versioned/fake"
	operatorinformers "open-cluster-management.io/api/client/operator/informers/externalversions"
	operatorapiv1 "open-cluster-management.io/api/operator/v1"
	fakemigrationclient "sigs.k8s.io/kube-storage-version-migrator/pkg/clients/clientset/fake"
	migrationclient "sigs.k8s.io/kube-storage-version-migrator/pkg/clients/clientset/typed/migration/v1alpha1"

	"open-cluster-management.io/registration-operator/pkg/helpers"
	testinghelper "open-cluster-management.io/registration-operator/pkg/helpers/testing"
)

var (
	ctx = context.Background()
)

type testController struct {
	clusterManagerController *clusterManagerController
	managementKubeClient     *fakekube.Clientset
	hubKubeClient            *fakekube.Clientset
	apiExtensionClient       *fakeapiextensions.Clientset
	operatorClient           *fakeoperatorlient.Clientset
}

func newClusterManager(name string) *operatorapiv1.ClusterManager {
	return &operatorapiv1.ClusterManager{
		ObjectMeta: metav1.ObjectMeta{
			Name:       name,
			Finalizers: []string{clusterManagerFinalizer},
		},
		Spec: operatorapiv1.ClusterManagerSpec{
			RegistrationImagePullSpec: "testregistration",
			DeployOption: operatorapiv1.ClusterManagerDeployOption{
				Mode: operatorapiv1.InstallModeDefault,
			},
			AddOnManagerConfiguration: &operatorapiv1.AddOnManagerConfiguration{
				Mode: operatorapiv1.ComponentModeTypeEnable,
			},
		},
	}
}

func newTestController(t *testing.T, clustermanager *operatorapiv1.ClusterManager) *testController {
	kubeClient := fakekube.NewSimpleClientset()
	kubeInfomers := kubeinformers.NewSharedInformerFactory(kubeClient, 5*time.Minute)
	fakeOperatorClient := fakeoperatorlient.NewSimpleClientset(clustermanager)
	operatorInformers := operatorinformers.NewSharedInformerFactory(fakeOperatorClient, 5*time.Minute)

	clusterManagerController := &clusterManagerController{
		clusterManagerClient: fakeOperatorClient.OperatorV1().ClusterManagers(),
		clusterManagerLister: operatorInformers.Operator().V1().ClusterManagers().Lister(),
		configMapLister:      kubeInfomers.Core().V1().ConfigMaps().Lister(),
		cache:                resourceapply.NewResourceCache(),
	}

	store := operatorInformers.Operator().V1().ClusterManagers().Informer().GetStore()
	if err := store.Add(clustermanager); err != nil {
		t.Fatal(err)
	}

	return &testController{
		clusterManagerController: clusterManagerController,
		operatorClient:           fakeOperatorClient,
	}
}

func setDeployment(clusterManagerName, clusterManagerNamespace string) []runtime.Object {
	var replicas = int32(1)

	return []runtime.Object{
		&appsv1.Deployment{
			ObjectMeta: metav1.ObjectMeta{
				Name:       clusterManagerName + "-registration-webhook",
				Namespace:  clusterManagerNamespace,
				Generation: 1,
			},
			Spec: appsv1.DeploymentSpec{
				Template: corev1.PodTemplateSpec{
					Spec: corev1.PodSpec{
						Containers: []corev1.Container{
							{
								Name: clusterManagerName + "-webhook",
							},
						},
					},
				},
				Replicas: &replicas,
			},
			Status: appsv1.DeploymentStatus{
				ReadyReplicas:      replicas,
				ObservedGeneration: 1,
			},
		},
		&appsv1.Deployment{
			ObjectMeta: metav1.ObjectMeta{
				Name:       clusterManagerName + "-registration-controller",
				Namespace:  clusterManagerNamespace,
				Generation: 1,
			},
			Spec: appsv1.DeploymentSpec{
				Template: corev1.PodTemplateSpec{
					Spec: corev1.PodSpec{
						Containers: []corev1.Container{
							{
								Name: "hub-registration-controller",
							},
						},
					},
				},
				Replicas: &replicas,
			},
			Status: appsv1.DeploymentStatus{
				ReadyReplicas:      replicas,
				ObservedGeneration: 1,
			},
		},
		&appsv1.Deployment{
			ObjectMeta: metav1.ObjectMeta{
				Name:       clusterManagerName + "-work-webhook",
				Namespace:  clusterManagerNamespace,
				Generation: 1,
			},
			Spec: appsv1.DeploymentSpec{
				Template: corev1.PodTemplateSpec{
					Spec: corev1.PodSpec{
						Containers: []corev1.Container{
							{
								Name: clusterManagerName + "-webhook",
							},
						},
					},
				},
				Replicas: &replicas,
			},
			Status: appsv1.DeploymentStatus{
				ReadyReplicas:      replicas,
				ObservedGeneration: 1,
			},
		},
		&appsv1.Deployment{
			ObjectMeta: metav1.ObjectMeta{
				Name:       clusterManagerName + "-placement-controller",
				Namespace:  clusterManagerNamespace,
				Generation: 1,
			},
			Spec: appsv1.DeploymentSpec{
				Template: corev1.PodTemplateSpec{
					Spec: corev1.PodSpec{
						Containers: []corev1.Container{
							{
								Name: "clustermanager-placement-controller",
							},
						},
					},
				},
				Replicas: &replicas,
			},
			Status: appsv1.DeploymentStatus{
				ReadyReplicas:      replicas,
				ObservedGeneration: 1,
			},
		},
		&appsv1.Deployment{
			ObjectMeta: metav1.ObjectMeta{
				Name:       clusterManagerName + "-addon-manager-controller",
				Namespace:  clusterManagerNamespace,
				Generation: 1,
			},
			Spec: appsv1.DeploymentSpec{
				Template: corev1.PodTemplateSpec{
					Spec: corev1.PodSpec{
						Containers: []corev1.Container{
							{
								Name: "addon-manager-controller",
							},
						},
					},
				},
				Replicas: &replicas,
			},
			Status: appsv1.DeploymentStatus{
				ReadyReplicas:      replicas,
				ObservedGeneration: 1,
			},
		},
	}
}

func setup(t *testing.T, tc *testController, cd []runtime.Object, crds ...runtime.Object) {
	fakeHubKubeClient := fakekube.NewSimpleClientset()
	fakeManagementKubeClient := fakekube.NewSimpleClientset(cd...)
	fakeAPIExtensionClient := fakeapiextensions.NewSimpleClientset(crds...)
	fakeMigrationClient := fakemigrationclient.NewSimpleClientset()

	// set clients in test controller
	tc.apiExtensionClient = fakeAPIExtensionClient
	tc.hubKubeClient = fakeHubKubeClient
	tc.managementKubeClient = fakeManagementKubeClient

	// set clients in clustermanager controller
	tc.clusterManagerController.recorder = eventstesting.NewTestingEventRecorder(t)
	tc.clusterManagerController.operatorKubeClient = fakeManagementKubeClient
	tc.clusterManagerController.generateHubClusterClients = func(hubKubeConfig *rest.Config) (kubernetes.Interface, apiextensionsclient.Interface, migrationclient.StorageVersionMigrationsGetter, error) {
		return fakeHubKubeClient, fakeAPIExtensionClient, fakeMigrationClient.MigrationV1alpha1(), nil
	}
	tc.clusterManagerController.ensureSAKubeconfigs = func(ctx context.Context, clusterManagerName, clusterManagerNamespace string, hubConfig *rest.Config, hubClient, managementClient kubernetes.Interface, recorder events.Recorder) error {
		return nil
	}
}

func ensureObject(t *testing.T, object runtime.Object, hubCore *operatorapiv1.ClusterManager) {
	access, err := meta.Accessor(object)
	if err != nil {
		t.Errorf("Unable to access objectmeta: %v", err)
	}

	switch o := object.(type) {
	case *corev1.Namespace:
		testinghelper.AssertEqualNameNamespace(t, access.GetName(), "", helpers.ClusterManagerNamespace(hubCore.Name, hubCore.Spec.DeployOption.Mode), "")
	case *appsv1.Deployment:
		if strings.Contains(o.Name, "registration") && hubCore.Spec.RegistrationImagePullSpec != o.Spec.Template.Spec.Containers[0].Image {
			t.Errorf("Registration image does not match to the expected.")
		}
		if strings.Contains(o.Name, "placement") && hubCore.Spec.PlacementImagePullSpec != o.Spec.Template.Spec.Containers[0].Image {
			t.Errorf("Placement image does not match to the expected.")
		}
		if strings.Contains(o.Name, "addon-manager") && hubCore.Spec.AddOnManagerImagePullSpec != o.Spec.Template.Spec.Containers[0].Image {
			t.Errorf("AddOnManager image does not match to the expected.")
		}
	}
}

// TestSyncDeploy tests sync manifests of hub component
func TestSyncDeploy(t *testing.T) {
	clusterManager := newClusterManager("testhub")
	tc := newTestController(t, clusterManager)
	clusterManagerNamespace := helpers.ClusterManagerNamespace(clusterManager.Name, clusterManager.Spec.DeployOption.Mode)
	cd := setDeployment(clusterManager.Name, clusterManagerNamespace)
	setup(t, tc, cd)

	syncContext := testinghelper.NewFakeSyncContext(t, "testhub")

	err := tc.clusterManagerController.sync(ctx, syncContext)
	if err != nil {
		t.Fatalf("Expected no error when sync, %v", err)
	}

	createKubeObjects := []runtime.Object{}
	kubeActions := append(tc.hubKubeClient.Actions(), tc.managementKubeClient.Actions()...) // record objects from both hub and management cluster
	for _, action := range kubeActions {
		if action.GetVerb() == "create" {
			object := action.(clienttesting.CreateActionImpl).Object
			createKubeObjects = append(createKubeObjects, object)
		}
	}

	// Check if resources are created as expected
	// We expect creat the namespace twice respectively in the management cluster and the hub cluster.
	testinghelper.AssertEqualNumber(t, len(createKubeObjects), 24)
	for _, object := range createKubeObjects {
		ensureObject(t, object, clusterManager)
	}

	createCRDObjects := []runtime.Object{}
	crdActions := tc.apiExtensionClient.Actions()
	for _, action := range crdActions {
		if action.GetVerb() == "create" {
			object := action.(clienttesting.CreateActionImpl).Object
			createCRDObjects = append(createCRDObjects, object)
		}
	}
	// Check if resources are created as expected
	testinghelper.AssertEqualNumber(t, len(createCRDObjects), 10)
}

func TestSyncDeployNoWebhook(t *testing.T) {
	clusterManager := newClusterManager("testhub")
	tc := newTestController(t, clusterManager)
	setup(t, tc, nil)

	syncContext := testinghelper.NewFakeSyncContext(t, "testhub")

	err := tc.clusterManagerController.sync(ctx, syncContext)
	if err != nil {
		t.Fatalf("Expected no error when sync, %v", err)
	}

	createKubeObjects := []runtime.Object{}
	kubeActions := append(tc.hubKubeClient.Actions(), tc.managementKubeClient.Actions()...) // record objects from both hub and management cluster
	for _, action := range kubeActions {
		if action.GetVerb() == "create" {
			object := action.(clienttesting.CreateActionImpl).Object
			createKubeObjects = append(createKubeObjects, object)
		}
	}

	// Check if resources are created as expected
	// We expect creat the namespace twice respectively in the management cluster and the hub cluster.
	testinghelper.AssertEqualNumber(t, len(createKubeObjects), 24)
	for _, object := range createKubeObjects {
		ensureObject(t, object, clusterManager)
	}

	createCRDObjects := []runtime.Object{}
	crdActions := tc.apiExtensionClient.Actions()
	for _, action := range crdActions {
		if action.GetVerb() == "create" {
			object := action.(clienttesting.CreateActionImpl).Object
			createCRDObjects = append(createCRDObjects, object)
		}
	}
	// Check if resources are created as expected
	testinghelper.AssertEqualNumber(t, len(createCRDObjects), 10)
}

// TestSyncDelete test cleanup hub deploy
func TestSyncDelete(t *testing.T) {
	clusterManager := newClusterManager("testhub")
	now := metav1.Now()
	clusterManager.ObjectMeta.SetDeletionTimestamp(&now)

	tc := newTestController(t, clusterManager)
	setup(t, tc, nil)

	syncContext := testinghelper.NewFakeSyncContext(t, "testhub")
	clusterManagerNamespace := helpers.ClusterManagerNamespace(clusterManager.Name, clusterManager.Spec.DeployOption.Mode)

	err := tc.clusterManagerController.sync(ctx, syncContext)
	if err != nil {
		t.Fatalf("Expected non error when sync, %v", err)
	}

	deleteKubeActions := []clienttesting.DeleteActionImpl{}
	kubeActions := append(tc.hubKubeClient.Actions(), tc.managementKubeClient.Actions()...)
	for _, action := range kubeActions {
		if action.GetVerb() == "delete" {
			deleteKubeAction := action.(clienttesting.DeleteActionImpl)
			deleteKubeActions = append(deleteKubeActions, deleteKubeAction)
		}
	}
	testinghelper.AssertEqualNumber(t, len(deleteKubeActions), 24) // delete namespace both from the hub cluster and the mangement cluster

	deleteCRDActions := []clienttesting.DeleteActionImpl{}
	crdActions := tc.apiExtensionClient.Actions()
	for _, action := range crdActions {
		if action.GetVerb() == "delete" {
			deleteCRDAction := action.(clienttesting.DeleteActionImpl)
			deleteCRDActions = append(deleteCRDActions, deleteCRDAction)
		}
	}
	// Check if resources are created as expected
	testinghelper.AssertEqualNumber(t, len(deleteCRDActions), 13)

	for _, action := range deleteKubeActions {
		switch action.Resource.Resource {
		case "namespaces":
			testinghelper.AssertEqualNameNamespace(t, action.Name, "", clusterManagerNamespace, "")
		}
	}
}

// TestDeleteCRD test delete crds
func TestDeleteCRD(t *testing.T) {
	clusterManager := newClusterManager("testhub")
	now := metav1.Now()
	clusterManager.ObjectMeta.SetDeletionTimestamp(&now)
	crd := &apiextensionsv1.CustomResourceDefinition{
		ObjectMeta: metav1.ObjectMeta{
			Name: "clustermanagementaddons.addon.open-cluster-management.io",
		},
	}

	tc := newTestController(t, clusterManager)
	setup(t, tc, nil, crd)

	// Return crd with the first get, and return not found with the 2nd get
	getCount := 0
	tc.apiExtensionClient.PrependReactor("get", "customresourcedefinitions", func(action clienttesting.Action) (handled bool, ret runtime.Object, err error) {
		if getCount == 0 {
			getCount = getCount + 1
			return true, crd, nil
		}
		return true, &apiextensionsv1.CustomResourceDefinition{}, errors.NewNotFound(
			apiextensionsv1.Resource("customresourcedefinitions"), "clustermanagementaddons.addon.open-cluster-management.io")

	})
	syncContext := testinghelper.NewFakeSyncContext(t, "testhub")
	err := tc.clusterManagerController.sync(ctx, syncContext)
	if err == nil {
		t.Fatalf("Expected error when sync at first time")
	}

	err = tc.clusterManagerController.sync(ctx, syncContext)
	if err != nil {
		t.Fatalf("Expected no error when sync at second time: %v", err)
	}
}

func TestIsIPFormat(t *testing.T) {
	cases := []struct {
		address    string
		isIPFormat bool
	}{
		{
			address:    "127.0.0.1",
			isIPFormat: true,
		},
		{
			address:    "localhost",
			isIPFormat: false,
		},
	}
	for _, c := range cases {
		if isIPFormat(c.address) != c.isIPFormat {
			t.Fatalf("expected %v, got %v", c.isIPFormat, isIPFormat(c.address))
		}
	}
}
