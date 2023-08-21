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

OPERATOR_SDK?=$(PERMANENT_TMP_GOPATH)/bin/operator-sdk
OPERATOR_SDK_VERSION?=v1.1.0
operatorsdk_gen_dir:=$(dir $(OPERATOR_SDK))
# CSV_VERSION is used to generate new CSV manifests
CSV_VERSION?=0.12.0

OPERATOR_SDK_ARCHOS:=x86_64-linux-gnu
ifeq ($(GOHOSTOS),darwin)
	ifeq ($(GOHOSTARCH),amd64)
		OPERATOR_SDK_ARCHOS:=x86_64-apple-darwin
	endif
endif

# Add packages to do unit test
GO_TEST_PACKAGES :=./pkg/...
GO_TEST_FLAGS := -race -coverprofile=coverage.out

IMAGE_REGISTRY?=quay.io/open-cluster-management
IMAGE_TAG?=latest

OPERATOR_IMAGE_NAME ?= $(IMAGE_REGISTRY)/registration-operator:$(IMAGE_TAG)
# WORK_IMAGE can be set in the env to override calculated value
WORK_IMAGE ?= $(IMAGE_REGISTRY)/work:$(IMAGE_TAG)
# REGISTRATION_IMAGE can be set in the env to override calculated value
REGISTRATION_IMAGE ?= $(IMAGE_REGISTRY)/registration:$(IMAGE_TAG)
# PLACEMENT_IMAGE can be set in the env to override calculated value
PLACEMENT_IMAGE ?= $(IMAGE_REGISTRY)/placement:$(IMAGE_TAG)
# ADDON_MANAGER_IMAGE can be set in the env to override calculated value
ADDON_MANAGER_IMAGE ?= $(IMAGE_REGISTRY)/addon-manager:$(IMAGE_TAG)

$(call build-image,registration,$(REGISTRATION_IMAGE),./build/Dockerfile.registration,.)
$(call build-image,work,$(WORK_IMAGE),./build/Dockerfile.work,.)
$(call build-image,placement,$(PLACEMENT_IMAGE),./build/Dockerfile.placement,.)
$(call build-image,registration-operator,$(OPERATOR_IMAGE_NAME),./build/Dockerfile.registration-operator,.)
$(call build-image,addon-manager,$(ADDON_MANAGER_IMAGE),./build/Dockerfile.addon,.)

copy-crd:
	bash -x hack/copy-crds.sh

patch-crd: ensure-yaml-patch
	bash hack/patch/patch-crd.sh $(YAML_PATCH)

update: patch-crd copy-crd update-csv

update-csv: ensure-operator-sdk
	cd deploy/cluster-manager && ../../$(OPERATOR_SDK) generate bundle --manifests --deploy-dir config/ --crds-dir config/crds/ --output-dir olm-catalog/cluster-manager/ --version $(CSV_VERSION)
	cd deploy/klusterlet && ../../$(OPERATOR_SDK) generate bundle --manifests --deploy-dir config/ --crds-dir config/crds/ --output-dir olm-catalog/klusterlet/ --version=$(CSV_VERSION)

	# delete useless serviceaccounts in manifests although they are copied from config by operator-sdk.
	rm ./deploy/cluster-manager/olm-catalog/cluster-manager/manifests/cluster-manager_v1_serviceaccount.yaml
	rm ./deploy/klusterlet/olm-catalog/klusterlet/manifests/klusterlet_v1_serviceaccount.yaml

verify-crds: patch-crd
	bash -x hack/verify-crds.sh

verify-gocilint:
	go install github.com/golangci/golangci-lint/cmd/golangci-lint@v1.53.2
	golangci-lint run --timeout=5m --modules-download-mode vendor ./...

install-golang-gci:
	go install github.com/daixiang0/gci@v0.10.1

fmt-imports: install-golang-gci
	gci write --skip-generated -s standard -s default -s "prefix(open-cluster-management.io)" -s "prefix(open-cluster-management.io/ocm)" cmd pkg test dependencymagnet

verify-fmt-imports: install-golang-gci
	@output=$$(gci diff --skip-generated -s standard -s default -s "prefix(open-cluster-management.io)" -s "prefix(open-cluster-management.io/ocm)" cmd pkg test dependencymagnet); \
	if [ -n "$$output" ]; then \
	    echo "Diff output is not empty: $$output"; \
	    echo "Please run 'make fmt-imports' to format the golang files imports automatically."; \
	    exit 1; \
	else \
	    echo "Diff output is empty"; \
	fi

verify: verify-fmt-imports verify-crds verify-gocilint

ensure-operator-sdk:
ifeq "" "$(wildcard $(OPERATOR_SDK))"
	$(info Installing operator-sdk into '$(OPERATOR_SDK)')
	mkdir -p '$(operatorsdk_gen_dir)'
	curl -s -f -L https://github.com/operator-framework/operator-sdk/releases/download/$(OPERATOR_SDK_VERSION)/operator-sdk-$(OPERATOR_SDK_VERSION)-$(OPERATOR_SDK_ARCHOS) -o '$(OPERATOR_SDK)'
	chmod +x '$(OPERATOR_SDK)';
else
	$(info Using existing operator-sdk from "$(OPERATOR_SDK)")
endif

# Include the integration/e2e setup makefile.
include ./test/integration-test.mk
include ./test/e2e-test.mk
