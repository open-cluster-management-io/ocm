# E2E Testing Guide

This guide explains how to run e2e tests locally and in CI. The e2e test system now supports both macOS and Linux, using docker or podman instead of imagebuilder.

## Quick Start

```bash
# Run e2e tests (builds images, loads to kind, and runs tests)
IMAGE_TAG=e2e make test-e2e
```

## Prerequisites

### Container Runtime
Install either Docker Desktop or Podman:

**macOS - Docker Desktop:**
- Download from: https://www.docker.com/products/docker-desktop

**macOS - Podman:**
```bash
brew install podman
podman machine init --memory 8192  # 8GB recommended
podman machine start
```

**Linux:**
- Docker is typically pre-installed on CI runners
- Or install podman: follow your distribution's instructions

### Kind
```bash
brew install kind  # macOS
# or
curl -Lo ./kind https://kind.sigs.k8s.io/dl/latest/kind-linux-amd64  # Linux
chmod +x ./kind && sudo mv ./kind /usr/local/bin/kind
```

### Kubectl
```bash
brew install kubectl  # macOS
# or install via your package manager on Linux
```

### Create Kind Cluster
```bash
kind create cluster
```

## Usage

### Full E2E Test (Default)
Builds images, loads them into kind, deploys hub and spoke, and runs tests:

```bash
IMAGE_TAG=e2e make test-e2e
```

**Time:** ~30-40 minutes (first run), ~20-30 minutes (with cached layers)

**Steps executed:**
1. Build all 5 images with docker/podman
2. Load images into kind cluster
3. Deploy hub (helm install cluster-manager)
4. Deploy spoke (helm install klusterlet)
5. Run e2e test suite

### Skip Image Building
When you only modify test code and don't need to rebuild images:

```bash
IMAGE_TAG=e2e SKIP_IMAGE_BUILD=true make test-e2e
```

**Time:** ~20-30 minutes (skips 10+ minutes of image building)

**Steps executed:**
1. ~~Build images~~ (skipped)
2. ~~Load images~~ (skipped)
3. Deploy hub
4. Deploy spoke
5. Run e2e tests

**Use this when:**
- Only test code changed (`test/e2e/*.go`)
- Images are already built and loaded in kind
- Iterating on test fixes

### Clean Up Test Environment
Properly clean up all e2e resources (ClusterManager, helm releases, namespaces):

```bash
make clean-e2e-env
```

**Important:** This waits for the ClusterManager controller to clean up resources before removing helm releases, avoiding stuck finalizers.

### Individual Steps

**Build images only:**
```bash
IMAGE_TAG=e2e make images-kind
```

**Load images only:**
```bash
IMAGE_TAG=e2e make load-images-kind
```

**Run tests only (assumes images already loaded):**
```bash
IMAGE_TAG=e2e make run-e2e
```

## Environment Variables

| Variable | Description | Default |
|----------|-------------|---------|
| `IMAGE_TAG` | Tag for built images | `latest` |
| `SKIP_IMAGE_BUILD` | Set to `true` to skip building and loading images | `false` |
| `CONTAINER_CLI` | Override auto-detection (`docker` or `podman`) | Auto-detected |
| `BUILD_PLATFORM` | Override platform (e.g., `linux/arm64`) | Auto-detected from kind |
| `KLUSTERLET_DEPLOY_MODE` | Deployment mode | `Default` |
| `REGISTRATION_DRIVER` | Registration driver | `csr` |

### Deploy Modes
```bash
# Default mode
IMAGE_TAG=e2e make test-e2e

# Singleton mode
IMAGE_TAG=e2e KLUSTERLET_DEPLOY_MODE=Singleton make test-e2e

# Hosted mode
IMAGE_TAG=e2e KLUSTERLET_DEPLOY_MODE=SingletonHosted make test-e2e
```

### Registration Drivers
```bash
# CSR-based authentication (default)
IMAGE_TAG=e2e make test-e2e

# GRPC-based authentication
IMAGE_TAG=e2e REGISTRATION_DRIVER=grpc make test-e2e
```

## How It Works

### Auto-Detection
The build system automatically detects:
- **Container Runtime**: docker or podman (whichever is available)
- **Kind Cluster Architecture**: queries the kind cluster to determine amd64 or arm64
- **Build Platform**: matches the kind cluster architecture

### Architecture Handling
- **macOS Apple Silicon**: Builds images with `--platform=linux/arm64`
- **macOS Intel**: Builds images with `--platform=linux/amd64`
- **Linux**: Builds images with `--platform=linux/amd64`

### Image Loading
- **Docker**: Uses `kind load docker-image`
- **Podman**: Uses `podman save | kind load image-archive` (since podman doesn't integrate with kind directly)

### Images Built
1. `registration-operator` - Operator managing cluster-manager and klusterlet
2. `registration` - Registration agent (includes server binary)
3. `work` - Work agent
4. `placement` - Placement controller
5. `addon-manager` - Add-on manager

## Troubleshooting

### Out of Disk Space (Podman)
```bash
# Clean up unused images
podman image prune -a -f

# Check disk usage
podman system df
```

### Podman Out of Memory
```bash
# Increase podman machine memory to 8GB
podman machine stop
podman machine set --memory 8192
podman machine start
```

### Wrong Architecture Images
```bash
# Check kind cluster architecture
kubectl get node -o jsonpath='{.items[0].status.nodeInfo.architecture}'

# Verify image architecture
podman inspect quay.io/open-cluster-management/registration:e2e | grep Architecture
```

### Stuck ClusterManager (Finalizer Issue)
If cleanup is interrupted, ClusterManager might get stuck. Clean it manually:

```bash
# Remove finalizer
kubectl patch clustermanager cluster-manager -p '{"metadata":{"finalizers":[]}}' --type=merge

# Force delete
kubectl delete clustermanager --all --force --grace-period=0

# Delete namespaces
kubectl delete ns open-cluster-management open-cluster-management-agent open-cluster-management-hub --force --grace-period=0
```

Then run `make clean-e2e-env` next time to avoid this.

### Clean Start
```bash
# Clean up test environment properly
make clean-e2e-env

# Or recreate kind cluster
kind delete cluster
kind create cluster

# Run tests
IMAGE_TAG=e2e make test-e2e
```

### Check Configuration
```bash
make show-kind-config
```

## CI/CD Integration

The GitHub Actions e2e workflow has been simplified. All e2e jobs now just run:

```yaml
- name: Test E2E
  run: IMAGE_TAG=e2e make test-e2e
```

The Makefile handles all image building, loading, deployment, and testing automatically.

## Development Workflow

### First Run
```bash
# Create kind cluster
kind create cluster

# Run tests
IMAGE_TAG=e2e make test-e2e
```

### Iterating on Application Code
```bash
# Make changes to cmd/ or pkg/
# ...

# Full rebuild and test
IMAGE_TAG=e2e make test-e2e
```

### Iterating on Test Code
```bash
# Make changes to test/e2e/*.go
# ...

# Skip image rebuild (saves ~10 minutes)
IMAGE_TAG=e2e SKIP_IMAGE_BUILD=true make test-e2e
```

### Cleanup Between Runs
```bash
# Proper cleanup
make clean-e2e-env

# Run next test
IMAGE_TAG=e2e make test-e2e
```
