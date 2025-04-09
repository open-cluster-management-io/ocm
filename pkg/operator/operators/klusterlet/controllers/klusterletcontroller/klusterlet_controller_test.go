package klusterletcontroller

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/ghodss/yaml"
	"github.com/openshift/library-go/pkg/assets"
	"github.com/openshift/library-go/pkg/operator/events"
	"github.com/openshift/library-go/pkg/operator/resource/resourceapply"
	"github.com/stretchr/testify/assert"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	fakeapiextensions "k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset/fake"
	"k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/rand"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/apimachinery/pkg/util/version"
	fakekube "k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/rest"
	clienttesting "k8s.io/client-go/testing"
	"k8s.io/client-go/tools/cache"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"
	clientcmdlatest "k8s.io/client-go/tools/clientcmd/api/latest"
	"k8s.io/klog/v2"

	fakeoperatorclient "open-cluster-management.io/api/client/operator/clientset/versioned/fake"
	operatorinformers "open-cluster-management.io/api/client/operator/informers/externalversions"
	fakeworkclient "open-cluster-management.io/api/client/work/clientset/versioned/fake"
	operatorapiv1 "open-cluster-management.io/api/operator/v1"
	workapiv1 "open-cluster-management.io/api/work/v1"
	"open-cluster-management.io/sdk-go/pkg/patcher"

	"open-cluster-management.io/ocm/manifests"
	commonhelpers "open-cluster-management.io/ocm/pkg/common/helpers"
	testingcommon "open-cluster-management.io/ocm/pkg/common/testing"
	"open-cluster-management.io/ocm/pkg/operator/helpers"
	testinghelper "open-cluster-management.io/ocm/pkg/operator/helpers/testing"
)

const (
	createVerb                   = "create"
	deleteVerb                   = "delete"
	crdResourceName              = "customresourcedefinitions"
	hostedKubeconfigCreationTime = "2021-01-02T15:04:05Z"
)

type testController struct {
	controller         *klusterletController
	cleanupController  *klusterletCleanupController
	kubeClient         *fakekube.Clientset
	apiExtensionClient *fakeapiextensions.Clientset
	operatorClient     *fakeoperatorclient.Clientset
	workClient         *fakeworkclient.Clientset
	operatorStore      cache.Store
	recorder           events.Recorder

	managedKubeClient         *fakekube.Clientset
	managedApiExtensionClient *fakeapiextensions.Clientset
	managedWorkClient         *fakeworkclient.Clientset
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

func newServiceAccountSecret(name, namespace string) *corev1.Secret {
	secret := newSecret(name, namespace)
	secret.Data["token"] = []byte("test-token")
	secret.Type = corev1.SecretTypeServiceAccountToken
	return secret
}

func removeKlusterletFinalizer(k *operatorapiv1.Klusterlet, f string) *operatorapiv1.Klusterlet {
	finalizers := make([]string, 0)
	for _, finalizer := range k.Finalizers {
		if finalizer == f {
			continue
		}
		finalizers = append(finalizers, finalizer)
	}
	k.SetFinalizers(finalizers)
	return k
}

func newKlusterlet(name, namespace, clustername string) *operatorapiv1.Klusterlet {
	return &operatorapiv1.Klusterlet{
		ObjectMeta: metav1.ObjectMeta{
			Name:       name,
			Finalizers: []string{klusterletFinalizer},
		},
		Spec: operatorapiv1.KlusterletSpec{
			RegistrationImagePullSpec: "testregistration",
			WorkImagePullSpec:         "testwork",
			ImagePullSpec:             "testagent",
			ClusterName:               clustername,
			Namespace:                 namespace,
			ExternalServerURLs:        []operatorapiv1.ServerURL{},
			RegistrationConfiguration: &operatorapiv1.RegistrationConfiguration{
				FeatureGates: []operatorapiv1.FeatureGate{
					{
						Feature: "AddonManagement",
						Mode:    "Enable",
					},
				},
				KubeAPIQPS:   10,
				KubeAPIBurst: 60,
			},
			WorkConfiguration: &operatorapiv1.WorkAgentConfiguration{
				KubeAPIQPS:   20,
				KubeAPIBurst: 50,
			},
			HubApiServerHostAlias: &operatorapiv1.HubApiServerHostAlias{
				IP:       "11.22.33.44",
				Hostname: "open-cluster-management.io",
			},
		},
	}
}

func newKlusterletHosted(name, namespace, clustername string) *operatorapiv1.Klusterlet {
	klusterlet := newKlusterlet(name, namespace, clustername)
	klusterlet.Spec.RegistrationConfiguration.RegistrationDriver = operatorapiv1.RegistrationDriver{}
	klusterlet.Spec.DeployOption.Mode = operatorapiv1.InstallModeHosted
	klusterlet.Finalizers = append(klusterlet.Finalizers, klusterletHostedFinalizer)
	return klusterlet
}

func newNamespace(name string) *corev1.Namespace {
	return &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
	}
}

func newNode(name string) *corev1.Node {
	return &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
			Labels: map[string]string{
				"node-role.kubernetes.io/master": "",
			},
		},
	}
}

func newServiceAccount(name, namespace string, referenceSecret string) *corev1.ServiceAccount {
	return &corev1.ServiceAccount{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Secrets: []corev1.ObjectReference{
			{
				Name:      referenceSecret,
				Namespace: namespace,
			},
		},
	}
}

func newTestController(t *testing.T, klusterlet *operatorapiv1.Klusterlet, recorder events.Recorder,
	appliedManifestWorks []runtime.Object, enableSyncLabels bool, objects ...runtime.Object) *testController {
	fakeKubeClient := fakekube.NewSimpleClientset(objects...)
	fakeAPIExtensionClient := fakeapiextensions.NewSimpleClientset()
	fakeOperatorClient := fakeoperatorclient.NewSimpleClientset(klusterlet)
	fakeWorkClient := fakeworkclient.NewSimpleClientset(appliedManifestWorks...)
	operatorInformers := operatorinformers.NewSharedInformerFactory(fakeOperatorClient, 5*time.Minute)
	kubeVersion, _ := version.ParseGeneric("v1.18.0")

	hubController := &klusterletController{
		patcher: patcher.NewPatcher[
			*operatorapiv1.Klusterlet, operatorapiv1.KlusterletSpec, operatorapiv1.KlusterletStatus](fakeOperatorClient.OperatorV1().Klusterlets()),
		kubeClient:        fakeKubeClient,
		klusterletLister:  operatorInformers.Operator().V1().Klusterlets().Lister(),
		kubeVersion:       kubeVersion,
		operatorNamespace: "open-cluster-management",
		cache:             resourceapply.NewResourceCache(),
		managedClusterClientsBuilder: newManagedClusterClientsBuilder(fakeKubeClient, fakeAPIExtensionClient,
			fakeWorkClient.WorkV1().AppliedManifestWorks(), recorder),
		enableSyncLabels: enableSyncLabels,
	}

	cleanupController := &klusterletCleanupController{
		patcher: patcher.NewPatcher[
			*operatorapiv1.Klusterlet, operatorapiv1.KlusterletSpec, operatorapiv1.KlusterletStatus](fakeOperatorClient.OperatorV1().Klusterlets()),
		kubeClient:        fakeKubeClient,
		klusterletLister:  operatorInformers.Operator().V1().Klusterlets().Lister(),
		kubeVersion:       kubeVersion,
		operatorNamespace: "open-cluster-management",
		managedClusterClientsBuilder: newManagedClusterClientsBuilder(fakeKubeClient, fakeAPIExtensionClient,
			fakeWorkClient.WorkV1().AppliedManifestWorks(), recorder),
	}

	store := operatorInformers.Operator().V1().Klusterlets().Informer().GetStore()
	if err := store.Add(klusterlet); err != nil {
		t.Fatal(err)
	}

	return &testController{
		controller:         hubController,
		cleanupController:  cleanupController,
		kubeClient:         fakeKubeClient,
		apiExtensionClient: fakeAPIExtensionClient,
		operatorClient:     fakeOperatorClient,
		workClient:         fakeWorkClient,
		operatorStore:      store,
		recorder:           recorder,
	}
}

func newTestControllerHosted(
	t *testing.T, klusterlet *operatorapiv1.Klusterlet,
	recorder events.Recorder,
	appliedManifestWorks []runtime.Object,
	objects ...runtime.Object) *testController {
	fakeKubeClient := fakekube.NewSimpleClientset(objects...)
	fakeAPIExtensionClient := fakeapiextensions.NewSimpleClientset()
	fakeOperatorClient := fakeoperatorclient.NewSimpleClientset(klusterlet)
	fakeWorkClient := fakeworkclient.NewSimpleClientset()
	operatorInformers := operatorinformers.NewSharedInformerFactory(fakeOperatorClient, 5*time.Minute)
	kubeVersion, _ := version.ParseGeneric("v1.18.0")

	klusterletNamespace := helpers.KlusterletNamespace(klusterlet)
	saRegistrationSecret := newServiceAccountSecret(fmt.Sprintf("%s-token", serviceAccountName("registration-sa", klusterlet)), klusterlet.Name)
	saWorkSecret := newServiceAccountSecret(fmt.Sprintf("%s-token", serviceAccountName("work-sa", klusterlet)), klusterlet.Name)
	fakeManagedKubeClient := fakekube.NewSimpleClientset()
	getRegistrationServiceAccountCount := 0
	getWorkServiceAccountCount := 0
	// fake the get serviceaccount, since there is no kubernetes controller to create service account related secret.
	// count the number of getting serviceaccount:
	// - the first call will return empty(Apply service account will invoke GetServiceAccount firstly),return empty will let the Apply service account to creation.
	// - After the first one, we will return a service account with secrets.
	fakeManagedKubeClient.PrependReactor("get", "serviceaccounts", func(action clienttesting.Action) (handled bool, ret runtime.Object, err error) {
		name := action.(clienttesting.GetAction).GetName()
		namespace := action.(clienttesting.GetAction).GetNamespace()
		if namespace == klusterletNamespace && name == serviceAccountName("registration-sa", klusterlet) {
			getRegistrationServiceAccountCount++
			if getRegistrationServiceAccountCount > 1 {
				sa := newServiceAccount(name, klusterletNamespace, saRegistrationSecret.Name)
				klog.Infof("return service account %s/%s, secret: %v", klusterletNamespace, name, sa.Secrets)
				return true, sa, nil
			}
		}

		if namespace == klusterletNamespace && name == serviceAccountName("work-sa", klusterlet) {
			getWorkServiceAccountCount++
			if getWorkServiceAccountCount > 1 {
				sa := newServiceAccount(name, klusterletNamespace, saWorkSecret.Name)
				klog.Infof("return service account %s/%s, secret: %v", klusterletNamespace, name, sa.Secrets)
				return true, sa, nil
			}
		}
		return false, nil, nil
	})
	fakeManagedKubeClient.PrependReactor("get", "secrets", func(action clienttesting.Action) (handled bool, ret runtime.Object, err error) {
		name := action.(clienttesting.GetAction).GetName()
		namespace := action.(clienttesting.GetAction).GetNamespace()
		if namespace == klusterletNamespace && name == saRegistrationSecret.Name {
			return true, saRegistrationSecret, nil
		}
		if namespace == klusterletNamespace && name == saWorkSecret.Name {
			return true, saWorkSecret, nil
		}
		return false, nil, nil
	})

	fakeManagedAPIExtensionClient := fakeapiextensions.NewSimpleClientset()
	fakeManagedWorkClient := fakeworkclient.NewSimpleClientset(appliedManifestWorks...)
	hubController := &klusterletController{
		patcher: patcher.NewPatcher[
			*operatorapiv1.Klusterlet, operatorapiv1.KlusterletSpec, operatorapiv1.KlusterletStatus](fakeOperatorClient.OperatorV1().Klusterlets()),
		kubeClient:        fakeKubeClient,
		klusterletLister:  operatorInformers.Operator().V1().Klusterlets().Lister(),
		kubeVersion:       kubeVersion,
		operatorNamespace: "open-cluster-management",
		cache:             resourceapply.NewResourceCache(),
		managedClusterClientsBuilder: &fakeManagedClusterBuilder{
			fakeWorkClient:         fakeManagedWorkClient,
			fakeAPIExtensionClient: fakeManagedAPIExtensionClient,
			fakeKubeClient:         fakeManagedKubeClient,
		},
	}
	cleanupController := &klusterletCleanupController{
		patcher: patcher.NewPatcher[
			*operatorapiv1.Klusterlet, operatorapiv1.KlusterletSpec, operatorapiv1.KlusterletStatus](fakeOperatorClient.OperatorV1().Klusterlets()),
		kubeClient:        fakeKubeClient,
		klusterletLister:  operatorInformers.Operator().V1().Klusterlets().Lister(),
		kubeVersion:       kubeVersion,
		operatorNamespace: "open-cluster-management",
		managedClusterClientsBuilder: &fakeManagedClusterBuilder{
			fakeWorkClient:         fakeManagedWorkClient,
			fakeAPIExtensionClient: fakeManagedAPIExtensionClient,
			fakeKubeClient:         fakeManagedKubeClient,
		},
	}

	store := operatorInformers.Operator().V1().Klusterlets().Informer().GetStore()
	if err := store.Add(klusterlet); err != nil {
		t.Fatal(err)
	}

	return &testController{
		controller:         hubController,
		cleanupController:  cleanupController,
		kubeClient:         fakeKubeClient,
		apiExtensionClient: fakeAPIExtensionClient,
		operatorClient:     fakeOperatorClient,
		workClient:         fakeWorkClient,
		operatorStore:      store,
		recorder:           recorder,

		managedKubeClient:         fakeManagedKubeClient,
		managedApiExtensionClient: fakeManagedAPIExtensionClient,
		managedWorkClient:         fakeManagedWorkClient,
	}
}

func (c *testController) setDefaultManagedClusterClientsBuilder() *testController {
	c.controller.managedClusterClientsBuilder = newManagedClusterClientsBuilder(
		c.kubeClient,
		c.apiExtensionClient,
		c.workClient.WorkV1().AppliedManifestWorks(),
		c.recorder,
	)
	c.cleanupController.managedClusterClientsBuilder = newManagedClusterClientsBuilder(
		c.kubeClient,
		c.apiExtensionClient,
		c.workClient.WorkV1().AppliedManifestWorks(),
		c.recorder,
	)
	return c
}

func getDeployments(actions []clienttesting.Action, verb, suffix string) *appsv1.Deployment {

	var deployments []*appsv1.Deployment
	for _, action := range actions {
		if action.GetVerb() != verb || action.GetResource().Resource != "deployments" {
			continue
		}

		if verb == createVerb {
			object := action.(clienttesting.CreateActionImpl).Object
			deployments = append(deployments, object.(*appsv1.Deployment))
		}

		if verb == "update" {
			object := action.(clienttesting.UpdateActionImpl).Object
			deployments = append(deployments, object.(*appsv1.Deployment))
		}
	}

	for _, deployment := range deployments {
		if strings.HasSuffix(deployment.Name, suffix) {
			return deployment
		}
	}

	return nil
}

func assertKlusterletDeployment(t *testing.T, actions []clienttesting.Action, verb, serverURL, clusterName string) {
	deployment := getDeployments(actions, verb, "agent")
	if deployment == nil {
		t.Errorf("klusterlet deployment not found")
		return
	}
	if len(deployment.Spec.Template.Spec.Containers) != 1 {
		t.Errorf("Expect 1 containers in deployment spec, actual %d", len(deployment.Spec.Template.Spec.Containers))
		return
	}

	args := deployment.Spec.Template.Spec.Containers[0].Args
	volumeMounts := deployment.Spec.Template.Spec.Containers[0].VolumeMounts
	volumes := deployment.Spec.Template.Spec.Volumes

	expectedArgs := []string{
		"/registration-operator",
		"agent",
		fmt.Sprintf("--spoke-cluster-name=%s", clusterName),
		"--bootstrap-kubeconfig=/spoke/bootstrap/kubeconfig",
	}

	if serverURL != "" {
		expectedArgs = append(expectedArgs, fmt.Sprintf("--spoke-external-server-urls=%s", serverURL))
	}

	expectedArgs = append(expectedArgs, "--agent-id=", "--workload-source-driver=kube", "--workload-source-config=/spoke/hub-kubeconfig/kubeconfig",
		"--status-sync-interval=60s", "--kube-api-qps=20", "--kube-api-burst=60",
		"--registration-auth=awsirsa",
		"--hub-cluster-arn=arn:aws:eks:us-west-2:123456789012:cluster/hub-cluster1",
		"--managed-cluster-arn=arn:aws:eks:us-west-2:123456789012:cluster/managed-cluster1",
		"--managed-cluster-role-suffix=7f8141296c75f2871e3d030f85c35692")

	if !equality.Semantic.DeepEqual(args, expectedArgs) {
		t.Errorf("Expect args %v, but got %v", expectedArgs, args)
		return
	}

	assert.True(t, isDotAwsMounted(volumeMounts))
	assert.True(t, isDotAwsVolumePresent(volumes))

}

func isDotAwsVolumePresent(volumes []corev1.Volume) bool {
	for _, volume := range volumes {
		if volume.Name == "dot-aws" {
			return true
		}
	}
	return false
}

func isDotAwsMounted(mounts []corev1.VolumeMount) bool {
	for _, mount := range mounts {
		if mount.Name == "dot-aws" && mount.MountPath == "/.aws" {
			return true
		}
	}
	return false
}

func assertRegistrationDeployment(t *testing.T, actions []clienttesting.Action, verb, serverURL, clusterName string, replica int32, awsAuth bool) {
	deployment := getDeployments(actions, verb, "registration-agent")
	if deployment == nil {
		t.Errorf("registration deployment not found")
		return
	}
	if len(deployment.Spec.Template.Spec.Containers) != 1 {
		t.Errorf("Expect 1 containers in deployment spec, actual %d", len(deployment.Spec.Template.Spec.Containers))
		return
	}

	args := deployment.Spec.Template.Spec.Containers[0].Args
	expectedArgs := []string{
		"/registration",
		"agent",
		fmt.Sprintf("--spoke-cluster-name=%s", clusterName),
		"--bootstrap-kubeconfig=/spoke/bootstrap/kubeconfig",
	}

	if serverURL != "" {
		expectedArgs = append(expectedArgs, fmt.Sprintf("--spoke-external-server-urls=%s", serverURL))
	}

	expectedArgs = append(expectedArgs, "--kube-api-qps=10", "--kube-api-burst=60")
	if awsAuth {
		expectedArgs = append(expectedArgs, "--registration-auth=awsirsa",
			"--hub-cluster-arn=arn:aws:eks:us-west-2:123456789012:cluster/hub-cluster1",
			"--managed-cluster-arn=arn:aws:eks:us-west-2:123456789012:cluster/managed-cluster1",
			"--managed-cluster-role-suffix=7f8141296c75f2871e3d030f85c35692")
	}
	if !equality.Semantic.DeepEqual(args, expectedArgs) {
		t.Errorf("Expect args %v, but got %v", expectedArgs, args)
		return
	}

	if *deployment.Spec.Replicas != replica {
		t.Errorf("Unexpected registration replica, expect %d, got %d", replica, *deployment.Spec.Replicas)
		return
	}
}

func assertWorkDeployment(t *testing.T, actions []clienttesting.Action, verb, clusterName string, mode operatorapiv1.InstallMode, replica int32) {
	deployment := getDeployments(actions, verb, "work-agent")
	if deployment == nil {
		t.Errorf("work deployment not found")
		return
	}
	if len(deployment.Spec.Template.Spec.Containers) != 1 {
		t.Errorf("Expect 1 containers in deployment spec, actual %d", len(deployment.Spec.Template.Spec.Containers))
		return
	}
	args := deployment.Spec.Template.Spec.Containers[0].Args
	expectArgs := []string{
		"/work",
		"agent",
		fmt.Sprintf("--spoke-cluster-name=%s", clusterName),
		"--workload-source-driver=kube",
		"--workload-source-config=/spoke/hub-kubeconfig/kubeconfig",
		"--agent-id=",
	}

	if helpers.IsHosted(mode) {
		expectArgs = append(expectArgs,
			"--spoke-kubeconfig=/spoke/config/kubeconfig",
			"--terminate-on-files=/spoke/config/kubeconfig")
	}
	expectArgs = append(expectArgs, "--terminate-on-files=/spoke/hub-kubeconfig/kubeconfig")

	if *deployment.Spec.Replicas == 1 {
		expectArgs = append(expectArgs, "--status-sync-interval=60s")
	}

	expectArgs = append(expectArgs, "--kube-api-qps=20", "--kube-api-burst=50")

	if !equality.Semantic.DeepEqual(args, expectArgs) {
		t.Errorf("Expect args %v, but got %v", expectArgs, args)
		return
	}
	if *deployment.Spec.Replicas != replica {
		t.Errorf("Unexpected registration replica, expect %d, got %d", replica, *deployment.Spec.Replicas)
		return
	}
}

func ensureObject(t *testing.T, object runtime.Object, klusterlet *operatorapiv1.Klusterlet, enableSyncLabels bool) {
	access, err := meta.Accessor(object)
	if err != nil {
		t.Errorf("Unable to access objectmeta: %v", err)
		return
	}

	if enableSyncLabels && !helpers.MapCompare(helpers.GetKlusterletAgentLabels(klusterlet), access.GetLabels()) {
		t.Errorf("the labels of klusterlet are not synced to %v", access.GetName())
		return
	}

	namespace := helpers.AgentNamespace(klusterlet)
	switch o := object.(type) { //nolint:gocritic
	case *appsv1.Deployment:
		if strings.Contains(access.GetName(), "registration") { //nolint:gocritic
			testingcommon.AssertEqualNameNamespace(
				t, access.GetName(), access.GetNamespace(),
				fmt.Sprintf("%s-registration-agent", klusterlet.Name), namespace)
			if klusterlet.Spec.RegistrationImagePullSpec != o.Spec.Template.Spec.Containers[0].Image {
				t.Errorf("Image does not match to the expected.")
				return
			}
		} else if strings.Contains(access.GetName(), "work") {
			testingcommon.AssertEqualNameNamespace(
				t, access.GetName(), access.GetNamespace(),
				fmt.Sprintf("%s-work-agent", klusterlet.Name), namespace)
			if klusterlet.Spec.WorkImagePullSpec != o.Spec.Template.Spec.Containers[0].Image {
				t.Errorf("Image does not match to the expected.")
				return
			}
		} else if strings.Contains(access.GetName(), "agent") {
			testingcommon.AssertEqualNameNamespace(
				t, access.GetName(), access.GetNamespace(),
				fmt.Sprintf("%s-agent", klusterlet.Name), namespace)
			if klusterlet.Spec.ImagePullSpec != o.Spec.Template.Spec.Containers[0].Image {
				t.Errorf("Image does not match to the expected.")
				return
			}
		} else {
			t.Errorf("unexpected deployment")
			return
		}
	}
}

// TestSyncDeploy test deployment of klusterlet components
func TestSyncDeploy(t *testing.T) {
	cases := []struct {
		name             string
		enableSyncLabels bool
	}{
		{
			name:             "disable sync labels",
			enableSyncLabels: false,
		},
		{
			name:             "enable sync labels",
			enableSyncLabels: true,
		},
	}

	for _, c := range cases {
		klusterlet := newKlusterlet("klusterlet", "testns", "cluster1")
		bootStrapSecret := newSecret(helpers.BootstrapHubKubeConfig, "testns")
		hubKubeConfigSecret := newSecret(helpers.HubKubeConfig, "testns")
		hubKubeConfigSecret.Data["kubeconfig"] = []byte("dummuykubeconnfig")
		namespace := newNamespace("testns")
		syncContext := testingcommon.NewFakeSyncContext(t, "klusterlet")

		t.Run(c.name, func(t *testing.T) {
			controller := newTestController(t, klusterlet, syncContext.Recorder(), nil, c.enableSyncLabels,
				bootStrapSecret, hubKubeConfigSecret, namespace)

			err := controller.controller.sync(context.TODO(), syncContext)
			if err != nil {
				t.Errorf("Expected non error when sync, %v", err)
			}

			var createObjects []runtime.Object
			kubeActions := controller.kubeClient.Actions()
			for _, action := range kubeActions {
				if action.GetVerb() == createVerb {
					object := action.(clienttesting.CreateActionImpl).Object
					createObjects = append(createObjects, object)
				}
			}

			// Check if resources are created as expected
			// 11 managed static manifests + 12 management static manifests - 2 duplicated service account manifests + 1 addon namespace + 2 deployments
			if len(createObjects) != 24 {
				t.Errorf("Expect 24 objects created in the sync loop, actual %d", len(createObjects))
			}
			for _, object := range createObjects {
				ensureObject(t, object, klusterlet, false)
			}

			apiExtenstionAction := controller.apiExtensionClient.Actions()
			var createCRDObjects []runtime.Object
			for _, action := range apiExtenstionAction {
				if action.GetVerb() == createVerb && action.GetResource().Resource == crdResourceName {
					object := action.(clienttesting.CreateActionImpl).Object
					createCRDObjects = append(createCRDObjects, object)
				}
			}
			if len(createCRDObjects) != 2 {
				t.Errorf("Expect 2 objects created in the sync loop, actual %d", len(createCRDObjects))
			}

			operatorAction := controller.operatorClient.Actions()
			testingcommon.AssertActions(t, operatorAction, "patch")
			klusterlet = &operatorapiv1.Klusterlet{}
			patchData := operatorAction[0].(clienttesting.PatchActionImpl).Patch
			err = json.Unmarshal(patchData, klusterlet)
			if err != nil {
				t.Fatal(err)
			}
			testinghelper.AssertOnlyConditions(
				t, klusterlet,
				testinghelper.NamedCondition(operatorapiv1.ConditionKlusterletApplied, "KlusterletApplied", metav1.ConditionTrue),
				testinghelper.NamedCondition(helpers.FeatureGatesTypeValid, helpers.FeatureGatesReasonAllValid, metav1.ConditionTrue),
			)
		})
	}
}

func TestSyncDeploySingleton(t *testing.T) {
	cases := []struct {
		name             string
		enableSyncLabels bool
	}{
		{
			name:             "disable sync labels",
			enableSyncLabels: false,
		},
		{
			name:             "enable sync labels",
			enableSyncLabels: true,
		},
	}

	for _, c := range cases {
		klusterlet := newKlusterlet("klusterlet", "testns", "cluster1")
		klusterlet.SetLabels(map[string]string{"test": "123", "abc": "abc", "123": "213"})
		klusterlet.Spec.DeployOption.Mode = operatorapiv1.InstallModeSingleton
		bootStrapSecret := newSecret(helpers.BootstrapHubKubeConfig, "testns")
		hubKubeConfigSecret := newSecret(helpers.HubKubeConfig, "testns")
		hubKubeConfigSecret.Data["kubeconfig"] = []byte("dummuykubeconnfig")
		namespace := newNamespace("testns")
		syncContext := testingcommon.NewFakeSyncContext(t, "klusterlet")

		t.Run(c.name, func(t *testing.T) {
			controller := newTestController(t, klusterlet, syncContext.Recorder(), nil,
				c.enableSyncLabels, bootStrapSecret, hubKubeConfigSecret, namespace)

			err := controller.controller.sync(context.TODO(), syncContext)
			if err != nil {
				t.Errorf("Expected non error when sync, %v", err)
			}

			var createObjects []runtime.Object
			kubeActions := controller.kubeClient.Actions()
			for _, action := range kubeActions {
				if action.GetVerb() == createVerb {
					object := action.(clienttesting.CreateActionImpl).Object
					createObjects = append(createObjects, object)

				}
			}

			// Check if resources are created as expected
			// 10 managed static manifests + 11 management static manifests - 1 service account manifests + 1 addon namespace + 1 deployments
			if len(createObjects) != 22 {
				t.Errorf("Expect 21 objects created in the sync loop, actual %d", len(createObjects))
			}
			for _, object := range createObjects {
				ensureObject(t, object, klusterlet, false)
			}

			apiExtenstionAction := controller.apiExtensionClient.Actions()
			var createCRDObjects []runtime.Object
			for _, action := range apiExtenstionAction {
				if action.GetVerb() == createVerb && action.GetResource().Resource == crdResourceName {
					object := action.(clienttesting.CreateActionImpl).Object
					createCRDObjects = append(createCRDObjects, object)
				}
			}
			if len(createCRDObjects) != 2 {
				t.Errorf("Expect 2 objects created in the sync loop, actual %d", len(createCRDObjects))
			}

			operatorAction := controller.operatorClient.Actions()
			testingcommon.AssertActions(t, operatorAction, "patch")
			klusterlet = &operatorapiv1.Klusterlet{}
			patchData := operatorAction[0].(clienttesting.PatchActionImpl).Patch
			err = json.Unmarshal(patchData, klusterlet)
			if err != nil {
				t.Fatal(err)
			}
			testinghelper.AssertOnlyConditions(
				t, klusterlet,
				testinghelper.NamedCondition(operatorapiv1.ConditionKlusterletApplied, "KlusterletApplied", metav1.ConditionTrue),
				testinghelper.NamedCondition(helpers.FeatureGatesTypeValid, helpers.FeatureGatesReasonAllValid, metav1.ConditionTrue),
			)
		})
	}
}

// TestSyncDeployHosted test deployment of klusterlet components in hosted mode
func TestSyncDeployHosted(t *testing.T) {
	klusterlet := newKlusterletHosted("klusterlet", "testns", "cluster1")
	meta.SetStatusCondition(&klusterlet.Status.Conditions, metav1.Condition{
		Type: operatorapiv1.ConditionReadyToApply, Status: metav1.ConditionTrue, Reason: "KlusterletPrepared",
		Message: "Klusterlet is ready to apply, the external managed kubeconfig secret was created at: " +
			hostedKubeconfigCreationTime,
	})
	agentNamespace := helpers.AgentNamespace(klusterlet)
	bootStrapSecret := newSecret(helpers.BootstrapHubKubeConfig, agentNamespace)
	hubKubeConfigSecret := newSecret(helpers.HubKubeConfig, agentNamespace)
	hubKubeConfigSecret.Data["kubeconfig"] = []byte("dummuykubeconnfig")
	// externalManagedSecret := newSecret(helpers.ExternalManagedKubeConfig, agentNamespace)
	// externalManagedSecret.Data["kubeconfig"] = []byte("dummuykubeconnfig")
	namespace := newNamespace(agentNamespace)
	pullSecret := newSecret(helpers.ImagePullSecret, "open-cluster-management")

	syncContext := testingcommon.NewFakeSyncContext(t, "klusterlet")
	controller := newTestControllerHosted(t, klusterlet, syncContext.Recorder(), nil, bootStrapSecret,
		hubKubeConfigSecret, namespace, pullSecret /*externalManagedSecret*/)

	err := controller.controller.sync(context.TODO(), syncContext)
	if err != nil {
		t.Errorf("Expected non error when sync, %v", err)
	}

	var createObjectsManagement []runtime.Object
	kubeActions := controller.kubeClient.Actions()
	for _, action := range kubeActions {
		if action.GetVerb() == createVerb {
			object := action.(clienttesting.CreateActionImpl).Object
			klog.Infof("management kube create: %v\t resource:%v \t namespace:%v", object.GetObjectKind(), action.GetResource(), action.GetNamespace())
			createObjectsManagement = append(createObjectsManagement, object)
		}
	}
	// Check if resources are created as expected on the management cluster
	// 11 static manifests + 2 secrets(external-managed-kubeconfig-registration,external-managed-kubeconfig-work) +
	// 2 deployments(registration-agent,work-agent) + 1 pull secret
	if len(createObjectsManagement) != 16 {
		t.Errorf("Expect 16 objects created in the sync loop, actual %d", len(createObjectsManagement))
	}
	for _, object := range createObjectsManagement {
		ensureObject(t, object, klusterlet, false)
	}

	var createObjectsManaged []runtime.Object
	for _, action := range controller.managedKubeClient.Actions() {
		if action.GetVerb() == createVerb {

			object := action.(clienttesting.CreateActionImpl).Object
			klog.Infof("managed kube create: %v\t resource:%v \t namespace:%v", object.GetObjectKind().GroupVersionKind(), action.GetResource(), action.GetNamespace())
			createObjectsManaged = append(createObjectsManaged, object)
		}
	}
	// Check if resources are created as expected on the managed cluster
	// 12 static manifests + 2 namespaces + 1 pull secret in the addon namespace
	if len(createObjectsManaged) != 15 {
		t.Errorf("Expect 15 objects created in the sync loop, actual %d", len(createObjectsManaged))
	}
	for _, object := range createObjectsManaged {
		ensureObject(t, object, klusterlet, false)
	}

	apiExtenstionAction := controller.apiExtensionClient.Actions()
	var createCRDObjects []runtime.Object
	for _, action := range apiExtenstionAction {
		if action.GetVerb() == createVerb && action.GetResource().Resource == crdResourceName {
			object := action.(clienttesting.CreateActionImpl).Object
			createCRDObjects = append(createCRDObjects, object)
		}
	}
	if len(createCRDObjects) != 0 {
		t.Errorf("Expect 0 objects created in the sync loop, actual %d", len(createCRDObjects))
	}

	var createCRDObjectsManaged []runtime.Object
	for _, action := range controller.managedApiExtensionClient.Actions() {
		if action.GetVerb() == createVerb && action.GetResource().Resource == crdResourceName {
			object := action.(clienttesting.CreateActionImpl).Object
			createCRDObjectsManaged = append(createCRDObjectsManaged, object)
		}
	}
	if len(createCRDObjectsManaged) != 2 {
		t.Errorf("Expect 2 objects created in the sync loop, actual %d", len(createCRDObjectsManaged))
	}

	operatorAction := controller.operatorClient.Actions()
	for _, action := range operatorAction {
		klog.Infof("operator actions, verb:%v \t resource:%v \t namespace:%v", action.GetVerb(), action.GetResource(), action.GetNamespace())
	}

	conditionReady := testinghelper.NamedCondition(operatorapiv1.ConditionReadyToApply, "KlusterletPrepared", metav1.ConditionTrue)
	conditionApplied := testinghelper.NamedCondition(operatorapiv1.ConditionKlusterletApplied, "KlusterletApplied", metav1.ConditionTrue)
	conditionFeaturesValid := testinghelper.NamedCondition(
		helpers.FeatureGatesTypeValid, helpers.FeatureGatesReasonAllValid, metav1.ConditionTrue)
	testingcommon.AssertActions(t, operatorAction, "patch")
	klusterlet = &operatorapiv1.Klusterlet{}
	patchData := operatorAction[0].(clienttesting.PatchActionImpl).Patch
	err = json.Unmarshal(patchData, klusterlet)
	if err != nil {
		t.Fatal(err)
	}
	testinghelper.AssertOnlyConditions(
		t, klusterlet, conditionReady, conditionApplied,
		conditionFeaturesValid)
}

func TestSyncDeployHostedCreateAgentNamespace(t *testing.T) {
	klusterlet := newKlusterletHosted("klusterlet", "testns", "cluster1")
	meta.SetStatusCondition(&klusterlet.Status.Conditions, metav1.Condition{
		Type: operatorapiv1.ConditionReadyToApply, Status: metav1.ConditionFalse, Reason: "KlusterletPrepareFailed",
		Message: "Failed to build managed cluster clients: secrets \"external-managed-kubeconfig\" not found",
	})
	syncContext := testingcommon.NewFakeSyncContext(t, "klusterlet")
	controller := newTestControllerHosted(t, klusterlet, syncContext.Recorder(), nil).setDefaultManagedClusterClientsBuilder()

	err := controller.controller.sync(context.TODO(), syncContext)
	if !errors.IsNotFound(err) {
		t.Errorf("Expected not found error when sync, but got %v", err)
	}

	kubeActions := controller.kubeClient.Actions()
	testingcommon.AssertGet(t, kubeActions[0], "", "v1", "namespaces")
	testingcommon.AssertAction(t, kubeActions[1], createVerb)
	if kubeActions[1].GetResource().Resource != "namespaces" {
		t.Errorf("expect object namespaces, but got %v", kubeActions[2].GetResource().Resource)
	}
}

func TestRemoveOldNamespace(t *testing.T) {
	klusterlet := newKlusterlet("klusterlet", "testns", "cluster1")
	bootStrapSecret := newSecret(helpers.BootstrapHubKubeConfig, "testns")
	hubKubeConfigSecret := newSecret(helpers.HubKubeConfig, "testns")
	hubKubeConfigSecret.Data["kubeconfig"] = []byte("dummuykubeconnfig")
	namespace := newNamespace("testns")
	oldNamespace := newNamespace("oldns")
	oldNamespace.Labels = map[string]string{
		klusterletNamespaceLabelKey: "klusterlet",
	}
	syncContext := testingcommon.NewFakeSyncContext(t, "klusterlet")
	controller := newTestController(t, klusterlet, syncContext.Recorder(), nil, false,
		bootStrapSecret, hubKubeConfigSecret, namespace, oldNamespace)

	err := controller.controller.sync(context.TODO(), syncContext)
	if err != nil {
		t.Errorf("Expected non error when sync, %v", err)
	}

	var deleteNamespaces []string
	kubeActions := controller.kubeClient.Actions()
	for _, action := range kubeActions {
		if action.GetVerb() == deleteVerb && action.GetResource().Resource == "namespaces" {
			ns := action.(clienttesting.DeleteActionImpl).Name
			deleteNamespaces = append(deleteNamespaces, ns)
		}
	}
	if len(deleteNamespaces) != 0 {
		t.Errorf("expect no namespace deleted before applied")
	}

	controller.kubeClient.ClearActions()
	meta.SetStatusCondition(&klusterlet.Status.Conditions, metav1.Condition{
		Type:   operatorapiv1.ConditionKlusterletApplied,
		Status: metav1.ConditionTrue,
	})
	if err := controller.operatorStore.Update(klusterlet); err != nil {
		t.Fatal(err)
	}
	err = controller.controller.sync(context.TODO(), syncContext)
	if err != nil {
		t.Errorf("Expected non error when sync, %v", err)
	}

	kubeActions = controller.kubeClient.Actions()
	for _, action := range kubeActions {
		if action.GetVerb() == deleteVerb && action.GetResource().Resource == "namespaces" {
			ns := action.(clienttesting.DeleteActionImpl).Name
			deleteNamespaces = append(deleteNamespaces, ns)
		}
	}

	expectedSet := sets.New[string]("oldns")
	actualSet := sets.New[string](deleteNamespaces...)
	if !expectedSet.Equal(actualSet) {
		t.Errorf("ns deletion not correct, got %v", actualSet)
	}
}

// TestSyncDeploy test deployment of klusterlet components
func TestSyncDisableAddonNamespace(t *testing.T) {
	klusterlet := newKlusterlet("klusterlet", "testns", "cluster1")
	bootStrapSecret := newSecret(helpers.BootstrapHubKubeConfig, "testns")
	hubKubeConfigSecret := newSecret(helpers.HubKubeConfig, "testns")
	hubKubeConfigSecret.Data["kubeconfig"] = []byte("dummuykubeconnfig")
	namespace := newNamespace("testns")
	syncContext := testingcommon.NewFakeSyncContext(t, "klusterlet")
	controller := newTestController(t, klusterlet, syncContext.Recorder(), nil, false,
		bootStrapSecret, hubKubeConfigSecret, namespace)
	controller.controller.disableAddonNamespace = true

	err := controller.controller.sync(context.TODO(), syncContext)
	if err != nil {
		t.Errorf("Expected non error when sync, %v", err)
	}

	var ns []runtime.Object
	kubeActions := controller.kubeClient.Actions()
	for _, action := range kubeActions {
		if action.GetVerb() == createVerb {
			object := action.(clienttesting.CreateActionImpl).Object
			if object.GetObjectKind().GroupVersionKind().Kind == "Namespace" {
				ns = append(ns, object)
			}
		}
	}

	// Check if resources are created as expected, only 0 ns is created
	if len(ns) != 0 {
		t.Errorf("Expect 0 ns created in the sync loop, actual %d", len(ns))
	}

	operatorAction := controller.operatorClient.Actions()
	testingcommon.AssertActions(t, operatorAction, "patch")
	klusterlet = &operatorapiv1.Klusterlet{}
	patchData := operatorAction[0].(clienttesting.PatchActionImpl).Patch
	err = json.Unmarshal(patchData, klusterlet)
	if err != nil {
		t.Fatal(err)
	}
	testinghelper.AssertOnlyConditions(
		t, klusterlet,
		testinghelper.NamedCondition(operatorapiv1.ConditionKlusterletApplied, "KlusterletApplied", metav1.ConditionTrue),
		testinghelper.NamedCondition(helpers.FeatureGatesTypeValid, helpers.FeatureGatesReasonAllValid, metav1.ConditionTrue),
	)
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
					operatorapiv1.ServerURL{URL: server})
			}
			actual := getServersFromKlusterlet(klusterlet)
			if actual != c.expected {
				t.Errorf("Expected to be same, actual %q, expected %q", actual, c.expected)
			}
		})
	}
}

func TestAWSIrsaAuthInSingletonModeWithInvalidClusterArns(t *testing.T) {
	klusterlet := newKlusterlet("klusterlet", "testns", "cluster1")
	awsIrsaRegistrationDriver := operatorapiv1.RegistrationDriver{
		AuthType: commonhelpers.AwsIrsaAuthType,
		AwsIrsa: &operatorapiv1.AwsIrsa{
			HubClusterArn:     "arn:aws:bks:us-west-2:123456789012:cluster/hub-cluster1",
			ManagedClusterArn: "arn:aws:eks:us-west-2:123456789012:cluster/managed-cluster1",
		},
	}
	klusterlet.Spec.RegistrationConfiguration.RegistrationDriver = awsIrsaRegistrationDriver
	klusterlet.Spec.DeployOption.Mode = operatorapiv1.InstallModeSingleton
	hubSecret := newSecret(helpers.HubKubeConfig, "testns")
	hubSecret.Data["kubeconfig"] = []byte("dummykubeconfig")
	hubSecret.Data["cluster-name"] = []byte("cluster1")
	objects := []runtime.Object{
		newNamespace("testns"),
		newSecret(helpers.BootstrapHubKubeConfig, "testns"),
		hubSecret,
	}

	syncContext := testingcommon.NewFakeSyncContext(t, "klusterlet")
	controller := newTestController(t, klusterlet, syncContext.Recorder(), nil, false,
		objects...)

	err := controller.controller.sync(context.TODO(), syncContext)
	if err != nil {
		assert.Equal(t, err.Error(), "HubClusterArn arn:aws:bks:us-west-2:123456789012:cluster/hub-cluster1 is not well formed")
	}

}

func TestAWSIrsaAuthInSingletonMode(t *testing.T) {
	klusterlet := newKlusterlet("klusterlet", "testns", "cluster1")
	awsIrsaRegistrationDriver := operatorapiv1.RegistrationDriver{
		AuthType: commonhelpers.AwsIrsaAuthType,
		AwsIrsa: &operatorapiv1.AwsIrsa{
			HubClusterArn:     "arn:aws:eks:us-west-2:123456789012:cluster/hub-cluster1",
			ManagedClusterArn: "arn:aws:eks:us-west-2:123456789012:cluster/managed-cluster1",
		},
	}
	klusterlet.Spec.RegistrationConfiguration.RegistrationDriver = awsIrsaRegistrationDriver
	klusterlet.Spec.DeployOption.Mode = operatorapiv1.InstallModeSingleton
	hubSecret := newSecret(helpers.HubKubeConfig, "testns")
	hubSecret.Data["kubeconfig"] = []byte("dummykubeconfig")
	hubSecret.Data["cluster-name"] = []byte("cluster1")
	objects := []runtime.Object{
		newNamespace("testns"),
		newSecret(helpers.BootstrapHubKubeConfig, "testns"),
		hubSecret,
	}

	syncContext := testingcommon.NewFakeSyncContext(t, "klusterlet")
	controller := newTestController(t, klusterlet, syncContext.Recorder(), nil, false,
		objects...)

	err := controller.controller.sync(context.TODO(), syncContext)
	if err != nil {
		t.Errorf("Expected non error when sync, %v", err)
	}

	assertKlusterletDeployment(t, controller.kubeClient.Actions(), createVerb, "", "cluster1")
}

func TestAWSIrsaAuthInNonSingletonMode(t *testing.T) {
	klusterlet := newKlusterlet("klusterlet", "testns", "cluster1")
	awsIrsaRegistrationDriver := operatorapiv1.RegistrationDriver{
		AuthType: commonhelpers.AwsIrsaAuthType,
		AwsIrsa: &operatorapiv1.AwsIrsa{
			HubClusterArn:     "arn:aws:eks:us-west-2:123456789012:cluster/hub-cluster1",
			ManagedClusterArn: "arn:aws:eks:us-west-2:123456789012:cluster/managed-cluster1",
		},
	}
	klusterlet.Spec.RegistrationConfiguration.RegistrationDriver = awsIrsaRegistrationDriver
	hubSecret := newSecret(helpers.HubKubeConfig, "testns")
	hubSecret.Data["kubeconfig"] = []byte("dummuykubeconnfig")
	hubSecret.Data["cluster-name"] = []byte("cluster1")
	objects := []runtime.Object{
		newNamespace("testns"),
		newSecret(helpers.BootstrapHubKubeConfig, "testns"),
		hubSecret,
	}

	syncContext := testingcommon.NewFakeSyncContext(t, "klusterlet")
	controller := newTestController(t, klusterlet, syncContext.Recorder(), nil, false,
		objects...)

	err := controller.controller.sync(context.TODO(), syncContext)
	if err != nil {
		t.Errorf("Expected non error when sync, %v", err)
	}

	assertRegistrationDeployment(t, controller.kubeClient.Actions(), createVerb, "", "cluster1", 1, true)
}

func TestReplica(t *testing.T) {
	klusterlet := newKlusterlet("klusterlet", "testns", "cluster1")
	hubSecret := newSecret(helpers.HubKubeConfig, "testns")
	hubSecret.Data["kubeconfig"] = []byte("dummuykubeconnfig")
	hubSecret.Data["cluster-name"] = []byte("cluster1")
	objects := []runtime.Object{
		newNamespace("testns"),
		newSecret(helpers.BootstrapHubKubeConfig, "testns"),
		hubSecret,
	}

	syncContext := testingcommon.NewFakeSyncContext(t, "klusterlet")
	controller := newTestController(t, klusterlet, syncContext.Recorder(), nil, false,
		objects...)

	err := controller.controller.sync(context.TODO(), syncContext)
	if err != nil {
		t.Errorf("Expected non error when sync, %v", err)
	}

	// should have 1 replica for registration deployment and 0 for work
	assertRegistrationDeployment(t, controller.kubeClient.Actions(), createVerb, "", "cluster1", 1, false)
	assertWorkDeployment(t, controller.kubeClient.Actions(), createVerb, "cluster1", operatorapiv1.InstallModeDefault, 0)

	klusterlet = newKlusterlet("klusterlet", "testns", "cluster1")
	klusterlet.Status.Conditions = []metav1.Condition{
		{
			Type:   operatorapiv1.ConditionHubConnectionDegraded,
			Status: metav1.ConditionFalse,
		},
	}

	if err := controller.operatorStore.Update(klusterlet); err != nil {
		t.Fatal(err)
	}

	controller.kubeClient.ClearActions()
	controller.operatorClient.ClearActions()

	err = controller.controller.sync(context.TODO(), syncContext)
	if err != nil {
		t.Errorf("Expected non error when sync, %v", err)
	}

	// should have 1 replica for work
	assertWorkDeployment(t, controller.kubeClient.Actions(), "update", "cluster1", operatorapiv1.InstallModeDefault, 1)

	controller.kubeClient.PrependReactor("list", "nodes", func(action clienttesting.Action) (handled bool, ret runtime.Object, err error) {
		if action.GetVerb() != "list" {
			return false, nil, nil
		}

		nodes := &corev1.NodeList{Items: []corev1.Node{*newNode("master1"), *newNode("master2"), *newNode("master3")}}

		return true, nodes, nil
	})

	controller.kubeClient.ClearActions()
	controller.operatorClient.ClearActions()

	err = controller.controller.sync(context.TODO(), syncContext)
	if err != nil {
		t.Errorf("Expected non error when sync, %v", err)
	}

	// should have 3 replicas for clusters with multiple nodes
	assertRegistrationDeployment(t, controller.kubeClient.Actions(), "update", "", "cluster1", 3, false)
	assertWorkDeployment(t, controller.kubeClient.Actions(), "update", "cluster1", operatorapiv1.InstallModeDefault, 3)
}

func TestClusterNameChange(t *testing.T) {
	klusterlet := newKlusterlet("klusterlet", "testns", "cluster1")
	namespace := newNamespace("testns")
	bootStrapSecret := newSecret(helpers.BootstrapHubKubeConfig, "testns")
	hubSecret := newSecret(helpers.HubKubeConfig, "testns")
	hubSecret.Data["kubeconfig"] = []byte("dummuykubeconnfig")
	hubSecret.Data["cluster-name"] = []byte("cluster1")
	syncContext := testingcommon.NewFakeSyncContext(t, "klusterlet")
	controller := newTestController(t, klusterlet, syncContext.Recorder(), nil, false,
		bootStrapSecret, hubSecret, namespace)

	err := controller.controller.sync(context.TODO(), syncContext)
	if err != nil {
		t.Errorf("Expected non error when sync, %v", err)
	}

	// Check if deployment has the right cluster name set
	assertRegistrationDeployment(t, controller.kubeClient.Actions(), createVerb, "", "cluster1", 1, false)

	operatorAction := controller.operatorClient.Actions()
	testingcommon.AssertActions(t, operatorAction, "patch")
	klusterlet = &operatorapiv1.Klusterlet{}
	patchData := operatorAction[0].(clienttesting.PatchActionImpl).Patch
	err = json.Unmarshal(patchData, klusterlet)
	if err != nil {
		t.Fatal(err)
	}

	testinghelper.AssertOnlyGenerationStatuses(
		t, klusterlet,
		testinghelper.NamedDeploymentGenerationStatus("klusterlet-registration-agent", "testns", 0),
		testinghelper.NamedDeploymentGenerationStatus("klusterlet-work-agent", "testns", 0),
	)

	// Update klusterlet with unset cluster name and rerun sync
	controller.kubeClient.ClearActions()
	controller.operatorClient.ClearActions()
	klusterlet = newKlusterlet("klusterlet", "testns", "")
	klusterlet.Generation = 1
	if err := controller.operatorStore.Update(klusterlet); err != nil {
		t.Fatal(err)
	}

	err = controller.controller.sync(context.TODO(), syncContext)
	if err != nil {
		t.Errorf("Expected non error when sync, %v", err)
	}
	assertRegistrationDeployment(t, controller.kubeClient.Actions(), "update", "", "", 1, false)

	// Update hubconfigsecret and sync again
	hubSecret.Data["cluster-name"] = []byte("cluster2")
	controller.kubeClient.PrependReactor("get", "secrets", func(action clienttesting.Action) (handled bool, ret runtime.Object, err error) {
		if action.GetVerb() != "get" {
			return false, nil, nil
		}

		getAction := action.(clienttesting.GetActionImpl)
		if getAction.Name != helpers.HubKubeConfig {
			return false, nil, errors.NewNotFound(
				corev1.Resource("secrets"), helpers.HubKubeConfig)
		}
		return true, hubSecret, nil
	})
	controller.kubeClient.ClearActions()

	err = controller.controller.sync(context.TODO(), syncContext)
	if err != nil {
		t.Errorf("Expected non error when sync, %v", err)
	}
	assertWorkDeployment(t, controller.kubeClient.Actions(), "update", "cluster2", "", 0)

	// Update klusterlet with different cluster name and rerun sync
	klusterlet = newKlusterlet("klusterlet", "testns", "cluster3")
	klusterlet.Generation = 2
	klusterlet.Spec.ExternalServerURLs = []operatorapiv1.ServerURL{{URL: "https://localhost"}}
	controller.kubeClient.ClearActions()
	controller.operatorClient.ClearActions()
	if err := controller.operatorStore.Update(klusterlet); err != nil {
		t.Fatal(err)
	}

	err = controller.controller.sync(context.TODO(), syncContext)
	if err != nil {
		t.Errorf("Expected non error when sync, %v", err)
	}
	assertRegistrationDeployment(t, controller.kubeClient.Actions(), "update", "https://localhost", "cluster3", 1, false)
	assertWorkDeployment(t, controller.kubeClient.Actions(), "update", "cluster3", "", 0)
}

func TestSyncWithPullSecret(t *testing.T) {
	klusterlet := newKlusterlet("klusterlet", "testns", "cluster1")
	bootStrapSecret := newSecret(helpers.BootstrapHubKubeConfig, "testns")
	hubKubeConfigSecret := newSecret(helpers.HubKubeConfig, "testns")
	hubKubeConfigSecret.Data["kubeconfig"] = []byte("dummuykubeconnfig")
	namespace := newNamespace("testns")
	pullSecret := newSecret(helpers.ImagePullSecret, "open-cluster-management")
	syncContext := testingcommon.NewFakeSyncContext(t, "klusterlet")
	controller := newTestController(t, klusterlet, syncContext.Recorder(), nil, false,
		bootStrapSecret, hubKubeConfigSecret, namespace, pullSecret)

	err := controller.controller.sync(context.TODO(), syncContext)
	if err != nil {
		t.Errorf("Expected non error when sync, %v", err)
	}

	var createdSecret *corev1.Secret
	kubeActions := controller.kubeClient.Actions()
	for _, action := range kubeActions {
		if action.GetVerb() == createVerb && action.GetResource().Resource == "secrets" {
			createdSecret = action.(clienttesting.CreateActionImpl).Object.(*corev1.Secret)
			break
		}
	}

	if createdSecret == nil || createdSecret.Name != helpers.ImagePullSecret {
		t.Errorf("Failed to sync pull secret")
	}
}

func TestDeployOnKube111(t *testing.T) {
	klusterlet := newKlusterlet("klusterlet", "testns", "cluster1")
	bootStrapSecret := newSecret(helpers.BootstrapHubKubeConfig, "testns")
	bootStrapSecret.Data["kubeconfig"] = newKubeConfig("testhost")
	hubKubeConfigSecret := newSecret(helpers.HubKubeConfig, "testns")
	hubKubeConfigSecret.Data["kubeconfig"] = []byte("dummuykubeconnfig")
	namespace := newNamespace("testns")
	syncContext := testingcommon.NewFakeSyncContext(t, "klusterlet")
	controller := newTestController(t, klusterlet, syncContext.Recorder(), nil, false,
		bootStrapSecret, hubKubeConfigSecret, namespace)
	kubeVersion, _ := version.ParseGeneric("v1.11.0")
	controller.controller.kubeVersion = kubeVersion
	controller.cleanupController.kubeVersion = kubeVersion

	ctx := context.TODO()
	err := controller.controller.sync(ctx, syncContext)
	if err != nil {
		t.Errorf("Expected non error when sync, %v", err)
	}

	var createObjects []runtime.Object
	kubeActions := controller.kubeClient.Actions()
	for _, action := range kubeActions {
		if action.GetVerb() == createVerb {
			object := action.(clienttesting.CreateActionImpl).Object
			createObjects = append(createObjects, object)
		}
	}

	// Check if resources are created as expected
	// 12 managed static manifests + 11 management static manifests -
	// 2 duplicated service account manifests + 1 addon namespace + 2 deployments + 2 kube111 clusterrolebindings
	if len(createObjects) != 26 {
		t.Errorf("Expect 26 objects created in the sync loop, actual %d", len(createObjects))
	}
	for _, object := range createObjects {
		ensureObject(t, object, klusterlet, false)
	}

	operatorAction := controller.operatorClient.Actions()
	testingcommon.AssertActions(t, operatorAction, "patch")
	updatedKlusterlet := &operatorapiv1.Klusterlet{}
	patchData := operatorAction[0].(clienttesting.PatchActionImpl).Patch
	err = json.Unmarshal(patchData, updatedKlusterlet)
	if err != nil {
		t.Fatal(err)
	}

	testinghelper.AssertOnlyConditions(
		t, updatedKlusterlet,
		testinghelper.NamedCondition(operatorapiv1.ConditionKlusterletApplied, "KlusterletApplied", metav1.ConditionTrue),
		testinghelper.NamedCondition(helpers.FeatureGatesTypeValid, helpers.FeatureGatesReasonAllValid, metav1.ConditionTrue),
	)

	// Delete the klusterlet
	now := metav1.Now()
	klusterlet.ObjectMeta.SetDeletionTimestamp(&now)
	if err := controller.operatorStore.Update(klusterlet); err != nil {
		t.Fatal(err)
	}
	controller.kubeClient.ClearActions()
	err = controller.cleanupController.sync(ctx, syncContext)
	if err != nil {
		t.Errorf("Expected non error when sync, %v", err)
	}

	var deleteActions []clienttesting.DeleteActionImpl
	kubeActions = controller.kubeClient.Actions()
	for _, action := range kubeActions {
		if action.GetVerb() == "delete" {
			deleteAction := action.(clienttesting.DeleteActionImpl)
			deleteActions = append(deleteActions, deleteAction)
		}
	}

	// 12 managed static manifests + 11 management static manifests + 1 hub kubeconfig + 2 namespaces + 2 deployments + 2 kube111 clusterrolebindings
	if len(deleteActions) != 31 {
		t.Errorf("Expected 30 delete actions, but got %d", len(deleteActions))
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
			name:     "CustomResourceRequirements",
			resource: burstable,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config := newFakeKlusterletConfigWithResourceRequirement(t, tt.resource)
			for _, file := range getManifestFiles() {
				manifest, err := manifests.KlusterletManifestFiles.ReadFile(file)
				if err != nil {
					t.Errorf("Failed to read file %s", file)
				}
				objData := assets.MustCreateAssetFromTemplate(file, manifest, config).Data
				deploy := &appsv1.Deployment{}
				if err = yaml.Unmarshal(objData, deploy); err != nil {
					t.Errorf("Failed to unmarshal deployment: %v", err)
				}
				actual := deploy.Spec.Template.Spec.Containers[0].Resources
				actualStr := actual.String()
				expectedStr := tt.resource.ResourceRequirements.String()
				if actualStr != expectedStr {
					t.Errorf("expect:\n%s\nbut got:\n%s", expectedStr, actualStr)
				}
			}
		})
	}
}

func newKubeConfig(host string) []byte {
	configData, _ := runtime.Encode(clientcmdlatest.Codec, &clientcmdapi.Config{
		Clusters: map[string]*clientcmdapi.Cluster{"test-cluster": {
			Server:                host,
			InsecureSkipTLSVerify: true,
		}},
		Contexts: map[string]*clientcmdapi.Context{"test-context": {
			Cluster: "test-cluster",
		}},
		CurrentContext: "test-context",
	})
	return configData
}

func newAppliedManifestWorks(host string, finalizers []string, terminated bool) *workapiv1.AppliedManifestWork {
	w := &workapiv1.AppliedManifestWork{
		ObjectMeta: metav1.ObjectMeta{
			Name:       fmt.Sprintf("%s-%s", fmt.Sprintf("%x", sha256.Sum256([]byte(host))), rand.String(6)),
			Finalizers: finalizers,
		},
	}

	if terminated {
		now := metav1.Now()
		w.DeletionTimestamp = &now
	}

	return w
}

func getManifestFiles() []string {
	return []string{
		"klusterlet/management/klusterlet-agent-deployment.yaml",
		"klusterlet/management/klusterlet-registration-deployment.yaml",
		"klusterlet/management/klusterlet-work-deployment.yaml",
	}
}

func newFakeKlusterletConfigWithResourceRequirement(t *testing.T, r *operatorapiv1.ResourceRequirement) klusterletConfig {
	klusterlet := &operatorapiv1.Klusterlet{
		ObjectMeta: metav1.ObjectMeta{
			Name: "fake-name",
		},
		Spec: operatorapiv1.KlusterletSpec{
			Namespace:                 "fake-namespace",
			RegistrationImagePullSpec: "fake-registration-image",
			WorkImagePullSpec:         "fake-work-image",
			ClusterName:               "fake-cluster",
			DeployOption: operatorapiv1.KlusterletDeployOption{
				Mode: operatorapiv1.InstallModeDefault,
			},
			HubApiServerHostAlias: &operatorapiv1.HubApiServerHostAlias{
				IP:       "10.99.199.199",
				Hostname: "fake-hostname",
			},
			ResourceRequirement: r,
		},
	}

	requirements, err := helpers.ResourceRequirements(klusterlet)
	if err != nil {
		t.Errorf("Failed to parse resource requirements: %v", err)
	}

	config := klusterletConfig{
		KlusterletName:                  klusterlet.Name,
		KlusterletNamespace:             helpers.KlusterletNamespace(klusterlet),
		AgentNamespace:                  helpers.AgentNamespace(klusterlet),
		RegistrationImage:               klusterlet.Spec.RegistrationImagePullSpec,
		WorkImage:                       klusterlet.Spec.WorkImagePullSpec,
		ClusterName:                     klusterlet.Spec.ClusterName,
		BootStrapKubeConfigSecret:       helpers.BootstrapHubKubeConfig,
		HubKubeConfigSecret:             helpers.HubKubeConfig,
		ExternalServerURL:               getServersFromKlusterlet(klusterlet),
		OperatorNamespace:               "fake-operator-namespace",
		Replica:                         1,
		ExternalManagedKubeConfigSecret: helpers.ExternalManagedKubeConfig,
		ExternalManagedKubeConfigRegistrationSecret: helpers.ExternalManagedKubeConfigRegistration,
		ExternalManagedKubeConfigWorkSecret:         helpers.ExternalManagedKubeConfigWork,
		InstallMode:                                 klusterlet.Spec.DeployOption.Mode,
		HubApiServerHostAlias:                       klusterlet.Spec.HubApiServerHostAlias,
		RegistrationServiceAccount:                  serviceAccountName("fake-registration-sa", klusterlet),
		WorkServiceAccount:                          serviceAccountName("fake-work-sa", klusterlet),
		ResourceRequirementResourceType:             operatorapiv1.ResourceQosClassResourceRequirement,
		ResourceRequirements:                        requirements,
	}
	return config
}

type fakeManagedClusterBuilder struct {
	fakeKubeClient         *fakekube.Clientset
	fakeAPIExtensionClient *fakeapiextensions.Clientset
	fakeWorkClient         *fakeworkclient.Clientset
}

func (f *fakeManagedClusterBuilder) withMode(_ operatorapiv1.InstallMode) managedClusterClientsBuilderInterface {
	return f
}

func (f *fakeManagedClusterBuilder) withKubeConfigSecret(_, _ string) managedClusterClientsBuilderInterface {
	return f
}

func (f *fakeManagedClusterBuilder) build(_ context.Context) (*managedClusterClients, error) {
	t, err := time.Parse(time.RFC3339, hostedKubeconfigCreationTime)
	if err != nil {
		return nil, err
	}
	creationTime := metav1.NewTime(t)
	return &managedClusterClients{
		kubeClient:                f.fakeKubeClient,
		apiExtensionClient:        f.fakeAPIExtensionClient,
		appliedManifestWorkClient: f.fakeWorkClient.WorkV1().AppliedManifestWorks(),
		kubeconfig: &rest.Config{
			Host: "testhost",
			TLSClientConfig: rest.TLSClientConfig{
				CAData: []byte("test"),
			},
		},
		kubeconfigSecretCreationTime: creationTime,
	}, nil
}

func TestGetAppliedManifestWorkEvictionGracePeriod(t *testing.T) {
	cases := []struct {
		name                        string
		klusterlet                  *operatorapiv1.Klusterlet
		workConfiguration           *operatorapiv1.WorkAgentConfiguration
		expectedEvictionGracePeriod string
	}{
		{
			name: "klusterlet is nil",
		},
		{
			name:       "without workConfiguration",
			klusterlet: newKlusterlet("test", "test-ns", "test"),
		},
		{
			name:              "without appliedManifestWorkEvictionGracePeriod",
			klusterlet:        newKlusterlet("test", "test-ns", "test"),
			workConfiguration: &operatorapiv1.WorkAgentConfiguration{},
		},
		{
			name:       "with appliedManifestWorkEvictionGracePeriod",
			klusterlet: newKlusterlet("test", "test-ns", "test"),
			workConfiguration: &operatorapiv1.WorkAgentConfiguration{
				AppliedManifestWorkEvictionGracePeriod: &metav1.Duration{
					Duration: 10 * time.Minute,
				},
			},
			expectedEvictionGracePeriod: "10m",
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if c.klusterlet != nil {
				c.klusterlet.Spec.WorkConfiguration = c.workConfiguration
			}

			actualString := getAppliedManifestWorkEvictionGracePeriod(c.klusterlet)
			if len(actualString) == 0 || len(c.expectedEvictionGracePeriod) == 0 {
				assert.Equal(t, c.expectedEvictionGracePeriod, actualString)
			} else {
				expected, err := time.ParseDuration(c.expectedEvictionGracePeriod)
				if err != nil {
					t.Errorf("Failed to parse duration: %s", c.expectedEvictionGracePeriod)
				}
				actual, err := time.ParseDuration(actualString)
				if err != nil {
					t.Errorf("Failed to parse duration: %s", actualString)
				}
				assert.Equal(t, expected, actual)
			}
		})
	}
}
