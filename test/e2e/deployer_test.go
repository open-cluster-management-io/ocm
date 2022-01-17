package e2e

import (
	"bytes"
	"context"
	"fmt"
	"io/ioutil"
	"reflect"
	"time"

	"github.com/openshift/library-go/pkg/operator/events"
	"github.com/openshift/library-go/pkg/operator/resource/resourceapply"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	apiextensionsv1beta1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1beta1"
	apiextensionsclient "k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/runtime/serializer/json"
	"k8s.io/apimachinery/pkg/runtime/serializer/streaming"
	"k8s.io/apimachinery/pkg/runtime/serializer/yaml"
	utilerrors "k8s.io/apimachinery/pkg/util/errors"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	workclientset "open-cluster-management.io/api/client/work/clientset/versioned"
	"open-cluster-management.io/work/deploy"
)

var (
	staticResourceFiles = []string{
		"spoke/appliedmanifestworks.crd.yaml",
		"spoke/clusterrole.yaml",
		"spoke/component_namespace.yaml",
		"spoke/service_account.yaml",
		"spoke/clusterrole_binding.yaml",
		"spoke/clusterrole_binding_addition.yaml",
	}
	deploymentFile            = "spoke/deployment.yaml"
	hubKubeconfigSecret       = "hub-kubeconfig-secret"
	defaultComponentNamespace = "open-cluster-management-agent"
)

type workAgentDeployer interface {
	Deploy() error
	Undeploy() error
}

type defaultWorkAgentDeployer struct {
	componentNamespace       string
	clusterName              string
	nameSuffix               string
	image                    string
	spokeKubeClient          kubernetes.Interface
	spokeDynamicClient       dynamic.Interface
	spokeApiExtensionsClient apiextensionsclient.Interface
	hubWorkClient            workclientset.Interface
	cache                    resourceapply.ResourceCache

	resources []runtime.Object
}

func newDefaultWorkAgentDeployer(
	clusterName string,
	nameSuffix string,
	image string,
	spokeKubeClient kubernetes.Interface,
	spokeDynamicClient dynamic.Interface,
	spokeApiExtensionsClient apiextensionsclient.Interface,
	hubWorkClient workclientset.Interface) workAgentDeployer {
	return &defaultWorkAgentDeployer{
		componentNamespace:       defaultComponentNamespace,
		clusterName:              clusterName,
		nameSuffix:               nameSuffix,
		image:                    image,
		spokeKubeClient:          spokeKubeClient,
		spokeDynamicClient:       spokeDynamicClient,
		spokeApiExtensionsClient: spokeApiExtensionsClient,
		hubWorkClient:            hubWorkClient,
		cache:                    resourceapply.NewResourceCache(),
	}
}

func (d *defaultWorkAgentDeployer) Deploy() error {
	// Apply static files
	clientHolder := resourceapply.NewKubeClientHolder(d.spokeKubeClient).WithAPIExtensionsClient(d.spokeApiExtensionsClient)
	applyResults := resourceapply.ApplyDirectly(
		context.Background(),
		clientHolder,
		events.NewInMemoryRecorder(""),
		d.cache,
		func(name string) ([]byte, error) {
			return deploy.SpokeManifestFiles.ReadFile(name)
		},
		staticResourceFiles...,
	)
	errs := []error{}
	for _, result := range applyResults {
		if result.Error != nil {
			errs = append(errs, fmt.Errorf("%q (%T): %v", result.File, result.Type, result.Error))
		}

		if result.Result != nil {
			d.resources = append(d.resources, result.Result)
		}
	}
	if len(errs) > 0 {
		return utilerrors.NewAggregate(errs)
	}

	// create/update hub kubeconfig secret
	data, err := d.getHubKubeconfigSecretData()
	if err != nil {
		return err
	}

	secret, err := d.spokeKubeClient.CoreV1().Secrets(d.componentNamespace).Get(context.TODO(), hubKubeconfigSecret, metav1.GetOptions{})
	switch {
	case errors.IsNotFound(err):
		secret := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: d.componentNamespace,
				Name:      hubKubeconfigSecret,
			},
			Data: data,
		}
		secret, err = d.spokeKubeClient.CoreV1().Secrets(secret.Namespace).Create(context.TODO(), secret, metav1.CreateOptions{})
	case err != nil:
		return err
	default:
		secret.Data = data
		secret, err = d.spokeKubeClient.CoreV1().Secrets(secret.Namespace).Update(context.TODO(), secret, metav1.UpdateOptions{})
	}
	if err != nil {
		return err
	}

	// create deployment
	deployment, err := assetToUnstructured(deploymentFile)
	if err != nil {
		return err
	}
	deployment, err = updateDeployment(
		deployment,
		d.componentNamespace,
		d.clusterName,
		d.image)
	if err != nil {
		return err
	}
	_, err = d.spokeDynamicClient.Resource(schema.GroupVersionResource{
		Group:    "apps",
		Version:  "v1",
		Resource: "deployments",
	}).Namespace(d.componentNamespace).Create(context.TODO(), deployment, metav1.CreateOptions{})
	return err
}

func (d *defaultWorkAgentDeployer) Undeploy() error {
	errs := []error{}
	// delete all manifest works
	err := wait.Poll(1*time.Second, 10*time.Second, func() (bool, error) {
		manifestWorkList, err := d.hubWorkClient.WorkV1().ManifestWorks(d.clusterName).List(context.Background(), metav1.ListOptions{})
		if err != nil {
			return false, nil
		}

		if len(manifestWorkList.Items) == 0 {
			return true, nil
		}

		for _, manifestWork := range manifestWorkList.Items {
			if manifestWork.DeletionTimestamp != nil && !manifestWork.DeletionTimestamp.IsZero() {
				continue
			}

			_ = d.hubWorkClient.WorkV1().ManifestWorks(d.clusterName).Delete(context.Background(), manifestWork.Name, metav1.DeleteOptions{})
		}

		return false, nil
	})
	if err != nil {
		errs = append(errs, err)
	}

	// delete cluster scope resources and the component namespace
	for _, resource := range d.resources {
		var err error
		switch t := resource.(type) {
		case *corev1.Namespace:
			err = d.spokeKubeClient.CoreV1().Namespaces().Delete(context.TODO(), t.Name, metav1.DeleteOptions{})
		case *corev1.ServiceAccount:
			err = d.spokeKubeClient.CoreV1().ServiceAccounts(t.Namespace).Delete(context.TODO(), t.Name, metav1.DeleteOptions{})
		case *rbacv1.ClusterRole:
			err = d.spokeKubeClient.RbacV1().ClusterRoles().Delete(context.TODO(), t.Name, metav1.DeleteOptions{})
		case *rbacv1.ClusterRoleBinding:
			err = d.spokeKubeClient.RbacV1().ClusterRoleBindings().Delete(context.TODO(), t.Name, metav1.DeleteOptions{})
		case *apiextensionsv1.CustomResourceDefinition:
			err = d.spokeApiExtensionsClient.ApiextensionsV1().CustomResourceDefinitions().Delete(context.TODO(), t.Name, metav1.DeleteOptions{})
		case *apiextensionsv1beta1.CustomResourceDefinition:
			err = d.spokeApiExtensionsClient.ApiextensionsV1beta1().CustomResourceDefinitions().Delete(context.TODO(), t.Name, metav1.DeleteOptions{})
		default:
			err = fmt.Errorf("unhandled type %T", resource)
		}

		if err != nil {
			errs = append(errs, err)
		}
	}

	if len(errs) > 0 {
		return utilerrors.NewAggregate(errs)
	}

	return nil
}

// getHubKubeconfigSecretData returns the data for HubKubeconfigSecret. It expects a
// secret exists in either open-cluster-management-agent/e2e-hub-kubeconfig-secret or
// open-cluster-management/e2e-bootstrap-secret
func (d *defaultWorkAgentDeployer) getHubKubeconfigSecretData() (map[string][]byte, error) {
	errs := []error{}
	secret, err := d.spokeKubeClient.CoreV1().Secrets("open-cluster-management-agent").Get(context.TODO(), "e2e-hub-kubeconfig-secret", metav1.GetOptions{})
	if err == nil {
		return secret.Data, nil
	}
	errs = append(errs, err)

	// fall back to open-cluster-management/e2e-bootstrap-secret
	secret, err = d.spokeKubeClient.CoreV1().Secrets("open-cluster-management").Get(context.TODO(), "e2e-bootstrap-secret", metav1.GetOptions{})
	if err == nil {
		return secret.Data, nil
	}
	errs = append(errs, err)
	return nil, utilerrors.NewAggregate(errs)
}

func assetToUnstructured(name string) (*unstructured.Unstructured, error) {
	yamlDecoder := yaml.NewDecodingSerializer(unstructured.UnstructuredJSONScheme)
	raw, err := deploy.SpokeManifestFiles.ReadFile(name)
	if err != nil {
		return nil, err
	}

	reader := json.YAMLFramer.NewFrameReader(ioutil.NopCloser(bytes.NewReader(raw)))
	d := streaming.NewDecoder(reader, yamlDecoder)
	obj, _, err := d.Decode(nil, nil)
	if err != nil {
		return nil, err
	}

	switch t := obj.(type) {
	case *unstructured.Unstructured:
		return t, nil
	default:
		return nil, fmt.Errorf("failed to convert object, unexpected type %s", reflect.TypeOf(obj))
	}
}

func updateDeployment(deployment *unstructured.Unstructured, namespace, clusterName, image string) (*unstructured.Unstructured, error) {
	deployment = deployment.DeepCopy()
	err := unstructured.SetNestedField(deployment.Object, namespace, "meta", "namespace")
	if err != nil {
		return nil, err
	}

	containers, found, err := unstructured.NestedSlice(deployment.Object, "spec", "template", "spec", "containers")
	if err != nil || !found || containers == nil {
		return nil, fmt.Errorf("deployment containers not found or error in spec: %v", err)
	}

	if err := unstructured.SetNestedField(containers[0].(map[string]interface{}), image, "image"); err != nil {
		return nil, err
	}

	args, found, err := unstructured.NestedSlice(containers[0].(map[string]interface{}), "args")
	if err != nil || !found || args == nil {
		return nil, fmt.Errorf("container args not found or error in spec: %v", err)
	}

	clusterNameArg := fmt.Sprintf("--spoke-cluster-name=%v", clusterName)
	args[2] = clusterNameArg

	if err := unstructured.SetNestedField(containers[0].(map[string]interface{}), args, "args"); err != nil {
		return nil, err
	}

	if err := unstructured.SetNestedField(deployment.Object, containers, "spec", "template", "spec", "containers"); err != nil {
		return nil, err
	}

	return deployment, nil
}
