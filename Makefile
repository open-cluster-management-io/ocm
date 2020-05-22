
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

IMAGE_REGISTRY?=quay.io/open-cluster-management
OPERATOR_SDK?=$(PERMANENT_TMP_GOPATH)/bin/operator-sdk
OPERATOR_SDK_VERSION?=v0.17.0
operatorsdk_gen_dir:=$(dir $(OPERATOR_SDK))
KUBECTL?=kubectl
KUBECONFIG ?= ./.kubeconfig
OLM_NAMESPACE?=olm

OPERATOR_SDK_ARCHOS:=x86_64-linux-gnu
ifeq ($(GOHOSTOS),darwin)
	ifeq ($(GOHOSTARCH),amd64)
		OPERATOR_SDK_ARCHOS:=x86_64-apple-darwin
	endif
endif

$(call add-bindata,hub,./manifests/hub/...,bindata,bindata,./pkg/operators/hub/bindata/bindata.go)
$(call add-bindata,spoke,./manifests/spoke/...,bindata,bindata,./pkg/operators/spoke/bindata/bindata.go)

copy-crd:
	bash -x hack/copy-crds.sh

update-all: copy-crd update-bindata-hub update-bindata-spoke update-csv

verify-crds:
	bash -x hack/verify-crds.sh

verify: verify-crds

update-csv: ensure-operator-sdk
	$(OPERATOR_SDK) generate csv --crd-dir=deploy/nucleus-hub/crds --deploy-dir=deploy/nucleus-hub --output-dir=deploy/nucleus-hub/olm-catalog/nucleus-hub --operator-name=nucleus-hub --csv-version=0.1.0
	$(OPERATOR_SDK) generate csv --crd-dir=deploy/nucleus-spoke/crds --deploy-dir=deploy/nucleus-spoke --output-dir=deploy/nucleus-spoke/olm-catalog/nucleus-spoke --operator-name=nucleus-spoke --csv-version=0.1.0

deploy: install-olm deploy-hub deploy-spoke

clean-deploy: clean-spoke clean-hub

install-olm: ensure-operator-sdk
	$(KUBECTL) get crds | grep clusterserviceversion ; if [ $$? -ne 0 ] ; then $(OPERATOR_SDK) olm install --version 0.14.1 --olm-namespace $(OLM_NAMESPACE); fi
	$(KUBECTL) get ns open-cluster-management ; if [ $$? -ne 0 ] ; then $(KUBECTL) create ns open-cluster-management ; fi

deploy-hub: install-olm
	$(OPERATOR_SDK) run --olm --operator-namespace open-cluster-management --operator-version 0.1.0 --manifests deploy/nucleus-hub/olm-catalog/nucleus-hub --olm-namespace $(OLM_NAMESPACE)
	$(KUBECTL) apply -f deploy/nucleus-hub/crds/nucleus_open-clustere-management_hubcores.cr.yaml

clean-hub: ensure-operator-sdk
	$(KUBECTL) delete -f deploy/nucleus-hub/crds/nucleus_open-clustere-management_hubcores.cr.yaml
	$(OPERATOR_SDK) cleanup --olm --operator-namespace open-cluster-management --operator-version 0.1.0 --manifests deploy/nucleus-hub/olm-catalog/nucleus-hub --olm-namespace $(OLM_NAMESPACE)

cluster-ip: 
  CLUSTER_IP?=$(shell $(KUBECTL) get svc kubernetes -n default -o jsonpath="{.spec.clusterIP}")

bootstrap-secret: cluster-ip
	$(KUBECTL) get ns open-cluster-management-spoke ; if [ $$? -ne 0 ] ; then $(KUBECTL) create ns open-cluster-management-spoke ; fi
	cp $(KUBECONFIG) dev-kubeconfig
	$(KUBECTL) config set clusters.kind-kind.server https://$(CLUSTER_IP) --kubeconfig dev-kubeconfig
	$(KUBECTL) delete secret bootstrap-hub-kubeconfig -n open-cluster-management-spoke --ignore-not-found
	$(KUBECTL) create secret generic bootstrap-hub-kubeconfig --from-file=kubeconfig=dev-kubeconfig -n open-cluster-management-spoke

deploy-spoke: install-olm bootstrap-secret
	$(OPERATOR_SDK) run --olm --operator-namespace open-cluster-management --operator-version 0.1.0 --manifests deploy/nucleus-spoke/olm-catalog/nucleus-spoke --olm-namespace $(OLM_NAMESPACE)
	$(KUBECTL) apply -f deploy/nucleus-spoke/crds/nucleus_open-clustere-management_spokecores.cr.yaml

clean-spoke: ensure-operator-sdk
	$(KUBECTL) delete -f deploy/nucleus-spoke/crds/nucleus_open-clustere-management_spokecores.cr.yaml
	$(OPERATOR_SDK) cleanup --olm --operator-namespace open-cluster-management --operator-version 0.1.0 --manifests deploy/nucleus-spoke/olm-catalog/nucleus-spoke --olm-namespace $(OLM_NAMESPACE)

ensure-operator-sdk:
ifeq "" "$(wildcard $(OPERATOR_SDK))"
	$(info Installing operator-sdk into '$(OPERATOR_SDK)')
	mkdir -p '$(operatorsdk_gen_dir)'
	curl -s -f -L https://github.com/operator-framework/operator-sdk/releases/download/$(OPERATOR_SDK_VERSION)/operator-sdk-$(OPERATOR_SDK_VERSION)-$(OPERATOR_SDK_ARCHOS) -o '$(OPERATOR_SDK)'
	chmod +x '$(OPERATOR_SDK)';
else
	$(info Using existing operator-sdk from "$(OPERATOR_SDK)")
endif

# This will call a macro called "build-image" which will generate image specific targets based on the parameters:
# $0 - macro name
# $1 - target suffix
# $2 - Dockerfile path
# $3 - context directory for image build
# It will generate target "image-$(1)" for builing the image an binding it as a prerequisite to target "images".
$(call build-image,nucleus,$(IMAGE_REGISTRY)/nucleus,./Dockerfile,.)

clean:
	$(RM) ./nucleus
.PHONY: clean

GO_TEST_PACKAGES :=./pkg/... ./cmd/...
