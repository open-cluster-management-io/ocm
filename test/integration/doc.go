// Package integration provides integration tests for open-cluster-management registration, the test cases include
// - managed cluster joining process
// - managed cluster health check
// - registration agent rotate its certificate after its certificate is expired
// - registration agent recovery from invalid bootstrap kubeconfig
// - registration agent recovery from invalid hub kubeconfig
package integration
