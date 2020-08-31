package klusterletcontroller

import (
	"fmt"
	"strings"
	"testing"
	"time"

	fakeoperatorclient "github.com/open-cluster-management/api/client/operator/clientset/versioned/fake"
	operatorinformers "github.com/open-cluster-management/api/client/operator/informers/externalversions"
	opratorapiv1 "github.com/open-cluster-management/api/operator/v1"
	"github.com/open-cluster-management/registration-operator/pkg/helpers"
	testinghelper "github.com/open-cluster-management/registration-operator/pkg/helpers/testing"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	fakeapiextensions "k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset/fake"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/version"
	fakekube "k8s.io/client-go/kubernetes/fake"
	clienttesting "k8s.io/client-go/testing"
	"k8s.io/client-go/tools/cache"
)

type testController struct {
	controller         *klusterletController
	kubeClient         *fakekube.Clientset
	apiExtensionClient *fakeapiextensions.Clientset
	operatorClient     *fakeoperatorclient.Clientset
	operatorStore      cache.Store
}

func newSecret(name, namespace string) *corev1.Secret {
	return &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Data: map[string][]byte{},
	}
}

func newWorkAgentDeployment(klusterletName, clusterName string) *appsv1.Deployment {
	return &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("%s-work-agent", klusterletName),
			Namespace: helpers.KlusterletDefaultNamespace,
		},
		Spec: appsv1.DeploymentSpec{
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Args: []string{"/work", "agent", fmt.Sprintf("--spoke-cluster-name=%s", clusterName)},
						},
					},
				},
			},
		},
	}
}

func newKlusterlet(name, namespace, clustername string) *opratorapiv1.Klusterlet {
	return &opratorapiv1.Klusterlet{
		ObjectMeta: metav1.ObjectMeta{
			Name:       name,
			Finalizers: []string{klusterletFinalizer},
		},
		Spec: opratorapiv1.KlusterletSpec{
			RegistrationImagePullSpec: "testregistration",
			WorkImagePullSpec:         "testwork",
			ClusterName:               clustername,
			Namespace:                 namespace,
			ExternalServerURLs:        []opratorapiv1.ServerURL{},
		},
	}
}

func newNamespace(name string) *corev1.Namespace {
	return &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
	}
}

func newTestController(klusterlet *opratorapiv1.Klusterlet, objects ...runtime.Object) *testController {
	fakeKubeClient := fakekube.NewSimpleClientset(objects...)
	fakeAPIExtensionClient := fakeapiextensions.NewSimpleClientset()
	fakeOperatorClient := fakeoperatorclient.NewSimpleClientset(klusterlet)
	operatorInformers := operatorinformers.NewSharedInformerFactory(fakeOperatorClient, 5*time.Minute)
	kubeVersion, _ := version.ParseGeneric("v1.18.0")

	hubController := &klusterletController{
		klusterletClient:   fakeOperatorClient.OperatorV1().Klusterlets(),
		kubeClient:         fakeKubeClient,
		apiExtensionClient: fakeAPIExtensionClient,
		klusterletLister:   operatorInformers.Operator().V1().Klusterlets().Lister(),
		kubeVersion:        kubeVersion,
		operatorNamespace:  "open-cluster-management",
	}

	store := operatorInformers.Operator().V1().Klusterlets().Informer().GetStore()
	store.Add(klusterlet)

	return &testController{
		controller:         hubController,
		kubeClient:         fakeKubeClient,
		apiExtensionClient: fakeAPIExtensionClient,
		operatorClient:     fakeOperatorClient,
		operatorStore:      store,
	}
}

func ensureDeployments(t *testing.T, actions []clienttesting.Action, verb, serverURL, registrationClusterName, workClusterName string, count int) {
	deployments := []*appsv1.Deployment{}
	for _, action := range actions {
		if action.GetVerb() != verb || action.GetResource().Resource != "deployments" {
			continue
		}

		if verb == "create" {
			object := action.(clienttesting.CreateActionImpl).Object
			deployments = append(deployments, object.(*appsv1.Deployment))
		}

		if verb == "update" {
			object := action.(clienttesting.UpdateActionImpl).Object
			deployments = append(deployments, object.(*appsv1.Deployment))
		}
	}

	if len(deployments) != count {
		t.Errorf("Expect %s %d deployment, actual  %d", verb, count, len(deployments))
	}

	for _, deployment := range deployments {
		if strings.HasSuffix(deployment.Name, "registration-agent") {
			ensureRegistrationDeployment(t, deployment, serverURL, registrationClusterName)
		} else if strings.HasSuffix(deployment.Name, "work-agent") {
			ensureWorkDeployment(t, deployment, workClusterName)
		} else {
			t.Errorf("Unexpected deployment name %s", deployment.Name)
		}
	}
}

func ensureRegistrationDeployment(t *testing.T, deployment *appsv1.Deployment, serverURL, clusterName string) {
	if len(deployment.Spec.Template.Spec.Containers) != 1 {
		t.Errorf("Expect 1 containers in deployment spec, actual %d", len(deployment.Spec.Template.Spec.Containers))
	}
	args := deployment.Spec.Template.Spec.Containers[0].Args
	if serverURL == "" && len(args) != 4 {
		t.Errorf("Expect 4 args in container spec, actual %d", len(args))
	}
	if serverURL != "" && len(deployment.Spec.Template.Spec.Containers[0].Args) != 5 {
		t.Errorf("Expect 5 args in container spec, actual %d", len(args))
	}
	clusterNameArg := ""
	serverURLArg := ""
	for _, arg := range args {
		if strings.HasPrefix(arg, "--cluster-name=") {
			clusterNameArg = arg
		}
		if strings.HasPrefix(arg, "--spoke-external-server-urls=") {
			serverURLArg = arg
		}
	}

	desiredServerURLArg := ""
	if serverURL != "" {
		desiredServerURLArg = fmt.Sprintf("--spoke-external-server-urls=%s", serverURL)
	}
	if serverURLArg != desiredServerURLArg {
		t.Errorf("Server url args not correct, expect %q, actual %q", desiredServerURLArg, serverURLArg)
	}

	desiredClusterNameArg := fmt.Sprintf("--cluster-name=%s", clusterName)
	if clusterNameArg != desiredClusterNameArg {
		t.Errorf("Cluster name arg not correct, expect %q, actual %q", desiredClusterNameArg, clusterNameArg)
	}
}

func ensureWorkDeployment(t *testing.T, deployment *appsv1.Deployment, clusterName string) {
	if len(deployment.Spec.Template.Spec.Containers) != 1 {
		t.Errorf("Expect 1 containers in deployment spec, actual %d", len(deployment.Spec.Template.Spec.Containers))
	}
	args := deployment.Spec.Template.Spec.Containers[0].Args
	if len(args) != 4 {
		t.Errorf("Expect 4 args in container spec, actual %d", len(args))
	}
	clusterNameArg := ""
	for _, arg := range args {
		if strings.HasPrefix(arg, "--spoke-cluster-name") {
			clusterNameArg = arg
		}
	}
	desiredClusterNameArg := fmt.Sprintf("--spoke-cluster-name=%s", clusterName)
	if desiredClusterNameArg != clusterNameArg {
		t.Errorf("Expect cluster namee arg is %q, actual %q", desiredClusterNameArg, clusterNameArg)
	}
}

func ensureObject(t *testing.T, object runtime.Object, klusterlet *opratorapiv1.Klusterlet) {
	access, err := meta.Accessor(object)
	if err != nil {
		t.Errorf("Unable to access objectmeta: %v", err)
	}

	switch o := object.(type) {
	case *appsv1.Deployment:
		if strings.Contains(access.GetName(), "registration") {
			testinghelper.AssertEqualNameNamespace(
				t, access.GetName(), access.GetNamespace(),
				fmt.Sprintf("%s-registration-agent", klusterlet.Name), klusterlet.Spec.Namespace)
			if klusterlet.Spec.RegistrationImagePullSpec != o.Spec.Template.Spec.Containers[0].Image {
				t.Errorf("Image does not match to the expected.")
			}
		} else if strings.Contains(access.GetName(), "work") {
			testinghelper.AssertEqualNameNamespace(
				t, access.GetName(), access.GetNamespace(),
				fmt.Sprintf("%s-work-agent", klusterlet.Name), klusterlet.Spec.Namespace)
			if klusterlet.Spec.WorkImagePullSpec != o.Spec.Template.Spec.Containers[0].Image {
				t.Errorf("Image does not match to the expected.")
			}
		} else {
			t.Errorf("Unexpected deployment")
		}
	}
}

// TestSyncDeploy test deployment of klusterlet components
func TestSyncDeploy(t *testing.T) {
	klusterlet := newKlusterlet("klusterlet", "testns", "cluster1")
	bootStrapSecret := newSecret(helpers.BootstrapHubKubeConfigSecret, "testns")
	hubKubeConfigSecret := newSecret(helpers.HubKubeConfigSecret, "testns")
	hubKubeConfigSecret.Data["kubeconfig"] = []byte("dummuykubeconnfig")
	namespace := newNamespace("testns")
	controller := newTestController(klusterlet, bootStrapSecret, hubKubeConfigSecret, namespace)
	syncContext := testinghelper.NewFakeSyncContext(t, "klusterlet")

	err := controller.controller.sync(nil, syncContext)
	if err != nil {
		t.Errorf("Expected non error when sync, %v", err)
	}

	createObjects := []runtime.Object{}
	kubeActions := controller.kubeClient.Actions()
	for _, action := range kubeActions {
		if action.GetVerb() == "create" {
			object := action.(clienttesting.CreateActionImpl).Object
			createObjects = append(createObjects, object)
		}
	}

	// Check if resources are created as expected
	if len(createObjects) != 11 {
		t.Errorf("Expect 11 objects created in the sync loop, actual %d", len(createObjects))
	}
	for _, object := range createObjects {
		ensureObject(t, object, klusterlet)
	}

	apiExtenstionAction := controller.apiExtensionClient.Actions()
	createCRDObjects := []runtime.Object{}
	for _, action := range apiExtenstionAction {
		if action.GetVerb() == "create" && action.GetResource().Resource == "customresourcedefinitions" {
			object := action.(clienttesting.CreateActionImpl).Object
			createCRDObjects = append(createCRDObjects, object)
		}
	}
	if len(createCRDObjects) != 1 {
		t.Errorf("Expect 1 objects created in the sync loop, actual %d", len(createCRDObjects))
	}

	operatorAction := controller.operatorClient.Actions()
	if len(operatorAction) != 2 {
		t.Errorf("Expect 2 actions in the sync loop, actual %#v", operatorAction)
	}

	testinghelper.AssertGet(t, operatorAction[0], "operator.open-cluster-management.io", "v1", "klusterlets")
	testinghelper.AssertAction(t, operatorAction[1], "update")
	testinghelper.AssertOnlyConditions(
		t, operatorAction[1].(clienttesting.UpdateActionImpl).Object,
		testinghelper.NamedCondition(klusterletApplied, "KlusterletApplied", metav1.ConditionTrue))
}

// TestSyncDelete test cleanup hub deploy
func TestSyncDelete(t *testing.T) {
	klusterlet := newKlusterlet("klusterlet", "testns", "")
	now := metav1.Now()
	klusterlet.ObjectMeta.SetDeletionTimestamp(&now)
	namespace := newNamespace("testns")
	controller := newTestController(klusterlet, namespace)
	syncContext := testinghelper.NewFakeSyncContext(t, "klusterlet")

	err := controller.controller.sync(nil, syncContext)
	if err != nil {
		t.Errorf("Expected non error when sync, %v", err)
	}

	deleteActions := []clienttesting.DeleteActionImpl{}
	kubeActions := controller.kubeClient.Actions()
	for _, action := range kubeActions {
		if action.GetVerb() == "delete" {
			deleteAction := action.(clienttesting.DeleteActionImpl)
			deleteActions = append(deleteActions, deleteAction)
		}
	}

	if len(kubeActions) != 13 {
		t.Errorf("Expected 13 delete actions, but got %d", len(kubeActions))
	}

	deleteCRDActions := []clienttesting.DeleteActionImpl{}
	crdActions := controller.apiExtensionClient.Actions()
	for _, action := range crdActions {
		if action.GetVerb() == "delete" {
			deleteAction := action.(clienttesting.DeleteActionImpl)
			deleteActions = append(deleteCRDActions, deleteAction)
		}
	}

	if len(crdActions) != 0 {
		t.Errorf("Expected 0 delete actions, but got %d", len(crdActions))
	}
}

// TestGetServersFromKlusterlet tests getServersFromKlusterlet func
func TestGetServersFromKlusterlet(t *testing.T) {
	cases := []struct {
		name     string
		servers  []string
		expected string
	}{
		{
			name:     "Null",
			servers:  nil,
			expected: "",
		},
		{
			name:     "Empty string",
			servers:  []string{},
			expected: "",
		},
		{
			name:     "Single server",
			servers:  []string{"https://server1"},
			expected: "https://server1",
		},
		{
			name:     "Multiple servers",
			servers:  []string{"https://server1", "https://server2"},
			expected: "https://server1,https://server2",
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			klusterlet := newKlusterlet("klusterlet", "testns", "")
			for _, server := range c.servers {
				klusterlet.Spec.ExternalServerURLs = append(klusterlet.Spec.ExternalServerURLs,
					opratorapiv1.ServerURL{URL: server})
			}
			actual := getServersFromKlusterlet(klusterlet)
			if actual != c.expected {
				t.Errorf("Expected to be same, actual %q, expected %q", actual, c.expected)
			}
		})
	}
}

func TestClusterNameChange(t *testing.T) {
	klusterlet := newKlusterlet("klusterlet", "testns", "cluster1")
	namespace := newNamespace("testns")
	bootStrapSecret := newSecret(helpers.BootstrapHubKubeConfigSecret, "testns")
	hubSecret := newSecret(helpers.HubKubeConfigSecret, "testns")
	hubSecret.Data["kubeconfig"] = []byte("dummuykubeconnfig")
	hubSecret.Data["cluster-name"] = []byte("cluster1")
	controller := newTestController(klusterlet, bootStrapSecret, hubSecret, namespace)
	syncContext := testinghelper.NewFakeSyncContext(t, "klusterlet")
	err := controller.controller.sync(nil, syncContext)
	if err != nil {
		t.Errorf("Expected non error when sync, %v", err)
	}

	// Check if deployment has the right cluster name set
	ensureDeployments(t, controller.kubeClient.Actions(), "create", "", "cluster1", "cluster1", 2)

	operatorAction := controller.operatorClient.Actions()
	if len(operatorAction) != 2 {
		t.Errorf("Expect 2 actions in the sync loop, actual %#v", operatorAction)
	}

	testinghelper.AssertGet(t, operatorAction[0], "operator.open-cluster-management.io", "v1", "klusterlets")
	testinghelper.AssertAction(t, operatorAction[1], "update")
	updatedKlusterlet := operatorAction[1].(clienttesting.UpdateActionImpl).Object.(*opratorapiv1.Klusterlet)
	testinghelper.AssertOnlyGenerationStatuses(
		t, updatedKlusterlet,
		testinghelper.NamedDeploymentGenerationStatus("klusterlet-registration-agent", "testns", 0),
		testinghelper.NamedDeploymentGenerationStatus("klusterlet-work-agent", "testns", 0),
	)

	// Update klusterlet with unset cluster name and rerun sync
	controller.kubeClient.ClearActions()
	controller.operatorClient.ClearActions()
	klusterlet = newKlusterlet("klusterlet", "testns", "")
	klusterlet.Generation = 1
	controller.operatorStore.Update(klusterlet)

	err = controller.controller.sync(nil, syncContext)
	if err != nil {
		t.Errorf("Expected non error when sync, %v", err)
	}
	ensureDeployments(t, controller.kubeClient.Actions(), "update", "", "", "cluster1", 1)

	// Update hubconfigsecret and sync again
	hubSecret.Data["cluster-name"] = []byte("cluster2")
	controller.kubeClient.PrependReactor("get", "secrets", func(action clienttesting.Action) (handled bool, ret runtime.Object, err error) {
		if action.GetVerb() != "get" {
			return false, nil, nil
		}

		getAction := action.(clienttesting.GetActionImpl)
		if getAction.Name != helpers.HubKubeConfigSecret {
			return false, nil, errors.NewNotFound(
				corev1.Resource("secrets"), helpers.HubKubeConfigSecret)
		}
		return true, hubSecret, nil
	})
	controller.kubeClient.ClearActions()

	err = controller.controller.sync(nil, syncContext)
	if err != nil {
		t.Errorf("Expected non error when sync, %v", err)
	}
	ensureDeployments(t, controller.kubeClient.Actions(), "update", "", "", "cluster2", 1)

	// Update klusterlet with different cluster name and rerun sync
	klusterlet = newKlusterlet("klusterlet", "testns", "cluster3")
	klusterlet.Generation = 2
	klusterlet.Spec.ExternalServerURLs = []opratorapiv1.ServerURL{{URL: "https://localhost"}}
	controller.kubeClient.ClearActions()
	controller.operatorClient.ClearActions()
	controller.operatorStore.Update(klusterlet)

	err = controller.controller.sync(nil, syncContext)
	if err != nil {
		t.Errorf("Expected non error when sync, %v", err)
	}
	ensureDeployments(t, controller.kubeClient.Actions(), "update", "https://localhost", "cluster3", "cluster3", 2)
}

func TestSyncWithPullSecret(t *testing.T) {
	klusterlet := newKlusterlet("klusterlet", "testns", "cluster1")
	bootStrapSecret := newSecret(helpers.BootstrapHubKubeConfigSecret, "testns")
	hubKubeConfigSecret := newSecret(helpers.HubKubeConfigSecret, "testns")
	hubKubeConfigSecret.Data["kubeconfig"] = []byte("dummuykubeconnfig")
	namespace := newNamespace("testns")
	pullSecret := newSecret(imagePullSecret, "open-cluster-management")
	controller := newTestController(klusterlet, bootStrapSecret, hubKubeConfigSecret, namespace, pullSecret)
	syncContext := testinghelper.NewFakeSyncContext(t, "klusterlet")

	err := controller.controller.sync(nil, syncContext)
	if err != nil {
		t.Errorf("Expected non error when sync, %v", err)
	}

	var createdSecret *corev1.Secret
	kubeActions := controller.kubeClient.Actions()
	for _, action := range kubeActions {
		if action.GetVerb() == "create" && action.GetResource().Resource == "secrets" {
			createdSecret = action.(clienttesting.CreateActionImpl).Object.(*corev1.Secret)
			break
		}
	}

	if createdSecret == nil || createdSecret.Name != imagePullSecret {
		t.Errorf("Failed to sync pull secret")
	}
}

func TestDeployOnKube111(t *testing.T) {
	klusterlet := newKlusterlet("klusterlet", "testns", "cluster1")
	bootStrapSecret := newSecret(helpers.BootstrapHubKubeConfigSecret, "testns")
	hubKubeConfigSecret := newSecret(helpers.HubKubeConfigSecret, "testns")
	hubKubeConfigSecret.Data["kubeconfig"] = []byte("dummuykubeconnfig")
	namespace := newNamespace("testns")
	controller := newTestController(klusterlet, bootStrapSecret, hubKubeConfigSecret, namespace)
	kubeVersion, _ := version.ParseGeneric("v1.11.0")
	controller.controller.kubeVersion = kubeVersion
	syncContext := testinghelper.NewFakeSyncContext(t, "klusterlet")

	err := controller.controller.sync(nil, syncContext)
	if err != nil {
		t.Errorf("Expected non error when sync, %v", err)
	}

	createObjects := []runtime.Object{}
	kubeActions := controller.kubeClient.Actions()
	for _, action := range kubeActions {
		if action.GetVerb() == "create" {
			object := action.(clienttesting.CreateActionImpl).Object
			createObjects = append(createObjects, object)
		}
	}

	// Check if resources are created as expected
	if len(createObjects) != 13 {
		t.Errorf("Expect 13 objects created in the sync loop, actual %d", len(createObjects))
	}
	for _, object := range createObjects {
		ensureObject(t, object, klusterlet)
	}

	operatorAction := controller.operatorClient.Actions()
	if len(operatorAction) != 2 {
		t.Errorf("Expect 2 actions in the sync loop, actual %#v", operatorAction)
	}

	testinghelper.AssertGet(t, operatorAction[0], "operator.open-cluster-management.io", "v1", "klusterlets")
	testinghelper.AssertAction(t, operatorAction[1], "update")
	testinghelper.AssertOnlyConditions(
		t, operatorAction[1].(clienttesting.UpdateActionImpl).Object,
		testinghelper.NamedCondition(klusterletApplied, "KlusterletApplied", metav1.ConditionTrue))

	// Delete the klusterlet
	now := metav1.Now()
	klusterlet.ObjectMeta.SetDeletionTimestamp(&now)
	controller.operatorStore.Update(klusterlet)
	controller.kubeClient.ClearActions()
	err = controller.controller.sync(nil, syncContext)
	if err != nil {
		t.Errorf("Expected non error when sync, %v", err)
	}

	deleteActions := []clienttesting.DeleteActionImpl{}
	kubeActions = controller.kubeClient.Actions()
	for _, action := range kubeActions {
		if action.GetVerb() == "delete" {
			deleteAction := action.(clienttesting.DeleteActionImpl)
			deleteActions = append(deleteActions, deleteAction)
		}
	}

	if len(kubeActions) != 15 {
		t.Errorf("Expected 15 delete actions, but got %d", len(kubeActions))
	}
}
