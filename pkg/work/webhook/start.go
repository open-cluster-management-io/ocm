package webhook

import (
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	_ "k8s.io/client-go/plugin/pkg/client/auth"

	ocmfeature "open-cluster-management.io/api/feature"
	workv1 "open-cluster-management.io/api/work/v1"
	workv1alpha1 "open-cluster-management.io/api/work/v1alpha1"

	commonoptions "open-cluster-management.io/ocm/pkg/common/options"
	"open-cluster-management.io/ocm/pkg/features"
	"open-cluster-management.io/ocm/pkg/work/webhook/common"
	webhookv1 "open-cluster-management.io/ocm/pkg/work/webhook/v1"
	webhookv1alpha1 "open-cluster-management.io/ocm/pkg/work/webhook/v1alpha1"
)

func (c *Options) SetupWebhookServer(opts *commonoptions.WebhookOptions) error {
	common.ManifestValidator.WithLimit(c.ManifestLimit)
	if err := opts.InstallScheme(
		clientgoscheme.AddToScheme,
		workv1.Install,
		workv1alpha1.Install,
	); err != nil {
		return err
	}
	opts.InstallWebhook(&webhookv1.ManifestWorkWebhook{})
	if features.HubMutableFeatureGate.Enabled(ocmfeature.ManifestWorkReplicaSet) {
		opts.InstallWebhook(&webhookv1alpha1.ManifestWorkReplicaSetWebhook{})
	}
	return nil
}
