# Development Guide

This document provides comprehensive guidelines for new contributors developing the Open Cluster Management (OCM) project. It also serves as a reference for AI tools performing code reviews and analysis.

## Table of Contents

- [Project Overview](#project-overview)
- [Development Environment Setup](#development-environment-setup)
- [Code Organization](#code-organization)
- [Development Workflow](#development-workflow)
- [Code Standards and Style](#code-standards-and-style)
- [Testing Guidelines](#testing-guidelines)
- [API Development](#api-development)
- [Build and Release](#build-and-release)
- [Troubleshooting](#troubleshooting)

## Project Overview

Open Cluster Management (OCM) is a CNCF sandbox project that provides multicluster and multicloud management capabilities for Kubernetes. The project consists of five core components:

- **registration**: Manages cluster registration and certificate lifecycle
- **placement**: Handles content placement decisions across clusters
- **work**: Distributes and applies manifests to managed clusters
- **registration-operator**: Deploys and manages OCM components
- **addon-manager**: Manages OCM add-ons lifecycle

### Key Technologies

- **Language**: Go 1.25.0
- **Framework**: Kubernetes operators with controller-runtime
- **Build System**: Make with OpenShift build machinery
- **Testing**: Ginkgo/Gomega for BDD-style tests
- **Packaging**: Helm charts and OLM bundles

## Development Environment Setup

### Prerequisites

- Go 1.25.0 ([installation guide](https://go.dev/doc/install))
- Docker or Podman (container engine)
- [Kind](https://kind.sigs.k8s.io/) (local Kubernetes clusters)
- [kubectl](https://kubernetes.io/docs/tasks/tools/) (Kubernetes CLI)
- [clusteradm](https://github.com/open-cluster-management-io/clusteradm) (OCM CLI)

### Quick Start

1. **Create development cluster**:
   ```bash
   kind create cluster --name ocm-dev
   ```

2. **Initialize OCM control plane**:
   ```bash
   # Replace <TAG> with a concrete release (e.g., vX.Y.Z)
   curl -fsSL https://raw.githubusercontent.com/open-cluster-management-io/clusteradm/<TAG>/hack/install.sh | bash
   clusteradm init --wait --bundle-version="<BUNDLE_VERSION>"
   ```

3. **Join cluster as managed cluster**:
   ```bash
   clusteradm join --hub-token <token> --hub-apiserver <url> --wait --cluster-name cluster1 --force-internal-endpoint-lookup --bundle-version="<BUNDLE_VERSION>"
   clusteradm accept --clusters cluster1
   ```

4. **Verify installation**:
   ```bash
   kubectl get clustermanager,managedcluster
   kubectl get pods -A | grep open-cluster-management
   ```

## Code Organization

```
ocm/
├── cmd/                    # Main applications entry points
│   ├── addon/             # Addon manager
│   ├── placement/         # Placement controller
│   ├── registration/      # Registration controller
│   ├── registration-operator/ # Operator
│   ├── server/            # GRPC server
│   └── work/              # Work controller
├── pkg/                   # Core business logic
│   ├── addon/             # Addon framework and controllers
│   ├── operator/          # Operator controllers
│   ├── placement/         # Placement logic and controllers
│   ├── registration/      # Registration logic
│   └── work/              # Work distribution logic
├── manifests/             # Deployment manifests
├── deploy/                # Helm charts and OLM bundles
├── test/                  # Test suites
│   ├── e2e/              # End-to-end tests
│   └── integration/      # Integration tests
└── solutions/             # Usage examples and solutions
```

### Key Packages

- `pkg/common/`: Shared utilities and helpers
- `pkg/registration/hub/`: Hub-side registration controllers
- `pkg/registration/spoke/`: Managed cluster registration logic
- `pkg/work/hub/`: Hub-side work distribution
- `pkg/work/spoke/`: Managed cluster work application
- `pkg/placement/controllers/`: Placement scheduling and decision logic
- `pkg/addon/controllers/`: Addon lifecycle management controllers

## Development Workflow

### 1. Making Code Changes

```bash
# Fork and clone the repository
git clone https://github.com/<your-username>/ocm.git
cd ocm

# Create feature branch
git checkout -b feature/your-feature-name

# Make changes and commit with DCO sign-off
git commit -s -m "Add new feature: description"
```

### 2. Building Images

Build all images:
```bash
IMAGE_REGISTRY=quay.io/your-org IMAGE_TAG=dev make images
```

Build specific component:
```bash
IMAGE_REGISTRY=quay.io/your-org IMAGE_TAG=dev make image-registration
```

Load images to kind cluster:
```bash
kind load docker-image quay.io/your-org/registration:dev --name ocm-dev
```

### 3. Testing Changes

Update operator images:
```bash
kubectl set image -n open-cluster-management deployment/cluster-manager registration-operator=quay.io/your-org/registration-operator:dev
```

Update component images via CustomResource:
```bash
kubectl edit clustermanager cluster-manager  # For hub components
kubectl edit klusterlet klusterlet           # For spoke components
```

## Code Standards and Style

### Go Code Style

- **Follow standard Go conventions**: Use `gofmt`, `go vet`, and `golangci-lint`
- **Variable naming**: Use camelCase for local variables, PascalCase for exported items
- **Comments**: Write clear comments for exported functions and complex logic
- **Interface design**: Keep interfaces small and focused (interface segregation principle)

### Import Organization

Imports must be organized in the following order:
1. Standard library packages
2. Third-party packages  
3. OCM packages

```go
import (
    // Standard library
    "context"
    "fmt"
    "time"

    // Third-party
    "github.com/spf13/cobra"
    "k8s.io/apimachinery/pkg/api/errors"
    "sigs.k8s.io/controller-runtime/pkg/client"

    // OCM packages
    "open-cluster-management.io/api/cluster/v1"
    "open-cluster-management.io/ocm/pkg/common/helpers"
)
```

### Code Quality Requirements

All code must pass these checks:
```bash
make verify                      # Run all verification checks
make verify-gocilint            # Linting with golangci-lint
make verify-fmt-imports         # Import formatting verification
make verify-crds               # CRD consistency checks
```

Auto-format imports:
```bash
make fmt-imports                # Automatically format import statements
```

### Naming Conventions

- **Files**: Use snake_case (e.g., `cluster_controller.go`)
- **Packages**: Use lowercase, single words when possible
- **Constants**: Use CamelCase (PascalCase for exported constants); reserve ALL_CAPS only when mirroring external specs
- **Controllers**: Suffix with `Controller` (e.g., `PlacementController`)
- **Interfaces**: Use descriptive names, often ending with `-er` (e.g., `ClusterLister`)

### Documentation Standards

- All exported functions, types, and constants must have comments
- Comments should start with the name of the item being documented
- Use complete sentences and proper punctuation
- Include examples for complex APIs

## Testing Guidelines

### Unit Tests

```bash
make test-unit                    # Run all unit tests
go test ./pkg/registration/...    # Test specific package
```

### Integration Tests

Integration tests use controller-runtime's envtest framework:

```bash
make test-integration                      # All integration tests
make test-registration-integration         # Registration only
make test-placement-integration           # Placement only
make test-work-integration               # Work only
make test-addon-integration              # Addon only
```

### E2E Tests

End-to-end tests run against real clusters and require a kind cluster with proper setup:

```bash
# 1. Create kind cluster and set KUBECONFIG
kind get kubeconfig --name ocm-e2e > /tmp/kubeconfig-ocm-e2e
export KUBECONFIG=/tmp/kubeconfig-ocm-e2e

# 2. Run E2E tests with required variables
IMAGE_TAG=e2e KLUSTERLET_DEPLOY_MODE=Singleton make test-e2e
```

E2E test variables:
- `IMAGE_TAG`: Tag for container images (default: latest)
- `KLUSTERLET_DEPLOY_MODE`: Deployment mode (Default, Singleton, SingletonHosted)
- `MANAGED_CLUSTER_NAME`: Name of the managed cluster (default: cluster1)
- `KUBECONFIG`: Must be set to point to test cluster

### Test Organization

- **Unit tests**: `*_test.go` files alongside source code
- **Integration tests**: `test/integration/` directory
- **E2E tests**: `test/e2e/` directory
- **Test utilities**: `test/framework/` and `pkg/*/testing/`

### Test Writing Guidelines

- Use Ginkgo/Gomega for BDD-style tests
- Write descriptive test names using `Describe`, `Context`, and `It`
- Use table-driven tests for multiple scenarios
- Mock external dependencies appropriately
- Ensure tests are deterministic and can run in parallel

## API Development

### Modifying APIs

OCM APIs are defined in the separate [api repository](https://github.com/open-cluster-management-io/api). To modify APIs:

1. **Clone both repositories**:
   ```bash
   git clone https://github.com/<your-username>/api.git
   git clone https://github.com/<your-username>/ocm.git
   ```

2. **Make API changes** in the api repository and run:
   ```bash
   cd api
   make update  # Generates CRDs and DeepCopy methods
   ```

3. **Use local API changes** in ocm repository:
   ```bash
   cd ocm
   go mod edit -replace open-cluster-management.io/api=/path/to/local/api
   go mod tidy && go mod vendor
   make update  # Updates CRDs, Helm charts, and operator manifests
   ```

4. **Remove replace directive** before submitting PR

### Update Process

The `make update` command handles:
- CRD copying from api repository
- Helm chart updates
- Operator manifest generation
- ClusterServiceVersion updates

## Build and Release

### Build System

The project uses OpenShift's build-machinery-go:

```bash
make all                        # Build all binaries
make images                     # Build all container images
make update                     # Update all generated manifests
```

### Image Build

Images are built for multiple architectures:
- `linux/amd64`
- `linux/arm64`

Registry configuration:
```bash
export IMAGE_REGISTRY=quay.io/open-cluster-management
export IMAGE_TAG=v0.x.x
```

### Available Make Targets

```bash
make build                      # Build binaries
make images                     # Build container images  
make test-unit                  # Run unit tests
make test-integration          # Run integration tests
make test-e2e                  # Run E2E tests
make verify                    # Run all verification checks
make update                    # Update generated files
make clean                     # Clean build artifacts
```

## Troubleshooting

### Common Issues

1. **API Server Connection Issues**:
   ```bash
   kubectl cluster-info          # Verify cluster connectivity
   kubectl get nodes            # Check cluster health
   ```

2. **Image Pull Failures**:
   ```bash
   kind load docker-image <image>  # Load to kind cluster
   kubectl describe pod <pod>      # Check image pull status
   ```

3. **Certificate Issues**:
   ```bash
   kubectl get csr               # Check certificate requests
   kubectl get secrets -A | grep tls  # Check TLS secrets
   ```

4. **Operator Issues**:
   ```bash
   kubectl logs -n open-cluster-management deployment/cluster-manager
   kubectl get clustermanager cluster-manager -o yaml
   ```

### Debug Tools

- Enable debug logging: Set `--v=4` or higher in component flags
- Use `kubectl describe` for resource status
- Check controller logs in `open-cluster-management*` namespaces
- Verify RBAC permissions with `kubectl auth can-i`

### Performance Debugging

- Use `go tool pprof` for CPU/memory profiling
- Monitor metrics endpoints (when enabled)
- Check resource utilization with `kubectl top`

## Contributing Guidelines

1. **Submit an issue** describing proposed changes
2. **Fork and develop** following this guide
3. **Add tests** for new functionality
4. **Run quality checks**: `make verify test-unit`
5. **Submit pull request** with DCO sign-off
6. **Address review feedback** promptly

For detailed contribution guidelines, see [CONTRIBUTING.md](CONTRIBUTING.md).

## Resources

- [Project Website](https://open-cluster-management.io/)
- [Community Slack](https://kubernetes.slack.com/channels/open-cluster-mgmt)
- [API Documentation](https://github.com/open-cluster-management-io/api)
- [Addon Framework](https://github.com/open-cluster-management-io/addon-framework)
- [User Scenarios](https://open-cluster-management.io/docs/scenarios/)