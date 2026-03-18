package tls

import (
	"context"
	"crypto/tls"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/klog/v2"
)

// LoadTLSConfigFromConfigMap loads TLS configuration from a ConfigMap in the specified namespace
// Returns nil if ConfigMap doesn't exist (not an error - fallback to defaults)
func LoadTLSConfigFromConfigMap(ctx context.Context, client kubernetes.Interface, namespace string) (*TLSConfig, error) {
	cm, err := client.CoreV1().ConfigMaps(namespace).Get(ctx, ConfigMapName, metav1.GetOptions{})
	if err != nil {
		if errors.IsNotFound(err) {
			klog.V(4).Infof("ConfigMap %s/%s not found, using default TLS config", namespace, ConfigMapName)
			return nil, nil // Not an error, caller should use defaults
		}
		return nil, fmt.Errorf("failed to get ConfigMap %s/%s: %w", namespace, ConfigMapName, err)
	}

	return ParseTLSConfigFromConfigMap(cm)
}

// ParseTLSConfigFromConfigMap parses TLS configuration from a ConfigMap
func ParseTLSConfigFromConfigMap(cm *corev1.ConfigMap) (*TLSConfig, error) {
	if cm == nil {
		return nil, fmt.Errorf("ConfigMap is nil")
	}

	cfg := &TLSConfig{}

	// Parse minimum TLS version
	minVersionStr, ok := cm.Data[ConfigMapKeyMinVersion]
	if !ok || minVersionStr == "" {
		minVersionStr = DefaultMinTLSVersion
	}

	minVersion, err := ParseTLSVersion(minVersionStr)
	if err != nil {
		return nil, fmt.Errorf("invalid minTLSVersion in ConfigMap: %w", err)
	}
	cfg.MinVersion = minVersion

	// Parse cipher suites
	cipherSuitesStr := cm.Data[ConfigMapKeyCipherSuites]
	if cipherSuitesStr != "" {
		cipherSuites, unsupported := ParseCipherSuites(cipherSuitesStr)
		if len(unsupported) > 0 {
			klog.Warningf("Unsupported cipher suites in ConfigMap %s/%s: %v", cm.Namespace, cm.Name, unsupported)
		}
		cfg.CipherSuites = cipherSuites
	}

	return cfg, nil
}

// CreateTLSConfigMap creates a ConfigMap with TLS profile configuration
func CreateTLSConfigMap(namespace, minVersion, cipherSuites string) *corev1.ConfigMap {
	return &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      ConfigMapName,
			Namespace: namespace,
		},
		Data: map[string]string{
			ConfigMapKeyMinVersion:   minVersion,
			ConfigMapKeyCipherSuites: cipherSuites,
		},
	}
}

// UpdateTLSConfigMap updates an existing ConfigMap with new TLS profile configuration
func UpdateTLSConfigMap(cm *corev1.ConfigMap, minVersion, cipherSuites string) {
	if cm.Data == nil {
		cm.Data = make(map[string]string)
	}
	cm.Data[ConfigMapKeyMinVersion] = minVersion
	cm.Data[ConfigMapKeyCipherSuites] = cipherSuites
}

// TLSConfigToFunc returns a function that can be used with controller-runtime's TLSOpts
// This is useful for configuring webhook servers and metrics servers
func TLSConfigToFunc(tlsCfg *TLSConfig) func(*tls.Config) {
	return func(config *tls.Config) {
		config.MinVersion = tlsCfg.MinVersion
		if tlsCfg.MinVersion == tls.VersionTLS13 {
			config.MaxVersion = tls.VersionTLS13
		} else if len(tlsCfg.CipherSuites) > 0 {
			config.CipherSuites = tlsCfg.CipherSuites
		}
	}
}
