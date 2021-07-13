SHELL :=/bin/bash

all: build
.PHONY: all

# Include the library makefile
include $(addprefix ./vendor/github.com/openshift/build-machinery-go/make/, \
	golang.mk \
	targets/openshift/deps.mk \
	targets/openshift/images.mk \
	targets/openshift/bindata.mk \
	lib/tmp.mk\
)

# IMAGE_NAME can be set in the env to override calculated value for registration-operator image
IMAGE_REGISTRY?=quay.io/open-cluster-management
IMAGE_TAG?=latest
IMAGE_NAME?=$(IMAGE_REGISTRY)/registration-operator:$(IMAGE_TAG)

# CSV_VERSION is used to generate new CSV manifests
CSV_VERSION?=0.5.0

# WORK_IMAGE can be set in the env to override calculated value
WORK_TAG?=latest
WORK_IMAGE?=$(IMAGE_REGISTRY)/work:$(WORK_TAG)

# REGISTRATION_IMAGE can be set in the env to override calculated value
REGISTRATION_TAG?=latest
REGISTRATION_IMAGE?=$(IMAGE_REGISTRY)/registration:$(REGISTRATION_TAG)

# PLACEMENT_IMAGE can be set in the env to override calculated value
PLACEMENT_TAG?=latest
PLACEMENT_IMAGE?=$(IMAGE_REGISTRY)/placement:$(PLACEMENT_TAG)

OPERATOR_SDK?=$(PERMANENT_TMP_GOPATH)/bin/operator-sdk
OPERATOR_SDK_VERSION?=v1.1.0
operatorsdk_gen_dir:=$(dir $(OPERATOR_SDK))
# On openshift, OLM is installed into openshift-operator-lifecycle-manager
OLM_NAMESPACE?=olm
OLM_VERSION?=0.16.1

KUSTOMIZE?=$(PERMANENT_TMP_GOPATH)/bin/kustomize
KUSTOMIZE_VERSION?=v3.5.4
KUSTOMIZE_ARCHIVE_NAME?=kustomize_$(KUSTOMIZE_VERSION)_$(GOHOSTOS)_$(GOHOSTARCH).tar.gz
kustomize_dir:=$(dir $(KUSTOMIZE))

KUBECTL?=kubectl
KUBECONFIG?=./.kubeconfig
HUB_KUBECONFIG=./.hub-kubeconfig

OPERATOR_SDK_ARCHOS:=x86_64-linux-gnu
ifeq ($(GOHOSTOS),darwin)
	ifeq ($(GOHOSTARCH),amd64)
		OPERATOR_SDK_ARCHOS:=x86_64-apple-darwin
	endif
endif

SED_CMD:=sed
ifeq ($(GOHOSTOS),darwin)
	ifeq ($(GOHOSTARCH),amd64)
		SED_CMD:=gsed
	endif
endif

$(call add-bindata,cluster-manager,./manifests/cluster-manager/...,bindata,bindata,./pkg/operators/clustermanager/bindata/bindata.go)
$(call add-bindata,klusterlet,./manifests/klusterlet/...,bindata,bindata,./pkg/operators/klusterlet/bindata/bindata.go)
$(call add-bindata,klusterletkube111,./manifests/klusterletkube111/...,kube111bindata,kube111bindata,./pkg/operators/klusterlet/kube111bindata/bindata.go)

copy-crd:
	bash -x hack/copy-crds.sh

update-all: copy-crd update-bindata-cluster-manager update-bindata-klusterlet update-bindata-klusterletkube111 update-csv

verify-crds:
	bash -x hack/verify-crds.sh

verify: verify-crds

update-csv: ensure-operator-sdk
	cd deploy/cluster-manager && ../../$(OPERATOR_SDK) generate bundle --manifests --deploy-dir config/ --crds-dir config/crds/ --output-dir olm-catalog/cluster-manager/ --version $(CSV_VERSION)
	cd deploy/klusterlet && ../../$(OPERATOR_SDK) generate bundle --manifests --deploy-dir config/ --crds-dir config/crds/ --output-dir olm-catalog/klusterlet/ --version=$(CSV_VERSION)

	# delete useless serviceaccounts in manifests although they are copied from config by operator-sdk.
	rm ./deploy/cluster-manager/olm-catalog/cluster-manager/manifests/cluster-manager_v1_serviceaccount.yaml
	rm ./deploy/klusterlet/olm-catalog/klusterlet/manifests/klusterlet_v1_serviceaccount.yaml

deploy: deploy-hub cluster-ip deploy-spoke

hub-kubeconfig:
	$(KUBECTL) config view --minify --flatten > $(HUB_KUBECONFIG)

clean-deploy: clean-spoke-cr clean-hub-cr clean-spoke-operator clean-hub-operator

deploy-hub: deploy-hub-operator apply-hub-cr hub-kubeconfig

deploy-spoke: deploy-spoke-operator apply-spoke-cr

deploy-hub-operator: ensure-kustomize
	$(KUSTOMIZE) build deploy/cluster-manager/config | $(KUBECTL) apply -f -

apply-hub-cr:
	$(SED_CMD) -e "s,quay.io/open-cluster-management/registration,$(REGISTRATION_IMAGE)," -e "s,quay.io/open-cluster-management/work,$(WORK_IMAGE)," -e "s,quay.io/open-cluster-management/placement,$(PLACEMENT_IMAGE)," deploy/cluster-manager/config/samples/operator_open-cluster-management_clustermanagers.cr.yaml | $(KUBECTL) apply -f -

clean-hub: clean-hub-cr clean-hub-operator

clean-spoke: clean-spoke-cr clean-spoke-operator

cluster-ip:
	$(eval HUB_CONTEXT := $(shell $(KUBECTL) config current-context --kubeconfig $(HUB_KUBECONFIG)))
	$(eval HUB_CLUSTER_IP := $(shell $(KUBECTL) get svc kubernetes -n default -o jsonpath="{.spec.clusterIP}" --kubeconfig $(HUB_KUBECONFIG)))
	$(KUBECTL) config set clusters.$(HUB_CONTEXT).server https://$(HUB_CLUSTER_IP) --kubeconfig $(HUB_KUBECONFIG)

bootstrap-secret:
	cp $(HUB_KUBECONFIG) deploy/klusterlet/config/samples/bootstrap/hub-kubeconfig
	$(KUBECTL) get ns open-cluster-management-agent; if [ $$? -ne 0 ] ; then $(KUBECTL) create ns open-cluster-management-agent; fi
	$(KUSTOMIZE) build deploy/klusterlet/config/samples/bootstrap | $(KUBECTL) apply -f -

deploy-spoke-operator: ensure-kustomize
	$(KUSTOMIZE) build deploy/klusterlet/config | $(KUBECTL) apply -f -

apply-spoke-cr: bootstrap-secret
	$(KUSTOMIZE) build deploy/klusterlet/config/samples | $(SED_CMD) -e "s,quay.io/open-cluster-management/registration,$(REGISTRATION_IMAGE)," -e "s,quay.io/open-cluster-management/work,$(WORK_IMAGE)," | $(KUBECTL) apply -f -

clean-hub-cr:
	$(KUBECTL) delete managedcluster --all --ignore-not-found
	$(KUSTOMIZE) build deploy/cluster-manager/config/samples | $(KUBECTL) delete --ignore-not-found -f -

clean-hub-operator:
	$(KUSTOMIZE) build deploy/cluster-manager/config | $(KUBECTL) delete --ignore-not-found -f -

clean-spoke-cr:
	$(KUSTOMIZE) build deploy/klusterlet/config/samples | $(KUBECTL) delete --ignore-not-found -f -
	$(KUSTOMIZE) build deploy/klusterlet/config/samples/bootstrap | $(KUBECTL) delete --ignore-not-found -f -

clean-spoke-operator:
	$(KUSTOMIZE) build deploy/klusterlet/config | $(KUBECTL) delete --ignore-not-found -f -
	$(KUBECTL) delete ns open-cluster-management-agent --ignore-not-found

# Registration e2e expects to read bootstrap secret from open-cluster-management/e2e-bootstrap-secret
# TODO: think about how to factor this
e2e-bootstrap-secret: cluster-ip
	$(KUBECTL) delete secret e2e-bootstrap-secret -n open-cluster-management --ignore-not-found
	$(KUBECTL) create secret generic e2e-bootstrap-secret --from-file=kubeconfig=$(HUB_KUBECONFIG) -n open-cluster-management

install-olm: ensure-operator-sdk
	$(KUBECTL) get crds | grep clusterserviceversion ; if [ $$? -ne 0 ] ; then $(OPERATOR_SDK) olm install --version $(OLM_VERSION); fi
	$(KUBECTL) get ns open-cluster-management ; if [ $$? -ne 0 ] ; then $(KUBECTL) create ns open-cluster-management ; fi

deploy-hub-operator-olm: install-olm
	$(OPERATOR_SDK) run packagemanifests deploy/cluster-manager/olm-catalog/cluster-manager/ --namespace open-cluster-management --version $(CSV_VERSION) --install-mode OwnNamespace --timeout=10m

clean-hub-olm: ensure-operator-sdk
	$(KUBECTL) delete -f deploy/cluster-manager/config/samples/operator_open-cluster-management_clustermanagers.cr.yaml --ignore-not-found
	$(OPERATOR_SDK) cleanup cluster-manager --namespace open-cluster-management --timeout 10m

deploy-spoke-operator-olm: install-olm bootstrap-secret
	$(OPERATOR_SDK) run packagemanifests deploy/klusterlet/olm-catalog/klusterlet/ --namespace open-cluster-management --version $(CSV_VERSION) --install-mode OwnNamespace --timeout=10m

clean-spoke-olm: ensure-operator-sdk
	$(KUBECTL) delete -f deploy/klusterlet/config/samples/operator_open-cluster-management_klusterlets.cr.yaml --ignore-not-found
	$(OPERATOR_SDK) cleanup klusterlet --namespace open-cluster-management --timeout 10m

test-e2e: deploy-hub deploy-spoke-operator run-e2e

run-e2e: cluster-ip bootstrap-secret
	go test -c ./test/e2e
	./e2e.test -test.v -ginkgo.v

clean-e2e:
	$(RM) ./e2e.test

ensure-operator-sdk:
ifeq "" "$(wildcard $(OPERATOR_SDK))"
	$(info Installing operator-sdk into '$(OPERATOR_SDK)')
	mkdir -p '$(operatorsdk_gen_dir)'
	curl -s -f -L https://github.com/operator-framework/operator-sdk/releases/download/$(OPERATOR_SDK_VERSION)/operator-sdk-$(OPERATOR_SDK_VERSION)-$(OPERATOR_SDK_ARCHOS) -o '$(OPERATOR_SDK)'
	chmod +x '$(OPERATOR_SDK)';
else
	$(info Using existing operator-sdk from "$(OPERATOR_SDK)")
endif

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

# This will call a macro called "build-image" which will generate image specific targets based on the parameters:
# $0 - macro name
# $1 - target suffix
# $2 - Dockerfile path
# $3 - context directory for image build
# It will generate target "image-$(1)" for building the image an binding it as a prerequisite to target "images".
$(call build-image,registration-operator,$(IMAGE_REGISTRY)/registration-operator,./Dockerfile,.)

clean:
	$(RM) ./registration-operator
.PHONY: clean

GO_TEST_PACKAGES :=./pkg/... ./cmd/...

include ./test/integration-test.mk
