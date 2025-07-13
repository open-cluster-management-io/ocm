package webhook

import (
	"context"
	"crypto/tls"

	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	_ "k8s.io/client-go/plugin/pkg/client/auth" // Import all auth plugins (e.g. Azure, GCP, OIDC, etc.) to ensure exec-entrypoint and run can make use of them.
	"k8s.io/klog/v2"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/metrics/server"
	"sigs.k8s.io/controller-runtime/pkg/webhook"

	clusterv1 "open-cluster-management.io/api/cluster/v1"

	internalv1 "open-cluster-management.io/ocm/pkg/registration/webhook/v1"
	internalv1beta2 "open-cluster-management.io/ocm/pkg/registration/webhook/v1beta2"
)

var (
	scheme = runtime.NewScheme()
)

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(clusterv1.Install(scheme))
	utilruntime.Must(internalv1beta2.Install(scheme))
}

func (c *Options) RunWebhookServer() error {
	logger := klog.LoggerWithName(klog.FromContext(context.Background()), "Webhook Server")
	// This line prevents controller-runtime from complaining about log.SetLogger never being called
	ctrl.SetLogger(logger)

	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{
		Scheme:                 scheme,
		HealthProbeBindAddress: c.HealthProbeBindAddr,
		Metrics: server.Options{
			BindAddress: c.MetricsBindAddr,
		},
		WebhookServer: webhook.NewServer(webhook.Options{
			Port:    c.Port,
			CertDir: c.CertDir,
			TLSOpts: []func(config *tls.Config){
				func(config *tls.Config) {
					config.MinVersion = tls.VersionTLS12
				},
			},
		}),
	})
	if err != nil {
		logger.Error(err, "unable to start manager")
		return err
	}

	if c.HealthProbeBindAddr != "" && c.HealthProbeBindAddr != "0" {
		// add healthz/readyz check handler
		if err := mgr.AddHealthzCheck("healthz-ping", healthz.Ping); err != nil {
			logger.Error(err, "unable to add healthz check handler")
			return err
		}

		if err := mgr.AddReadyzCheck("readyz-ping", healthz.Ping); err != nil {
			logger.Error(err, "unable to add readyz check handler")
			return err
		}
	}

	if err = (&internalv1.ManagedClusterWebhook{}).Init(mgr); err != nil {
		logger.Error(err, "unable to create ManagedCluster webhook")
		return err
	}
	if err = (&internalv1beta2.ManagedClusterSetBindingWebhook{}).Init(mgr); err != nil {
		logger.Error(err, "unable to create ManagedClusterSetBinding webhook", "version", "v1beta2")
		return err
	}

	logger.Info("starting manager")
	if err := mgr.Start(ctrl.SetupSignalHandler()); err != nil {
		logger.Error(err, "problem running manager")
		return err
	}
	return nil
}
