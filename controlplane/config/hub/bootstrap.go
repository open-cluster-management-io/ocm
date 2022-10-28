package hub

import (
	"context"
	"embed"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/klog/v2"
	"k8s.io/kubernetes/cmd/kubeadm/app/phases/bootstraptoken/clusterinfo"

	confighelpers "open-cluster-management.io/ocm-controlplane/config/helpers"
)

var HubNameSpace = "open-cluster-management-hub"
var HubSA = "hub-sa"
var PublicNamespace = "kube-public"

// TODO(ycyaoxdu): not hard-code this
var kubeconfigpath = ".ocmconfig/cert/kube-aggregator.kubeconfig"

//go:embed *.yaml
var fs embed.FS

func Bootstrap(ctx context.Context, discoveryClient discovery.DiscoveryInterface, dynamicClient dynamic.Interface, kubeClient kubernetes.Interface) error {
	// bootstrap namespace first

	var defaultns = &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: "default",
		},
	}
	_, err := kubeClient.CoreV1().Namespaces().Create(ctx, defaultns, metav1.CreateOptions{})
	if err != nil {
		klog.Errorf("failed to bootstrap default namespace: %v", err)
		// nolint:nilerr
		return nil // don't klog.Fatal. This only happens when context is cancelled.
	}

	var sa = &corev1.ServiceAccount{
		ObjectMeta: metav1.ObjectMeta{
			Name: HubSA,
		},
	}
	_, err = kubeClient.CoreV1().ServiceAccounts("default").Create(ctx, sa, metav1.CreateOptions{})
	if err != nil {
		klog.Errorf("failed to bootstrap hub serviceaccount: %v", err)
		// nolint:nilerr
		return nil // don't klog.Fatal. This only happens when context is cancelled.
	}

	var hubns = &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: HubNameSpace,
		},
	}
	_, err = kubeClient.CoreV1().Namespaces().Create(ctx, hubns, metav1.CreateOptions{})
	if err != nil {
		klog.Errorf("failed to bootstrap hub namespace: %v", err)
		// nolint:nilerr
		return nil // don't klog.Fatal. This only happens when context is cancelled.
	}

	// poll until kube-public created
	if err = wait.PollInfinite(1*time.Second, func() (bool, error) {
		_, err := kubeClient.CoreV1().Namespaces().Get(ctx, PublicNamespace, metav1.GetOptions{})
		if err != nil {
			return false, nil
		}
		return true, nil
	}); err == nil {
		// configmap cluster-info
		err = clusterinfo.CreateBootstrapConfigMapIfNotExists(kubeClient, kubeconfigpath)
		if err != nil {
			klog.Errorf("failed to bootstrap cluster-info configmap: %v", err)
			// nolint:nilerr
			return nil // don't klog.Fatal. This only happens when context is cancelled.
		}
	}

	return bootstrap(ctx, discoveryClient, dynamicClient)
}

func bootstrap(ctx context.Context, discoveryClient discovery.DiscoveryInterface, dynamicClient dynamic.Interface) error {
	return confighelpers.Bootstrap(ctx, discoveryClient, dynamicClient, fs)
}
