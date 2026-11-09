/* Copyright Contributors to the Open Cluster Management project */

package main

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"open-cluster-management.io/ocm/poc/aggregate-api-server/internal/server"
	"open-cluster-management.io/ocm/poc/aggregate-api-server/internal/store"
)

func main() {
	var (
		listenAddress = flag.String("listen-address", ":8443", "HTTPS listen address")
		tlsCert       = flag.String("tls-cert-file", "", "server certificate PEM path")
		tlsKey        = flag.String("tls-key-file", "", "server private key PEM path")
		proxyCA       = flag.String("requestheader-client-ca-file", "", "CA bundle used to verify the API aggregation proxy client certificate")
		proxyNames    = flag.String("requestheader-allowed-names", envOr("REQUESTHEADER_ALLOWED_NAMES", "front-proxy-client"), "comma-separated API aggregation proxy certificate common names")
		postgresDSN   = flag.String("postgres-dsn", os.Getenv("POSTGRES_DSN"), "PostgreSQL connection string")
	)
	flag.Parse()
	if *tlsCert == "" || *tlsKey == "" || *proxyCA == "" || *postgresDSN == "" {
		log.Fatal("--tls-cert-file, --tls-key-file, --requestheader-client-ca-file, and --postgres-dsn are required")
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	storage, err := store.OpenPostgres(ctx, *postgresDSN)
	if err != nil {
		log.Fatalf("initialize PostgreSQL storage: %v", err)
	}
	defer storage.Close()

	kubeConfig, err := rest.InClusterConfig()
	if err != nil {
		log.Fatalf("load in-cluster credentials for delegated authorization: %v", err)
	}
	kubeClient, err := kubernetes.NewForConfig(kubeConfig)
	if err != nil {
		log.Fatalf("create Kubernetes client for delegated authorization: %v", err)
	}

	serverCertificate, err := tls.LoadX509KeyPair(*tlsCert, *tlsKey)
	if err != nil {
		log.Fatalf("load serving certificate: %v", err)
	}
	caPEM, err := os.ReadFile(*proxyCA)
	if err != nil {
		log.Fatalf("read API aggregation proxy CA: %v", err)
	}
	clientCAs := x509.NewCertPool()
	if !clientCAs.AppendCertsFromPEM(caPEM) {
		log.Fatal("API aggregation proxy CA file contains no certificates")
	}
	allowedNames := map[string]struct{}{}
	for _, name := range strings.Split(*proxyNames, ",") {
		if name = strings.TrimSpace(name); name != "" {
			allowedNames[name] = struct{}{}
		}
	}
	if len(allowedNames) == 0 {
		log.Fatal("--requestheader-allowed-names must contain at least one certificate common name")
	}

	handler := &server.Handler{Store: storage, Objects: storage, Authorizer: server.NewSARAuthorizer(kubeClient), RequireProxyClientCert: true}
	httpServer := &http.Server{
		Addr:              *listenAddress,
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
		TLSConfig: &tls.Config{
			MinVersion:   tls.VersionTLS12,
			Certificates: []tls.Certificate{serverCertificate},
			// The aggregator's availability and discovery probes may connect
			// without a client certificate; presented certificates must verify
			// against the aggregation proxy CA. API resource paths additionally
			// require a verified client certificate in the handler.
			ClientAuth: tls.VerifyClientCertIfGiven,
			ClientCAs:  clientCAs,
			VerifyConnection: func(state tls.ConnectionState) error {
				if len(state.PeerCertificates) == 0 {
					return nil
				}
				if _, ok := allowedNames[state.PeerCertificates[0].Subject.CommonName]; !ok {
					return fmt.Errorf("API aggregation proxy certificate common name %q is not allowed", state.PeerCertificates[0].Subject.CommonName)
				}
				return nil
			},
		},
	}

	serverErrors := make(chan error, 1)
	go func() {
		serverErrors <- httpServer.ListenAndServeTLS("", "")
	}()
	select {
	case err := <-serverErrors:
		if err != nil && err != http.ErrServerClosed {
			log.Fatalf("serve aggregated API: %v", err)
		}
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := httpServer.Shutdown(shutdownCtx); err != nil {
			fmt.Fprintf(os.Stderr, "shutdown aggregated API: %v\n", err)
		}
	}
}

func envOr(name, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}
