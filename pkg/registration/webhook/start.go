package webhook

import (
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"

	clusterv1 "open-cluster-management.io/api/cluster/v1"

	commonoptions "open-cluster-management.io/ocm/pkg/common/options"
	internalv1 "open-cluster-management.io/ocm/pkg/registration/webhook/v1"
	internalv1beta2 "open-cluster-management.io/ocm/pkg/registration/webhook/v1beta2"
)

func SetupWebhookServer(opts *commonoptions.WebhookOptions) error {
	if err := opts.InstallScheme(
		clientgoscheme.AddToScheme,
		clusterv1.Install,
		internalv1beta2.Install,
	); err != nil {
		return err
	}
	opts.InstallWebhook(
		&internalv1.ManagedClusterWebhook{},
		&internalv1beta2.ManagedClusterSetBindingWebhook{})

	return nil
}
