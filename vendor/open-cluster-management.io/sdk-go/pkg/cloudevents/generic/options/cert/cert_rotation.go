package cert

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"os"
	"reflect"
	"sync"
	"time"

	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/util/workqueue"
	"k8s.io/klog/v2"
)

const workItemKey = "key"

type Connection interface {
	Close() error
}

type reloadFunc func(*tls.CertificateRequestInfo) (*tls.Certificate, error)

// CertCallbackRefreshDuration is exposed so that integration tests can crank up the reload speed.
var CertCallbackRefreshDuration = 5 * time.Minute

type clientCertRotating struct {
	sync.RWMutex

	clientCert *tls.Certificate

	reload reloadFunc
	conn   Connection

	// queue only ever has one item, but it has nice error handling backoff/retry semantics
	queue workqueue.RateLimitingInterface
}

func StartClientCertRotating(reload reloadFunc, conn Connection) {
	r := &clientCertRotating{
		reload: reload,
		conn:   conn,
		queue:  workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(), "ClientCertRotator"),
	}

	go r.run(context.Background())
}

// run starts the controller and blocks until context is closed.
func (c *clientCertRotating) run(ctx context.Context) {
	defer utilruntime.HandleCrash()
	defer c.queue.ShutDown()

	klog.V(3).Infof("Starting client certificate rotation controller")
	defer klog.V(3).Infof("Shutting down client certificate rotation controller")

	go wait.Until(c.runWorker, time.Second, ctx.Done())

	go func() {
		if err := wait.PollUntilContextCancel(
			ctx,
			CertCallbackRefreshDuration,
			true,
			func(ctx context.Context) (bool, error) {
				c.queue.Add(workItemKey)
				return false, nil
			},
		); err != nil {
			utilruntime.HandleError(fmt.Errorf("unable to poll client certs: %w", err))
		}
	}()

	<-ctx.Done()
}

func (c *clientCertRotating) runWorker() {
	for c.processNextWorkItem() {
	}
}

func (c *clientCertRotating) processNextWorkItem() bool {
	dsKey, quit := c.queue.Get()
	if quit {
		return false
	}
	defer c.queue.Done(dsKey)

	err := c.loadClientCert()
	if err == nil {
		c.queue.Forget(dsKey)
		return true
	}

	utilruntime.HandleError(fmt.Errorf("%v failed with : %v", dsKey, err))
	c.queue.AddRateLimited(dsKey)

	return true
}

// loadClientCert calls the callback and rotates connections if needed
func (c *clientCertRotating) loadClientCert() error {
	cert, err := c.reload(nil)
	if err != nil {
		return err
	}

	// check to see if we have a change. If the values are the same, do nothing.
	c.RLock()
	haveCert := c.clientCert != nil
	if certsEqual(c.clientCert, cert) {
		c.RUnlock()
		return nil
	}
	c.RUnlock()

	c.Lock()
	c.clientCert = cert
	c.Unlock()

	// The first certificate requested is not a rotation that is worth closing connections for
	if !haveCert {
		return nil
	}

	if c.conn == nil {
		return fmt.Errorf("no connection close function set")
	}

	klog.V(1).Infof("certificate rotation detected, shutting down client connections to start using new credentials")
	c.conn.Close()

	return nil
}

// certsEqual compares tls Certificates, ignoring the Leaf which may get filled in dynamically
func certsEqual(left, right *tls.Certificate) bool {
	if left == nil || right == nil {
		return left == right
	}

	if !byteMatrixEqual(left.Certificate, right.Certificate) {
		return false
	}

	if !reflect.DeepEqual(left.PrivateKey, right.PrivateKey) {
		return false
	}

	if !byteMatrixEqual(left.SignedCertificateTimestamps, right.SignedCertificateTimestamps) {
		return false
	}

	if !bytes.Equal(left.OCSPStaple, right.OCSPStaple) {
		return false
	}

	return true
}

func byteMatrixEqual(left, right [][]byte) bool {
	if len(left) != len(right) {
		return false
	}

	for i := range left {
		if !bytes.Equal(left[i], right[i]) {
			return false
		}
	}
	return true
}

type certificateCacheEntry struct {
	cert  *tls.Certificate
	err   error
	birth time.Time
}

// isStale returns true when this cache entry is too old to be usable
func (c *certificateCacheEntry) isStale() bool {
	return time.Since(c.birth) > time.Second
}

func newCertificateCacheEntry(certFile, keyFile string) certificateCacheEntry {
	cert, err := tls.LoadX509KeyPair(certFile, keyFile)
	return certificateCacheEntry{cert: &cert, err: err, birth: time.Now()}
}

// CachingCertificateLoader ensures that we don't hammer the filesystem when opening many connections
// the underlying cert files are read at most once every second
func CachingCertificateLoader(certFile, keyFile string) func() (*tls.Certificate, error) {
	current := newCertificateCacheEntry(certFile, keyFile)
	var currentMtx sync.RWMutex

	return func() (*tls.Certificate, error) {
		currentMtx.RLock()
		if current.isStale() {
			currentMtx.RUnlock()

			currentMtx.Lock()
			defer currentMtx.Unlock()

			if current.isStale() {
				current = newCertificateCacheEntry(certFile, keyFile)
			}
		} else {
			defer currentMtx.RUnlock()
		}

		return current.cert, current.err
	}
}

// AutoLoadTLSConfig returns a TLS configuration for the given CA, client certificate, key files
// that can be used to establish a TLS connection.
// If CA is not provided, the system cert pool will be used.
// If client certificate and key are provided, they will be used for client authentication.
// And a goroutine will be started to periodically refresh client certificates for this connection.
func AutoLoadTLSConfig(caFile, certFile, keyFile string, conn Connection) (*tls.Config, error) {
	var tlsConfig *tls.Config
	if caFile != "" {
		certPool, err := rootCAs(caFile)
		if err != nil {
			return nil, err
		}
		tlsConfig = &tls.Config{
			RootCAs:    certPool,
			MinVersion: tls.VersionTLS13,
			MaxVersion: tls.VersionTLS13,
		}
		if certFile != "" && keyFile != "" {
			// Set client certificate and key getter for tls config
			tlsConfig.GetClientCertificate = func(cri *tls.CertificateRequestInfo) (*tls.Certificate, error) {
				return CachingCertificateLoader(certFile, keyFile)()
			}
			// Start a goroutine to periodically refresh client certificates for this connection
			StartClientCertRotating(tlsConfig.GetClientCertificate, conn)
		}
	}

	return tlsConfig, nil
}

// rootCAs returns a cert pool to verify the TLS connection.
// If the caFile is not provided, the default system certificate pool will be returned
// If the caFile is provided, the provided CA will be appended to the system certificate pool
func rootCAs(caFile string) (*x509.CertPool, error) {
	certPool, err := x509.SystemCertPool()
	if err != nil {
		return nil, err
	}

	if len(caFile) == 0 {
		klog.Warningf("CA file is not provided, TLS connection will be verified with the system cert pool")
		return certPool, nil
	}

	caPEM, err := os.ReadFile(caFile)
	if err != nil {
		return nil, err
	}

	if ok := certPool.AppendCertsFromPEM(caPEM); !ok {
		return nil, fmt.Errorf("invalid CA %s", caFile)
	}

	return certPool, nil
}
