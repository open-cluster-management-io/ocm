# Copyright Contributors to the Open Cluster Management project
#
# Makefile for the ManifestWork SQL APIService POC.
#
# Deploy targets install an APIService that owns the whole
# work.open-cluster-management.io/v1 group/version. They refuse to run when the
# ManifestWork CRD is present, but never point them at an OCM hub anyway: use a
# disposable cluster.

SHELL := /usr/bin/env bash
.SHELLFLAGS := -eu -o pipefail -c
.DELETE_ON_ERROR:
.DEFAULT_GOAL := help

# Cluster and deployment settings
KIND_CLUSTER_NAME ?= ocm-api-poc
NAMESPACE ?= ocm-api-poc
IMAGE ?= ocm-aggregate-api-poc:dev
LOCAL_PULL_POLICY ?= IfNotPresent
DB_PASSWORD ?= $(shell openssl rand -hex 16 2>/dev/null)

# kubectl targeting. smoke/undeploy use KUBECONTEXT when set; the deploy
# targets set it themselves. deploy-local requires either an explicit
# KUBECONTEXT or CONFIRM_CURRENT_CONTEXT=yes to target the current context.
KUBECONTEXT ?=
CONFIRM_CURRENT_CONTEXT ?= no

# Auto-detect a container runtime (docker preferred, podman fallback).
CONTAINER_ENGINE ?= $(shell command -v docker 2>/dev/null || command -v podman 2>/dev/null || true)
KIND_CONTEXT := kind-$(KIND_CLUSTER_NAME)
KIND_ENV := $(if $(findstring podman,$(CONTAINER_ENGINE)),KIND_EXPERIMENTAL_PROVIDER=podman,)

# kind load docker-image shells out to the docker CLI even when kind itself
# uses the podman provider, so with podman the image is saved to an archive
# and loaded with kind load image-archive instead. The archive must carry a
# docker.io-qualified tag: containerd imports RepoTags verbatim, while CRI
# resolves short image names to docker.io/library/<name>. When overriding
# IMAGE under podman, use a fully qualified name or a single-segment one.
PODMAN_IMAGE := $(if $(findstring /,$(IMAGE)),$(IMAGE),docker.io/library/$(IMAGE))
KIND_IMAGE_ARCHIVE ?= /tmp/ocm-api-poc-image.tar

.PHONY: help test image push-image kind-create kind-delete deploy-kind redeploy-kind deploy-local smoke undeploy

help: ## Show this help
	@awk 'BEGIN {FS = ":.*##"; printf "Usage: make \033[36m<target>\033[0m\n\nTargets:\n"} /^[a-zA-Z_-]+:.*?##/ {printf "  \033[36m%-16s\033[0m %s\n", $$1, $$2}' $(MAKEFILE_LIST)

test: ## Run unit tests (set POSTGRES_TEST_DSN to include the PostgreSQL integration test)
	go test ./...

image: ## Build the POC container image ($(IMAGE))
	@test -n "$(CONTAINER_ENGINE)" || { echo "docker or podman is required to build $(IMAGE)" >&2; exit 1; }
	@if [[ -n '$(findstring podman,$(CONTAINER_ENGINE))' ]]; then \
		$(CONTAINER_ENGINE) build -t $(IMAGE) -t $(PODMAN_IMAGE) .; \
	else \
		$(CONTAINER_ENGINE) build -t $(IMAGE) .; \
	fi

push-image: image ## Push $(IMAGE) to a registry reachable by the target cluster
	@test -n "$(CONTAINER_ENGINE)" || { echo "docker or podman is required to push $(IMAGE)" >&2; exit 1; }
	$(CONTAINER_ENGINE) push $(IMAGE)

kind-create: ## Create (or reuse) the disposable kind cluster and export its kubeconfig
	@test -n "$(CONTAINER_ENGINE)" || { echo "docker or podman is required to run kind" >&2; exit 1; }
	@clusters="$$($(KIND_ENV) kind get clusters 2>/dev/null || true)"; \
	if grep -qx '$(KIND_CLUSTER_NAME)' <<<"$$clusters"; then \
		echo "Reusing kind cluster $(KIND_CLUSTER_NAME)"; \
		$(KIND_ENV) kind export kubeconfig --name $(KIND_CLUSTER_NAME); \
	else \
		echo "Creating kind cluster $(KIND_CLUSTER_NAME)"; \
		$(KIND_ENV) kind create cluster --name $(KIND_CLUSTER_NAME); \
	fi

kind-delete: ## Delete the disposable kind cluster
	$(KIND_ENV) kind delete cluster --name $(KIND_CLUSTER_NAME)

deploy-kind: kind-create image ## Build, load into kind, deploy, and smoke test the POC
	@if [[ -n '$(findstring podman,$(CONTAINER_ENGINE))' ]]; then \
		load_image='$(PODMAN_IMAGE)'; \
		rm -f $(KIND_IMAGE_ARCHIVE); \
		$(CONTAINER_ENGINE) save -o $(KIND_IMAGE_ARCHIVE) "$$load_image"; \
		$(KIND_ENV) kind load image-archive $(KIND_IMAGE_ARCHIVE) --name $(KIND_CLUSTER_NAME); \
	else \
		load_image='$(IMAGE)'; \
		$(KIND_ENV) kind load docker-image $(IMAGE) --name $(KIND_CLUSTER_NAME); \
	fi; \
	OCM_API_POC_IMAGE="$$load_image" OCM_API_POC_PULL_POLICY=Never \
		OCM_API_POC_NAMESPACE='$(NAMESPACE)' OCM_API_POC_DB_PASSWORD='$(DB_PASSWORD)' \
		OCM_API_POC_KUBECONTEXT='$(KIND_CONTEXT)' bash deploy/install.sh && \
	$(MAKE) smoke KUBECONTEXT='$(KIND_CONTEXT)'

redeploy-kind: ## Remove POC resources, then rebuild and redeploy in the kind cluster
	$(MAKE) undeploy KUBECONTEXT='$(KIND_CONTEXT)'
	$(MAKE) deploy-kind

deploy-local: ## Deploy to a kubectl context: KUBECONTEXT=<name>, or CONFIRM_CURRENT_CONTEXT=yes for the current one
	@if [[ -z '$(strip $(KUBECONTEXT))' && '$(CONFIRM_CURRENT_CONTEXT)' != yes ]]; then \
		echo "Refusing to deploy without an explicit target context." >&2; \
		echo "Usage: make deploy-local KUBECONTEXT=<kubectl-context> IMAGE=<registry-image>" >&2; \
		echo "       make deploy-local CONFIRM_CURRENT_CONTEXT=yes IMAGE=<registry-image>   # use the current context" >&2; \
		echo "The image must be pullable by the target cluster; run 'make push-image IMAGE=<registry-image>' first." >&2; \
		exit 1; \
	fi; \
	if [[ -n '$(strip $(KUBECONTEXT))' ]]; then context='$(KUBECONTEXT)'; \
	else context="$$(kubectl config current-context)"; echo "Using current kubectl context: $$context"; \
	fi; \
	OCM_API_POC_IMAGE='$(IMAGE)' OCM_API_POC_PULL_POLICY='$(LOCAL_PULL_POLICY)' \
		OCM_API_POC_NAMESPACE='$(NAMESPACE)' OCM_API_POC_DB_PASSWORD='$(DB_PASSWORD)' \
		OCM_API_POC_KUBECONTEXT="$$context" bash deploy/install.sh && \
	$(MAKE) smoke KUBECONTEXT="$$context"

smoke: ## Run the kubectl smoke test (set KUBECONTEXT or CONFIRM_CURRENT_CONTEXT=yes)
	@if [[ -z '$(strip $(KUBECONTEXT))' && '$(CONFIRM_CURRENT_CONTEXT)' != yes ]]; then \
		echo "Refusing to create/delete smoke-test resources without an explicit context." >&2; \
		echo "Usage: make smoke KUBECONTEXT=<kubectl-context>" >&2; \
		exit 1; \
	fi
	OCM_API_POC_KUBECONTEXT='$(strip $(KUBECONTEXT))' bash deploy/smoke.sh

undeploy: ## Remove the POC APIService, ClusterRoleBinding, and namespace
	@if [[ -z '$(strip $(KUBECONTEXT))' && '$(CONFIRM_CURRENT_CONTEXT)' != yes ]]; then \
		echo "Refusing cleanup without an explicit target context." >&2; \
		echo "Usage: make undeploy KUBECONTEXT=<kubectl-context>" >&2; \
		exit 1; \
	fi
	OCM_API_POC_KUBECONTEXT='$(strip $(KUBECONTEXT))' OCM_API_POC_CONFIRM_CURRENT_CONTEXT='$(CONFIRM_CURRENT_CONTEXT)' OCM_API_POC_NAMESPACE='$(NAMESPACE)' bash deploy/uninstall.sh
