# pkg/tls

Helpers for loading, parsing, and applying TLS profile configuration in OCM components.
TLS settings (minimum version and cipher suites) are stored in a well-known ConfigMap
(`ocm-tls-profile`) and optionally propagated via command-line flags.

## ConfigMap format

```yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: ocm-tls-profile        # tls.ConfigMapName
  namespace: <component-namespace>
data:
  minTLSVersion: VersionTLS13  # tls.ConfigMapKeyMinVersion
  cipherSuites: TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256,TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256
  #             ^ tls.ConfigMapKeyCipherSuites
```

Supported `minTLSVersion` values: `VersionTLS10`, `VersionTLS11`, `VersionTLS12` (default),
`VersionTLS13`.

Cipher suite names use the IANA format (e.g. `TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256`),
which matches what Go's `crypto/tls` package uses. TLS 1.3 cipher suites are fixed by
the Go runtime and cannot be configured via `cipherSuites`.

## Use cases

### 1. Component receiving TLS flags

Components that receive `--tls-min-version` and `--tls-cipher-suites` flags from their
operator do not watch the ConfigMap directly.

```go
import sdktls "open-cluster-management.io/sdk-go/pkg/tls"

// Parse TLS configuration from command-line flags.
// Returns nil if neither flag was provided (use GetDefaultTLSConfig as fallback).
tlsCfg, err := sdktls.ConfigFromFlags(minVersionFlag, cipherSuitesFlag)
if err != nil {
    return err
}
if tlsCfg == nil {
    tlsCfg = sdktls.GetDefaultTLSConfig()
}

// Apply to a controller-runtime webhook/metrics server via TLSOpts.
mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{
    WebhookServer: webhook.NewServer(webhook.Options{
        TLSOpts: []func(*tls.Config){sdktls.ConfigToFunc(tlsCfg)},
    }),
})
```

### 2. Operator or addon agent watching the ConfigMap

Operators and addon agents load the TLS profile at startup and restart when it changes
so that the new settings take effect without a manual pod restart.

`StartTLSConfigMapWatcher` combines the load, watcher setup, and background goroutine
into a single call. It returns the `TLSConfig` active at startup.

```go
import sdktls "open-cluster-management.io/sdk-go/pkg/tls"

// Load current TLS config and start watching for changes.
// Falls back to TLS 1.2 defaults when the ConfigMap does not exist.
tlsCfg, err := sdktls.StartTLSConfigMapWatcher(ctx, kubeClient, namespace, func() {
    klog.Info("TLS ConfigMap changed, restarting")
    os.Exit(0) // Kubernetes will restart the pod with the new settings.
})
if err != nil {
    return err
}

// Use tlsCfg immediately — it reflects what the process is running with.
klog.Infof("TLS min version: %s", sdktls.VersionToString(tlsCfg.MinVersion))
```

### 3. Operator passing TLS settings to managed components via flags

An operator that manages other components can read its own TLS profile and forward the
settings as flags when deploying those components.

```go
tlsCfg, err := sdktls.LoadTLSConfigFromConfigMap(ctx, kubeClient, operatorNamespace)
if err != nil {
    return err
}
if tlsCfg == nil {
    tlsCfg = sdktls.GetDefaultTLSConfig()
}

args := []string{
    "--tls-min-version", sdktls.VersionToString(tlsCfg.MinVersion),
    "--tls-cipher-suites", sdktls.CipherSuitesToString(tlsCfg.CipherSuites),
}
// Pass args to the managed component's Deployment.
```

## API reference

| Function | Description |
|---|---|
| `StartTLSConfigMapWatcher(ctx, client, namespace, onChangeFn)` | Load config + start watcher in one call. Returns active `*TLSConfig`. |
| `LoadTLSConfigFromConfigMap(ctx, client, namespace)` | Load TLS config from ConfigMap. Returns `nil, nil` if CM not found. |
| `GetDefaultTLSConfig()` | Returns TLS 1.2 with Go's default cipher suites. |
| `ConfigFromFlags(minVersion, cipherSuites)` | Parse TLS config from flag strings. Returns `nil` if both are empty. |
| `ConfigToFunc(tlsCfg)` | Returns a `func(*tls.Config)` for use with controller-runtime `TLSOpts`. |
| `VersionToString(version)` | Convert a `crypto/tls` version constant to its string name. |
| `CipherSuitesToString(suites)` | Convert cipher suite IDs back to a comma-separated IANA-format string. |

Constants: `ConfigMapName`, `ConfigMapKeyMinVersion`, `ConfigMapKeyCipherSuites`.
