package webhook

import (
	"k8s.io/klog/v2"
	"open-cluster-management.io/work/pkg/webhook/common"

	"k8s.io/apimachinery/pkg/runtime"

	// Import all Kubernetes client auth plugins (e.g. Azure, GCP, OIDC, etc.)
	// to ensure that exec-entrypoint and run can make use of them.
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	_ "k8s.io/client-go/plugin/pkg/client/auth"
	workv1 "open-cluster-management.io/api/work/v1"
	webhookv1 "open-cluster-management.io/work/pkg/webhook/v1"

	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
)

var (
	scheme = runtime.NewScheme()
)

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(workv1.Install(scheme))
}

func (c *Options) RunWebhookServer() error {
	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{
		Scheme:                 scheme,
		Port:                   c.Port,
		HealthProbeBindAddress: ":8000",
		CertDir:                c.CertDir,
		WebhookServer:          &webhook.Server{TLSMinVersion: "1.2"},
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

	common.ManifestValidator.WithLimit(c.ManifestLimit)

	if err = (&webhookv1.ManifestWorkWebhook{}).Init(mgr); err != nil {
		klog.Error(err, "unable to create ManagedCluster webhook")
		return err
	}

	klog.Info("starting manager")
	if err := mgr.Start(ctrl.SetupSignalHandler()); err != nil {
		klog.Error(err, "problem running manager")
		return err
	}
	return nil
}
