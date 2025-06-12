package options

import (
	"context"
	"crypto/tls"
	"fmt"

	"github.com/spf13/pflag"
	"k8s.io/apimachinery/pkg/runtime"
	_ "k8s.io/client-go/plugin/pkg/client/auth" // Import all auth plugins (e.g. Azure, GCP, OIDC, etc.) to ensure exec-entrypoint and run can make use of them.
	"k8s.io/client-go/rest"
	"k8s.io/klog/v2"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/metrics/server"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
)

type WebhookOptions struct {
	Port            int
	MetricsPort     int
	HealthProbePort int
	CertDir         string
	scheme          *runtime.Scheme
	webhooks        []WebhookInitializer

	// for testing
	cfg *rest.Config
}

func NewWebhookOptions() *WebhookOptions {
	return &WebhookOptions{
		Port:            9443,
		MetricsPort:     8080,
		HealthProbePort: 8000,
		scheme:          runtime.NewScheme(),
	}
}

func (c *WebhookOptions) AddFlags(fs *pflag.FlagSet) {
	fs.IntVar(&c.Port, "port", c.Port,
		"Port is the port that the webhook server serves at.")
	fs.IntVar(&c.MetricsPort, "metrics-port", c.MetricsPort,
		"MetricsPort is the port the metric endpoint serves at.")
	fs.IntVar(&c.HealthProbePort, "health-probe-port", c.HealthProbePort,
		"HealthProbePort is the port the health probe endpoint serves at.")
	fs.StringVar(&c.CertDir, "certdir", c.CertDir,
		"CertDir is the directory that contains the server key and certificate. If not set, "+
			"webhook server would look up the server key and certificate in {TempDir}/k8s-webhook-server/serving-certs")
}

type WebhookInitializer interface {
	Init(mgr ctrl.Manager) error
}

func (c *WebhookOptions) InstallWebhook(webhooks ...WebhookInitializer) {
	c.webhooks = append(c.webhooks, webhooks...)
}

func (c *WebhookOptions) InstallScheme(fns ...func(s *runtime.Scheme) error) error {
	for _, f := range fns {
		if err := f(c.scheme); err != nil {
			return err
		}
	}
	return nil
}

func (c *WebhookOptions) RunWebhookServer(ctx context.Context) error {
	logger := klog.LoggerWithName(klog.FromContext(ctx), "Webhook Server")
	// This line prevents controller-runtime from complaining about log.SetLogger never being called
	ctrl.SetLogger(logger)

	if c.cfg == nil {
		c.cfg = ctrl.GetConfigOrDie()
	}

	healthProbeBindAddress, metricsBindAddress := "0", "0"
	if c.HealthProbePort > 0 {
		healthProbeBindAddress = fmt.Sprintf(":%d", c.HealthProbePort)
	}
	if c.MetricsPort > 0 {
		metricsBindAddress = fmt.Sprintf(":%d", c.MetricsPort)
	}

	mgr, err := ctrl.NewManager(c.cfg, ctrl.Options{
		Scheme:                 c.scheme,
		HealthProbeBindAddress: healthProbeBindAddress,
		Metrics: server.Options{
			BindAddress: metricsBindAddress,
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

	if c.HealthProbePort != 0 {
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

	for _, webhookInitializer := range c.webhooks {
		if err := webhookInitializer.Init(mgr); err != nil {
			logger.Error(err, "unable to initialize webhook server")
			return err
		}
	}

	logger.Info("starting manager")
	if err := mgr.Start(ctx); err != nil {
		logger.Error(err, "problem running manager")
		return err
	}
	return nil
}
