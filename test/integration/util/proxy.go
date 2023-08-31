package util

import (
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"time"
)

// ProxyServer is a simple proxy server that supports http tunnel
type ProxyServer struct {
	httpServer      *http.Server
	HTTPProxyURL    string
	httpServerErrCh chan error

	httpsServer      *http.Server
	HTTPSProxyURL    string
	httpsServerErrCh chan error
	certData         []byte
	keyData          []byte
}

func NewProxyServer(certData, keyData []byte) *ProxyServer {
	return &ProxyServer{
		httpServerErrCh:  make(chan error, 1),
		certData:         certData,
		keyData:          keyData,
		httpsServerErrCh: make(chan error, 1),
	}
}

func (ps *ProxyServer) Start(ctx context.Context, timeout time.Duration) error {
	var failures []string
	go func() {
		select {
		case <-ctx.Done():
			fmt.Printf("[proxy-server] - shutting down...\n")
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			stopServer(ctx, "http", ps.httpServer)
			stopServer(ctx, "https", ps.httpsServer)
		case err := <-ps.httpServerErrCh:
			msg := fmt.Sprintf("failed to start http server: %v", err)
			failures = append(failures, msg)
			fmt.Printf("[proxy-server] - %s\n", msg)
		case err := <-ps.httpsServerErrCh:
			msg := fmt.Sprintf("failed to start https server: %v", err)
			failures = append(failures, msg)
			fmt.Printf("[proxy-server] - %s\n", msg)
		}
	}()

	go func() {
		ps.httpServerErrCh <- ps.startHTTP()
	}()

	go func() {
		ps.httpsServerErrCh <- ps.startHTTPS()
	}()

	// wait for the proxy server started
	timeoutCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	timer := time.NewTimer(1 * time.Second)
	for {
		timer.Reset(1 * time.Second)
		select {
		case <-timeoutCtx.Done():
			if ps.ready() {
				return nil
			}
			return fmt.Errorf("failed to start proxy server in %v", timeout)
		case <-timer.C:
			if len(failures) > 0 {
				return fmt.Errorf("failed to start proxy server: %v", strings.Join(failures, ", "))
			}

			if ps.ready() {
				return nil
			}
		}
	}
}

func (ps *ProxyServer) ready() bool {
	if len(ps.HTTPProxyURL) == 0 {
		return false
	}

	if len(ps.HTTPSProxyURL) == 0 {
		return false
	}

	return true
}

func stopServer(ctx context.Context, name string, server *http.Server) {
	if server == nil {
		return
	}
	if err := server.Shutdown(ctx); err != nil {
		fmt.Printf("[proxy-server] - failed to stop %s server: %v\n", name, err)
	}
}

func (ps *ProxyServer) startHTTP() error {
	ps.httpServer = newHTTPServer(nil)

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return err
	}

	ps.HTTPProxyURL = fmt.Sprintf("http://%s", ln.Addr().String())
	fmt.Printf("[proxy-server] - starting http proxy server on %s...\n", ps.HTTPProxyURL)
	return ps.httpServer.Serve(ln)
}

func (ps *ProxyServer) startHTTPS() error {
	serverCert, err := tls.X509KeyPair(ps.certData, ps.keyData)
	if err != nil {
		return err
	}

	ps.httpsServer = newHTTPServer(&tls.Config{
		Certificates: []tls.Certificate{serverCert},
		MinVersion:   tls.VersionTLS12,
	})

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return err
	}

	ps.HTTPSProxyURL = fmt.Sprintf("https://%s", ln.Addr().String())
	fmt.Printf("[proxy-server] - starting https proxy server on %s...\n", ps.HTTPSProxyURL)
	return ps.httpsServer.ServeTLS(ln, "", "")
}

func newHTTPServer(tlsConfig *tls.Config) *http.Server {
	return &http.Server{
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method == http.MethodConnect {
				handleTunneling(w, r)
			} else {
				handleHTTP(w, r)
			}
		}),
		// Disable HTTP/2.
		TLSNextProto:      make(map[string]func(*http.Server, *tls.Conn, http.Handler)),
		TLSConfig:         tlsConfig,
		ReadHeaderTimeout: 30 * time.Second,
	}
}

func handleTunneling(w http.ResponseWriter, r *http.Request) {
	fmt.Printf("[proxy-server] - handleTunneling: %s\n", r.RequestURI)

	dest_conn, err := net.DialTimeout("tcp", r.Host, 10*time.Second)
	if err != nil {
		http.Error(w, err.Error(), http.StatusServiceUnavailable)
		return
	}
	w.WriteHeader(http.StatusOK)
	hijacker, ok := w.(http.Hijacker)
	if !ok {
		http.Error(w, "Hijacking not supported", http.StatusInternalServerError)
		return
	}
	client_conn, _, err := hijacker.Hijack()
	if err != nil {
		http.Error(w, err.Error(), http.StatusServiceUnavailable)
	}
	go transfer(dest_conn, client_conn)
	go transfer(client_conn, dest_conn)
}

func transfer(destination io.WriteCloser, source io.ReadCloser) {
	defer destination.Close()
	defer source.Close()
	_, err := io.Copy(destination, source)
	if err != nil {
		fmt.Printf("[proxy-server] - failed to transfer traffic: %v\n", err)
	}
}

func handleHTTP(w http.ResponseWriter, req *http.Request) {
	fmt.Printf("[proxy-server] - handleHTTP: %s\n", req.RequestURI)
	resp, err := http.DefaultTransport.RoundTrip(req)
	if err != nil {
		http.Error(w, err.Error(), http.StatusServiceUnavailable)
		return
	}
	defer resp.Body.Close()
	copyHeader(w.Header(), resp.Header)
	w.WriteHeader(resp.StatusCode)
	_, err = io.Copy(w, resp.Body)
	if err != nil {
		fmt.Printf("[proxy-server] - failed to handleHTTP %q: %v\n", req.RequestURI, err)
	}
}

func copyHeader(dst, src http.Header) {
	for k, vv := range src {
		for _, v := range vv {
			dst.Add(k, v)
		}
	}
}
