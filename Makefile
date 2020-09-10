
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

IMAGE_REGISTRY?=quay.io/open-cluster-management
IMAGE_TAG?=latest
IMAGE_NAME?=$(IMAGE_REGISTRY)/registration:$(IMAGE_TAG)
KUBECONFIG ?= ./.kubeconfig
KUBECTL?=kubectl
KUSTOMIZE?=$(PERMANENT_TMP_GOPATH)/bin/kustomize
KUSTOMIZE_VERSION?=v3.5.4
KUSTOMIZE_ARCHIVE_NAME?=kustomize_$(KUSTOMIZE_VERSION)_$(GOHOSTOS)_$(GOHOSTARCH).tar.gz
kustomize_dir:=$(dir $(KUSTOMIZE))

$(call add-bindata,managedcluster,./pkg/hub/managedcluster/manifests/...,bindata,bindata,./pkg/hub/managedcluster/bindata/bindata.go)
$(call add-bindata,spokecluster-e2e,./deploy/spoke/...,bindata,bindata,./test/e2e/bindata/bindata.go)

# This will call a macro called "build-image" which will generate image specific targets based on the parameters:
# $0 - macro name
# $1 - target suffix
# $2 - Dockerfile path
# $3 - context directory for image build
# It will generate target "image-$(1)" for building the image and binding it as a prerequisite to target "images".
$(call build-image,registration,$(IMAGE_REGISTRY)/registration,./Dockerfile,.)

clean:
	$(RM) ./registration
.PHONY: clean

deploy-hub: ensure-kustomize
	cp deploy/hub/kustomization.yaml deploy/hub/kustomization.yaml.tmp
	cd deploy/hub && ../../$(KUSTOMIZE) edit set image quay.io/open-cluster-management/registration:latest=$(IMAGE_NAME)
	$(KUSTOMIZE) build deploy/hub | $(KUBECTL) apply -f -
	mv deploy/hub/kustomization.yaml.tmp deploy/hub/kustomization.yaml

deploy-webhook: ensure-kustomize
	cp deploy/webhook/kustomization.yaml deploy/webhook/kustomization.yaml.tmp
	cd deploy/webhook && ../../$(KUSTOMIZE) edit set image quay.io/open-cluster-management/registration:latest=$(IMAGE_NAME)
	$(KUSTOMIZE) build deploy/webhook | $(KUBECTL) apply -f -
	mv deploy/webhook/kustomization.yaml.tmp deploy/webhook/kustomization.yaml

cluster-ip: 
  CLUSTER_IP?=$(shell $(KUBECTL) get svc kubernetes -n default -o jsonpath="{.spec.clusterIP}")

bootstrap-secret: cluster-ip
	cp $(KUBECONFIG) dev-kubeconfig
	$(KUBECTL) config set clusters.kind-kind.server https://$(CLUSTER_IP) --kubeconfig dev-kubeconfig
	$(KUBECTL) create secret generic bootstrap-secret --from-file=kubeconfig=dev-kubeconfig -n open-cluster-management

e2e-bootstrap-secret: cluster-ip
	cp $(KUBECONFIG) e2e-kubeconfig
	$(KUBECTL) config set clusters.kind-kind.server https://$(CLUSTER_IP) --kubeconfig e2e-kubeconfig
	$(KUBECTL) delete secret e2e-bootstrap-secret -n open-cluster-management --ignore-not-found
	$(KUBECTL) create secret generic e2e-bootstrap-secret --from-file=kubeconfig=e2e-kubeconfig -n open-cluster-management

deploy-spoke: ensure-kustomize
	cp deploy/spoke/kustomization.yaml deploy/spoke/kustomization.yaml.tmp
	cd deploy/spoke && ../../$(KUSTOMIZE) edit set image quay.io/open-cluster-management/registration:latest=$(IMAGE_NAME)
	$(KUSTOMIZE) build deploy/spoke | $(KUBECTL) apply -f -
	mv deploy/spoke/kustomization.yaml.tmp deploy/spoke/kustomization.yaml

deploy-all: deploy-hub deploy-webhook bootstrap-secret deploy-spoke

build: build-e2e

build-e2e:
	go test -c ./test/e2e -mod=vendor

test-e2e: build-e2e ensure-kustomize deploy-hub deploy-webhook e2e-bootstrap-secret
	./e2e.test -test.v -ginkgo.v

clean-e2e:
	$(RM) ./e2e.test

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

GO_TEST_PACKAGES :=./pkg/... ./cmd/...

include ./test/integration-test.mk
