package registration

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"time"

	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	corev1informers "k8s.io/client-go/informers/core/v1"
	corev1client "k8s.io/client-go/kubernetes/typed/core/v1"
	"k8s.io/klog/v2"

	"open-cluster-management.io/sdk-go/pkg/basecontroller/events"
	"open-cluster-management.io/sdk-go/pkg/basecontroller/factory"

	"open-cluster-management.io/ocm/pkg/common/queue"
)

// hubKubeconfigSecretController watches the HubKubeconfig secret, if the secret is changed, this controller creates/updates the
// corresponding configuration files from the secret
type hubKubeconfigSecretController struct {
	hubKubeconfigDir             string
	hubKubeconfigSecretNamespace string
	hubKubeconfigSecretName      string
	spokeCoreClient              corev1client.CoreV1Interface
}

// NewHubKubeconfigSecretController returns a new HubKubeconfigSecretController
func NewHubKubeconfigSecretController(
	hubKubeconfigDir, hubKubeconfigSecretNamespace, hubKubeconfigSecretName string,
	spokeCoreClient corev1client.CoreV1Interface,
	spokeSecretInformer corev1informers.SecretInformer) factory.Controller {
	s := &hubKubeconfigSecretController{
		hubKubeconfigDir:             hubKubeconfigDir,
		hubKubeconfigSecretNamespace: hubKubeconfigSecretNamespace,
		hubKubeconfigSecretName:      hubKubeconfigSecretName,
		spokeCoreClient:              spokeCoreClient,
	}

	return factory.New().
		WithFilteredEventsInformersQueueKeysFunc(
			queue.QueueKeyByMetaName,
			func(obj interface{}) bool {
				accessor, err := meta.Accessor(obj)
				if err != nil {
					return false
				}
				// only enqueue when hub kubeconfig secret is changed
				if accessor.GetNamespace() == hubKubeconfigSecretNamespace && accessor.GetName() == hubKubeconfigSecretName {
					return true
				}
				return false
			}, spokeSecretInformer.Informer()).
		WithSync(s.sync).
		ResyncEvery(5 * time.Minute).
		ToController("HubKubeconfigSecretController")
}

func (s *hubKubeconfigSecretController) sync(ctx context.Context, syncCtx factory.SyncContext, _ string) error {
	logger := klog.FromContext(ctx)
	logger.V(4).Info("Reconciling Hub KubeConfig secret", "hubKubeconfigSecretName", s.hubKubeconfigSecretName)
	return DumpSecret(s.spokeCoreClient, s.hubKubeconfigSecretNamespace, s.hubKubeconfigSecretName, s.hubKubeconfigDir, ctx, syncCtx.Recorder())
}

// DumpSecret dumps the data in the given seccret into a directory in file system.
// The output directory will be created if not exists.
// TO DO: remove the file once the corresponding key is removed from secret.
func DumpSecret(
	coreV1Client corev1client.CoreV1Interface,
	secretNamespace, secretName, outputDir string,
	ctx context.Context,
	recorder events.Recorder) error {
	secret, err := coreV1Client.Secrets(secretNamespace).Get(ctx, secretName, metav1.GetOptions{})
	if errors.IsNotFound(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("unable to get secret %s/%s : %w", secretNamespace, secretName, err)
	}

	if err := os.MkdirAll(outputDir, 0700); err != nil {
		return fmt.Errorf("unable to create dir %q : %w", outputDir, err)
	}

	// create/update files from the secret
	for key, data := range secret.Data {
		filename := path.Clean(path.Join(outputDir, key))
		lastData, err := os.ReadFile(filepath.Clean(filename))
		switch {
		case os.IsNotExist(err):
			// create file
			if err := os.WriteFile(filename, data, 0600); err != nil {
				return fmt.Errorf("unable to write file %q: %w", filename, err)
			}
			recorder.Event(ctx, "FileCreated", fmt.Sprintf("File %q is created from secret %s/%s", filename, secretNamespace, secretName))
		case err != nil:
			return fmt.Errorf("unable to read file %q: %w", filename, err)
		case bytes.Equal(lastData, data):
			// skip file without any change
			continue
		default:
			// update file
			if err := os.WriteFile(path.Clean(filename), data, 0600); err != nil {
				return fmt.Errorf("unable to write file %q: %w", filename, err)
			}
			recorder.Event(ctx, "FileUpdated", fmt.Sprintf("File %q is updated from secret %s/%s", filename, secretNamespace, secretName))
		}
	}
	return nil
}
