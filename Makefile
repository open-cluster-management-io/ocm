
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
CSV_VERSION?=0.4.0

# WORK_IMAGE can be set in the env to override calculated value
WORK_TAG?=latest
WORK_IMAGE?=$(IMAGE_REGISTRY)/work:$(WORK_TAG)

# REGISTRATION_IMAGE can be set in the env to override calculated value
REGISTRATION_TAG?=latest
REGISTRATION_IMAGE?=$(IMAGE_REGISTRY)/registration:$(REGISTRATION_TAG)

OPERATOR_SDK?=$(PERMANENT_TMP_GOPATH)/bin/operator-sdk
OPERATOR_SDK_VERSION?=v1.1.0
operatorsdk_gen_dir:=$(dir $(OPERATOR_SDK))
# On openshift, OLM is installed into openshift-operator-lifecycle-manager
OLM_NAMESPACE?=olm
OLM_VERSION?=0.16.1

KUBECTL?=kubectl
KUBECONFIG?=./.kubeconfig
KLUSTERLET_KUBECONFIG_CONTEXT?=$(shell $(KUBECTL) config current-context)
KLUSTERLET_KIND_KUBECONFIG?=$(HOME)/cluster1-kubeconfig
HUB_KIND_KUBECONFIG?=$(HOME)/hub-kubeconfig
MANAGED_CLUSTER?=cluster1
HUB_CLUSTER?=hub

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

munge-hub-csv:
	mkdir -p munge-csv
	cp deploy/cluster-manager/olm-catalog/cluster-manager/manifests/cluster-manager.clusterserviceversion.yaml munge-csv/cluster-manager.clusterserviceversion.yaml.unmunged
	$(SED_CMD) -e "s,quay.io/open-cluster-management/registration-operator:latest,$(IMAGE_NAME)," -i deploy/cluster-manager/olm-catalog/cluster-manager/manifests/cluster-manager.clusterserviceversion.yaml

munge-spoke-csv:
	mkdir -p munge-csv
	cp deploy/klusterlet/olm-catalog/klusterlet/manifests/klusterlet.clusterserviceversion.yaml munge-csv/klusterlet.clusterserviceversion.yaml.unmunged
	$(SED_CMD) -e "s,quay.io/open-cluster-management/registration-operator:latest,$(IMAGE_NAME)," -i deploy/klusterlet/olm-catalog/klusterlet/manifests/klusterlet.clusterserviceversion.yaml

unmunge-csv:
	mv munge-csv/cluster-manager.clusterserviceversion.yaml.unmunged deploy/cluster-manager/olm-catalog/cluster-manager/manifests/cluster-manager.clusterserviceversion.yaml
	mv munge-csv/klusterlet.clusterserviceversion.yaml.unmunged deploy/klusterlet/olm-catalog/klusterlet/manifests/klusterlet.clusterserviceversion.yaml

deploy: install-olm deploy-hub deploy-spoke unmunge-csv

clean-deploy: clean-spoke clean-hub

install-olm: ensure-operator-sdk
	$(KUBECTL) get crds | grep clusterserviceversion ; if [ $$? -ne 0 ] ; then $(OPERATOR_SDK) olm install --version $(OLM_VERSION); fi
	$(KUBECTL) get ns open-cluster-management ; if [ $$? -ne 0 ] ; then $(KUBECTL) create ns open-cluster-management ; fi

install-olm-kind: ensure-operator-sdk
	$(KUBECTL) config use-context kind-$(MANAGED_CLUSTER)
	$(KUBECTL) get crds | grep clusterserviceversion ; if [ $$? -ne 0 ] ; then $(OPERATOR_SDK) olm install --version $(OLM_VERSION); fi
	$(KUBECTL) get ns open-cluster-management ; if [ $$? -ne 0 ] ; then $(KUBECTL) create ns open-cluster-management ; fi

deploy-hub: deploy-hub-operator apply-hub-cr

deploy-hub-operator: install-olm munge-hub-csv
	$(OPERATOR_SDK) run packagemanifests deploy/cluster-manager/olm-catalog/cluster-manager/ --namespace open-cluster-management --version $(CSV_VERSION) --install-mode OwnNamespace --timeout=10m

apply-hub-cr:
	$(SED_CMD) -e "s,quay.io/open-cluster-management/registration,$(REGISTRATION_IMAGE)," deploy/cluster-manager/config/samples/operator_open-cluster-management_clustermanagers.cr.yaml | $(KUBECTL) apply -f -

clean-hub: ensure-operator-sdk
	$(KUBECTL) delete -f deploy/cluster-manager/config/samples/operator_open-cluster-management_clustermanagers.cr.yaml --ignore-not-found
	$(OPERATOR_SDK) cleanup cluster-manager --namespace open-cluster-management --timeout 10m

cluster-ip:
  CLUSTER_IP?=$(shell $(KUBECTL) get svc kubernetes -n default -o jsonpath="{.spec.clusterIP}")

cluster-hub-ip-kind:
  	CLUSTER_IP_KIND?=$(shell $(KUBECTL) --kubeconfig $(HOME)/hub-kubeconfig config view | grep server | awk '{ print $2 }' | cut -f3 -d/)

bootstrap-secret: cluster-ip
	cp $(KUBECONFIG) dev-kubeconfig
	$(KUBECTL) config use-context $(KLUSTERLET_KUBECONFIG_CONTEXT)
	$(KUBECTL) get ns open-cluster-management-agent; if [ $$? -ne 0 ] ; then $(KUBECTL) create ns open-cluster-management-agent; fi
	$(KUBECTL) config set clusters.kind-$(MANAGED_CLUSTER).server https://$(CLUSTER_IP) --kubeconfig dev-kubeconfig
	$(KUBECTL) delete secret bootstrap-hub-kubeconfig -n open-cluster-management-agent --ignore-not-found
	$(KUBECTL) create secret generic bootstrap-hub-kubeconfig --from-file=kubeconfig=dev-kubeconfig -n open-cluster-management-agent

bootstrap-secret-kind: cluster-hub-ip-kind
	cp $(HUB_KIND_KUBECONFIG) dev-kubeconfig
	$(KUBECTL) config use-context kind-$(MANAGED_CLUSTER)
	$(KUBECTL) get ns open-cluster-management-agent; if [ $$? -ne 0 ] ; then $(KUBECTL) create ns open-cluster-management-agent; fi
	$(KUBECTL) --kubeconfig dev-kubeconfig config set clusters.kind-$(HUB_CLUSTER).server https://$(CLUSTER_IP_KIND)
	$(KUBECTL) delete secret bootstrap-hub-kubeconfig -n open-cluster-management-agent --ignore-not-found
	$(KUBECTL) create secret generic bootstrap-hub-kubeconfig --from-file=kubeconfig=dev-kubeconfig -n open-cluster-management-agent

# Registration e2e expects to read bootstrap secret from open-cluster-management/e2e-bootstrap-secret
# TODO: think about how to factor this
e2e-bootstrap-secret: cluster-ip
	cp $(KUBECONFIG) e2e-kubeconfig
	$(KUBECTL) config set clusters.kind-kind.server https://$(CLUSTER_IP) --kubeconfig e2e-kubeconfig
	$(KUBECTL) delete secret e2e-bootstrap-secret -n open-cluster-management --ignore-not-found
	$(KUBECTL) create secret generic e2e-bootstrap-secret --from-file=kubeconfig=e2e-kubeconfig -n open-cluster-management

deploy-spoke: deploy-spoke-operator apply-spoke-cr

deploy-spoke-operator: install-olm munge-spoke-csv bootstrap-secret
	$(OPERATOR_SDK) run packagemanifests deploy/klusterlet/olm-catalog/klusterlet/ --namespace open-cluster-management --version $(CSV_VERSION) --install-mode OwnNamespace --timeout=10m

deploy-spoke-kind: deploy-spoke-operator-kind apply-spoke-cr-kind

deploy-spoke-operator-kind: install-olm-kind munge-spoke-csv bootstrap-secret-kind
	$(OPERATOR_SDK) run packagemanifests deploy/klusterlet/olm-catalog/klusterlet/ --namespace open-cluster-management --version $(CSV_VERSION) --install-mode OwnNamespace --timeout=10m

apply-spoke-cr:
	$(SED_CMD) -e "s,quay.io/open-cluster-management/registration,$(REGISTRATION_IMAGE)," -e "s,quay.io/open-cluster-management/work,$(WORK_IMAGE)," deploy/klusterlet/config/samples/operator_open-cluster-management_klusterlets.cr.yaml | $(KUBECTL) apply -f -

apply-spoke-cr-kind:
	$(KUBECTL) config use-context kind-$(MANAGED_CLUSTER)
	$(KUBECTL) apply -f deploy/klusterlet/config/samples/operator_open-cluster-management_klusterlets.cr.yaml

clean-spoke: ensure-operator-sdk
	$(KUBECTL) delete -f deploy/klusterlet/config/samples/operator_open-cluster-management_klusterlets.cr.yaml --ignore-not-found
	$(OPERATOR_SDK) cleanup klusterlet --namespace open-cluster-management --timeout 10m

test-e2e: deploy-hub deploy-spoke-operator run-e2e

run-e2e:
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
