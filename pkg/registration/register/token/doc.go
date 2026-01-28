// Package token provides a token-based authentication driver for addon registration only.
// This driver uses Kubernetes ServiceAccount token projection to authenticate addons with the hub cluster.
// It is designed to be forked from cluster-level drivers (CSR/gRPC/AWS IRSA) and cannot be used
// for cluster registration itself.
package token
