# Makefile targets for building and loading images into kind
# Works with both docker and podman on all platforms
# Replaces imagebuilder which doesn't work well on macOS

# Auto-detect container runtime (docker or podman)
# Only check when actually needed (not during container builds)
CONTAINER_CLI ?= $(shell command -v docker 2>/dev/null || command -v podman 2>/dev/null)
ifdef CONTAINER_CLI
CONTAINER_RUNTIME := $(notdir $(CONTAINER_CLI))
endif

# Detect kind cluster architecture
# Kind clusters can be amd64 or arm64 depending on the host
KIND_CLUSTER_ARCH ?= $(shell $(KUBECTL) get node -o jsonpath='{.items[0].status.nodeInfo.architecture}' 2>/dev/null || echo "amd64")
KIND_CLUSTER_NAME ?= $(shell $(KUBECTL) config current-context 2>/dev/null | sed 's/kind-//')

# Set platform for builds to match kind cluster
BUILD_PLATFORM ?= linux/$(KIND_CLUSTER_ARCH)

# Docker/podman build settings
# These are only used by kind-specific targets below

# Function to load image into kind
# $1 - image name with tag
define load-image-to-kind
	@echo "Loading $(1) into kind cluster $(KIND_CLUSTER_NAME)..."
	@if [ "$(CONTAINER_RUNTIME)" = "podman" ]; then \
		echo "  Using podman save + kind load..."; \
		$(CONTAINER_CLI) save $(1) | kind load image-archive --name=$(KIND_CLUSTER_NAME) /dev/stdin; \
	else \
		echo "  Using kind load docker-image..."; \
		kind load docker-image --name=$(KIND_CLUSTER_NAME) $(1); \
	fi
endef

# Build all images with correct architecture for kind
images-kind:
ifndef CONTAINER_CLI
	$(error Neither docker nor podman found. Please install one of them.)
endif
	@echo "Building images for kind cluster..."
	@echo "  Container runtime: $(CONTAINER_RUNTIME)"
	@echo "  Kind cluster arch: $(KIND_CLUSTER_ARCH)"
	@echo "  Build platform: $(BUILD_PLATFORM)"
	@$(MAKE) images \
		IMAGE_BUILD_BUILDER="$(CONTAINER_RUNTIME) build" \
		IMAGE_BUILD_DEFAULT_FLAGS="--platform=$(BUILD_PLATFORM)"
.PHONY: images-kind

# Load all images into kind cluster
load-images-kind:
	$(call load-image-to-kind,$(OPERATOR_IMAGE_NAME))
	$(call load-image-to-kind,$(REGISTRATION_IMAGE))
	$(call load-image-to-kind,$(WORK_IMAGE))
	$(call load-image-to-kind,$(PLACEMENT_IMAGE))
	$(call load-image-to-kind,$(ADDON_MANAGER_IMAGE))
	@echo "All images loaded into kind cluster successfully!"
.PHONY: load-images-kind

# Build and load images in one step
images-and-load-kind: images-kind load-images-kind
.PHONY: images-and-load-kind

# Display detected settings
show-kind-config:
ifndef CONTAINER_CLI
	$(error Neither docker nor podman found. Please install one of them.)
endif
	@echo "Container runtime: $(CONTAINER_RUNTIME)"
	@echo "Kind cluster name: $(KIND_CLUSTER_NAME)"
	@echo "Kind cluster arch: $(KIND_CLUSTER_ARCH)"
	@echo "Build platform: $(BUILD_PLATFORM)"
	@echo "Image registry: $(IMAGE_REGISTRY)"
	@echo "Image tag: $(IMAGE_TAG)"
.PHONY: show-kind-config
