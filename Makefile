SHELL :=/bin/bash

all: build
.PHONY: all

# Include the library makefile
include $(addprefix ./vendor/github.com/openshift/build-machinery-go/make/, \
	golang.mk \
	targets/openshift/deps.mk \
	targets/openshift/images.mk \
	targets/openshift/bindata.mk \
	lib/tmp.mk \
)

# Tools for deploy
KUBECONFIG ?= ./.kubeconfig
KUBECTL?=kubectl
PWD=$(shell pwd)
KUSTOMIZE?=$(PWD)/$(PERMANENT_TMP_GOPATH)/bin/kustomize
KUSTOMIZE_VERSION?=v3.5.4
KUSTOMIZE_ARCHIVE_NAME?=kustomize_$(KUSTOMIZE_VERSION)_$(GOHOSTOS)_$(GOHOSTARCH).tar.gz
kustomize_dir:=$(dir $(KUSTOMIZE))

# Image URL to use all building/pushing image targets;
GO_BUILD_PACKAGES :=./examples/...
IMAGE ?= helloworld-addon
IMAGE_REGISTRY ?= quay.io/open-cluster-management
IMAGE_TAG ?= latest
EXAMPLE_IMAGE_NAME ?= $(IMAGE_REGISTRY)/$(IMAGE):$(IMAGE_TAG)

GIT_HOST ?= open-cluster-management.io
BASE_DIR := $(shell basename $(PWD))
DEST := $(GOPATH)/src/$(GIT_HOST)/$(BASE_DIR)

# Add packages to do unit test
GO_TEST_PACKAGES :=./pkg/...

$(call add-bindata,foundation-agent,./examples/helloworld/manifests/...,bindata,bindata,./examples/helloworld/bindata/bindata.go)

# This will call a macro called "build-image" which will generate image specific targets based on the parameters:
# $0 - macro name
# $1 - target suffix
# $2 - Dockerfile path
# $3 - context directory for image build
# It will generate target "image-$(1)" for building the image and binding it as a prerequisite to target "images".
$(call build-image,$(IMAGE),$(IMAGE_REGISTRY)/$(IMAGE),./Dockerfile,.)

deploy-hub:
	examples/deploy/managedcluster/hub/install.sh

deploy-klusterlet:
	examples/deploy/managedcluster/klusterlet/install.sh

deploy-example: ensure-kustomize
	cp examples/deploy/addon/kustomization.yaml examples/deploy/addon/kustomization.yaml.tmp
	cd examples/deploy/addon && $(KUSTOMIZE) edit set image helloworld-controller=$(EXAMPLE_IMAGE_NAME) && $(KUSTOMIZE) edit add configmap image-config --from-literal=EXAMPLE_IMAGE_NAME=$(EXAMPLE_IMAGE_NAME)
	$(KUSTOMIZE) build examples/deploy/addon | $(KUBECTL) apply -f -
	mv examples/deploy/addon/kustomization.yaml.tmp examples/deploy/addon/kustomization.yaml

undeploy-example: ensure-kustomize
	$(KUSTOMIZE) build examples/deploy/addon | $(KUBECTL) delete --ignore-not-found -f -

build-e2e:
	go test -c ./test/e2e

test-e2e: build-e2e deploy-hub deploy-klusterlet deploy-example
	./e2e.test -test.v -ginkgo.v

# Ensure kustomize
ensure-kustomize:
ifeq "" "$(wildcard $(KUSTOMIZE))"
	$(info Installing kustomize into '$(KUSTOMIZE)')
	mkdir -p '$(kustomize_dir)'
	curl -s -f -L https://github.com/kubernetes-sigs/kustomize/releases/download/kustomize%2F$(KUSTOMIZE_VERSION)/$(KUSTOMIZE_ARCHIVE_NAME) -o '$(kustomize_dir)$(KUSTOMIZE_ARCHIVE_NAME)'
	tar -C '$(kustomize_dir)' -zvxf '$(kustomize_dir)$(KUSTOMIZE_ARCHIVE_NAME)'
	chmod +x '$(KUSTOMIZE)';
else
	$(info Using existing kustomize from "$(KUSTOMIZE)")
endif

include ./test/integration-test.mk