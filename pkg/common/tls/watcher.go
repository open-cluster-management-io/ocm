package tls

import (
	"context"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/fields"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"
	"k8s.io/klog/v2"
)

const (
	// defaultResyncPeriod is how often the informer does a full resync
	defaultResyncPeriod = 10 * time.Minute
)

// ConfigMapWatcher watches the TLS ConfigMap and triggers restart when it changes
type ConfigMapWatcher struct {
	client       kubernetes.Interface
	namespace    string
	cancelFunc   context.CancelFunc
	initialHash  string
	onChangeFunc func()
}

// NewConfigMapWatcher creates a new ConfigMap watcher
// cancelFunc will be called when the ConfigMap changes, triggering graceful shutdown
func NewConfigMapWatcher(client kubernetes.Interface, namespace string, cancelFunc context.CancelFunc) *ConfigMapWatcher {
	return &ConfigMapWatcher{
		client:     client,
		namespace:  namespace,
		cancelFunc: cancelFunc,
	}
}

// SetOnChangeFunc sets a custom function to call when ConfigMap changes
// If not set, defaults to calling cancelFunc for graceful shutdown
func (w *ConfigMapWatcher) SetOnChangeFunc(f func()) {
	w.onChangeFunc = f
}

// Start starts watching the ConfigMap for changes
// It uses an informer for reliable watching with automatic retries
func (w *ConfigMapWatcher) Start(ctx context.Context) error {
	logger := klog.FromContext(ctx)
	logger.Info("Starting TLS ConfigMap watcher", "namespace", w.namespace, "configmap", ConfigMapName)

	// Load initial ConfigMap to get baseline hash
	cm, err := LoadTLSConfigFromConfigMap(ctx, w.client, w.namespace)
	if err != nil {
		logger.Error(err, "Failed to load initial TLS ConfigMap")
		// Continue anyway - ConfigMap might be created later
	}
	if cm != nil {
		w.initialHash = hashTLSConfig(cm)
		logger.Info("Initial TLS config loaded", "hash", w.initialHash)
	}

	// Create informer factory with field selector to only watch our ConfigMap
	informerFactory := informers.NewSharedInformerFactoryWithOptions(
		w.client,
		defaultResyncPeriod,
		informers.WithNamespace(w.namespace),
		informers.WithTweakListOptions(func(opts *metav1.ListOptions) {
			opts.FieldSelector = fields.OneTermEqualSelector("metadata.name", ConfigMapName).String()
		}),
	)

	// Get ConfigMap informer
	cmInformer := informerFactory.Core().V1().ConfigMaps().Informer()

	// Add event handlers
	_, err = cmInformer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {
			cm := obj.(*corev1.ConfigMap)
			if cm.Name != ConfigMapName {
				return
			}
			logger.V(4).Info("TLS ConfigMap added", "configmap", cm.Name)
			// If we didn't have initial config, this is the first creation
			if w.initialHash == "" {
				w.initialHash = hashConfigMap(cm)
				logger.Info("TLS ConfigMap created, initial config set", "hash", w.initialHash)
			}
		},
		UpdateFunc: func(oldObj, newObj interface{}) {
			oldCM := oldObj.(*corev1.ConfigMap)
			newCM := newObj.(*corev1.ConfigMap)
			if newCM.Name != ConfigMapName {
				return
			}

			// Check if TLS data actually changed
			oldHash := hashConfigMap(oldCM)
			newHash := hashConfigMap(newCM)

			if oldHash != newHash {
				logger.Info("TLS ConfigMap modified, triggering restart",
					"configmap", newCM.Name,
					"oldHash", oldHash,
					"newHash", newHash,
				)
				w.triggerRestart()
			}
		},
		DeleteFunc: func(obj interface{}) {
			cm := obj.(*corev1.ConfigMap)
			if cm.Name != ConfigMapName {
				return
			}
			logger.Info("TLS ConfigMap deleted, triggering restart to use default config", "configmap", cm.Name)
			w.triggerRestart()
		},
	})
	if err != nil {
		return err
	}

	// Start the informer
	informerFactory.Start(ctx.Done())

	// Wait for cache sync
	logger.Info("Waiting for TLS ConfigMap informer cache sync")
	if !cache.WaitForCacheSync(ctx.Done(), cmInformer.HasSynced) {
		return ctx.Err()
	}
	logger.Info("TLS ConfigMap informer cache synced")

	return nil
}

// triggerRestart triggers the restart mechanism
func (w *ConfigMapWatcher) triggerRestart() {
	if w.onChangeFunc != nil {
		w.onChangeFunc()
	} else if w.cancelFunc != nil {
		// Default behavior: cancel context for graceful shutdown
		w.cancelFunc()
	}
}

// hashConfigMap creates a simple hash of the ConfigMap's TLS data
func hashConfigMap(cm *corev1.ConfigMap) string {
	if cm == nil || cm.Data == nil {
		return ""
	}
	// Simple concatenation of relevant fields for change detection
	return cm.Data[ConfigMapKeyMinVersion] + "|" + cm.Data[ConfigMapKeyCipherSuites]
}

// hashTLSConfig creates a hash from TLSConfig
func hashTLSConfig(cfg *TLSConfig) string {
	if cfg == nil {
		return ""
	}
	minVer := TLSVersionToString(cfg.MinVersion)
	ciphers := CipherSuitesToString(cfg.CipherSuites)
	return minVer + "|" + ciphers
}
