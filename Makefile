
all: build
.PHONY: all

# Include the library makefile
include $(addprefix ./vendor/github.com/openshift/build-machinery-go/make/, \
	golang.mk \
	targets/openshift/deps.mk \
	targets/openshift/images.mk \
	targets/openshift/bindata.mk \
)

IMAGE_REGISTRY?=quay.io

$(call add-bindata,spokecluster,./pkg/hub/spokecluster/manifests/...,bindata,bindata,./pkg/hub/spokecluster/bindata/bindata.go)
# This will call a macro called "build-image" which will generate image specific targets based on the parameters:
# $0 - macro name
# $1 - target suffix
# $2 - Dockerfile path
# $3 - context directory for image build
# It will generate target "image-$(1)" for builing the image an binding it as a prerequisite to target "images".
$(call build-image,registration,$(IMAGE_REGISTRY)/open-cluster-management/registration,./Dockerfile,.)


clean:
	$(RM) ./registration
.PHONY: clean

GO_TEST_PACKAGES :=./pkg/... ./cmd/...

include ./test/integration-test.mk
