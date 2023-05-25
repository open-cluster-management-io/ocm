package webhook

import (
	"k8s.io/klog/v2"

	"k8s.io/apimachinery/pkg/runtime"

	// Import all Kubernetes client auth plugins (e.g. Azure, GCP, OIDC, etc.)
	// to ensure that exec-entrypoint and run can make use of them.
	_ "k8s.io/client-go/plugin/pkg/client/auth"
	clusterv1 "open-cluster-management.io/api/cluster/v1"
	internalv1 "open-cluster-management.io/registration/pkg/webhook/v1"
	internalv1beta1 "open-cluster-management.io/registration/pkg/webhook/v1beta1"
	internalv1beta2 "open-cluster-management.io/registration/pkg/webhook/v1beta2"

	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"

	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
)

var (
	scheme = runtime.NewScheme()
)

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(clusterv1.Install(scheme))
	utilruntime.Must(internalv1beta1.Install(scheme))
	utilruntime.Must(internalv1beta2.Install(scheme))
}

func (c *Options) RunWebhookServer() error {
	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{
		Scheme:                 scheme,
		Port:                   c.Port,
		HealthProbeBindAddress: ":8000",
		CertDir:                c.CertDir,
		WebhookServer:          &webhook.Server{TLSMinVersion: "1.3"},
	})

	if err != nil {
		klog.Error(err, "unable to start manager")
		return err
	}

	// add healthz/readyz check handler
	if err := mgr.AddHealthzCheck("healthz-ping", healthz.Ping); err != nil {
		klog.Errorf("unable to add healthz check handler: %v", err)
		return err
	}

	if err := mgr.AddReadyzCheck("readyz-ping", healthz.Ping); err != nil {
		klog.Errorf("unable to add readyz check handler: %v", err)
		return err
	}

	if err = (&internalv1.ManagedClusterWebhook{}).Init(mgr); err != nil {
		klog.Error(err, "unable to create ManagedCluster webhook")
		return err
	}
	if err = (&internalv1beta1.ManagedClusterSetBindingWebhook{}).Init(mgr); err != nil {
		klog.Error(err, "unable to create ManagedClusterSetBinding webhook", "v1beta1")
		return err
	}
	if err = (&internalv1beta2.ManagedClusterSetBindingWebhook{}).Init(mgr); err != nil {
		klog.Error(err, "unable to create ManagedClusterSetBinding webhook", "v1beta1")
		return err
	}
	if err = (&internalv1beta1.ManagedClusterSet{}).SetupWebhookWithManager(mgr); err != nil {
		klog.Error(err, "unable to create ManagedClusterSet webhook", "v1beta1")
		return err
	}
	if err = (&internalv1beta2.ManagedClusterSet{}).SetupWebhookWithManager(mgr); err != nil {
		klog.Error(err, "unable to create ManagedClusterSet webhook", "v1beta2")
		return err
	}

	klog.Info("starting manager")
	if err := mgr.Start(ctrl.SetupSignalHandler()); err != nil {
		klog.Error(err, "problem running manager")
		return err
	}
	return nil
}
