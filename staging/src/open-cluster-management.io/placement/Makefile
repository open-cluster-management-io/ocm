SHELL :=/bin/bash

all: build
.PHONY: all

# Include the library makefile
include $(addprefix ./vendor/github.com/openshift/build-machinery-go/make/, \
	golang.mk \
	targets/openshift/deps.mk \
	targets/openshift/images.mk \
	lib/tmp.mk \
)

# Image URL to use all building/pushing image targets;
IMAGE ?= placement
IMAGE_TAG?=latest
IMAGE_REGISTRY ?= quay.io/open-cluster-management
IMAGE_NAME?=$(IMAGE_REGISTRY)/$(IMAGE):$(IMAGE_TAG)
KUBECTL?=kubectl
KUSTOMIZE?=$(PWD)/$(PERMANENT_TMP_GOPATH)/bin/kustomize
KUSTOMIZE_VERSION?=v3.5.4
KUSTOMIZE_ARCHIVE_NAME?=kustomize_$(KUSTOMIZE_VERSION)_$(GOHOSTOS)_$(GOHOSTARCH).tar.gz
kustomize_dir:=$(dir $(KUSTOMIZE))

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
$(call build-image,$(IMAGE),$(IMAGE_REGISTRY)/$(IMAGE):${IMAGE_TAG},./Dockerfile,.)

update: copy-crd

copy-crd:
	bash -x hack/copy-crds.sh

verify: verify-crds

verify-crds:
	bash -x hack/verify-crds.sh

deploy-hub: ensure-kustomize
	cp deploy/hub/kustomization.yaml deploy/hub/kustomization.yaml.tmp
	cd deploy/hub && $(KUSTOMIZE) edit set image quay.io/open-cluster-management/placement:latest=$(IMAGE_NAME)
	$(KUSTOMIZE) build deploy/hub | $(KUBECTL) apply -f -
	mv deploy/hub/kustomization.yaml.tmp deploy/hub/kustomization.yaml

undeploy-hub:
	$(KUSTOMIZE) build deploy/hub | $(KUBECTL) delete --ignore-not-found -f -

build-e2e:
	go test -c ./test/e2e -mod=vendor

test-e2e: build-e2e ensure-kustomize deploy-hub
	./e2e.test -test.v -ginkgo.v

clean-e2e:
	$(RM) ./e2e.test

build-scalability:
	go test -c ./test/scalability -mod=vendor

test-scalability: build-scalability ensure-kustomize deploy-hub
	./scalability.test -test.v -ginkgo.v

clean-scalability:
	$(RM) ./scalability.test

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
