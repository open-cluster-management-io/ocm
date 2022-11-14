package hub

import (
	"bytes"
	"context"
	"embed"
	"fmt"
	"html/template"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/serializer/yaml"
	"k8s.io/apimachinery/pkg/util/wait"
	yamlutil "k8s.io/apimachinery/pkg/util/yaml"
	genericapiserver "k8s.io/apiserver/pkg/server"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/restmapper"
	"k8s.io/client-go/tools/clientcmd"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"
	bootstrapapi "k8s.io/cluster-bootstrap/token/api"
	"k8s.io/klog/v2"
	"k8s.io/kubernetes/cmd/kubeadm/app/phases/bootstraptoken/clusterinfo"
	"k8s.io/kubernetes/cmd/kubeadm/app/util/apiclient"
	clusteradmhelpers "open-cluster-management.io/clusteradm/pkg/helpers"

	confighelpers "open-cluster-management.io/ocm-controlplane/config/helpers"
)

var HubNameSpace = "open-cluster-management-hub"
var HubSA = "hub-sa"

//go:embed *.yaml
var fs embed.FS

type Hub struct {
	TokenID     string
	TokenSecret string
}

const BootstrapTokenSecret = `
apiVersion: v1
kind: Secret
metadata:
  name: bootstrap-token-{{ .TokenID }}
  namespace: kube-system
  labels:
    app: cluster-manager
type: bootstrap.kubernetes.io/token
stringData:
  # Token ID and secret. Required.
  token-id: {{ .TokenID }}
  token-secret: {{ .TokenSecret }}

  # Allowed usages.
  usage-bootstrap-authentication: "true"

  # Extra groups to authenticate the token as. Must start with "system:bootstrappers:"
  auth-extra-groups: system:bootstrappers:managedcluster
`

func bootstrapTokenSecret(ctx context.Context, discoveryClient discovery.DiscoveryInterface, dynamicClient dynamic.Interface) error {
	var hub = Hub{
		TokenID:     clusteradmhelpers.RandStringRunes_az09(6),
		TokenSecret: clusteradmhelpers.RandStringRunes_az09(16),
	}
	tmpl := template.Must(template.New("bootstrap").Parse(BootstrapTokenSecret))

	var buf bytes.Buffer
	err := tmpl.Execute(&buf, hub)
	if err != nil {
		klog.Errorf("failed to execute template: %v", err)
		return err
	}
	decoder := yamlutil.NewYAMLOrJSONDecoder(&buf, buf.Len())

	var rawObj runtime.RawExtension
	if err = decoder.Decode(&rawObj); err != nil {
		return err
	}

	obj, gvk, err := yaml.NewDecodingSerializer(unstructured.UnstructuredJSONScheme).Decode(rawObj.Raw, nil, nil)
	if err != nil {
		return err
	}
	unstructuredMap, err := runtime.DefaultUnstructuredConverter.ToUnstructured(obj)
	if err != nil {
		return err
	}

	unstructuredObj := &unstructured.Unstructured{Object: unstructuredMap}

	gr, err := restmapper.GetAPIGroupResources(discoveryClient)
	if err != nil {
		return err
	}

	mapper := restmapper.NewDiscoveryRESTMapper(gr)
	mapping, err := mapper.RESTMapping(gvk.GroupKind(), gvk.Version)
	if err != nil {
		return err
	}

	var dri dynamic.ResourceInterface
	if mapping.Scope.Name() == meta.RESTScopeNameNamespace {
		dri = dynamicClient.Resource(mapping.Resource).Namespace(unstructuredObj.GetNamespace())
	} else {
		dri = dynamicClient.Resource(mapping.Resource)
	}

	obj2, err := dri.Create(context.Background(), unstructuredObj, metav1.CreateOptions{})
	if err != nil && !errors.IsAlreadyExists(err) {
		return err
	}

	fmt.Printf("%s/%s created", obj2.GetKind(), obj2.GetName())
	return nil
}

func Bootstrap(ctx context.Context, config genericapiserver.Config, discoveryClient discovery.DiscoveryInterface, dynamicClient dynamic.Interface, kubeClient kubernetes.Interface) error {
	// bootstrap namespace first
	var defaultns = &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: metav1.NamespaceDefault,
		},
	}
	_, err := kubeClient.CoreV1().Namespaces().Create(ctx, defaultns, metav1.CreateOptions{})
	if err != nil && !errors.IsAlreadyExists(err) {
		klog.Errorf("failed to bootstrap default namespace: %v", err)
		// nolint:nilerr
		return nil // don't klog.Fatal. This only happens when context is cancelled.
	}

	// poll until kube-public created
	if err = wait.PollInfinite(1*time.Second, func() (bool, error) {
		_, err := kubeClient.CoreV1().Namespaces().Get(ctx, metav1.NamespacePublic, metav1.GetOptions{})
		if err != nil {
			return false, nil
		}
		return true, nil
	}); err == nil {
		// configmap cluster-info
		caData, _ := config.SecureServing.Cert.CurrentCertKeyContent()
		kubeconfig := clientcmdapi.Config{
			Clusters: map[string]*clientcmdapi.Cluster{
				"": {
					Server:                   "https://" + config.ExternalAddress,
					CertificateAuthorityData: caData,
				},
			},
		}

		kubeconfigRaw, err := clientcmd.Write(kubeconfig)
		if err != nil {
			return err
		}

		klog.V(1).Infoln("[bootstrap-token] creating/updating ConfigMap in kube-public namespace")
		err = apiclient.CreateOrUpdateConfigMap(kubeClient, &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      bootstrapapi.ConfigMapClusterInfo,
				Namespace: metav1.NamespacePublic,
			},
			Data: map[string]string{
				bootstrapapi.KubeConfigKey: string(kubeconfigRaw),
			},
		})

		if err != nil && !errors.IsAlreadyExists(err) {
			// don't klog.Fatal. This only happens when context is cancelled.
			klog.Errorf("failed to bootstrap cluster-info configmap: %v", err)
			// nolint:nilerr
		}

		err = clusterinfo.CreateClusterInfoRBACRules(kubeClient)
		if err != nil && !errors.IsAlreadyExists(err) {
			// don't klog.Fatal. This only happens when context is cancelled.
			klog.Errorf("failed to bootstrap cluster-info rbac: %v", err)
			// nolint:nilerr
		}
	} else {
		klog.Errorf("failed to get namespace %s: %w", metav1.NamespacePublic, err)
		// nolint:nilerr
	}

	if err = wait.PollInfinite(1*time.Second, func() (bool, error) {
		_, err := kubeClient.CoreV1().Namespaces().Get(ctx, metav1.NamespaceSystem, metav1.GetOptions{})
		if err != nil {
			return false, nil
		}
		return true, nil
	}); err == nil {
		err = bootstrapTokenSecret(ctx, discoveryClient, dynamicClient)
		if err != nil {
			klog.Errorf("failed to bootstrap token secret: %v", err)
			// nolint:nilerr
		}
	} else {
		klog.Errorf("failed to get namespace %s: %w", metav1.NamespaceSystem, err)
		// nolint:nilerr
	}

	return bootstrap(ctx, discoveryClient, dynamicClient)
}

func bootstrap(ctx context.Context, discoveryClient discovery.DiscoveryInterface, dynamicClient dynamic.Interface) error {
	return confighelpers.Bootstrap(ctx, discoveryClient, dynamicClient, fs)
}
