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
include ./test/integration/integration-test.mk

# Add packages to do unit test
GO_TEST_PACKAGES :=./pkg/...

IMAGE_REGISTRY?=quay.io/open-cluster-management
IMAGE_TAG?=latest

$(call build-image,registration,$(IMAGE_REGISTRY)/registration:$(IMAGE_TAG),./build/Dockerfile.registration,.)
$(call build-image,work,$(IMAGE_REGISTRY)/work:$(IMAGE_TAG),./build/Dockerfile.work,.)
$(call build-image,placement,$(IMAGE_REGISTRY)/placement:$(IMAGE_TAG),./build/Dockerfile.placement,.)
$(call build-image,registration-operator,$(IMAGE_REGISTRY)/registration-operator:$(IMAGE_TAG),./build/Dockerfile.registration-operator,.)

copy-crd:
	bash -x hack/copy-crds.sh

patch-crd: ensure-yaml-patch
	bash hack/patch/patch-crd.sh $(YAML_PATCH)

update: patch-crd copy-crd

verify-crds: patch-crd
	bash -x hack/verify-crds.sh

verify: verify-crds
