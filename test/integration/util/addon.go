package util

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	apiextensionsclient "k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/rest"
	"k8s.io/utils/pointer"
	"sigs.k8s.io/controller-runtime/pkg/envtest"

	addonwebhook "open-cluster-management.io/ocm/pkg/addon/webhook"
	commonoptions "open-cluster-management.io/ocm/pkg/common/options"
)

func StartWebhook(ctx context.Context, testEnv *envtest.Environment, cfg *rest.Config) error {
	webhookOptions := commonoptions.NewWebhookOptions()
	webhookOptions.Port = testEnv.WebhookInstallOptions.LocalServingPort
	webhookOptions.CertDir = testEnv.WebhookInstallOptions.LocalServingCertDir
	webhookOptions.Cfg = cfg
	err := addonwebhook.SetupWebhookServer(webhookOptions)

	if err != nil {
		return err
	}
	return webhookOptions.RunWebhookServer(ctx)
}

func AddConversionForAddonAPI(ctx context.Context, apiExtClient apiextensionsclient.Interface, testEnv *envtest.Environment, names ...string) error {
	caBundle, err := os.ReadFile(
		filepath.Join(
			testEnv.WebhookInstallOptions.LocalServingCertDir,
			"tls.crt",
		),
	)
	if err != nil {
		return err
	}

	for _, name := range names {
		crd, err := apiExtClient.ApiextensionsV1().CustomResourceDefinitions().Get(
			ctx, name, metav1.GetOptions{})
		if err != nil {
			return err
		}
		crd.Spec.Conversion = &apiextensionsv1.CustomResourceConversion{
			Strategy: apiextensionsv1.WebhookConverter,
			Webhook: &apiextensionsv1.WebhookConversion{
				ClientConfig: &apiextensionsv1.WebhookClientConfig{
					URL: pointer.String(fmt.Sprintf("https://%s:%d/convert",
						testEnv.WebhookInstallOptions.LocalServingHost,
						testEnv.WebhookInstallOptions.LocalServingPort)),
					CABundle: caBundle,
				},
				ConversionReviewVersions: []string{"v1"},
			},
		}
		_, err = apiExtClient.
			ApiextensionsV1().
			CustomResourceDefinitions().
			Update(ctx, crd, metav1.UpdateOptions{})
		if err != nil {
			return err
		}
	}

	return nil
}
