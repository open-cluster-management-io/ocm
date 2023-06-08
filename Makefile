SHELL :=/bin/bash

all: build
.PHONY: all

# Include the library makefile
include $(addprefix ./vendor/github.com/openshift/build-machinery-go/make/, \
	golang.mk \
	targets/openshift/deps.mk \
	targets/openshift/images.mk \
	targets/openshift/kustomize.mk \
	lib/tmp.mk \
)

export GOHOSTOS    ?=$(shell go env GOHOSTOS)
export GOHOSTARCH  ?=$(shell go env GOHOSTARCH)

# Tools for deploy
export KUBECONFIG ?= ./.kubeconfig
export MANAGED_CLUSTER_NAME ?= hub
export HOSTED_MANAGED_CLUSTER_NAME ?= managed
export HOSTED_MANAGED_KLUSTERLET_NAME ?= managed
export HOSTED_MANAGED_KUBECONFIG_SECRET_NAME ?= e2e-hosted-managed-kubeconfig

KUBECTL?=kubectl
KUSTOMIZE_VERSION:=4.5.5
PWD=$(shell pwd)

# Image URL to use all building/pushing image targets;
MANAGER_IMAGE := addon-manager
EXAMPLE_IMAGE ?= addon-examples
IMAGE_REGISTRY ?= quay.io/open-cluster-management
IMAGE_TAG ?= latest
export MANAGER_IMAGE_NAME ?= $(IMAGE_REGISTRY)/$(MANAGER_IMAGE):$(IMAGE_TAG)
export EXAMPLE_IMAGE_NAME ?= $(IMAGE_REGISTRY)/$(EXAMPLE_IMAGE):$(IMAGE_TAG)

GIT_HOST ?= open-cluster-management.io
BASE_DIR := $(shell basename $(PWD))
DEST := $(GOPATH)/src/$(GIT_HOST)/$(BASE_DIR)

# Add packages to do unit test
GO_TEST_PACKAGES :=./pkg/...

# This will call a macro called "build-image" which will generate image specific targets based on the parameters:
# $0 - macro name
# $1 - target suffix
# $2 - Dockerfile path
# $3 - context directory for image build
# It will generate target "image-$(1)" for building the image and binding it as a prerequisite to target "images".
$(call build-image,$(MANAGER_IMAGE),$(IMAGE_REGISTRY)/$(MANAGER_IMAGE):$(IMAGE_TAG),./build/Dockerfile,.)
$(call build-image,$(EXAMPLE_IMAGE),$(IMAGE_REGISTRY)/$(EXAMPLE_IMAGE):$(IMAGE_TAG),./build/Dockerfile.example,.)

verify-gocilint:
	go install github.com/golangci/golangci-lint/cmd/golangci-lint@v1.46.2
	golangci-lint run --timeout=3m --modules-download-mode vendor ./...

verify: verify-gocilint

deploy-ocm:
	examples/deploy/ocm/install.sh

deploy-hosted-ocm:
	examples/deploy/hosted-ocm/install.sh

deploy-addon-manager: ensure-kustomize
	cp deploy/kustomization.yaml deploy/kustomization.yaml.tmp
	cd deploy && ../$(KUSTOMIZE) edit set image quay.io/open-cluster-management/addon-manager=$(MANAGER_IMAGE_NAME)
	$(KUSTOMIZE) build deploy | $(KUBECTL) apply -f -
	mv deploy/kustomization.yaml.tmp deploy/kustomization.yaml

deploy-busybox: ensure-kustomize
	cp examples/deploy/addon/busybox/kustomization.yaml examples/deploy/addon/busybox/kustomization.yaml.tmp
	cd examples/deploy/addon/busybox && ../../../../$(KUSTOMIZE) edit set image quay.io/open-cluster-management/addon-examples=$(EXAMPLE_IMAGE_NAME)
	$(KUSTOMIZE) build examples/deploy/addon/busybox | $(KUBECTL) apply -f -
	mv examples/deploy/addon/busybox/kustomization.yaml.tmp examples/deploy/addon/busybox/kustomization.yaml

deploy-helloworld: ensure-kustomize
	cp examples/deploy/addon/helloworld/kustomization.yaml examples/deploy/addon/helloworld/kustomization.yaml.tmp
	cd examples/deploy/addon/helloworld && ../../../../$(KUSTOMIZE) edit set image quay.io/open-cluster-management/addon-examples=$(EXAMPLE_IMAGE_NAME)
	$(KUSTOMIZE) build examples/deploy/addon/helloworld | $(KUBECTL) apply -f -
	mv examples/deploy/addon/helloworld/kustomization.yaml.tmp examples/deploy/addon/helloworld/kustomization.yaml

deploy-helloworld-helm: ensure-kustomize
	cp examples/deploy/addon/helloworld-helm/kustomization.yaml examples/deploy/addon/helloworld-helm/kustomization.yaml.tmp
	cd examples/deploy/addon/helloworld-helm && ../../../../$(KUSTOMIZE) edit set image quay.io/open-cluster-management/addon-examples=$(EXAMPLE_IMAGE_NAME)
	$(KUSTOMIZE) build examples/deploy/addon/helloworld-helm | $(KUBECTL) apply -f -
	mv examples/deploy/addon/helloworld-helm/kustomization.yaml.tmp examples/deploy/addon/helloworld-helm/kustomization.yaml

deploy-helloworld-hosted: ensure-kustomize
	cp examples/deploy/addon/helloworld-hosted/kustomization.yaml examples/deploy/addon/helloworld-hosted/kustomization.yaml.tmp
	cd examples/deploy/addon/helloworld-hosted && ../../../../$(KUSTOMIZE) edit set image quay.io/open-cluster-management/addon-examples=$(EXAMPLE_IMAGE_NAME)
	$(KUSTOMIZE) build examples/deploy/addon/helloworld-hosted | $(KUBECTL) apply -f -
	mv examples/deploy/addon/helloworld-hosted/kustomization.yaml.tmp examples/deploy/addon/helloworld-hosted/kustomization.yaml

deploy-helloworld-template: ensure-kustomize
	$(KUBECTL) create namespace $(MANAGED_CLUSTER_NAME) --dry-run=client -o yaml | $(KUBECTL) apply -f -
# remove the following line when the registration-operator is supported to install the addon template CRD
	$(KUBECTL) apply -f ./vendor/open-cluster-management.io/api/addon/v1alpha1/0000_03_addon.open-cluster-management.io_addontemplates.crd.yaml
	cp examples/deploy/addon/helloworld-template/kustomization.yaml examples/deploy/addon/helloworld-template/kustomization.yaml.tmp
	cd examples/deploy/addon/helloworld-template && ../../../../$(KUSTOMIZE) edit set image quay.io/open-cluster-management/addon-examples=$(EXAMPLE_IMAGE_NAME)
	$(KUSTOMIZE) build examples/deploy/addon/helloworld-template | $(KUBECTL) apply -f -
	mv examples/deploy/addon/helloworld-template/kustomization.yaml.tmp examples/deploy/addon/helloworld-template/kustomization.yaml

undeploy-addon:
	$(KUBECTL) delete -f examples/deploy/addon/helloworld-hosted/resources/helloworld_hosted_clustermanagementaddon.yaml --ignore-not-found
	$(KUBECTL) delete -f examples/deploy/addon/helloworld-helm/resources/helloworld_helm_clustermanagementaddon.yaml --ignore-not-found
	$(KUBECTL) delete -f examples/deploy/addon/helloworld/resources/helloworld_clustermanagementaddon.yaml --ignore-not-found
	$(KUBECTL) delete -f examples/deploy/addon/busybox/resources/busybox_clustermanagementaddon.yaml --ignore-not-found

undeploy-busybox: ensure-kustomize
	$(KUSTOMIZE) build examples/deploy/addon/busybox | $(KUBECTL) delete --ignore-not-found -f -

undeploy-helloworld: ensure-kustomize
	$(KUSTOMIZE) build examples/deploy/addon/helloworld | $(KUBECTL) delete --ignore-not-found -f -

undeploy-helloworld-helm: ensure-kustomize
	$(KUSTOMIZE) build examples/deploy/addon/helloworld-helm | $(KUBECTL) delete --ignore-not-found -f -

undeploy-helloworld-hosted: ensure-kustomize
	$(KUSTOMIZE) build examples/deploy/addon/helloworld-hosted | $(KUBECTL) delete --ignore-not-found -f -

build-e2e:
	go test -c ./test/e2e

test-e2e: build-e2e deploy-ocm deploy-addon-manager deploy-helloworld deploy-helloworld-helm deploy-helloworld-template
	./e2e.test -test.v -ginkgo.v

build-hosted-e2e:
	go test -c ./test/e2ehosted

test-hosted-e2e: build-hosted-e2e deploy-hosted-ocm deploy-addon-manager deploy-helloworld-hosted
	./e2ehosted.test -test.v -ginkgo.v

RUNTIME ?= podman
build-images: build-image-manager build-image-examples
build-image-manager:
	$(RUNTIME) build \
		-f ./build/Dockerfile \
		-t $(MANAGER_IMAGE_NAME) .
build-image-examples:
	$(RUNTIME) build \
		-f ./build/Dockerfile.example \
		-t $(EXAMPLE_IMAGE_NAME) .

include ./test/integration-test.mk