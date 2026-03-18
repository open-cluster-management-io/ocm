package tls_test

import (
	"context"
	"crypto/tls"
	"os"

	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/webhook"

	tlslib "open-cluster-management.io/ocm/pkg/common/tls"
)

// Example_operatorWatchAndRestart shows how operators and addon-managers
// watch the TLS ConfigMap and restart when it changes
func Example_operatorWatchAndRestart() {
	// Create Kubernetes client
	// client := kubernetes.NewForConfigOrDie(ctrl.GetConfigOrDie())
	// namespace := "open-cluster-management-hub" // or operator's namespace

	// ctx, cancel := context.WithCancel(context.Background())
	// defer cancel()

	// Load initial TLS config from ConfigMap
	// tlsConfig, err := tlslib.LoadTLSConfigFromConfigMap(ctx, client, namespace)
	// if err != nil {
	//     // ConfigMap doesn't exist yet, use default
	//     tlsConfig = tlslib.GetDefaultTLSConfig()
	// }

	// Start ConfigMap watcher in background
	// The watcher will call os.Exit(0) when ConfigMap changes
	// watcher := tlslib.NewConfigMapWatcher(client, namespace, cancel)
	// watcher.SetOnChangeFunc(func() {
	//     klog.Info("TLS ConfigMap changed, restarting operator")
	//     os.Exit(0) // Kubernetes will restart the pod
	// })
	//
	// go func() {
	//     if err := watcher.Start(ctx); err != nil {
	//         klog.Error(err, "ConfigMap watcher failed")
	//     }
	// }()

	// Start your operator with the loaded TLS config
	// mgr.Start(ctx)

	// Prevent unused import errors
	_ = context.Background
	_ = os.Exit
}

// Example_componentWithFlags shows how components (Case 3) use TLS flags
// Components receive --tls-min-version and --tls-cipher-suites from operators
// and do NOT watch ConfigMaps
func Example_componentWithFlags() {
	// Parse TLS configuration from command-line flags
	// Flags are set by operator when deploying the component
	minVersion := "VersionTLS13"                                    // from --tls-min-version flag
	cipherSuites := "TLS_AES_128_GCM_SHA256,TLS_AES_256_GCM_SHA384" // from --tls-cipher-suites flag

	tlsConfig, err := tlslib.TLSConfigFromFlags(minVersion, cipherSuites)
	if err != nil {
		panic(err)
	}
	if tlsConfig == nil {
		// No flags provided, use default TLS 1.2
		tlsConfig = tlslib.GetDefaultTLSConfig()
	}

	// Use TLS config in controller-runtime webhook server
	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{
		WebhookServer: webhook.NewServer(webhook.Options{
			Port: 9443,
			TLSOpts: []func(*tls.Config){
				tlslib.TLSConfigToFunc(tlsConfig),
			},
		}),
	})
	if err != nil {
		panic(err)
	}

	_ = mgr // mgr.Start(ctx)
}

// Example_operatorCreatesConfigMap shows how operators (Case 2) create ConfigMaps
// for their managed components
func Example_operatorCreatesConfigMap() {
	// Operator reads TLS config from its own namespace's ConfigMap
	// and creates ConfigMap in managed namespace
	cm := tlslib.CreateTLSConfigMap(
		"managed-namespace",
		"VersionTLS13", // minVersion from operator's ConfigMap
		"TLS_AES_128_GCM_SHA256,TLS_AES_256_GCM_SHA384", // cipherSuites from operator's ConfigMap
	)

	_ = cm // client.CoreV1().ConfigMaps(cm.Namespace).Create(ctx, cm, metav1.CreateOptions{})
}
