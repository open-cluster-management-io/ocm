
all: build
.PHONY: all

IMAGE_REGISTRY?=quay.io

# Include the library makefile
include $(addprefix ./vendor/github.com/openshift/build-machinery-go/make/, \
	golang.mk \
	targets/openshift/deps.mk \
	targets/openshift/images.mk \
	targets/openshift/bindata.mk \
)

$(call add-bindata,hub,./manifests/hub/...,bindata,bindata,./pkg/operators/hub/bindata/bindata.go)

copy-crd:
	bash -x hack/copy-crds.sh

update-all: copy-crd update

verify-crds:
	bash -x hack/verify-crds.sh

verify: verify-crds

# This will call a macro called "build-image" which will generate image specific targets based on the parameters:
# $0 - macro name
# $1 - target suffix
# $2 - Dockerfile path
# $3 - context directory for image build
# It will generate target "image-$(1)" for builing the image an binding it as a prerequisite to target "images".
$(call build-image,nucleus,$(IMAGE_REGISTRY)/open-cluster-management/nucleus,./Dockerfile,.)

clean:
	$(RM) ./nucleus
.PHONY: clean

GO_TEST_PACKAGES :=./pkg/... ./cmd/...
