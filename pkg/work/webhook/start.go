package webhook

import (
	"context"
	"crypto/tls"

	"k8s.io/apimachinery/pkg/runtime"
	// Import all Kubernetes client auth plugins (e.g. Azure, GCP, OIDC, etc.)
	// to ensure that exec-entrypoint and run can make use of them.
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	_ "k8s.io/client-go/plugin/pkg/client/auth"
	"k8s.io/klog/v2"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/metrics/server"
	"sigs.k8s.io/controller-runtime/pkg/webhook"

	ocmfeature "open-cluster-management.io/api/feature"
	workv1 "open-cluster-management.io/api/work/v1"
	workv1alpha1 "open-cluster-management.io/api/work/v1alpha1"

	"open-cluster-management.io/ocm/pkg/features"
	"open-cluster-management.io/ocm/pkg/work/webhook/common"
	webhookv1 "open-cluster-management.io/ocm/pkg/work/webhook/v1"
	webhookv1alpha1 "open-cluster-management.io/ocm/pkg/work/webhook/v1alpha1"
)

var (
	scheme = runtime.NewScheme()
)

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(workv1.Install(scheme))
	utilruntime.Must(workv1alpha1.Install(scheme))
}

func (c *Options) RunWebhookServer() error {
	logger := klog.LoggerWithName(klog.FromContext(context.Background()), "work webhook")
	ctrl.SetLogger(logger)

	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{
		Scheme:                 scheme,
		HealthProbeBindAddress: c.HealthProbeBindAddr,
		Metrics: server.Options{
			BindAddress: c.MetricsBindAddr,
		},
		WebhookServer: webhook.NewServer(webhook.Options{
			TLSOpts: []func(config *tls.Config){
				func(config *tls.Config) {
					config.MinVersion = tls.VersionTLS12
				},
			},
			Port:    c.Port,
			CertDir: c.CertDir,
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

	common.ManifestValidator.WithLimit(c.ManifestLimit)

	if err = (&webhookv1.ManifestWorkWebhook{}).Init(mgr); err != nil {
		logger.Error(err, "unable to create ManifestWork webhook")
		return err
	}

	if features.HubMutableFeatureGate.Enabled(ocmfeature.ManifestWorkReplicaSet) {
		if err = (&webhookv1alpha1.ManifestWorkReplicaSetWebhook{}).Init(mgr); err != nil {
			logger.Error(err, "unable to create ManifestWorkReplicaSet webhook")
			return err
		}
	}

	logger.Info("starting manager")
	if err = mgr.Start(ctrl.SetupSignalHandler()); err != nil {
		logger.Error(err, "problem running manager")
		return err
	}
	return nil
}
