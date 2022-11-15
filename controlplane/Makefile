BINARYDIR := bin

KUBECTL?=oc
KUSTOMIZE?=kustomize

HUB_NAME?=ocm-controlplane

IMAGE_REGISTRY?=quay.io/open-cluster-management
IMAGE_TAG?=latest
IMAGE_NAME?=$(IMAGE_REGISTRY)/controlplane:$(IMAGE_TAG)


all: clean vendor build run
.PHONY: all

run:
	hack/start-ocm-controlplane.sh
.PHONY: run

# the script will automatically start a exteral etcd
run-with-external-etcd:
	hack/start-ocm-controlplane.sh false
.PHONY: run-with-external-etcd

build: 
	$(shell if [ ! -e $(BINARYDIR) ];then mkdir -p $(BINARYDIR); fi)
	go build -o bin/ocm-controlplane cmd/main.go 
.PHONY: build

image:
	docker build -f Dockerfile -t $(IMAGE_NAME) .

clean:
	rm -rf bin .ocmconfig
.PHONY: clean

vendor: 
	go mod tidy 
	go mod vendor
.PHONY: vendor

update:
	bash -x hack/crd-update/copy-crds.sh
.PHONY: update

deploy:
	$(KUBECTL) get ns $(HUB_NAME); if [ $$? -ne 0 ] ; then $(KUBECTL) create ns $(HUB_NAME); fi
	hack/deploy-ocm-controlplane.sh

destroy:
	$(KUSTOMIZE) build hack/deploy/controlplane | $(KUBECTL) delete --namespace $(HUB_NAME) --ignore-not-found -f -
	$(KUBECTL) delete ns $(HUB_NAME) --ignore-not-found
	rm -r hack/deploy/cert-$(HUB_NAME)

deploy-work-manager-addon:
	$(KUBECTL) apply -k hack/deploy/addon/work-manager/hub --kubeconfig=hack/deploy/cert-$(HUB_NAME)/kubeconfig
	cp hack/deploy/addon/work-manager/manager/kustomization.yaml hack/deploy/addon/work-manager/manager/kustomization.yaml.tmp
	cd hack/deploy/addon/work-manager/manager && $(KUSTOMIZE) edit set namespace $(HUB_NAME)
	$(KUSTOMIZE) build hack/deploy/addon/work-manager/manager | $(KUBECTL) apply -f -
	mv hack/deploy/addon/work-manager/manager/kustomization.yaml.tmp hack/deploy/addon/work-manager/manager/kustomization.yaml

deploy-managed-serviceaccount-addon:
	$(KUBECTL) apply -k hack/deploy/addon/managed-serviceaccount/hub --kubeconfig=hack/deploy/cert-$(HUB_NAME)/kubeconfig
	cp hack/deploy/addon/managed-serviceaccount/manager/kustomization.yaml hack/deploy/addon/managed-serviceaccount/manager/kustomization.yaml.tmp
	cd hack/deploy/addon/managed-serviceaccount/manager && $(KUSTOMIZE) edit set namespace $(HUB_NAME)
	$(KUSTOMIZE) build hack/deploy/addon/managed-serviceaccount/manager | $(KUBECTL) apply -f -
	mv hack/deploy/addon/managed-serviceaccount/manager/kustomization.yaml.tmp hack/deploy/addon/managed-serviceaccount/manager/kustomization.yaml

deploy-all: deploy deploy-work-manager-addon deploy-managed-serviceaccount-addon
