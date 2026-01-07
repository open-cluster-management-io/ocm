package clustermanagercontroller

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/ghodss/yaml"
	"github.com/openshift/library-go/pkg/assets"
	"github.com/openshift/library-go/pkg/operator/resource/resourceapply"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	apiextensionsclient "k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset"
	fakeapiextensions "k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset/fake"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	kubeinformers "k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	fakekube "k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/rest"
	clienttesting "k8s.io/client-go/testing"
	fakemigrationclient "sigs.k8s.io/kube-storage-version-migrator/pkg/clients/clientset/fake"
	migrationclient "sigs.k8s.io/kube-storage-version-migrator/pkg/clients/clientset/typed/migration/v1alpha1"

	fakeoperatorlient "open-cluster-management.io/api/client/operator/clientset/versioned/fake"
	operatorinformers "open-cluster-management.io/api/client/operator/informers/externalversions"
	ocmfeature "open-cluster-management.io/api/feature"
	operatorapiv1 "open-cluster-management.io/api/operator/v1"
	"open-cluster-management.io/sdk-go/pkg/basecontroller/events"
	"open-cluster-management.io/sdk-go/pkg/patcher"

	"open-cluster-management.io/ocm/manifests"
	testingcommon "open-cluster-management.io/ocm/pkg/common/testing"
	"open-cluster-management.io/ocm/pkg/operator/helpers"
)

var (
	ctx        = context.Background()
	createVerb = "create"
)

type testController struct {
	clusterManagerController *clusterManagerController
	managementKubeClient     *fakekube.Clientset
	hubKubeClient            *fakekube.Clientset
	apiExtensionClient       *fakeapiextensions.Clientset
	operatorClient           *fakeoperatorlient.Clientset
}

func newClusterManager(name string) *operatorapiv1.ClusterManager {
	featureGate := operatorapiv1.FeatureGate{
		Feature: "ManifestWorkReplicaSet",
		Mode:    operatorapiv1.FeatureGateModeTypeEnable,
	}

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
				FeatureGates: []operatorapiv1.FeatureGate{
					{Feature: "AddonManagement", Mode: operatorapiv1.FeatureGateModeTypeEnable},
				},
			},
			WorkConfiguration: &operatorapiv1.WorkConfiguration{
				FeatureGates: []operatorapiv1.FeatureGate{featureGate},
				WorkDriver:   operatorapiv1.WorkDriverTypeKube,
			},
		},
	}
}

// newCaBundleConfigMap creates the CA bundle ConfigMap that the cert rotation controller creates.
// This is required for the cluster manager controller to proceed with deploying CRDs.
func newCaBundleConfigMap(namespace string) *corev1.ConfigMap {
	return &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      helpers.CaBundleConfigmap,
			Namespace: namespace,
		},
		Data: map[string]string{
			"ca-bundle.crt": "-----BEGIN CERTIFICATE-----\ntest-ca-bundle\n-----END CERTIFICATE-----",
		},
	}
}

// newTestControllerWithoutCaBundle creates a test controller without the CA bundle ConfigMap
// to test the behavior when the cert rotation controller hasn't created it yet.
func newTestControllerWithoutCaBundle(t *testing.T, clustermanager *operatorapiv1.ClusterManager) *testController {
	kubeClient := fakekube.NewSimpleClientset()
	kubeInfomers := kubeinformers.NewSharedInformerFactory(kubeClient, 5*time.Minute)
	fakeOperatorClient := fakeoperatorlient.NewSimpleClientset(clustermanager)
	operatorInformers := operatorinformers.NewSharedInformerFactory(fakeOperatorClient, 5*time.Minute)

	clusterManagerController := &clusterManagerController{
		patcher: patcher.NewPatcher[
			*operatorapiv1.ClusterManager, operatorapiv1.ClusterManagerSpec, operatorapiv1.ClusterManagerStatus](
			fakeOperatorClient.OperatorV1().ClusterManagers()),
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

// newTestController creates a test controller with all required dependencies including
// the CA bundle ConfigMap. This simulates the normal state where the cert rotation
// controller has already created the CA bundle ConfigMap.
func newTestController(t *testing.T, clustermanager *operatorapiv1.ClusterManager) *testController {
	clusterManagerNamespace := helpers.ClusterManagerNamespace(clustermanager.Name, clustermanager.Spec.DeployOption.Mode)
	caBundleConfigMap := newCaBundleConfigMap(clusterManagerNamespace)

	kubeClient := fakekube.NewSimpleClientset(caBundleConfigMap)
	kubeInfomers := kubeinformers.NewSharedInformerFactory(kubeClient, 5*time.Minute)
	fakeOperatorClient := fakeoperatorlient.NewSimpleClientset(clustermanager)
	operatorInformers := operatorinformers.NewSharedInformerFactory(fakeOperatorClient, 5*time.Minute)

	clusterManagerController := &clusterManagerController{
		patcher: patcher.NewPatcher[
			*operatorapiv1.ClusterManager, operatorapiv1.ClusterManagerSpec, operatorapiv1.ClusterManagerStatus](
			fakeOperatorClient.OperatorV1().ClusterManagers()),
		clusterManagerLister: operatorInformers.Operator().V1().ClusterManagers().Lister(),
		configMapLister:      kubeInfomers.Core().V1().ConfigMaps().Lister(),
		cache:                resourceapply.NewResourceCache(),
	}

	store := operatorInformers.Operator().V1().ClusterManagers().Informer().GetStore()
	if err := store.Add(clustermanager); err != nil {
		t.Fatal(err)
	}

	// Add the CA bundle ConfigMap to the informer store
	configMapStore := kubeInfomers.Core().V1().ConfigMaps().Informer().GetStore()
	if err := configMapStore.Add(caBundleConfigMap); err != nil {
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
		&appsv1.Deployment{
			ObjectMeta: metav1.ObjectMeta{
				Name:       clusterManagerName + "-work-controller",
				Namespace:  clusterManagerNamespace,
				Generation: 1,
			},
			Spec: appsv1.DeploymentSpec{
				Template: corev1.PodTemplateSpec{
					Spec: corev1.PodSpec{
						Containers: []corev1.Container{
							{
								Name: clusterManagerName + "-work-controller",
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
				Name:       clusterManagerName + "-addon-webhook",
				Namespace:  clusterManagerNamespace,
				Generation: 1,
			},
			Spec: appsv1.DeploymentSpec{
				Template: corev1.PodTemplateSpec{
					Spec: corev1.PodSpec{
						Containers: []corev1.Container{
							{
								Name: clusterManagerName + "-addon-webhook",
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
				Name:       clusterManagerName + "-grpc-server",
				Namespace:  clusterManagerNamespace,
				Generation: 1,
			},
			Spec: appsv1.DeploymentSpec{
				Template: corev1.PodTemplateSpec{
					Spec: corev1.PodSpec{
						Containers: []corev1.Container{
							{
								Name: clusterManagerName + "-grpc-server",
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
	tc.clusterManagerController.operatorKubeClient = fakeManagementKubeClient
	tc.clusterManagerController.generateHubClusterClients = func(hubKubeConfig *rest.Config) (
		kubernetes.Interface, apiextensionsclient.Interface, migrationclient.StorageVersionMigrationsGetter, error) {
		return fakeHubKubeClient, fakeAPIExtensionClient, fakeMigrationClient.MigrationV1alpha1(), nil
	}
	tc.clusterManagerController.ensureSAKubeconfigs = func(ctx context.Context,
		clusterManagerName, clusterManagerNamespace string, hubConfig *rest.Config,
		hubClient, managementClient kubernetes.Interface, recorder events.Recorder,
		mwctrEnabled, addonManagerEnabled, grpcAuthEnabled bool) error {
		return nil
	}
}

func ensureObject(t *testing.T, object runtime.Object, hubCore *operatorapiv1.ClusterManager, enableSyncLabels bool) {
	access, err := meta.Accessor(object)
	if err != nil {
		t.Errorf("Unable to access objectmeta: %v", err)
	}

	labels := helpers.GetClusterManagerHubLabels(hubCore, enableSyncLabels)
	if enableSyncLabels && !helpers.MapCompare(access.GetLabels(), labels) {
		t.Errorf("the labels of the clustermanager are not synced to %v %v %v", access.GetName(), hubCore.GetLabels(), access.GetLabels())
		return
	}

	switch o := object.(type) { //nolint:gocritic
	case *corev1.Namespace:
		testingcommon.AssertEqualNameNamespace(t, access.GetName(), "", helpers.ClusterManagerNamespace(hubCore.Name, hubCore.Spec.DeployOption.Mode), "")
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
		if strings.Contains(o.Name, "grpc-server") && hubCore.Spec.RegistrationImagePullSpec != o.Spec.Template.Spec.Containers[0].Image {
			t.Errorf("GRPCServer image does not match to the expected.")
		}
	}
}

func assertDeployments(t *testing.T, clusterManager *operatorapiv1.ClusterManager, expectedCreatedKubeObjects, expectedCreatedCRDs int) {
	tc := newTestController(t, clusterManager)
	clusterManagerNamespace := helpers.ClusterManagerNamespace(clusterManager.Name, clusterManager.Spec.DeployOption.Mode)
	cd := setDeployment(clusterManager.Name, clusterManagerNamespace)
	setup(t, tc, cd)

	syncContext := testingcommon.NewFakeSyncContext(t, "testhub")

	err := tc.clusterManagerController.sync(ctx, syncContext, "testhub")
	if err != nil {
		t.Fatalf("Expected no error when sync, %v", err)
	}

	var createKubeObjects []runtime.Object
	kubeActions := append(tc.hubKubeClient.Actions(), tc.managementKubeClient.Actions()...) // record objects from both hub and management cluster
	for _, action := range kubeActions {
		if action.GetVerb() == createVerb {
			object := action.(clienttesting.CreateActionImpl).Object
			createKubeObjects = append(createKubeObjects, object)
		}
	}

	// Check if resources are created as expected
	// We expect create the namespace twice respectively in the management cluster and the hub cluster.
	testingcommon.AssertEqualNumber(t, len(createKubeObjects), expectedCreatedKubeObjects)
	for _, object := range createKubeObjects {
		ensureObject(t, object, clusterManager, true)
	}

	var createCRDObjects []runtime.Object
	crdActions := tc.apiExtensionClient.Actions()
	for _, action := range crdActions {
		if action.GetVerb() == createVerb {
			object := action.(clienttesting.CreateActionImpl).Object
			createCRDObjects = append(createCRDObjects, object)
		}
	}
	// Check if resources are created as expected
	testingcommon.AssertEqualNumber(t, len(createCRDObjects), expectedCreatedCRDs)
}

func assertDeletion(t *testing.T, clusterManager *operatorapiv1.ClusterManager, expectedDeleteActions, expectedDeleteCRDs int) {
	tc := newTestController(t, clusterManager)
	setup(t, tc, nil)

	syncContext := testingcommon.NewFakeSyncContext(t, "testhub")
	clusterManagerNamespace := helpers.ClusterManagerNamespace(clusterManager.Name, clusterManager.Spec.DeployOption.Mode)

	err := tc.clusterManagerController.sync(ctx, syncContext, "testhub")
	if err != nil {
		t.Fatalf("Expected non error when sync, %v", err)
	}

	var deleteKubeActions []clienttesting.DeleteActionImpl
	kubeActions := append(tc.hubKubeClient.Actions(), tc.managementKubeClient.Actions()...)
	for _, action := range kubeActions {
		if action.GetVerb() == "delete" {
			deleteKubeAction := action.(clienttesting.DeleteActionImpl)
			deleteKubeActions = append(deleteKubeActions, deleteKubeAction)
		}
	}
	testingcommon.AssertEqualNumber(t, len(deleteKubeActions), expectedDeleteActions) // delete namespace both from the hub cluster and the mangement cluster

	var deleteCRDActions []clienttesting.DeleteActionImpl
	crdActions := tc.apiExtensionClient.Actions()
	for _, action := range crdActions {
		if action.GetVerb() == "delete" {
			deleteCRDAction := action.(clienttesting.DeleteActionImpl)
			deleteCRDActions = append(deleteCRDActions, deleteCRDAction)
		}
	}
	// Check if resources are created as expected
	testingcommon.AssertEqualNumber(t, len(deleteCRDActions), expectedDeleteCRDs)

	for _, action := range deleteKubeActions {
		switch action.Resource.Resource { //nolint:gocritic
		case "namespaces":
			testingcommon.AssertEqualNameNamespace(t, action.Name, "", clusterManagerNamespace, "")
		}
	}
}

func TestSyncSecret(t *testing.T) {
	operatorNamespace := helpers.DefaultComponentNamespace
	tests := []struct {
		name                                    string
		clusterManager                          func() *operatorapiv1.ClusterManager
		imagePullSecret, workDriverConfigSecret *corev1.Secret
	}{
		{
			name: "sync imagePullSecret, workDriverConfigSecret",
			clusterManager: func() *operatorapiv1.ClusterManager {
				clusterManager := newClusterManager("testhub")
				clusterManager.Spec.WorkConfiguration.FeatureGates = append(clusterManager.Spec.WorkConfiguration.FeatureGates,
					operatorapiv1.FeatureGate{
						Feature: string(ocmfeature.CloudEventsDrivers),
						Mode:    operatorapiv1.FeatureGateModeTypeEnable,
					})
				clusterManager.Spec.WorkConfiguration.WorkDriver = operatorapiv1.WorkDriverTypeGrpc
				return clusterManager
			},
			imagePullSecret: newSecret(helpers.ImagePullSecret, operatorNamespace),
			workDriverConfigSecret: &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name: helpers.WorkDriverConfigSecret,
				},
				Data: map[string][]byte{
					"config.yaml": []byte("url: grpc.example.com:8443"),
				},
			},
		},
		{
			name: "sync imagePullSecret",
			clusterManager: func() *operatorapiv1.ClusterManager {
				return newClusterManager("testhub")
			},
			imagePullSecret: newSecret(helpers.ImagePullSecret, operatorNamespace),
		},
		{
			name: "sync workDriverConfigSecret",
			clusterManager: func() *operatorapiv1.ClusterManager {
				clusterManager := newClusterManager("testhub")
				clusterManager.Spec.WorkConfiguration.FeatureGates = append(clusterManager.Spec.WorkConfiguration.FeatureGates,
					operatorapiv1.FeatureGate{
						Feature: string(ocmfeature.CloudEventsDrivers),
						Mode:    operatorapiv1.FeatureGateModeTypeEnable,
					})
				clusterManager.Spec.WorkConfiguration.WorkDriver = operatorapiv1.WorkDriverTypeGrpc
				return clusterManager
			},
			workDriverConfigSecret: &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name: helpers.WorkDriverConfigSecret,
				},
				Data: map[string][]byte{
					"config.yaml": []byte("url: grpc.example.com:8443"),
				},
			},
		},
	}
	for _, c := range tests {
		cm := c.clusterManager()
		tc := newTestController(t, cm)
		tc.clusterManagerController.operatorNamespace = operatorNamespace
		clusterManagerNamespace := helpers.ClusterManagerNamespace(cm.Name, cm.Spec.DeployOption.Mode)
		setup(t, tc, nil)

		syncContext := testingcommon.NewFakeSyncContext(t, "testhub")

		if c.imagePullSecret != nil {
			if _, err := tc.managementKubeClient.CoreV1().Secrets(operatorNamespace).Create(ctx, c.imagePullSecret, metav1.CreateOptions{}); err != nil {
				t.Fatalf("Failed to create image pull secret: %v", err)
			}
		}

		if c.workDriverConfigSecret != nil {
			if _, err := tc.managementKubeClient.CoreV1().Secrets(operatorNamespace).Create(ctx, c.workDriverConfigSecret, metav1.CreateOptions{}); err != nil {
				t.Fatalf("Failed to create work driver config secret: %v", err)
			}
		}

		err := tc.clusterManagerController.sync(ctx, syncContext, "testhub")
		if err != nil {
			t.Fatalf("Expected no error when sync, %v", err)
		}

		syncedSecret, err := tc.hubKubeClient.CoreV1().Secrets(clusterManagerNamespace).Get(ctx, helpers.WorkDriverConfigSecret, metav1.GetOptions{})
		if c.workDriverConfigSecret == nil && !errors.IsNotFound(err) {
			t.Fatalf("excpected no secret %v but got: %v", helpers.WorkDriverConfigSecret, err)
		}
		if c.workDriverConfigSecret != nil {
			if err != nil {
				t.Fatalf("Failed to get synced work driver config secret: %v", err)
			}

			if string(syncedSecret.Data["config.yaml"]) != "url: grpc.example.com:8443" {
				t.Fatalf("Expected secret data to be url: grpc.example.com:8443")
			}
		}

		_, err = tc.hubKubeClient.CoreV1().Secrets(clusterManagerNamespace).Get(ctx, helpers.ImagePullSecret, metav1.GetOptions{})
		if c.imagePullSecret == nil && !errors.IsNotFound(err) {
			t.Fatalf("excpected no secret %v but got: %v", helpers.ImagePullSecret, err)
		}
		if c.imagePullSecret != nil && err != nil {
			t.Fatalf("Failed to get synced image pull secret: %v", err)
		}

		deploymentList, err := tc.hubKubeClient.AppsV1().Deployments(clusterManagerNamespace).List(ctx, metav1.ListOptions{})
		if err != nil {
			t.Fatalf("Failed to list deployment: %v", err)
		}
		for _, deployment := range deploymentList.Items {
			if c.imagePullSecret == nil && len(deployment.Spec.Template.Spec.ImagePullSecrets) != 0 {
				t.Fatalf("Expected no image pull secret in deployment. %v", deployment.Name)
			}

			if c.imagePullSecret != nil && len(deployment.Spec.Template.Spec.ImagePullSecrets) == 0 {
				t.Fatalf("Expected image pull secret in deployment. %v", deployment.Name)
			}

			if c.imagePullSecret != nil && deployment.Spec.Template.Spec.ImagePullSecrets[0].Name != helpers.ImagePullSecret {
				t.Fatalf("Expected correct image pull secret name in deployment. %v", deployment.Name)
			}
		}
	}
}

// TestSyncDeploy tests sync manifests of hub component
func TestSyncDeploy(t *testing.T) {
	labels := map[string]string{"app": "test", helpers.HubLabelKey: "testhub", "abc": "abc",
		"open-cluster-management.io/cluster-name": "test"}
	clusterManager := newClusterManager("testhub")
	clusterManager.SetLabels(labels)
	assertDeployments(t, clusterManager, 30, 12)
}

func TestSyncDeployWithGRPCAuthEnabled(t *testing.T) {
	labels := map[string]string{"app": "test", helpers.HubLabelKey: "testhub", "abc": "abc",
		"open-cluster-management.io/cluster-name": "test"}
	clusterManager := newClusterManager("testhub")
	clusterManager.SetLabels(labels)
	clusterManager.Spec.RegistrationConfiguration = &operatorapiv1.RegistrationHubConfiguration{
		RegistrationDrivers: []operatorapiv1.RegistrationDriverHub{
			{
				AuthType: operatorapiv1.CSRAuthType,
			},
			{
				AuthType: operatorapiv1.GRPCAuthType,
			},
		},
	}
	assertDeployments(t, clusterManager, 34, 12)
}

func TestSyncDeployNoWebhook(t *testing.T) {
	clusterManager := newClusterManager("testhub")
	tc := newTestController(t, clusterManager)
	setup(t, tc, nil)

	syncContext := testingcommon.NewFakeSyncContext(t, "testhub")

	err := tc.clusterManagerController.sync(ctx, syncContext, "testhub")
	if err != nil {
		t.Fatalf("Expected no error when sync, %v", err)
	}

	var createKubeObjects []runtime.Object
	kubeActions := append(tc.hubKubeClient.Actions(), tc.managementKubeClient.Actions()...) // record objects from both hub and management cluster
	for _, action := range kubeActions {
		if action.GetVerb() == createVerb {
			object := action.(clienttesting.CreateActionImpl).Object
			createKubeObjects = append(createKubeObjects, object)
		}
	}

	// Check if resources are created as expected
	// We expect create the namespace twice respectively in the management cluster and the hub cluster.
	testingcommon.AssertEqualNumber(t, len(createKubeObjects), 33)
	for _, object := range createKubeObjects {
		ensureObject(t, object, clusterManager, false)
	}

	var createCRDObjects []runtime.Object
	crdActions := tc.apiExtensionClient.Actions()
	for _, action := range crdActions {
		if action.GetVerb() == createVerb {
			object := action.(clienttesting.CreateActionImpl).Object
			createCRDObjects = append(createCRDObjects, object)
		}
	}
	// Check if resources are created as expected
	testingcommon.AssertEqualNumber(t, len(createCRDObjects), 12)
}

// TestSyncDeployWithoutCaBundle tests that sync requeues when the CA bundle ConfigMap
// doesn't exist. This ensures the controller waits for the cert rotation controller to create
// the ConfigMap before proceeding, preventing CRDs from being created with invalid "placeholder"
// CA bundles that would cause webhook conversion to fail.
func TestSyncDeployWithoutCaBundle(t *testing.T) {
	clusterManager := newClusterManager("testhub")
	tc := newTestControllerWithoutCaBundle(t, clusterManager)
	clusterManagerNamespace := helpers.ClusterManagerNamespace(clusterManager.Name, clusterManager.Spec.DeployOption.Mode)
	cd := setDeployment(clusterManager.Name, clusterManagerNamespace)
	setup(t, tc, cd)

	syncContext := testingcommon.NewFakeSyncContext(t, "testhub")

	err := tc.clusterManagerController.sync(ctx, syncContext, "testhub")
	// sync should return nil (requeue via AddAfter) instead of an error
	if err != nil {
		t.Fatalf("Expected no error when CA bundle ConfigMap is missing (should requeue via AddAfter), but got: %v", err)
	}

	// Note: We can't easily verify the requeue here because AddAfter adds items with a delay.
	// The important thing is that sync returns nil (indicating graceful requeue) instead of
	// proceeding with an invalid CA bundle or returning an error.
}

// TestSyncDelete test cleanup hub deploy
func TestSyncDelete(t *testing.T) {
	clusterManager := newClusterManager("testhub")
	now := metav1.Now()
	clusterManager.ObjectMeta.SetDeletionTimestamp(&now)

	assertDeletion(t, clusterManager, 32, 16)
}

func TestSyncDeleteWithGRPCAuthEnabled(t *testing.T) {
	clusterManager := newClusterManager("testhub")
	clusterManager.Spec.RegistrationConfiguration = &operatorapiv1.RegistrationHubConfiguration{
		RegistrationDrivers: []operatorapiv1.RegistrationDriverHub{
			{
				AuthType: operatorapiv1.CSRAuthType,
			},
			{
				AuthType: operatorapiv1.GRPCAuthType,
			},
		},
	}
	now := metav1.Now()
	clusterManager.ObjectMeta.SetDeletionTimestamp(&now)
	assertDeletion(t, clusterManager, 36, 16)
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
			getCount++
			return true, crd, nil
		}
		return true, &apiextensionsv1.CustomResourceDefinition{}, errors.NewNotFound(
			apiextensionsv1.Resource("customresourcedefinitions"), "clustermanagementaddons.addon.open-cluster-management.io")

	})
	syncContext := testingcommon.NewFakeSyncContext(t, "testhub")
	err := tc.clusterManagerController.sync(ctx, syncContext, "testhub")
	if err == nil {
		t.Fatalf("Expected error when sync at first time")
	}

	err = tc.clusterManagerController.sync(ctx, syncContext, "testhub")
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

func TestRenderingResourceRequirements(t *testing.T) {
	defaultResource := &operatorapiv1.ResourceRequirement{
		Type: operatorapiv1.ResourceQosClassDefault,
		ResourceRequirements: &corev1.ResourceRequirements{
			Requests: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("2m"),
				corev1.ResourceMemory: resource.MustParse("16Mi"),
			},
		},
	}
	bestEffort := &operatorapiv1.ResourceRequirement{
		Type:                 operatorapiv1.ResourceQosClassBestEffort,
		ResourceRequirements: &corev1.ResourceRequirements{},
	}
	burstable := &operatorapiv1.ResourceRequirement{
		Type: operatorapiv1.ResourceQosClassResourceRequirement,
		ResourceRequirements: &corev1.ResourceRequirements{
			Limits: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("1"),
				corev1.ResourceMemory: resource.MustParse("1Gi"),
			},
			Requests: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("100m"),
				corev1.ResourceMemory: resource.MustParse("100Mi"),
			},
		},
	}
	tests := []struct {
		name     string
		resource *operatorapiv1.ResourceRequirement
	}{
		{
			name:     "DefaultResourceRequirements",
			resource: defaultResource,
		},
		{
			name:     "BestEffortResourceRequirements",
			resource: bestEffort,
		},
		{
			name:     "SpecificResourceRequirements",
			resource: burstable,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config := newFakeHubConfigWithResourceRequirement(t, tt.resource)
			for _, file := range getManifestFiles() {
				manifest, err := manifests.ClusterManagerManifestFiles.ReadFile(file)
				if err != nil {
					t.Errorf("Failed to read file %s", file)
				}
				objData := assets.MustCreateAssetFromTemplate(file, manifest, config).Data
				deploy := &appsv1.Deployment{}
				if err = yaml.Unmarshal(objData, deploy); err != nil {
					t.Errorf("Failed to unmarshal deployment: %v", err)
				}
				actual := deploy.Spec.Template.Spec.Containers[0].Resources
				actualString := actual.String()
				expectedStr := tt.resource.ResourceRequirements.String()
				if actualString != expectedStr {
					t.Errorf("expect:\n%s\nbut got:\n%s", expectedStr, actualString)
				}
			}
		})
	}
}

func newFakeHubConfigWithResourceRequirement(t *testing.T, r *operatorapiv1.ResourceRequirement) manifests.HubConfig {
	clusterManager := &operatorapiv1.ClusterManager{
		ObjectMeta: metav1.ObjectMeta{
			Name: "fake-cluster-manager",
		},
		Spec: operatorapiv1.ClusterManagerSpec{
			RegistrationImagePullSpec: "fake-registration-image",
			WorkImagePullSpec:         "fake-work-image",
			PlacementImagePullSpec:    "fake-placement-image",
			AddOnManagerImagePullSpec: "fake-addon-manager-image",
			ResourceRequirement:       r,
		},
	}

	resourceRequirements, err := helpers.ResourceRequirements(context.Background(), clusterManager)
	if err != nil {
		t.Errorf("Failed to parse resource requirements: %v", err)
	}

	hubConfig := manifests.HubConfig{
		ClusterManagerName:      clusterManager.Name,
		ClusterManagerNamespace: helpers.ClusterManagerNamespace(clusterManager.Name, clusterManager.Spec.DeployOption.Mode),
		RegistrationImage:       clusterManager.Spec.RegistrationImagePullSpec,
		WorkImage:               clusterManager.Spec.WorkImagePullSpec,
		PlacementImage:          clusterManager.Spec.PlacementImagePullSpec,
		AddOnManagerImage:       clusterManager.Spec.AddOnManagerImagePullSpec,
		Replica:                 1,
		HostedMode:              false,
		RegistrationWebhook: manifests.Webhook{
			Port: defaultWebhookPort,
		},
		WorkWebhook: manifests.Webhook{
			Port: defaultWebhookPort,
		},
		ResourceRequirementResourceType: helpers.ResourceType(clusterManager),
		ResourceRequirements:            resourceRequirements,
	}
	return hubConfig
}

func getManifestFiles() []string {
	return []string{
		"cluster-manager/management/addon-manager/deployment.yaml",
		"cluster-manager/management/addon-manager/webhook-deployment.yaml",
		"cluster-manager/management/work/deployment.yaml",
		"cluster-manager/management/placement/deployment.yaml",
		"cluster-manager/management/registration/deployment.yaml",
		"cluster-manager/management/registration/webhook-deployment.yaml",
		"cluster-manager/management/work/webhook-deployment.yaml",
	}
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

// TestGRPCServiceLoadBalancerType tests that the GRPC service is of LoadBalancer type when configured
func TestGRPCServiceLoadBalancerType(t *testing.T) {
	tests := []struct {
		name                string
		clusterManager      *operatorapiv1.ClusterManager
		expectedServiceType corev1.ServiceType
		expectedPort        int32
		description         string
	}{
		{
			name: "GRPC service with LoadBalancer type",
			clusterManager: func() *operatorapiv1.ClusterManager {
				cm := newClusterManager("testhub")
				cm.Spec.RegistrationConfiguration = &operatorapiv1.RegistrationHubConfiguration{
					RegistrationDrivers: []operatorapiv1.RegistrationDriverHub{
						{
							AuthType: operatorapiv1.GRPCAuthType,
						},
					},
				}
				cm.Spec.ServerConfiguration = &operatorapiv1.ServerConfiguration{
					EndpointsExposure: []operatorapiv1.EndpointExposure{
						{
							Protocol: "grpc",
							GRPC: &operatorapiv1.Endpoint{
								Type: operatorapiv1.EndpointTypeLoadBalancer,
							},
						},
					},
				}
				return cm
			}(),
			expectedServiceType: corev1.ServiceTypeLoadBalancer,
			expectedPort:        443,
			description:         "GRPC service should be LoadBalancer type when endpoint type is loadBalancer",
		},
		{
			name: "GRPC service with ClusterIP type (hostname endpoint)",
			clusterManager: func() *operatorapiv1.ClusterManager {
				cm := newClusterManager("testhub")
				cm.Spec.RegistrationConfiguration = &operatorapiv1.RegistrationHubConfiguration{
					RegistrationDrivers: []operatorapiv1.RegistrationDriverHub{
						{
							AuthType: operatorapiv1.GRPCAuthType,
						},
					},
				}
				cm.Spec.ServerConfiguration = &operatorapiv1.ServerConfiguration{
					EndpointsExposure: []operatorapiv1.EndpointExposure{
						{
							Protocol: "grpc",
							GRPC: &operatorapiv1.Endpoint{
								Type: operatorapiv1.EndpointTypeHostname,
								Hostname: &operatorapiv1.HostnameConfig{
									Host: "grpc.example.com",
								},
							},
						},
					},
				}
				return cm
			}(),
			expectedServiceType: corev1.ServiceTypeClusterIP,
			expectedPort:        8090,
			description:         "GRPC service should be ClusterIP type when endpoint type is hostname",
		},
		{
			name: "GRPC service with default ClusterIP type (no server configuration)",
			clusterManager: func() *operatorapiv1.ClusterManager {
				cm := newClusterManager("testhub")
				cm.Spec.RegistrationConfiguration = &operatorapiv1.RegistrationHubConfiguration{
					RegistrationDrivers: []operatorapiv1.RegistrationDriverHub{
						{
							AuthType: operatorapiv1.GRPCAuthType,
						},
					},
				}
				return cm
			}(),
			expectedServiceType: corev1.ServiceTypeClusterIP,
			expectedPort:        8090,
			description:         "GRPC service should be ClusterIP type when no server configuration is specified",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			tc := newTestController(t, test.clusterManager)
			clusterManagerNamespace := helpers.ClusterManagerNamespace(test.clusterManager.Name, test.clusterManager.Spec.DeployOption.Mode)
			cd := setDeployment(test.clusterManager.Name, clusterManagerNamespace)
			setup(t, tc, cd)

			syncContext := testingcommon.NewFakeSyncContext(t, test.clusterManager.Name)

			// Call sync to create resources
			err := tc.clusterManagerController.sync(ctx, syncContext, "testhub")
			if err != nil {
				t.Fatalf("Expected no error when sync, %v", err)
			}

			// Find the GRPC service in the created objects
			grpcServiceName := test.clusterManager.Name + "-grpc-server"
			var grpcServiceFound bool
			var actualServiceType corev1.ServiceType
			var actualServicePort int32

			kubeActions := append(tc.hubKubeClient.Actions(), tc.managementKubeClient.Actions()...)
			for _, action := range kubeActions {
				if action.GetVerb() == createVerb {
					object := action.(clienttesting.CreateActionImpl).Object
					if service, ok := object.(*corev1.Service); ok {
						if service.Name == grpcServiceName && service.Namespace == clusterManagerNamespace {
							grpcServiceFound = true
							actualServiceType = service.Spec.Type
							actualServicePort = service.Spec.Ports[0].Port
							break
						}
					}
				}
			}

			if !grpcServiceFound {
				t.Fatalf("Test %q failed: GRPC service %s not found in namespace %s", test.name, grpcServiceName, clusterManagerNamespace)
			}

			if actualServiceType != test.expectedServiceType {
				t.Errorf("Test %q failed: %s. Expected service type %q, but got %q", test.name, test.description, test.expectedServiceType, actualServiceType)
			}
			if actualServicePort != test.expectedPort {
				t.Errorf("Test %q failed: Expected service port %d, but got %d", test.name, test.expectedPort, actualServicePort)
			}
		})
	}
}

// TestWorkControllerEnabledByFeatureGates tests that work controller is enabled when specific feature gates are enabled
func TestWorkControllerEnabledByFeatureGates(t *testing.T) {
	tests := []struct {
		name                   string
		featureGates           []operatorapiv1.FeatureGate
		expectedWorkController bool
		description            string
	}{
		{
			name: "ManifestWorkReplicaSet feature gate enabled",
			featureGates: []operatorapiv1.FeatureGate{
				{Feature: string(ocmfeature.ManifestWorkReplicaSet), Mode: operatorapiv1.FeatureGateModeTypeEnable},
			},
			expectedWorkController: true,
			description:            "Work controller should be enabled when ManifestWorkReplicaSet feature gate is enabled",
		},
		{
			name: "CleanUpCompletedManifestWork feature gate enabled",
			featureGates: []operatorapiv1.FeatureGate{
				{Feature: string(ocmfeature.CleanUpCompletedManifestWork), Mode: operatorapiv1.FeatureGateModeTypeEnable},
			},
			expectedWorkController: true,
			description:            "Work controller should be enabled when CleanUpCompletedManifestWork feature gate is enabled",
		},
		{
			name: "Both ManifestWorkReplicaSet and CleanUpCompletedManifestWork enabled",
			featureGates: []operatorapiv1.FeatureGate{
				{Feature: string(ocmfeature.ManifestWorkReplicaSet), Mode: operatorapiv1.FeatureGateModeTypeEnable},
				{Feature: string(ocmfeature.CleanUpCompletedManifestWork), Mode: operatorapiv1.FeatureGateModeTypeEnable},
			},
			expectedWorkController: true,
			description:            "Work controller should be enabled when both feature gates are enabled",
		},
		{
			name: "ManifestWorkReplicaSet disabled, CleanUpCompletedManifestWork enabled",
			featureGates: []operatorapiv1.FeatureGate{
				{Feature: string(ocmfeature.ManifestWorkReplicaSet), Mode: operatorapiv1.FeatureGateModeTypeDisable},
				{Feature: string(ocmfeature.CleanUpCompletedManifestWork), Mode: operatorapiv1.FeatureGateModeTypeEnable},
			},
			expectedWorkController: true,
			description:            "Work controller should be enabled when at least one required feature gate is enabled",
		},
		{
			name: "ManifestWorkReplicaSet enabled, CleanUpCompletedManifestWork disabled",
			featureGates: []operatorapiv1.FeatureGate{
				{Feature: string(ocmfeature.ManifestWorkReplicaSet), Mode: operatorapiv1.FeatureGateModeTypeEnable},
				{Feature: string(ocmfeature.CleanUpCompletedManifestWork), Mode: operatorapiv1.FeatureGateModeTypeDisable},
			},
			expectedWorkController: true,
			description:            "Work controller should be enabled when at least one required feature gate is enabled",
		},
		{
			name: "Both ManifestWorkReplicaSet and CleanUpCompletedManifestWork disabled",
			featureGates: []operatorapiv1.FeatureGate{
				{Feature: string(ocmfeature.ManifestWorkReplicaSet), Mode: operatorapiv1.FeatureGateModeTypeDisable},
				{Feature: string(ocmfeature.CleanUpCompletedManifestWork), Mode: operatorapiv1.FeatureGateModeTypeDisable},
			},
			expectedWorkController: false,
			description:            "Work controller should be disabled when both feature gates are disabled",
		},
		{
			name: "Only other feature gates enabled",
			featureGates: []operatorapiv1.FeatureGate{
				{Feature: string(ocmfeature.CloudEventsDrivers), Mode: operatorapiv1.FeatureGateModeTypeEnable},
			},
			expectedWorkController: false,
			description:            "Work controller should be disabled when only unrelated feature gates are enabled",
		},
		{
			name:                   "No work feature gates specified",
			featureGates:           []operatorapiv1.FeatureGate{},
			expectedWorkController: false,
			description:            "Work controller should be disabled when no work feature gates are specified",
		},
		{
			name: "ManifestWorkReplicaSet enabled with other feature gates",
			featureGates: []operatorapiv1.FeatureGate{
				{Feature: string(ocmfeature.ManifestWorkReplicaSet), Mode: operatorapiv1.FeatureGateModeTypeEnable},
				{Feature: string(ocmfeature.CloudEventsDrivers), Mode: operatorapiv1.FeatureGateModeTypeEnable},
			},
			expectedWorkController: true,
			description:            "Work controller should be enabled when ManifestWorkReplicaSet is enabled regardless of other feature gates",
		},
		{
			name: "CleanUpCompletedManifestWork enabled with other feature gates",
			featureGates: []operatorapiv1.FeatureGate{
				{Feature: string(ocmfeature.CleanUpCompletedManifestWork), Mode: operatorapiv1.FeatureGateModeTypeEnable},
				{Feature: string(ocmfeature.CloudEventsDrivers), Mode: operatorapiv1.FeatureGateModeTypeDisable},
			},
			expectedWorkController: true,
			description:            "Work controller should be enabled when CleanUpCompletedManifestWork is enabled regardless of other feature gates",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			clusterManager := &operatorapiv1.ClusterManager{
				ObjectMeta: metav1.ObjectMeta{
					Name:       "test-cluster-manager",
					Finalizers: []string{clusterManagerFinalizer},
				},
				Spec: operatorapiv1.ClusterManagerSpec{
					RegistrationImagePullSpec: "testregistration",
					DeployOption: operatorapiv1.ClusterManagerDeployOption{
						Mode: operatorapiv1.InstallModeDefault,
					},
					WorkConfiguration: &operatorapiv1.WorkConfiguration{
						FeatureGates: test.featureGates,
						WorkDriver:   operatorapiv1.WorkDriverTypeKube,
					},
				},
			}

			tc := newTestController(t, clusterManager)
			setup(t, tc, nil)

			syncContext := testingcommon.NewFakeSyncContext(t, "test-cluster-manager")

			// Call sync to trigger the feature gate processing
			err := tc.clusterManagerController.sync(ctx, syncContext, "test-cluster-manager")
			if err != nil {
				t.Fatalf("Expected no error when sync, %v", err)
			}

			// Check if work controller deployment is created or not based on feature gates
			clusterManagerNamespace := helpers.ClusterManagerNamespace(clusterManager.Name, clusterManager.Spec.DeployOption.Mode)
			workControllerDeploymentName := clusterManager.Name + "-work-controller"

			var workControllerDeploymentFound bool
			kubeActions := append(tc.hubKubeClient.Actions(), tc.managementKubeClient.Actions()...)
			for _, action := range kubeActions {
				if action.GetVerb() == createVerb {
					object := action.(clienttesting.CreateActionImpl).Object
					if deployment, ok := object.(*appsv1.Deployment); ok {
						if deployment.Name == workControllerDeploymentName && deployment.Namespace == clusterManagerNamespace {
							workControllerDeploymentFound = true
							break
						}
					}
				}
			}

			if test.expectedWorkController && !workControllerDeploymentFound {
				t.Errorf("Test %q failed: %s, but work controller deployment was not created", test.name, test.description)
			}

			if !test.expectedWorkController && workControllerDeploymentFound {
				t.Errorf("Test %q failed: %s, but work controller deployment was created", test.name, test.description)
			}
		})
	}
}
