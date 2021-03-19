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

$(call add-bindata,registration-agent,./pkg/addonmanager/registration/manifests/...,bindata,bindata,./pkg/addonmanager/registration/bindata/bindata.go)

# Image URL to use all building/pushing image targets;
IMAGE ?= addon-manager
IMAGE_REGISTRY ?= quay.io/open-cluster-management

GIT_HOST ?= github.com/open-cluster-management
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
$(call build-image,$(IMAGE),$(IMAGE_REGISTRY)/$(IMAGE),./Dockerfile,.)

example:
	go build -mod=vendor -trimpath github.com/open-cluster-management/addon-framework/examples/helloworld
