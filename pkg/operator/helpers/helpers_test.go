package helpers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/openshift/library-go/pkg/assets"
	"github.com/openshift/library-go/pkg/operator/events"
	"github.com/openshift/library-go/pkg/operator/events/eventstesting"
	"github.com/openshift/library-go/pkg/operator/resource/resourceapply"
	operatorhelpers "github.com/openshift/library-go/pkg/operator/v1helpers"
	admissionv1 "k8s.io/api/admissionregistration/v1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	fakeapiextensions "k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset/fake"
	"k8s.io/apimachinery/pkg/api/equality"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/version"
	fakekube "k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/rest"
	clienttesting "k8s.io/client-go/testing"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"
	clientcmdlatest "k8s.io/client-go/tools/clientcmd/api/latest"
	"k8s.io/component-base/featuregate"
	fakeapiregistration "k8s.io/kube-aggregator/pkg/client/clientset_generated/clientset/fake"
	clocktesting "k8s.io/utils/clock/testing"

	ocmfeature "open-cluster-management.io/api/feature"
	operatorapiv1 "open-cluster-management.io/api/operator/v1"

	"open-cluster-management.io/ocm/manifests"
)

const nameFoo = "foo"

func newValidatingWebhookConfiguration(name, svc, svcNameSpace string) *admissionv1.ValidatingWebhookConfiguration {
	return &admissionv1.ValidatingWebhookConfiguration{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
		Webhooks: []admissionv1.ValidatingWebhook{
			{
				ClientConfig: admissionv1.WebhookClientConfig{
					Service: &admissionv1.ServiceReference{
						Name:      svc,
						Namespace: svcNameSpace,
					},
				},
			},
		},
	}
}

func newMutatingWebhookConfiguration(name, svc, svcNameSpace string) *admissionv1.MutatingWebhookConfiguration {
	return &admissionv1.MutatingWebhookConfiguration{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
		Webhooks: []admissionv1.MutatingWebhook{
			{
				ClientConfig: admissionv1.WebhookClientConfig{
					Service: &admissionv1.ServiceReference{
						Name:      svc,
						Namespace: svcNameSpace,
					},
				},
			},
		},
	}
}

func newUnstructured(
	apiVersion, kind, namespace, name string, content map[string]interface{}) *unstructured.Unstructured {
	object := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": apiVersion,
			"kind":       kind,
			"metadata": map[string]interface{}{
				"namespace": namespace,
				"name":      name,
			},
		},
	}
	for key, val := range content {
		object.Object[key] = val
	}

	return object
}

func newDeployment(name, namespace string, generation int64) *appsv1.Deployment {
	return &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:       name,
			Namespace:  namespace,
			Generation: generation,
		},
	}
}

func TestApplyValidatingWebhookConfiguration(t *testing.T) {
	testcase := []struct {
		name          string
		existing      []runtime.Object
		expected      *admissionv1.ValidatingWebhookConfiguration
		expectUpdated bool
	}{
		{
			name:          "Create a new configuration",
			expectUpdated: true,
			existing:      []runtime.Object{},
			expected:      newValidatingWebhookConfiguration("test", "svc1", "svc1"),
		},
		{
			name:          "update an existing configuration",
			expectUpdated: true,
			existing:      []runtime.Object{newValidatingWebhookConfiguration("test", "svc1", "svc1")},
			expected:      newValidatingWebhookConfiguration("test", "svc2", "svc2"),
		},
		{
			name:          "skip update",
			expectUpdated: false,
			existing:      []runtime.Object{newValidatingWebhookConfiguration("test", "svc1", "svc1")},
			expected:      newValidatingWebhookConfiguration("test", "svc1", "svc1"),
		},
	}

	for _, c := range testcase {
		t.Run(c.name, func(t *testing.T) {
			fakeKubeClient := fakekube.NewSimpleClientset(c.existing...)
			_, updated, err := ApplyValidatingWebhookConfiguration(fakeKubeClient.AdmissionregistrationV1(), c.expected)
			if err != nil {
				t.Errorf("Expected no error when applying: %v", err)
			}

			if updated != c.expectUpdated {
				t.Errorf("Expect update is %t, but got %t", c.expectUpdated, updated)
			}
		})
	}
}

func TestApplyMutatingWebhookConfiguration(t *testing.T) {
	testcase := []struct {
		name          string
		existing      []runtime.Object
		expected      *admissionv1.MutatingWebhookConfiguration
		expectUpdated bool
	}{
		{
			name:          "Create a new configuration",
			expectUpdated: true,
			existing:      []runtime.Object{},
			expected:      newMutatingWebhookConfiguration("test", "svc1", "svc1"),
		},
		{
			name:          "update an existing configuration",
			expectUpdated: true,
			existing:      []runtime.Object{newMutatingWebhookConfiguration("test", "svc1", "svc1")},
			expected:      newMutatingWebhookConfiguration("test", "svc2", "svc2"),
		},
		{
			name:          "skip update",
			expectUpdated: false,
			existing:      []runtime.Object{newMutatingWebhookConfiguration("test", "svc1", "svc1")},
			expected:      newMutatingWebhookConfiguration("test", "svc1", "svc1"),
		},
	}

	for _, c := range testcase {
		t.Run(c.name, func(t *testing.T) {
			fakeKubeClient := fakekube.NewSimpleClientset(c.existing...)
			_, updated, err := ApplyMutatingWebhookConfiguration(fakeKubeClient.AdmissionregistrationV1(), c.expected)
			if err != nil {
				t.Errorf("Expected no error when applying: %v", err)
			}

			if updated != c.expectUpdated {
				t.Errorf("Expect update is %t, but got %t", c.expectUpdated, updated)
			}
		})
	}
}

func TestApplyDirectly(t *testing.T) {
	testcase := []struct {
		name                  string
		applyFiles            map[string]runtime.Object
		applyFileNames        []string
		nilapiExtensionClient bool
		expectErr             bool
	}{
		{
			name: "Apply webhooks & secret",
			applyFiles: map[string]runtime.Object{
				"validatingwebhooks": newUnstructured(
					"admissionregistration.k8s.io/v1", "ValidatingWebhookConfiguration", "", "",
					map[string]interface{}{"webhooks": []interface{}{}}),
				"mutatingwebhooks": newUnstructured(
					"admissionregistration.k8s.io/v1", "MutatingWebhookConfiguration", "", "",
					map[string]interface{}{"webhooks": []interface{}{}}),
				"secret": newUnstructured(
					"v1", "Secret", "ns1", "n1", map[string]interface{}{"data": map[string]interface{}{"key1": []byte("key1")}}),
			},
			applyFileNames: []string{"validatingwebhooks", "mutatingwebhooks", "secret"},
			expectErr:      false,
		},
		{
			name: "Apply CRD",
			applyFiles: map[string]runtime.Object{
				"crd": newUnstructured("apiextensions.k8s.io/v1", "CustomResourceDefinition", "", "", map[string]interface{}{}),
			},
			applyFileNames: []string{"crd"},
			expectErr:      false,
		},
		{
			name: "Apply CRD with nil apiExtensionClient",
			applyFiles: map[string]runtime.Object{
				"crd": newUnstructured("apiextensions.k8s.io/v1", "CustomResourceDefinition", "", "", map[string]interface{}{}),
			},
			applyFileNames:        []string{"crd"},
			nilapiExtensionClient: true,
			expectErr:             true,
		},
		{
			name: "Apply unhandled object",
			applyFiles: map[string]runtime.Object{
				"kind1": newUnstructured("v1", "Kind1", "ns1", "n1", map[string]interface{}{"spec": map[string]interface{}{"key1": []byte("key1")}}),
			},
			applyFileNames: []string{"kind1"},
			expectErr:      true,
		},
	}

	for _, c := range testcase {
		t.Run(c.name, func(t *testing.T) {
			fakeKubeClient := fakekube.NewSimpleClientset()
			fakeExtensionClient := fakeapiextensions.NewSimpleClientset()
			fakeApplyFunc := func(name string) ([]byte, error) {
				if c.applyFiles[name] == nil {
					return nil, fmt.Errorf("failed to find file")
				}

				return json.Marshal(c.applyFiles[name])
			}

			cache := resourceapply.NewResourceCache()
			var results []resourceapply.ApplyResult
			switch {
			case c.nilapiExtensionClient:
				results = ApplyDirectly(
					context.TODO(),
					fakeKubeClient, nil,
					eventstesting.NewTestingEventRecorder(t),
					cache,
					fakeApplyFunc,
					c.applyFileNames...,
				)
			default:
				results = ApplyDirectly(
					context.TODO(),
					fakeKubeClient, fakeExtensionClient,
					eventstesting.NewTestingEventRecorder(t),
					cache,
					fakeApplyFunc,
					c.applyFileNames...,
				)
			}

			var aggregatedErr []error
			for _, r := range results {
				if r.Error != nil {
					aggregatedErr = append(aggregatedErr, r.Error)
				}
			}

			if len(aggregatedErr) == 0 && c.expectErr {
				t.Errorf("Expect an apply error: %s", c.name)
			}
			if len(aggregatedErr) != 0 && !c.expectErr {
				t.Errorf("Expect no apply error, %v", operatorhelpers.NewMultiLineAggregate(aggregatedErr))
			}
		})
	}
}

func TestDeleteStaticObject(t *testing.T) {
	applyFiles := map[string]runtime.Object{
		"validatingwebhooks": newUnstructured(
			"admissionregistration.k8s.io/v1", "ValidatingWebhookConfiguration", "", "",
			map[string]interface{}{"webhooks": []interface{}{}}),
		"mutatingwebhooks": newUnstructured(
			"admissionregistration.k8s.io/v1", "MutatingWebhookConfiguration", "", "",
			map[string]interface{}{"webhooks": []interface{}{}}),
		"secret": newUnstructured(
			"v1", "Secret", "ns1", "n1", map[string]interface{}{"data": map[string]interface{}{"key1": []byte("key1")}}),
		"crd": newUnstructured(
			"apiextensions.k8s.io/v1beta1", "CustomResourceDefinition", "", "", map[string]interface{}{}),
		"kind1": newUnstructured(
			"v1", "Kind1", "ns1", "n1", map[string]interface{}{"spec": map[string]interface{}{"key1": []byte("key1")}}),
	}
	testcase := []struct {
		name                  string
		applyFileName         string
		expectErr             bool
		nilapiExtensionClient bool
	}{
		{
			name:          "Delete validating webhooks",
			applyFileName: "validatingwebhooks",
			expectErr:     false,
		},
		{
			name:          "Delete mutating webhooks",
			applyFileName: "mutatingwebhooks",
			expectErr:     false,
		},
		{
			name:          "Delete secret",
			applyFileName: "secret",
			expectErr:     false,
		},
		{
			name:          "Delete crd",
			applyFileName: "crd",
			expectErr:     false,
		},
		{
			name:                  "Delete crd with nil apiExtensionClient",
			applyFileName:         "crd",
			nilapiExtensionClient: true,
			expectErr:             true,
		},
		{
			name:          "Delete unhandled object",
			applyFileName: "kind1",
			expectErr:     true,
		},
	}

	for _, c := range testcase {
		t.Run(c.name, func(t *testing.T) {
			fakeKubeClient := fakekube.NewSimpleClientset()
			fakeResgistrationClient := fakeapiregistration.NewSimpleClientset().ApiregistrationV1()
			fakeExtensionClient := fakeapiextensions.NewSimpleClientset()
			fakeAssetFunc := func(name string) ([]byte, error) {
				if applyFiles[name] == nil {
					return nil, fmt.Errorf("failed to find file")
				}

				return json.Marshal(applyFiles[name])
			}

			var err error
			switch {
			case c.nilapiExtensionClient:
				err = CleanUpStaticObject(
					context.TODO(),
					fakeKubeClient, nil, fakeResgistrationClient,
					fakeAssetFunc,
					c.applyFileName,
				)
			default:
				err = CleanUpStaticObject(
					context.TODO(),
					fakeKubeClient, fakeExtensionClient, fakeResgistrationClient,
					fakeAssetFunc,
					c.applyFileName,
				)
			}

			if err == nil && c.expectErr {
				t.Errorf("Expect an apply error")
			}
			if err != nil && !c.expectErr {
				t.Errorf("Expect no apply error, %v", err)
			}
		})
	}
}

func TestLoadClientConfigFromSecret(t *testing.T) {
	testcase := []struct {
		name             string
		secret           *corev1.Secret
		expectedCertData []byte
		expectedKeyData  []byte
		expectedErr      string
	}{
		{
			name:        "load from secret without kubeconfig",
			secret:      newKubeConfigSecret("ns1", "secret1", nil, nil, nil),
			expectedErr: "unable to find kubeconfig in secret \"ns1\" \"secret1\"",
		},
		{
			name:   "load kubeconfig without references to external key/cert files",
			secret: newKubeConfigSecret("ns1", "secret1", newKubeConfig("testhost", "", ""), nil, nil),
		},
		{
			name: "load kubeconfig with references to external key/cert files",
			secret: newKubeConfigSecret("ns1", "secret1",
				newKubeConfig("testhost", "tls.crt", "tls.key"), []byte("--- TRUNCATED ---"), []byte("--- REDACTED ---")),
			expectedCertData: []byte("--- TRUNCATED ---"),
			expectedKeyData:  []byte("--- REDACTED ---"),
		},
	}

	for _, c := range testcase {
		t.Run(c.name, func(t *testing.T) {
			config, err := LoadClientConfigFromSecret(c.secret)

			if len(c.expectedErr) > 0 && err == nil {
				t.Errorf("expected %q error", c.expectedErr)
			}

			if len(c.expectedErr) > 0 && err != nil && err.Error() != c.expectedErr {
				t.Errorf("expected %q error, but got %q", c.expectedErr, err.Error())
			}

			if len(c.expectedErr) == 0 && err != nil {
				t.Errorf("unexpected err: %v", err)
			}

			if len(c.expectedCertData) > 0 && !reflect.DeepEqual(c.expectedCertData, config.CertData) {
				t.Errorf("unexpected cert data")
			}

			if len(c.expectedKeyData) > 0 && !reflect.DeepEqual(c.expectedKeyData, config.KeyData) {
				t.Errorf("unexpected key data")
			}
		})
	}
}

func newKubeConfig(host, certFile, keyFile string) []byte {
	configData, _ := runtime.Encode(clientcmdlatest.Codec, &clientcmdapi.Config{
		Clusters: map[string]*clientcmdapi.Cluster{"test-cluster": {
			Server:                fmt.Sprintf("https://%s:443", host),
			InsecureSkipTLSVerify: true,
		}},
		AuthInfos: map[string]*clientcmdapi.AuthInfo{"test-auth": {
			ClientCertificate: certFile,
			ClientKey:         keyFile,
		}},
		Contexts: map[string]*clientcmdapi.Context{"test-context": {
			Cluster:  "test-cluster",
			AuthInfo: "test-auth",
		}},
		CurrentContext: "test-context",
	})
	return configData
}

func newKubeConfigSecret(namespace, name string, kubeConfigData, certData, keyData []byte) *corev1.Secret {
	data := map[string][]byte{}
	if len(kubeConfigData) > 0 {
		data["kubeconfig"] = kubeConfigData
	}
	if len(certData) > 0 {
		data["tls.crt"] = certData
	}
	if len(keyData) > 0 {
		data["tls.key"] = keyData
	}

	return &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Data: data,
	}
}

func TestDeterminReplica(t *testing.T) {
	kubeVersionV113, _ := version.ParseGeneric("v1.13.0")
	kubeVersionV114, _ := version.ParseGeneric("v1.14.0")
	kubeVersionV122, _ := version.ParseGeneric("v1.22.5+5c84e52")

	cases := []struct {
		name            string
		mode            operatorapiv1.InstallMode
		existingNodes   []runtime.Object
		kubeVersion     *version.Version
		expectedReplica int32
	}{
		{
			name:            "single node",
			existingNodes:   []runtime.Object{newNode("node1")},
			expectedReplica: singleReplica,
		},
		{
			name:            "multiple node",
			existingNodes:   []runtime.Object{newNode("node1"), newNode("node2"), newNode("node3")},
			expectedReplica: defaultReplica,
		},
		{
			name:            "single node hosted mode",
			mode:            operatorapiv1.InstallModeHosted,
			existingNodes:   []runtime.Object{newNode("node1")},
			expectedReplica: singleReplica,
		},
		{
			name:            "multiple node hosted mode",
			mode:            operatorapiv1.InstallModeHosted,
			existingNodes:   []runtime.Object{newNode("node1"), newNode("node2"), newNode("node3")},
			expectedReplica: singleReplica,
		},
		{
			name:            "kube v1.13",
			existingNodes:   []runtime.Object{newNode("node1"), newNode("node2"), newNode("node3")},
			kubeVersion:     kubeVersionV113,
			expectedReplica: singleReplica,
		},
		{
			name:            "kube v1.14",
			existingNodes:   []runtime.Object{newNode("node1"), newNode("node2"), newNode("node3")},
			kubeVersion:     kubeVersionV114,
			expectedReplica: defaultReplica,
		},
		{
			name:            "kube v1.22.5+5c84e52",
			existingNodes:   []runtime.Object{newNode("node1"), newNode("node2"), newNode("node3")},
			kubeVersion:     kubeVersionV122,
			expectedReplica: defaultReplica,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			fakeKubeClient := fakekube.NewSimpleClientset(c.existingNodes...)
			replica := DetermineReplica(context.Background(), fakeKubeClient, c.mode, c.kubeVersion, "node-role.kubernetes.io/master=")
			if replica != c.expectedReplica {
				t.Errorf("Unexpected replica, actual: %d, expected: %d", replica, c.expectedReplica)
			}
		})
	}
}

func TestAgentPriorityClassName(t *testing.T) {
	kubeVersionV113, _ := version.ParseGeneric("v1.13.0")
	kubeVersionV114, _ := version.ParseGeneric("v1.14.0")
	kubeVersionV122, _ := version.ParseGeneric("v1.22.5+5c84e52")

	cases := []struct {
		name                      string
		klusterlet                *operatorapiv1.Klusterlet
		kubeVersion               *version.Version
		expectedPriorityClassName string
	}{
		{
			name:        "klusterlet is nil",
			kubeVersion: kubeVersionV114,
		},
		{
			name: "kubeVersion is nil",
			klusterlet: &operatorapiv1.Klusterlet{
				Spec: operatorapiv1.KlusterletSpec{
					PriorityClassName: "test",
				},
			},
		},
		{
			name:        "klusterlet without PriorityClass",
			kubeVersion: kubeVersionV114,
			klusterlet:  &operatorapiv1.Klusterlet{},
		},
		{
			name:        "kube v1.13",
			kubeVersion: kubeVersionV113,
			klusterlet: &operatorapiv1.Klusterlet{
				Spec: operatorapiv1.KlusterletSpec{
					PriorityClassName: "test",
				},
			},
		},
		{
			name:        "kube v1.14",
			kubeVersion: kubeVersionV114,
			klusterlet: &operatorapiv1.Klusterlet{
				Spec: operatorapiv1.KlusterletSpec{
					PriorityClassName: "test",
				},
			},
			expectedPriorityClassName: "test",
		},
		{
			name: "kube v1.22.5+5c84e52",
			klusterlet: &operatorapiv1.Klusterlet{
				Spec: operatorapiv1.KlusterletSpec{
					PriorityClassName: "test",
				},
			},
			kubeVersion:               kubeVersionV122,
			expectedPriorityClassName: "test",
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			priorityClassName := AgentPriorityClassName(c.klusterlet, c.kubeVersion)
			if priorityClassName != c.expectedPriorityClassName {
				t.Errorf("Unexpected priorityClassName, actual: %s, expected: %s", priorityClassName, c.expectedPriorityClassName)
			}
		})
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

func newDeploymentUnstructured(name, namespace string) *unstructured.Unstructured {
	spec := map[string]interface{}{
		"spec": map[string]interface{}{
			"template": map[string]interface{}{
				"spec": map[string]interface{}{
					"containers": []map[string]interface{}{
						{
							"name":  "hub-registration-controller",
							"image": "quay.io/open-cluster-management/registration:latest",
						},
					},
				}}}}

	return newUnstructured("apps/v1", "Deployment", namespace, name, spec)
}

func TestApplyDeployment(t *testing.T) {
	testcases := []struct {
		name                string
		deploymentName      string
		deploymentNamespace string
		nodePlacement       operatorapiv1.NodePlacement
		expectErr           bool
	}{
		{
			name:                "Apply a deployment without nodePlacement",
			deploymentName:      "cluster-manager-registration-controller",
			deploymentNamespace: ClusterManagerDefaultNamespace,
			expectErr:           false,
		},
		{
			name:                "Apply a deployment with nodePlacement",
			deploymentName:      "cluster-manager-registration-controller",
			deploymentNamespace: ClusterManagerDefaultNamespace,
			nodePlacement: operatorapiv1.NodePlacement{
				NodeSelector: map[string]string{"node-role.kubernetes.io/infra": ""},
				Tolerations: []corev1.Toleration{
					{
						Key:      "node-role.kubernetes.io/infra",
						Operator: corev1.TolerationOpExists,
						Effect:   corev1.TaintEffectNoSchedule,
					},
				},
			},
			expectErr: false,
		},
	}

	for _, c := range testcases {
		t.Run(c.name, func(t *testing.T) {
			fakeKubeClient := fakekube.NewSimpleClientset()
			_, _, err := ApplyDeployment(
				context.TODO(),
				fakeKubeClient, []operatorapiv1.GenerationStatus{}, c.nodePlacement,
				func(name string) ([]byte, error) {
					return json.Marshal(newDeploymentUnstructured(c.deploymentName, c.deploymentNamespace))
				},
				eventstesting.NewTestingEventRecorder(t),
				c.deploymentName,
			)
			if err != nil && !c.expectErr {
				t.Errorf("Expect an apply error")
			}

			deployment, err := fakeKubeClient.AppsV1().Deployments(c.deploymentNamespace).Get(context.TODO(), c.deploymentName, metav1.GetOptions{})
			if err != nil {
				t.Errorf("Expect an get error")
			}

			if !reflect.DeepEqual(deployment.Spec.Template.Spec.NodeSelector, c.nodePlacement.NodeSelector) {
				t.Errorf("Expect nodeSelector %v, got %v", c.nodePlacement.NodeSelector, deployment.Spec.Template.Spec.NodeSelector)
			}
			if !reflect.DeepEqual(deployment.Spec.Template.Spec.Tolerations, c.nodePlacement.Tolerations) {
				t.Errorf("Expect Tolerations %v, got %v", c.nodePlacement.Tolerations, deployment.Spec.Template.Spec.Tolerations)
			}
		})
	}
}

func TestApplyEndpoints(t *testing.T) {
	tests := []struct {
		name             string
		existing         []runtime.Object
		input            *corev1.Endpoints
		verifyActions    func(actions []clienttesting.Action, t *testing.T)
		expectedModified bool
	}{
		{
			name: "create",
			existing: []runtime.Object{
				&corev1.Namespace{
					ObjectMeta: metav1.ObjectMeta{Name: nameFoo},
				},
			},
			input: &corev1.Endpoints{
				ObjectMeta: metav1.ObjectMeta{
					Name:      nameFoo,
					Namespace: nameFoo,
				},
				Subsets: []corev1.EndpointSubset{
					{
						Addresses: []corev1.EndpointAddress{
							{
								IP: "192.13.12.1",
							},
						},
						Ports: []corev1.EndpointPort{
							{
								Port:     80,
								Protocol: "tcp",
							},
						},
					},
				},
			},
			expectedModified: true,
			verifyActions: func(actions []clienttesting.Action, t *testing.T) {
				if len(actions) != 2 {
					t.Fatal("action count mismatch")
				}
				if !actions[0].Matches("get", "endpoints") || actions[0].(clienttesting.GetAction).GetName() != nameFoo {
					t.Error("unexpected action:", actions[0])
				}
				if !actions[1].Matches("create", "endpoints") {
					t.Error("unexpected action:", actions[1])
				}
			},
		},
		{
			name: "remain same",
			existing: []runtime.Object{
				&corev1.Namespace{
					ObjectMeta: metav1.ObjectMeta{Name: nameFoo},
				},
				&corev1.Endpoints{
					ObjectMeta: metav1.ObjectMeta{
						Name:      nameFoo,
						Namespace: nameFoo,
					},
					Subsets: []corev1.EndpointSubset{
						{
							Addresses: []corev1.EndpointAddress{
								{
									IP: "192.13.12.1",
								},
							},
							Ports: []corev1.EndpointPort{
								{
									Port: 80,
								},
							},
						},
					},
				},
			},
			input: &corev1.Endpoints{
				ObjectMeta: metav1.ObjectMeta{
					Name:      nameFoo,
					Namespace: nameFoo,
				},
				Subsets: []corev1.EndpointSubset{
					{
						Addresses: []corev1.EndpointAddress{
							{
								IP: "192.13.12.1",
							},
						},
						Ports: []corev1.EndpointPort{
							{
								Port: 80,
							},
						},
					},
				},
			},
			expectedModified: false,
			verifyActions: func(actions []clienttesting.Action, t *testing.T) {
				if len(actions) != 1 {
					t.Fatal("action count mismatch")
				}
				if !actions[0].Matches("get", "endpoints") || actions[0].(clienttesting.GetAction).GetName() != nameFoo {
					t.Error("unexpected action:", actions[0])
				}
			},
		},
		{
			name: "update",
			existing: []runtime.Object{
				&corev1.Namespace{
					ObjectMeta: metav1.ObjectMeta{Name: nameFoo},
				},
				&corev1.Endpoints{
					ObjectMeta: metav1.ObjectMeta{
						Name:      nameFoo,
						Namespace: nameFoo,
					},
					Subsets: []corev1.EndpointSubset{
						{
							Addresses: []corev1.EndpointAddress{
								{
									IP: "192.13.12.1",
								},
							},
							Ports: []corev1.EndpointPort{
								{
									Port: 80,
								},
							},
						},
					},
				},
			},
			input: &corev1.Endpoints{
				ObjectMeta: metav1.ObjectMeta{
					Name:      nameFoo,
					Namespace: nameFoo,
				},
				Subsets: []corev1.EndpointSubset{
					{
						Addresses: []corev1.EndpointAddress{
							{
								IP: "192.13.12.1",
							},
						},
						Ports: []corev1.EndpointPort{
							{
								Port: 81,
							},
						},
					},
				},
			},
			expectedModified: true,
			verifyActions: func(actions []clienttesting.Action, t *testing.T) {
				if len(actions) != 2 {
					t.Fatal("action count mismatch")
				}
				if !actions[0].Matches("get", "endpoints") || actions[0].(clienttesting.GetAction).GetName() != nameFoo {
					t.Error("unexpected action:", actions[0])
				}
				if !actions[1].Matches("update", "endpoints") {
					t.Error("unexpected action:", actions[1])
				}
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			client := fakekube.NewSimpleClientset(test.existing...)
			_, actualModified, err := ApplyEndpoints(context.TODO(), client.CoreV1(), test.input)
			if err != nil {
				t.Fatal(err)
			}
			if test.expectedModified != actualModified {
				t.Errorf("expected %v, got %v", test.expectedModified, actualModified)
			}
			test.verifyActions(client.Actions(), t)
		})
	}
}

func TestGetRelatedResource(t *testing.T) {
	cases := []struct {
		name                    string
		manifestFile            string
		config                  manifests.HubConfig
		expectedErr             error
		expectedRelatedResource operatorapiv1.RelatedResourceMeta
	}{
		{
			name:         "get correct crd relatedResources",
			manifestFile: "cluster-manager/hub/0000_00_addon.open-cluster-management.io_clustermanagementaddons.crd.yaml",
			config: manifests.HubConfig{
				ClusterManagerName: "test",
				Replica:            1,
			},
			expectedErr: nil,
			expectedRelatedResource: operatorapiv1.RelatedResourceMeta{
				Group:     "apiextensions.k8s.io",
				Version:   "v1",
				Resource:  "customresourcedefinitions",
				Namespace: "",
				Name:      "clustermanagementaddons.addon.open-cluster-management.io",
			},
		},
		{
			name:         "get correct clusterrole relatedResources",
			manifestFile: "cluster-manager/hub/cluster-manager-registration-clusterrole.yaml",
			config: manifests.HubConfig{
				ClusterManagerName: "test",
				Replica:            1,
			},
			expectedErr: nil,
			expectedRelatedResource: operatorapiv1.RelatedResourceMeta{
				Group:     "rbac.authorization.k8s.io",
				Version:   "v1",
				Resource:  "clusterroles",
				Namespace: "",
				Name:      "open-cluster-management:test-registration:controller",
			},
		},
		{
			name:         "get correct deployment relatedResources",
			manifestFile: "cluster-manager/management/cluster-manager-registration-deployment.yaml",
			config: manifests.HubConfig{
				ClusterManagerName:      "test",
				ClusterManagerNamespace: "test-namespace",
				Replica:                 1,
			},
			expectedErr: nil,
			expectedRelatedResource: operatorapiv1.RelatedResourceMeta{
				Group:     "apps",
				Version:   "v1",
				Resource:  "deployments",
				Namespace: "test-namespace",
				Name:      "test-registration-controller",
			},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			template, err := manifests.ClusterManagerManifestFiles.ReadFile(c.manifestFile)
			if err != nil {
				t.Errorf("failed to read file %v", err)
			}
			objData := assets.MustCreateAssetFromTemplate(c.manifestFile, template, c.config).Data

			relatedResource, err := GenerateRelatedResource(objData)
			if !errors.Is(err, c.expectedErr) {
				t.Errorf(cmp.Diff(err, c.expectedErr))
			}
			if !reflect.DeepEqual(relatedResource, c.expectedRelatedResource) {
				t.Errorf(cmp.Diff(err, c.expectedErr))
			}
		})

	}
}

func TestSetRelatedResourcesStatusesWithObj(t *testing.T) {
	cases := []struct {
		name                    string
		manifestFile            string
		config                  manifests.HubConfig
		relatedResources        []operatorapiv1.RelatedResourceMeta
		expectedRelatedResource []operatorapiv1.RelatedResourceMeta
	}{
		{
			name:         "append obj to nil relatedResources",
			manifestFile: "cluster-manager/hub/0000_00_addon.open-cluster-management.io_clustermanagementaddons.crd.yaml",
			config: manifests.HubConfig{
				ClusterManagerName: "test",
				Replica:            1,
			},
			relatedResources: nil,
			expectedRelatedResource: []operatorapiv1.RelatedResourceMeta{
				{
					Group:     "apiextensions.k8s.io",
					Version:   "v1",
					Resource:  "customresourcedefinitions",
					Namespace: "",
					Name:      "clustermanagementaddons.addon.open-cluster-management.io",
				},
			},
		},
		{
			name:         "append obj to empty relatedResources",
			manifestFile: "cluster-manager/hub/0000_00_addon.open-cluster-management.io_clustermanagementaddons.crd.yaml",
			config: manifests.HubConfig{
				ClusterManagerName: "test",
				Replica:            1,
			},
			relatedResources: []operatorapiv1.RelatedResourceMeta{},
			expectedRelatedResource: []operatorapiv1.RelatedResourceMeta{
				{
					Group:     "apiextensions.k8s.io",
					Version:   "v1",
					Resource:  "customresourcedefinitions",
					Namespace: "",
					Name:      "clustermanagementaddons.addon.open-cluster-management.io",
				},
			},
		},
		{
			name:         "append obj to relatedResources",
			manifestFile: "cluster-manager/hub/0000_00_addon.open-cluster-management.io_clustermanagementaddons.crd.yaml",
			config: manifests.HubConfig{
				ClusterManagerName: "test",
				Replica:            1,
			},
			relatedResources: []operatorapiv1.RelatedResourceMeta{
				{
					Group:     "apps",
					Version:   "v1",
					Resource:  "deployments",
					Namespace: "test-namespace",
					Name:      "test-registration-controller",
				},
			},
			expectedRelatedResource: []operatorapiv1.RelatedResourceMeta{
				{
					Group:     "apps",
					Version:   "v1",
					Resource:  "deployments",
					Namespace: "test-namespace",
					Name:      "test-registration-controller",
				},
				{
					Group:     "apiextensions.k8s.io",
					Version:   "v1",
					Resource:  "customresourcedefinitions",
					Namespace: "",
					Name:      "clustermanagementaddons.addon.open-cluster-management.io",
				},
			},
		},
		{
			name:         "append duplicate obj to relatedResources",
			manifestFile: "cluster-manager/hub/0000_00_addon.open-cluster-management.io_clustermanagementaddons.crd.yaml",
			config: manifests.HubConfig{
				ClusterManagerName: "test",
				Replica:            1,
			},
			relatedResources: []operatorapiv1.RelatedResourceMeta{
				{
					Group:     "apiextensions.k8s.io",
					Version:   "v1",
					Resource:  "customresourcedefinitions",
					Namespace: "",
					Name:      "clustermanagementaddons.addon.open-cluster-management.io",
				},
			},
			expectedRelatedResource: []operatorapiv1.RelatedResourceMeta{
				{
					Group:     "apiextensions.k8s.io",
					Version:   "v1",
					Resource:  "customresourcedefinitions",
					Namespace: "",
					Name:      "clustermanagementaddons.addon.open-cluster-management.io",
				},
			},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			template, err := manifests.ClusterManagerManifestFiles.ReadFile(c.manifestFile)
			if err != nil {
				t.Errorf("failed to read file %v", err)
			}
			objData := assets.MustCreateAssetFromTemplate(c.manifestFile, template, c.config).Data

			relatedResources := c.relatedResources
			SetRelatedResourcesStatusesWithObj(&relatedResources, objData)
			c.relatedResources = relatedResources
			if !reflect.DeepEqual(c.relatedResources, c.expectedRelatedResource) {
				t.Errorf("Expect to get %v, but got %v", c.expectedRelatedResource, c.relatedResources)
			}
		})

	}
}

func TestRemoveRelatedResourcesStatusesWithObj(t *testing.T) {
	cases := []struct {
		name                    string
		manifestFile            string
		config                  manifests.HubConfig
		relatedResources        []operatorapiv1.RelatedResourceMeta
		expectedRelatedResource []operatorapiv1.RelatedResourceMeta
	}{
		{
			name:         "remove obj from nil relatedResources",
			manifestFile: "cluster-manager/hub/0000_00_addon.open-cluster-management.io_clustermanagementaddons.crd.yaml",
			config: manifests.HubConfig{
				ClusterManagerName: "test",
				Replica:            1,
			},
			relatedResources:        nil,
			expectedRelatedResource: nil,
		},
		{
			name:         "remove obj from empty relatedResources",
			manifestFile: "cluster-manager/hub/0000_00_addon.open-cluster-management.io_clustermanagementaddons.crd.yaml",
			config: manifests.HubConfig{
				ClusterManagerName: "test",
				Replica:            1,
			},
			relatedResources:        []operatorapiv1.RelatedResourceMeta{},
			expectedRelatedResource: []operatorapiv1.RelatedResourceMeta{},
		},
		{
			name:         "remove obj from relatedResources",
			manifestFile: "cluster-manager/hub/0000_00_addon.open-cluster-management.io_clustermanagementaddons.crd.yaml",
			config: manifests.HubConfig{
				ClusterManagerName: "test",
				Replica:            1,
			},
			relatedResources: []operatorapiv1.RelatedResourceMeta{
				{
					Group:     "apps",
					Version:   "v1",
					Resource:  "deployments",
					Namespace: "test-namespace",
					Name:      "test-registration-controller",
				},
				{
					Group:     "apiextensions.k8s.io",
					Version:   "v1",
					Resource:  "customresourcedefinitions",
					Namespace: "",
					Name:      "clustermanagementaddons.addon.open-cluster-management.io",
				},
			},
			expectedRelatedResource: []operatorapiv1.RelatedResourceMeta{
				{
					Group:     "apps",
					Version:   "v1",
					Resource:  "deployments",
					Namespace: "test-namespace",
					Name:      "test-registration-controller",
				},
			},
		},
		{
			name:         "remove not exist obj from relatedResources",
			manifestFile: "cluster-manager/hub/0000_00_addon.open-cluster-management.io_clustermanagementaddons.crd.yaml",
			config: manifests.HubConfig{
				ClusterManagerName: "test",
				Replica:            1,
			},
			relatedResources: []operatorapiv1.RelatedResourceMeta{
				{
					Group:     "apps",
					Version:   "v1",
					Resource:  "deployments",
					Namespace: "test-namespace",
					Name:      "test-registration-controller",
				},
			},
			expectedRelatedResource: []operatorapiv1.RelatedResourceMeta{
				{
					Group:     "apps",
					Version:   "v1",
					Resource:  "deployments",
					Namespace: "test-namespace",
					Name:      "test-registration-controller",
				},
			},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			template, err := manifests.ClusterManagerManifestFiles.ReadFile(c.manifestFile)
			if err != nil {
				t.Errorf("failed to read file %v", err)
			}
			objData := assets.MustCreateAssetFromTemplate(c.manifestFile, template, c.config).Data

			relatedResources := c.relatedResources
			RemoveRelatedResourcesStatusesWithObj(&relatedResources, objData)
			c.relatedResources = relatedResources
			if !reflect.DeepEqual(c.relatedResources, c.expectedRelatedResource) {
				t.Errorf("Expect to get %v, but got %v", c.expectedRelatedResource, c.relatedResources)
			}
		})

	}
}

func TestKlusterletNamespace(t *testing.T) {
	testcases := []struct {
		name       string
		klusterlet *operatorapiv1.Klusterlet
		expect     string
	}{
		{
			name: "Default mode without spec namespace",
			klusterlet: &operatorapiv1.Klusterlet{
				ObjectMeta: metav1.ObjectMeta{
					Name: "klusterlet",
				},
				Spec: operatorapiv1.KlusterletSpec{
					Namespace:    "",
					DeployOption: operatorapiv1.KlusterletDeployOption{},
				}},
			expect: KlusterletDefaultNamespace,
		},
		{
			name: "Default mode with spec namespace",
			klusterlet: &operatorapiv1.Klusterlet{
				ObjectMeta: metav1.ObjectMeta{
					Name: "klusterlet",
				},
				Spec: operatorapiv1.KlusterletSpec{
					Namespace:    "open-cluster-management-test",
					DeployOption: operatorapiv1.KlusterletDeployOption{},
				}},
			expect: "open-cluster-management-test",
		},
		{
			name: "Hosted mode with spec namespace",
			klusterlet: &operatorapiv1.Klusterlet{
				ObjectMeta: metav1.ObjectMeta{
					Name: "klusterlet",
				},
				Spec: operatorapiv1.KlusterletSpec{
					Namespace:    "open-cluster-management-test",
					DeployOption: operatorapiv1.KlusterletDeployOption{Mode: operatorapiv1.InstallModeHosted},
				},
			},
			expect: "open-cluster-management-test",
		},
		{
			name: "Hosted mode without spec namespace",
			klusterlet: &operatorapiv1.Klusterlet{
				ObjectMeta: metav1.ObjectMeta{
					Name: "klusterlet",
				},
				Spec: operatorapiv1.KlusterletSpec{
					Namespace:    "",
					DeployOption: operatorapiv1.KlusterletDeployOption{Mode: operatorapiv1.InstallModeHosted},
				},
			},
			expect: KlusterletDefaultNamespace,
		},
	}

	for _, c := range testcases {
		t.Run(c.name, func(t *testing.T) {
			namespace := KlusterletNamespace(c.klusterlet)
			if namespace != c.expect {
				t.Errorf("Expect namespace %v, got %v", c.expect, namespace)
			}
		})
	}
}

func TestAgentNamespace(t *testing.T) {
	testcases := []struct {
		name       string
		klusterlet *operatorapiv1.Klusterlet
		expect     string
	}{
		{
			name: "Default mode without spec namespace",
			klusterlet: &operatorapiv1.Klusterlet{
				ObjectMeta: metav1.ObjectMeta{
					Name: "klusterlet",
				},
				Spec: operatorapiv1.KlusterletSpec{
					Namespace:    "",
					DeployOption: operatorapiv1.KlusterletDeployOption{},
				}},
			expect: KlusterletDefaultNamespace,
		},
		{
			name: "Default mode with spec namespace",
			klusterlet: &operatorapiv1.Klusterlet{
				ObjectMeta: metav1.ObjectMeta{
					Name: "klusterlet",
				},
				Spec: operatorapiv1.KlusterletSpec{
					Namespace:    "open-cluster-management-test",
					DeployOption: operatorapiv1.KlusterletDeployOption{},
				}},
			expect: "open-cluster-management-test",
		},
		{
			name: "Hosted mode with spec namespace",
			klusterlet: &operatorapiv1.Klusterlet{
				ObjectMeta: metav1.ObjectMeta{
					Name: "klusterlet",
				},
				Spec: operatorapiv1.KlusterletSpec{
					Namespace:    "open-cluster-management-test",
					DeployOption: operatorapiv1.KlusterletDeployOption{Mode: operatorapiv1.InstallModeHosted},
				},
			},
			expect: "klusterlet",
		},
		{
			name: "Hosted mode without spec namespace",
			klusterlet: &operatorapiv1.Klusterlet{
				ObjectMeta: metav1.ObjectMeta{
					Name: "klusterlet",
				},
				Spec: operatorapiv1.KlusterletSpec{
					Namespace:    "",
					DeployOption: operatorapiv1.KlusterletDeployOption{Mode: operatorapiv1.InstallModeHosted},
				},
			},
			expect: "klusterlet",
		},
	}

	for _, c := range testcases {
		t.Run(c.name, func(t *testing.T) {
			namespace := AgentNamespace(c.klusterlet)
			if namespace != c.expect {
				t.Errorf("Expect namespace %v, got %v", c.expect, namespace)
			}
		})
	}
}

func TestSyncSecret(t *testing.T) {
	tt := []struct {
		name                        string
		sourceNamespace, sourceName string
		targetNamespace, targetName string
		ownerRefs                   []metav1.OwnerReference
		existingObjects             []runtime.Object
		expectedSecret              *corev1.Secret
		expectedChanged             bool
		expectedErr                 string
	}{
		{
			name:            "syncing existing secret succeeds when the target is missing",
			sourceNamespace: "sourceNamespace",
			sourceName:      "sourceName",
			targetNamespace: "targetNamespace",
			targetName:      "targetName",
			ownerRefs:       nil,
			existingObjects: []runtime.Object{
				&corev1.Secret{
					ObjectMeta: metav1.ObjectMeta{
						Namespace: "sourceNamespace",
						Name:      "sourceName",
					},
					Type: corev1.SecretTypeOpaque,
					Data: map[string][]byte{nameFoo: []byte("bar")},
				},
			},
			expectedSecret: &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "targetNamespace",
					Name:      "targetName",
				},
				Type: corev1.SecretTypeOpaque,
				Data: map[string][]byte{nameFoo: []byte("bar")},
			},
			expectedChanged: true,
			expectedErr:     "",
		},
		{
			name:            "syncing existing secret succeeds when the target is present and needs update",
			sourceNamespace: "sourceNamespace",
			sourceName:      "sourceName",
			targetNamespace: "targetNamespace",
			targetName:      "targetName",
			ownerRefs:       nil,
			existingObjects: []runtime.Object{
				&corev1.Secret{
					ObjectMeta: metav1.ObjectMeta{
						Namespace: "sourceNamespace",
						Name:      "sourceName",
					},
					Type: corev1.SecretTypeOpaque,
					Data: map[string][]byte{nameFoo: []byte("bar2")},
				},
				&corev1.Secret{
					ObjectMeta: metav1.ObjectMeta{
						Namespace: "targetNamespace",
						Name:      "targetName",
					},
					Type: corev1.SecretTypeOpaque,
					Data: map[string][]byte{nameFoo: []byte("bar1")},
				},
			},
			expectedSecret: &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "targetNamespace",
					Name:      "targetName",
				},
				Type: corev1.SecretTypeOpaque,
				Data: map[string][]byte{nameFoo: []byte("bar2")},
			},
			expectedChanged: true,
			expectedErr:     "",
		},
		{
			name:            "syncing missing source secret doesn't fail",
			sourceNamespace: "sourceNamespace",
			sourceName:      "sourceName",
			targetNamespace: "targetNamespace",
			targetName:      "targetName",
			ownerRefs:       nil,
			existingObjects: []runtime.Object{},
			expectedSecret:  nil,
			expectedChanged: true,
			expectedErr:     "",
		},
		{
			name:            "syncing service account token doesn't sync without the token being present",
			sourceNamespace: "sourceNamespace",
			sourceName:      "sourceName",
			targetNamespace: "targetNamespace",
			targetName:      "targetName",
			ownerRefs:       nil,
			existingObjects: []runtime.Object{
				&corev1.Secret{
					ObjectMeta: metav1.ObjectMeta{
						Namespace: "sourceNamespace",
						Name:      "sourceName",
					},
					Type: corev1.SecretTypeServiceAccountToken,
					Data: map[string][]byte{nameFoo: []byte("bar")},
				},
			},
			expectedSecret:  nil,
			expectedChanged: false,
			expectedErr:     "secret sourceNamespace/sourceName doesn't have a token yet",
		},
		{
			name:            "syncing service account token strips \"managed\" annotations",
			sourceNamespace: "sourceNamespace",
			sourceName:      "sourceName",
			targetNamespace: "targetNamespace",
			targetName:      "targetName",
			ownerRefs:       nil,
			existingObjects: []runtime.Object{
				&corev1.Secret{
					ObjectMeta: metav1.ObjectMeta{
						Namespace: "sourceNamespace",
						Name:      "sourceName",
						Annotations: map[string]string{
							corev1.ServiceAccountNameKey: nameFoo,
							corev1.ServiceAccountUIDKey:  "bar",
						},
					},
					Type: corev1.SecretTypeServiceAccountToken,
					Data: map[string][]byte{"token": []byte("top-secret")},
				},
			},
			expectedSecret: &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "targetNamespace",
					Name:      "targetName",
				},
				Type: corev1.SecretTypeOpaque,
				Data: map[string][]byte{"token": []byte("top-secret")},
			},
			expectedChanged: true,
			expectedErr:     "",
		},
	}

	for _, tc := range tt {
		t.Run(tc.name, func(t *testing.T) {
			client := fakekube.NewSimpleClientset(tc.existingObjects...)
			clientTarget := fakekube.NewSimpleClientset()
			secret, changed, err := SyncSecret(
				context.TODO(), client.CoreV1(), clientTarget.CoreV1(),
				events.NewInMemoryRecorder("test", clocktesting.NewFakePassiveClock(time.Now())), tc.sourceNamespace, tc.sourceName,
				tc.targetNamespace, tc.targetName, tc.ownerRefs, nil)

			if (err == nil && len(tc.expectedErr) != 0) || (err != nil && err.Error() != tc.expectedErr) {
				t.Errorf("%s: expected error %v, got %v", tc.name, tc.expectedErr, err)
				return
			}

			if !equality.Semantic.DeepEqual(secret, tc.expectedSecret) {
				t.Errorf("%s: secrets differ: %s", tc.name, cmp.Diff(tc.expectedSecret, secret))
			}

			if changed != tc.expectedChanged {
				t.Errorf("%s: expected changed %t, got %t", tc.name, tc.expectedChanged, changed)
			}
		})
	}
}

func TestGetHubKubeconfig(t *testing.T) {
	managemengConfig := &rest.Config{Host: "localhost"}
	tt := []struct {
		name         string
		mode         operatorapiv1.InstallMode
		secret       []runtime.Object
		namespace    string
		expectedHost string
		expectedErr  bool
	}{
		{
			name:         "default mode",
			mode:         operatorapiv1.InstallModeDefault,
			secret:       []runtime.Object{},
			namespace:    "test",
			expectedHost: "localhost",
			expectedErr:  false,
		},
		{
			name:         "empty mode",
			mode:         "",
			secret:       []runtime.Object{},
			namespace:    "test",
			expectedHost: "localhost",
			expectedErr:  false,
		},
		{
			name:         "hosted mode no secret",
			mode:         operatorapiv1.InstallModeHosted,
			secret:       []runtime.Object{},
			namespace:    "test",
			expectedHost: "localhost",
			expectedErr:  true,
		},
		{
			name: "hosted mode",
			mode: operatorapiv1.InstallModeHosted,
			secret: []runtime.Object{
				newKubeConfigSecret("test", ExternalHubKubeConfig,
					newKubeConfig("testhost", "tls.crt", "tls.key"), []byte("--- TRUNCATED ---"), []byte("--- REDACTED ---"))},
			namespace:    "test",
			expectedHost: "https://testhost:443",
			expectedErr:  false,
		},
	}

	for _, tc := range tt {
		t.Run(tc.name, func(t *testing.T) {
			client := fakekube.NewSimpleClientset(tc.secret...)
			config, err := GetHubKubeconfig(context.TODO(), managemengConfig, client, tc.namespace, tc.mode)
			if tc.expectedErr && err == nil {
				t.Error("Expect to get err, but got nil")
			}
			if !tc.expectedErr && err != nil {
				t.Errorf("Expect not err, but got %v", err)
			}
			if err != nil {
				return
			}
			if config.Host != tc.expectedHost {
				t.Errorf("Expect host %s, but got %s", tc.expectedHost, config.Host)
			}
		})
	}
}

func TestConvertToFeatureGateFlags(t *testing.T) {
	cases := []struct {
		name         string
		features     []operatorapiv1.FeatureGate
		desiredFlags []string
		desiredMsg   string
	}{
		{
			name:         "unset",
			features:     []operatorapiv1.FeatureGate{},
			desiredFlags: []string{},
		},
		{
			name: "enable feature",
			features: []operatorapiv1.FeatureGate{
				{Feature: "ClusterClaim", Mode: operatorapiv1.FeatureGateModeTypeEnable},
				{Feature: "AddonManagement", Mode: operatorapiv1.FeatureGateModeTypeEnable},
			},
			desiredFlags: []string{},
		},
		{
			name: "disable feature",
			features: []operatorapiv1.FeatureGate{
				{Feature: "ClusterClaim", Mode: operatorapiv1.FeatureGateModeTypeDisable},
				{Feature: "AddonManagement", Mode: operatorapiv1.FeatureGateModeTypeDisable},
			},
			desiredFlags: []string{"--feature-gates=ClusterClaim=false", "--feature-gates=AddonManagement=false"},
		},
		{
			name: "invalid feature",
			features: []operatorapiv1.FeatureGate{
				{Feature: "Foo", Mode: operatorapiv1.FeatureGateModeTypeDisable},
				{Feature: "Bar", Mode: operatorapiv1.FeatureGateModeTypeDisable},
			},
			desiredFlags: []string{},
			desiredMsg:   "test: [Foo Bar]",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			flags, msg := ConvertToFeatureGateFlags("test", tc.features, ocmfeature.DefaultSpokeRegistrationFeatureGates)
			if msg != tc.desiredMsg {
				t.Errorf("Name: %s, unexpected message, got: %s, desired %s", tc.name, msg, tc.desiredMsg)
			}
			if !equality.Semantic.DeepEqual(flags, tc.desiredFlags) {
				t.Errorf("Name: %s, unexpected flags, got %v, desired %v", tc.name, flags, tc.desiredFlags)
			}
		})
	}
}

func TestFeatureGateEnabled(t *testing.T) {
	cases := []struct {
		name          string
		features      []operatorapiv1.FeatureGate
		featureName   featuregate.Feature
		desiredResult bool
	}{
		{
			name:          "default",
			features:      []operatorapiv1.FeatureGate{},
			featureName:   ocmfeature.ClusterClaim,
			desiredResult: true,
		},
		{
			name: "disable",
			features: []operatorapiv1.FeatureGate{
				{Feature: "ClusterClaim", Mode: operatorapiv1.FeatureGateModeTypeDisable},
			},
			featureName:   ocmfeature.ClusterClaim,
			desiredResult: false,
		},
		{
			name: "enable",
			features: []operatorapiv1.FeatureGate{
				{Feature: "AddonManagement", Mode: operatorapiv1.FeatureGateModeTypeEnable},
				{Feature: "ClusterClaim", Mode: operatorapiv1.FeatureGateModeTypeDisable},
			},
			featureName:   ocmfeature.AddonManagement,
			desiredResult: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			enabled := FeatureGateEnabled(tc.features, ocmfeature.DefaultSpokeRegistrationFeatureGates, tc.featureName)
			if enabled != tc.desiredResult {
				t.Errorf("Name: %s, expect feature enabled is %v, but got %v", tc.name, tc.desiredResult, enabled)
			}
		})
	}
}
