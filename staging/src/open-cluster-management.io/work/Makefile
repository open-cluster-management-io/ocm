SHELL :=/bin/bash

all: build
.PHONY: all

# Include the library makefile
include $(addprefix ./vendor/github.com/openshift/build-machinery-go/make/, \
	golang.mk \
	targets/openshift/deps.mk \
	targets/openshift/images.mk \
	targets/openshift/kustomize.mk \
	lib/tmp.mk \
)

KUBECTL?=kubectl
IMAGE_REGISTRY?=quay.io/open-cluster-management
IMAGE_TAG?=latest
IMAGE_NAME?=$(IMAGE_REGISTRY)/work:$(IMAGE_TAG)
KUBECONFIG?=./.kubeconfig
HUB_KUBECONFIG?=$(KUBECONFIG)
HUB_KUBECONFIG_CONTEXT?=$(shell $(KUBECTL) --kubeconfig $(HUB_KUBECONFIG) config current-context)
SPOKE_KUBECONFIG?=$(KUBECONFIG)
SPOKE_KUBECONFIG_CONTEXT?=$(shell $(KUBECTL) --kubeconfig $(SPOKE_KUBECONFIG) config current-context)
PWD=$(shell pwd)

# This will call a macro called "build-image" which will generate image specific targets based on the parameters:
# $0 - macro name
# $1 - target suffix
# $2 - Dockerfile path
# $3 - context directory for image build
# It will generate target "image-$(1)" for builing the image an binding it as a prerequisite to target "images".
$(call build-image,work,$(IMAGE_REGISTRY)/work:$(IMAGE_TAG),./Dockerfile,.)

verify:
	go install github.com/golangci/golangci-lint/cmd/golangci-lint@v1.45.2
	golangci-lint run --timeout=3m --modules-download-mode vendor ./...

clean:
	$(RM) ./work
.PHONY: clean

cluster-ip:
	$(KUBECTL) config use-context $(HUB_KUBECONFIG_CONTEXT) --kubeconfig $(HUB_KUBECONFIG)
	$(eval CLUSTER_IP?=$(shell $(KUBECTL) --kubeconfig $(HUB_KUBECONFIG) get svc kubernetes -n default -o jsonpath="{.spec.clusterIP}"))

hub-kubeconfig-secret: cluster-ip
	$(KUBECTL) config use-context $(SPOKE_KUBECONFIG_CONTEXT) --kubeconfig $(SPOKE_KUBECONFIG)
	$(KUBECTL) apply -f deploy/spoke/component_namespace.yaml --kubeconfig $(SPOKE_KUBECONFIG)
	$(KUBECTL) delete secret hub-kubeconfig-secret -n open-cluster-management-agent --ignore-not-found --kubeconfig $(SPOKE_KUBECONFIG)
	$(KUBECTL) config use-context $(HUB_KUBECONFIG_CONTEXT) --kubeconfig $(HUB_KUBECONFIG)
	$(KUBECTL) config view --flatten --minify --kubeconfig $(HUB_KUBECONFIG) > hub-kubeconfig
ifeq ($(HUB_KUBECONFIG), $(SPOKE_KUBECONFIG))
ifeq ($(HUB_KUBECONFIG_CONTEXT), $(SPOKE_KUBECONFIG_CONTEXT))
	$(KUBECTL) config set clusters.$(HUB_KUBECONFIG_CONTEXT).server https://$(CLUSTER_IP) --kubeconfig hub-kubeconfig
endif
endif
	$(KUBECTL) config use-context $(SPOKE_KUBECONFIG_CONTEXT) --kubeconfig $(SPOKE_KUBECONFIG)
	$(KUBECTL) create secret generic hub-kubeconfig-secret --from-file=kubeconfig=hub-kubeconfig -n open-cluster-management-agent --kubeconfig $(SPOKE_KUBECONFIG)
	$(RM) ./hub-kubeconfig

e2e-hub-kubeconfig-secret: cluster-ip
	cp $(HUB_KUBECONFIG) e2e-hub-kubeconfig
	$(KUBECTL) apply -f deploy/spoke/component_namespace.yaml --kubeconfig $(SPOKE_KUBECONFIG)
	$(KUBECTL) config set clusters.$(HUB_KUBECONFIG_CONTEXT).server https://$(CLUSTER_IP) --kubeconfig e2e-hub-kubeconfig
	$(KUBECTL) delete secret e2e-hub-kubeconfig-secret -n open-cluster-management-agent --ignore-not-found --kubeconfig $(SPOKE_KUBECONFIG)
	$(KUBECTL) create secret generic e2e-hub-kubeconfig-secret --from-file=kubeconfig=e2e-hub-kubeconfig -n open-cluster-management-agent --kubeconfig $(SPOKE_KUBECONFIG)
	$(RM) ./e2e-hub-kubeconfig

create-cluster-ns:
	$(KUSTOMIZE) build deploy/cluster_namespae | $(KUBECTL) --kubeconfig $(HUB_KUBECONFIG) apply -f -

deploy-work-agent: ensure-kustomize create-cluster-ns hub-kubeconfig-secret
	cp deploy/spoke/kustomization.yaml deploy/spoke/kustomization.yaml.tmp
	cd deploy/spoke && ../../$(KUSTOMIZE) edit set image quay.io/open-cluster-management/work:latest=$(IMAGE_NAME)
	$(KUBECTL) config use-context $(SPOKE_KUBECONFIG_CONTEXT) --kubeconfig $(SPOKE_KUBECONFIG)
	$(KUSTOMIZE) build deploy/spoke | $(KUBECTL) --kubeconfig $(SPOKE_KUBECONFIG) apply -f -
	mv deploy/spoke/kustomization.yaml.tmp deploy/spoke/kustomization.yaml
	$(KUBECTL) --kubeconfig $(SPOKE_KUBECONFIG) apply -f deploy/spoke/role_extension-apiserver.yaml 
	$(KUBECTL) --kubeconfig $(SPOKE_KUBECONFIG) apply -f deploy/spoke/role_binding_extension-apiserver.yaml

deploy-hub: ensure-kustomize
	cp deploy/hub/kustomization.yaml deploy/hub/kustomization.yaml.tmp
	cp deploy/hub/webhook.yaml deploy/hub/webhook.yaml.tmp
	bash -x hack/inject-ca.sh
	cd deploy/hub && ../../$(KUSTOMIZE) edit set image quay.io/open-cluster-management/work:latest=$(IMAGE_NAME)
	$(KUBECTL) config use-context $(HUB_KUBECONFIG_CONTEXT) --kubeconfig $(HUB_KUBECONFIG)
	$(KUSTOMIZE) build deploy/hub | $(KUBECTL) --kubeconfig $(HUB_KUBECONFIG) apply -f -
	mv deploy/hub/kustomization.yaml.tmp deploy/hub/kustomization.yaml
	mv deploy/hub/webhook.yaml.tmp deploy/hub/webhook.yaml

clean-work-agent:
	$(KUBECTL) config use-context $(SPOKE_KUBECONFIG_CONTEXT) --kubeconfig $(SPOKE_KUBECONFIG)
	$(KUSTOMIZE) build deploy/spoke | $(KUBECTL) --kubeconfig $(SPOKE_KUBECONFIG) delete --ignore-not-found -f -

clean-hub:
	$(KUBECTL) config use-context $(HUB_KUBECONFIG_CONTEXT) --kubeconfig $(HUB_KUBECONFIG)
	$(KUSTOMIZE) build deploy/hub | $(KUBECTL) --kubeconfig $(SPOKE_KUBECONFIG) delete --ignore-not-found -f -

remove-cluster-ns:
	$(KUBECTL) config use-context $(HUB_KUBECONFIG_CONTEXT) --kubeconfig $(HUB_KUBECONFIG)
	$(KUSTOMIZE) build deploy/cluster_namespae | $(KUBECTL) --kubeconfig $(HUB_KUBECONFIG) delete --ignore-not-found -f -

deploy: deploy-hub deploy-work-agent

undeploy: remove-cluster-ns clean-work-agent clean-hub

build-e2e:
	go test -c ./test/e2e -mod=vendor

test-e2e: build-e2e deploy-hub e2e-hub-kubeconfig-secret
	./e2e.test -test.v -ginkgo.v -deploy-agent=true -nil-executor-validating=true -webhook-deployment-name=work-webhook

clean-e2e:
	$(RM) ./e2e.test

GO_TEST_PACKAGES :=./pkg/... ./cmd/...

include ./test/integration-test.mk
