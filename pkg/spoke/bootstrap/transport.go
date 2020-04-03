package bootstrap

import (
	"crypto/tls"
	"fmt"
	"net"
	"net/http"
	"time"

	utilnet "k8s.io/apimachinery/pkg/util/net"
	"k8s.io/apimachinery/pkg/util/wait"
	restclient "k8s.io/client-go/rest"
	"k8s.io/client-go/util/certificate"
	"k8s.io/client-go/util/connrotation"
	"k8s.io/klog"
)

// UpdateTransport instruments a restconfig with a transport that dynamically uses
// certificates provided by the manager for TLS client auth.
//
// The config must not already provide an explicit transport.
//
// The returned function allows forcefully closing all active connections.
//
// The returned transport periodically checks the manager to determine if the
// certificate has changed. If it has, the transport shuts down all existing client
// connections, forcing the client to re-handshake with the server and use the
// new certificate.
//
// The exitAfter duration, if set, will terminate the current process if a certificate
// is not available from the store (because it has been deleted on disk or is corrupt)
// or if the certificate has expired and the server is responsive. This allows the
// process parent or the bootstrap credentials an opportunity to retrieve a new initial
// certificate.
//
// stopCh should be used to indicate when the transport is unused and doesn't need
// to continue checking the manager.
func UpdateTransport(stopCh <-chan struct{}, clientConfig *restclient.Config, clientCertificateManager certificate.Manager, exitAfter time.Duration) (func(), error) {
	return updateTransport(stopCh, 10*time.Second, clientConfig, clientCertificateManager, exitAfter)
}

// updateTransport is an internal method that exposes how often this method checks that the
// client cert has changed.
func updateTransport(stopCh <-chan struct{}, period time.Duration, clientConfig *restclient.Config, clientCertificateManager certificate.Manager, exitAfter time.Duration) (func(), error) {
	if clientConfig.Transport != nil || clientConfig.Dial != nil {
		return nil, fmt.Errorf("there is already a transport or dialer configured")
	}

	d := connrotation.NewDialer((&net.Dialer{Timeout: 30 * time.Second, KeepAlive: 30 * time.Second}).DialContext)

	if clientCertificateManager != nil {
		if err := addCertRotation(stopCh, period, clientConfig, clientCertificateManager, exitAfter, d); err != nil {
			return nil, err
		}
	} else {
		clientConfig.Dial = d.DialContext
	}

	return d.CloseAll, nil
}

func addCertRotation(stopCh <-chan struct{}, period time.Duration, clientConfig *restclient.Config, clientCertificateManager certificate.Manager, exitAfter time.Duration, d *connrotation.Dialer) error {
	tlsConfig, err := restclient.TLSConfigFor(clientConfig)
	if err != nil {
		return fmt.Errorf("unable to configure TLS for the rest client: %v", err)
	}
	if tlsConfig == nil {
		tlsConfig = &tls.Config{}
	}

	tlsConfig.Certificates = nil
	tlsConfig.GetClientCertificate = func(requestInfo *tls.CertificateRequestInfo) (*tls.Certificate, error) {
		cert := clientCertificateManager.Current()
		if cert == nil {
			return &tls.Certificate{Certificate: nil}, nil
		}
		return cert, nil
	}

	lastCertAvailable := time.Now()
	lastCert := clientCertificateManager.Current()
	go wait.Until(func() {
		curr := clientCertificateManager.Current()

		if exitAfter > 0 {
			now := time.Now()
			if curr == nil {
				// the certificate has been deleted from disk or is otherwise corrupt
				if now.After(lastCertAvailable.Add(exitAfter)) {
					if clientCertificateManager.ServerHealthy() {
						klog.Fatalf("It has been %s since a valid client cert was found and the server is responsive, exiting.", exitAfter)
					} else {
						klog.Errorf("It has been %s since a valid client cert was found, but the server is not responsive. A restart may be necessary to retrieve new initial credentials.", exitAfter)
					}
				}
			} else {
				// the certificate is expired
				if now.After(curr.Leaf.NotAfter) {
					if clientCertificateManager.ServerHealthy() {
						klog.Fatal("The currently active client certificate has expired and the server is responsive, exiting.")
					} else {
						klog.Error("The currently active client certificate has expired, but the server is not responsive. A restart may be necessary to retrieve new initial credentials.")
					}
				}
				lastCertAvailable = now
			}
		}

		if curr == nil || lastCert == curr {
			// Cert hasn't been rotated.
			return
		}
		lastCert = curr

		klog.Info("certificate rotation detected, shutting down client connections to start using new credentials")
		// The cert has been rotated. Close all existing connections to force the client
		// to reperform its TLS handshake with new cert.
		d.CloseAll()
	}, period, stopCh)

	clientConfig.Transport = utilnet.SetTransportDefaults(&http.Transport{
		Proxy:               http.ProxyFromEnvironment,
		TLSHandshakeTimeout: 10 * time.Second,
		TLSClientConfig:     tlsConfig,
		MaxIdleConnsPerHost: 25,
		DialContext:         d.DialContext,
	})

	// Zero out all existing TLS options since our new transport enforces them.
	clientConfig.CertData = nil
	clientConfig.KeyData = nil
	clientConfig.CertFile = ""
	clientConfig.KeyFile = ""
	clientConfig.CAData = nil
	clientConfig.CAFile = ""
	clientConfig.Insecure = false
	clientConfig.NextProtos = nil

	return nil
}
