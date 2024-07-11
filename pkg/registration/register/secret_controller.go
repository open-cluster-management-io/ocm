package register

import (
	"context"
	"fmt"
	"time"

	"github.com/openshift/library-go/pkg/controller/factory"
	"github.com/openshift/library-go/pkg/operator/events"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	corev1client "k8s.io/client-go/kubernetes/typed/core/v1"
	"k8s.io/client-go/tools/clientcmd"
)

// ControllerResyncInterval is exposed so that integration tests can crank up the constroller sync speed.
var ControllerResyncInterval = 5 * time.Minute

// secretController run process in driver to get credential and keeps the defeined secret in secretOption update-to-date
type secretController struct {
	SecretOption
	option               any
	driver               RegisterDriver
	controllerName       string
	statusUpdater        StatusUpdateFunc
	additionalSecretData map[string][]byte
	secretToSave         *corev1.Secret
}

// NewSecretController return an instance of secretController
func NewSecretController(
	secretOption SecretOption,
	option any,
	driver RegisterDriver,
	statusUpdater StatusUpdateFunc,
	recorder events.Recorder,
	controllerName string,
) factory.Controller {
	additionalSecretData := map[string][]byte{}
	if secretOption.BootStrapKubeConfig != nil {
		kubeConfigTemplate, err := BaseKubeConfigFromBootStrap(secretOption.BootStrapKubeConfig)
		if err != nil {
			utilruntime.Must(err)
		}
		kubeConfig := driver.BuildKubeConfigFromTemplate(kubeConfigTemplate)
		if kubeConfig != nil {
			kubeconfigData, err := clientcmd.Write(*kubeConfig)
			if err != nil {
				utilruntime.Must(err)
			}
			additionalSecretData[KubeconfigFile] = kubeconfigData
		}
	}

	if len(secretOption.ClusterName) > 0 {
		additionalSecretData[ClusterNameFile] = []byte(secretOption.ClusterName)
	}

	if len(secretOption.AgentName) > 0 {
		additionalSecretData[AgentNameFile] = []byte(secretOption.AgentName)
	}

	c := secretController{
		SecretOption:         secretOption,
		driver:               driver,
		controllerName:       controllerName,
		statusUpdater:        statusUpdater,
		additionalSecretData: additionalSecretData,
		option:               option,
	}

	f := factory.New().
		WithFilteredEventsInformersQueueKeyFunc(func(obj runtime.Object) string {
			return factory.DefaultQueueKey
		}, func(obj interface{}) bool {
			accessor, err := meta.Accessor(obj)
			if err != nil {
				return false
			}
			// only enqueue a specific secret
			if accessor.GetNamespace() == c.SecretNamespace && accessor.GetName() == c.SecretName {
				return true
			}
			return false
		}, secretOption.ManagementSecretInformer)

	driverInformer, driverFilter := driver.InformerHandler(option)
	if driverInformer != nil && driverFilter != nil {
		f = f.WithFilteredEventsInformersQueueKeyFunc(func(obj runtime.Object) string {
			return factory.DefaultQueueKey
		}, driverFilter, driverInformer)
	}

	return f.WithSync(c.sync).
		ResyncEvery(ControllerResyncInterval).
		ToController(controllerName, recorder)
}

func (c *secretController) sync(ctx context.Context, syncCtx factory.SyncContext) error {
	// get secret containing client certificate
	secret, err := c.ManagementCoreClient.Secrets(c.SecretNamespace).Get(ctx, c.SecretName, metav1.GetOptions{})
	switch {
	case apierrors.IsNotFound(err):
		secret = &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: c.SecretNamespace,
				Name:      c.SecretName,
			},
		}
	case err != nil:
		return fmt.Errorf("unable to get secret %q: %w", c.SecretNamespace+"/"+c.SecretName, err)
	}

	if secret.Data == nil {
		secret.Data = map[string][]byte{}
	}

	if c.secretToSave == nil {
		secret, cond, err := c.driver.Process(ctx, c.controllerName, secret, c.additionalSecretData, syncCtx.Recorder(), c.option)
		if cond != nil {
			if updateErr := c.statusUpdater(ctx, *cond); updateErr != nil {
				return updateErr
			}
		}
		if err != nil {
			return err
		}
		if secret == nil {
			return nil
		}

		if len(c.additionalSecretData) > 0 {
			// append additional data into client certificate secret
			for k, v := range c.additionalSecretData {
				secret.Data[k] = v
			}
		}
		c.secretToSave = secret
	}

	// save the changes into secret
	if err := saveSecret(c.ManagementCoreClient, c.SecretNamespace, c.secretToSave); err != nil {
		return err
	}
	syncCtx.Recorder().Eventf("SecretSave", "Secret %s/%s for %s is updated",
		c.SecretNamespace, c.SecretName, c.controllerName)
	// clean the cached secret.
	c.secretToSave = nil

	return nil
}

func saveSecret(spokeCoreClient corev1client.CoreV1Interface, secretNamespace string, secret *corev1.Secret) error {
	var err error
	if secret.ResourceVersion == "" {
		_, err = spokeCoreClient.Secrets(secretNamespace).Create(context.Background(), secret, metav1.CreateOptions{})
		return err
	}
	_, err = spokeCoreClient.Secrets(secretNamespace).Update(context.Background(), secret, metav1.UpdateOptions{})
	return err
}
