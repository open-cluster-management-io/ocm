package hubclientcert

import (
	"bytes"
	"context"
	"fmt"
	"io/ioutil"
	"os"
	"path"
	"time"

	"github.com/openshift/library-go/pkg/controller/factory"
	"github.com/openshift/library-go/pkg/operator/events"

	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	corev1informers "k8s.io/client-go/informers/core/v1"
	corev1client "k8s.io/client-go/kubernetes/typed/core/v1"
	corev1lister "k8s.io/client-go/listers/core/v1"
	"k8s.io/klog/v2"
)

// hubKubeconfigSecretController watches the HubKubeconfig secret, if the secret is changed, this controller creates/updates the
// corresponding configuration files from the secret
type hubKubeconfigSecretController struct {
	hubKubeconfigDir             string
	hubKubeconfigSecretNamespace string
	hubKubeconfigSecretName      string
	spokeCoreClient              corev1client.CoreV1Interface
	spokeSecretLister            corev1lister.SecretLister
}

// NewHubKubeconfigSecretController returns a new HubKubeconfigSecretController
func NewHubKubeconfigSecretController(
	hubKubeconfigDir, hubKubeconfigSecretNamespace, hubKubeconfigSecretName string,
	spokeCoreClient corev1client.CoreV1Interface,
	spokeSecretInformer corev1informers.SecretInformer,
	recorder events.Recorder) factory.Controller {
	s := &hubKubeconfigSecretController{
		hubKubeconfigDir:             hubKubeconfigDir,
		hubKubeconfigSecretNamespace: hubKubeconfigSecretNamespace,
		hubKubeconfigSecretName:      hubKubeconfigSecretName,
		spokeCoreClient:              spokeCoreClient,
		spokeSecretLister:            spokeSecretInformer.Lister(),
	}

	return factory.New().
		WithInformersQueueKeyFunc(
			func(obj runtime.Object) string {
				accessor, err := meta.Accessor(obj)
				if err != nil {
					return ""
				}
				if accessor.GetNamespace() == hubKubeconfigSecretNamespace && accessor.GetName() == hubKubeconfigSecretName {
					return accessor.GetName()
				}
				return ""
			}, spokeSecretInformer.Informer()).
		WithSync(s.sync).
		ResyncEvery(5*time.Minute).
		ToController("HubKubeconfigSecretController", recorder)
}

func (s *hubKubeconfigSecretController) sync(ctx context.Context, syncCtx factory.SyncContext) error {
	queueKey := syncCtx.QueueKey()
	if queueKey == "" {
		return nil
	}
	klog.V(4).Infof("Reconciling Hub KubeConfig secret %q", s.hubKubeconfigSecretName)
	secret, err := s.spokeCoreClient.Secrets(s.hubKubeconfigSecretNamespace).Get(ctx, s.hubKubeconfigSecretName, metav1.GetOptions{})
	if errors.IsNotFound(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("unable to get secret %s/%s : %w", s.hubKubeconfigSecretNamespace, s.hubKubeconfigSecretName, err)
	}

	// if the secret is invalid, ignore it
	if !hasValidKubeconfig(secret) {
		return nil
	}

	if err := os.MkdirAll(s.hubKubeconfigDir, 0700); err != nil {
		return fmt.Errorf("unable to create dir %q : %w", s.hubKubeconfigDir, err)
	}

	// create/update configuration files from the secret
	for key, data := range secret.Data {
		configFilePath := path.Join(s.hubKubeconfigDir, key)
		if err := writeConfigFile(configFilePath, data, syncCtx.Recorder()); err != nil {
			return fmt.Errorf("unable to write config file %q: %w", configFilePath, err)
		}
	}
	return nil
}

// writeConfigFile creates or updates a specified file and record an event to log it.
func writeConfigFile(filename string, data []byte, recorder events.Recorder) error {
	lastData, err := ioutil.ReadFile(path.Clean(filename))
	if os.IsNotExist(err) {
		if err := ioutil.WriteFile(path.Clean(filename), data, 0400); err != nil {
			return err
		}
		recorder.Event("HubKubeConfigFileCreated", fmt.Sprintf("Hub config file %q is created from hub kubeconfig secret", filename))
		return nil
	}
	if err != nil {
		return err
	}

	if bytes.Equal(lastData, data) {
		return nil
	}

	if err := ioutil.WriteFile(path.Clean(filename), data, 0400); err != nil {
		return err
	}

	recorder.Event("HubKubeConfigFileUpdated", fmt.Sprintf("Hub config file %q is updated from hub kubeconfig secret", filename))
	return nil
}
