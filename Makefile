SHELL :=/bin/bash

all: build
.PHONY: all

# Include the library makefile
include $(addprefix ./vendor/github.com/openshift/build-machinery-go/make/, \
	golang.mk \
	targets/openshift/deps.mk \
	targets/openshift/images.mk \
	targets/openshift/yaml-patch.mk\
	lib/tmp.mk\
)

# Include the integration setup makefile.
include ./test/integration-test.mk

RUNTIME ?= docker

# Add packages to do unit test
GO_TEST_PACKAGES :=./pkg/...

IMAGE_REGISTRY?=quay.io/open-cluster-management
IMAGE_TAG?=latest

REGISTRATION_PATH=staging/src/open-cluster-management.io/registration
WORK_PATH=staging/src/open-cluster-management.io/work
PLACEMENT_PATH=staging/src/open-cluster-management.io/placement
REGISTRATION_OPERATOR_PATH=staging/src/open-cluster-management.io/registration-operator

image-registration:
	$(RUNTIME) build \
		-f build/Dockerfile.registration \
		-t $(IMAGE_REGISTRY)/registration:$(IMAGE_TAG) .

image-work:
	$(RUNTIME) build \
		-f build/Dockerfile.work \
		-t $(IMAGE_REGISTRY)/work:$(IMAGE_TAG) .

image-placement:
	$(RUNTIME) build \
		-f build/Dockerfile.placement \
		-t $(IMAGE_REGISTRY)/placement:$(IMAGE_TAG) .

image-registration-operator:
	$(RUNTIME) build \
		-f build/Dockerfile.registration-operator \
		-t $(IMAGE_REGISTRY)/registration-operator:$(IMAGE_TAG) .

images:
	make image-registration
	make image-work
	make image-placement
	make image-registration-operator

copy-crd:
	bash -x hack/copy-crds.sh

patch-crd: ensure-yaml-patch
	bash hack/patch/patch-crd.sh $(YAML_PATCH)

update: patch-crd copy-crd

verify-crds: patch-crd
	bash -x hack/verify-crds.sh

verify: verify-crds
