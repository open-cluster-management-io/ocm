package tls

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	listerscorev1 "k8s.io/client-go/listers/core/v1"
	"k8s.io/client-go/tools/cache"
	"k8s.io/klog/v2"

	"open-cluster-management.io/sdk-go/pkg/basecontroller/factory"
)

// LoadTLSConfigFromConfigMap loads TLS configuration from a ConfigMap in the specified namespace.
// Returns nil if the ConfigMap doesn't exist (not an error — caller should use defaults).
func LoadTLSConfigFromConfigMap(ctx context.Context, client kubernetes.Interface, namespace string) (*TLSConfig, error) {
	cm, err := client.CoreV1().ConfigMaps(namespace).Get(ctx, ConfigMapName, metav1.GetOptions{})
	if err != nil {
		if errors.IsNotFound(err) {
			klog.V(4).Infof("ConfigMap %s/%s not found, using default TLS config", namespace, ConfigMapName)
			return nil, nil
		}
		return nil, fmt.Errorf("failed to get ConfigMap %s/%s: %w", namespace, ConfigMapName, err)
	}

	return parseTLSConfigFromConfigMap(cm)
}

// tlsConfigMapController watches the TLS profile ConfigMap and calls onChangeFn when its
// data changes. It is built on top of the SDK's base controller so it can read from the
// informer cache (lister) instead of making live API calls.
type tlsConfigMapController struct {
	namespace  string
	lister     listerscorev1.ConfigMapLister
	onChangeFn func()
	mu         sync.Mutex
	lastHash   string
}

func (c *tlsConfigMapController) sync(_ context.Context, _ factory.SyncContext, _ string) error {
	cm, err := c.lister.ConfigMaps(c.namespace).Get(ConfigMapName)
	if err != nil && !errors.IsNotFound(err) {
		return err
	}

	var currentHash string
	if err == nil {
		currentHash = hashConfigMapData(cm.Data)
	}

	c.mu.Lock()
	changed := currentHash != c.lastHash
	if changed {
		c.lastHash = currentHash
	}
	c.mu.Unlock()

	if changed {
		c.onChangeFn()
	}
	return nil
}

// StartTLSConfigMapWatcher starts a controller that watches the TLS profile ConfigMap and
// calls onChangeFn whenever its data changes. It blocks until the informer cache is synced,
// then returns the TLSConfig active at startup while the controller continues running in the
// background until ctx is canceled.
func StartTLSConfigMapWatcher(
	ctx context.Context,
	client kubernetes.Interface,
	namespace string,
	onChangeFn func(),
) (*TLSConfig, error) {
	if onChangeFn == nil {
		return nil, fmt.Errorf("onChangeFn must not be nil")
	}

	informerFactory := informers.NewSharedInformerFactoryWithOptions(
		client,
		10*time.Minute,
		informers.WithNamespace(namespace),
		informers.WithTweakListOptions(func(opts *metav1.ListOptions) {
			opts.FieldSelector = fields.OneTermEqualSelector("metadata.name", ConfigMapName).String()
		}),
	)

	cmInformer := informerFactory.Core().V1().ConfigMaps()
	ctrl := &tlsConfigMapController{
		namespace:  namespace,
		lister:     cmInformer.Lister(),
		onChangeFn: onChangeFn,
	}

	controller := factory.New().
		WithInformersQueueKeysFunc(factory.DefaultQueueKeysFunc, cmInformer.Informer()).
		WithSync(ctrl.sync).
		ToController("tls-configmap-controller")

	informerFactory.Start(ctx.Done())
	if !cache.WaitForCacheSync(ctx.Done(), cmInformer.Informer().HasSynced) {
		return nil, ctx.Err()
	}

	// Seed lastHash from the warm lister so the first Sync() call (which replays the
	// list-phase Add event) does not trigger a spurious onChangeFn call.
	var initialTLSConfig *TLSConfig
	cm, err := ctrl.lister.ConfigMaps(namespace).Get(ConfigMapName)
	switch {
	case err == nil:
		ctrl.lastHash = hashConfigMapData(cm.Data)
		initialTLSConfig, err = parseTLSConfigFromConfigMap(cm)
		if err != nil {
			return nil, err
		}
	case errors.IsNotFound(err):
		ctrl.lastHash = ""
		initialTLSConfig = GetDefaultTLSConfig()
	default:
		return nil, fmt.Errorf("failed to get ConfigMap from lister: %w", err)
	}

	go controller.Run(ctx, 1)

	return initialTLSConfig, nil
}

// parseTLSConfigFromConfigMap parses TLS configuration from a ConfigMap.
func parseTLSConfigFromConfigMap(cm *corev1.ConfigMap) (*TLSConfig, error) {
	if cm == nil {
		return nil, fmt.Errorf("ConfigMap is nil")
	}

	cfg := &TLSConfig{}

	// Parse minimum TLS version
	minVersionStr, ok := cm.Data[ConfigMapKeyMinVersion]
	if !ok || minVersionStr == "" {
		minVersionStr = defaultMinTLSVersion
	}

	minVersion, err := parseTLSVersion(minVersionStr)
	if err != nil {
		return nil, fmt.Errorf("invalid minTLSVersion in ConfigMap: %w", err)
	}
	cfg.MinVersion = minVersion

	// Parse cipher suites
	cipherSuitesStr := cm.Data[ConfigMapKeyCipherSuites]
	if cipherSuitesStr != "" {
		cipherSuites, unsupported := parseCipherSuites(cipherSuitesStr)
		if len(unsupported) > 0 {
			klog.Warningf("Unsupported cipher suites in ConfigMap %s/%s: %v", cm.Namespace, cm.Name, unsupported)
			if len(cipherSuites) == 0 {
				return nil, fmt.Errorf("invalid cipherSuites in ConfigMap %s/%s: no supported cipher suites found",
					cm.Namespace, cm.Name)
			}
		}
		cfg.CipherSuites = cipherSuites
	}

	return cfg, nil
}

// hashConfigMapData returns a deterministic string representation of a ConfigMap's data map.
// Keys and values are quoted so that neither can contain the separators used here.
func hashConfigMapData(data map[string]string) string {
	if len(data) == 0 {
		return ""
	}
	keys := make([]string, 0, len(data))
	for k := range data {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, fmt.Sprintf("%q=%q", k, data[k]))
	}
	return strings.Join(parts, "|")
}
