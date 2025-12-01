// Copyright Contributors to the Open Cluster Management project

package webhook

import (
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"

	addonv1alpha1 "open-cluster-management.io/ocm/pkg/addon/webhook/v1alpha1"
	addonv1beta1 "open-cluster-management.io/ocm/pkg/addon/webhook/v1beta1"
	commonoptions "open-cluster-management.io/ocm/pkg/common/options"
)

// SetupWebhookServer sets up the webhook server with addon API schemes and conversion webhooks
func SetupWebhookServer(opts *commonoptions.WebhookOptions) error {
	// Install schemes
	if err := opts.InstallScheme(
		clientgoscheme.AddToScheme,
		addonv1alpha1.Install,
		addonv1beta1.Install,
	); err != nil {
		return err
	}

	// Register ManagedClusterAddOn conversion webhooks (v1alpha1 is Hub)
	opts.InstallWebhook(
		&addonv1alpha1.ManagedClusterAddOnWebhook{},
		&addonv1beta1.ManagedClusterAddOnWebhook{},
	)

	// Register ClusterManagementAddOn conversion webhooks (v1alpha1 is Hub)
	opts.InstallWebhook(
		&addonv1alpha1.ClusterManagementAddOnWebhook{},
		&addonv1beta1.ClusterManagementAddOnWebhook{},
	)

	return nil
}
