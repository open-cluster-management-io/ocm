
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
KUBECONFIG ?= ./.kubeconfig

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

deploy-hub:
	kustomize build deploy/hub | kubectl apply -f -

cluster-ip: 
  CLUSTER_IP?=$(shell kubectl get svc kubernetes -n default -o jsonpath="{.spec.clusterIP}")

bootstrap-secret: cluster-ip
	cp $(KUBECONFIG) dev-kubeconfig
	kubectl config set clusters.kind-kind.server https://$(CLUSTER_IP) --kubeconfig dev-kubeconfig
	kubectl create secret generic bootstrap-secret --from-file=kubeconfig=dev-kubeconfig -n open-cluster-management

deploy-spoke:
	kustomize build deploy/spoke | kubectl apply -f -

# test-e2e target is currently a NOP that deploys the hub and self-joins the
# hosting cluster to itself as a spoke; it will be used to prototype e2e in ci.
test-e2e: deploy-hub bootstrap-secret deploy-spoke

GO_TEST_PACKAGES :=./pkg/... ./cmd/...

include ./test/integration-test.mk
