package managedcluster

import (
	"bytes"
	"context"
	"fmt"
	"io/ioutil"
	"os"
	"path"
	"path/filepath"
	"time"

	"github.com/openshift/library-go/pkg/controller/factory"
	"github.com/openshift/library-go/pkg/operator/events"

	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	corev1informers "k8s.io/client-go/informers/core/v1"
	corev1client "k8s.io/client-go/kubernetes/typed/core/v1"
	"k8s.io/klog/v2"
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
	spokeSecretInformer corev1informers.SecretInformer,
	recorder events.Recorder) factory.Controller {
	s := &hubKubeconfigSecretController{
		hubKubeconfigDir:             hubKubeconfigDir,
		hubKubeconfigSecretNamespace: hubKubeconfigSecretNamespace,
		hubKubeconfigSecretName:      hubKubeconfigSecretName,
		spokeCoreClient:              spokeCoreClient,
	}

	return factory.New().
		WithFilteredEventsInformersQueueKeyFunc(
			func(obj runtime.Object) string {
				accessor, _ := meta.Accessor(obj)
				return accessor.GetName()
			},
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
		ResyncEvery(5*time.Minute).
		ToController("HubKubeconfigSecretController", recorder)
}

func (s *hubKubeconfigSecretController) sync(ctx context.Context, syncCtx factory.SyncContext) error {
	klog.V(4).Infof("Reconciling Hub KubeConfig secret %q", s.hubKubeconfigSecretName)
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
		lastData, err := ioutil.ReadFile(filepath.Clean(filename))
		switch {
		case os.IsNotExist(err):
			// create file
			if err := ioutil.WriteFile(filename, data, 0600); err != nil {
				return fmt.Errorf("unable to write file %q: %w", filename, err)
			}
			recorder.Event("FileCreated", fmt.Sprintf("File %q is created from secret %s/%s", filename, secretNamespace, secretName))
		case err != nil:
			return fmt.Errorf("unable to read file %q: %w", filename, err)
		case bytes.Equal(lastData, data):
			// skip file without any change
			continue
		default:
			// update file
			if err := ioutil.WriteFile(path.Clean(filename), data, 0600); err != nil {
				return fmt.Errorf("unable to write file %q: %w", filename, err)
			}
			recorder.Event("FileUpdated", fmt.Sprintf("File %q is updated from secret %s/%s", filename, secretNamespace, secretName))
		}
	}
	return nil
}
