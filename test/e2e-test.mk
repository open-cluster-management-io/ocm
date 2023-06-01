# Include the library makefile
include $(addprefix ./vendor/github.com/openshift/build-machinery-go/make/, \
	targets/openshift/kustomize.mk \
)

KUBECTL?=kubectl
KUBECONFIG?=./.kubeconfig
HUB_KUBECONFIG?=./.hub-kubeconfig

SED_CMD:=sed
ifeq ($(GOHOSTOS),darwin)
	ifeq ($(GOHOSTARCH),amd64)
		SED_CMD:=gsed
	endif
endif

hub-kubeconfig:
	$(KUBECTL) config view --minify --flatten > $(HUB_KUBECONFIG)

deploy-hub: deploy-hub-operator apply-hub-cr hub-kubeconfig

deploy-hub-operator: ensure-kustomize
	cp deploy/cluster-manager/config/kustomization.yaml deploy/cluster-manager/config/kustomization.yaml.tmp
	cd deploy/cluster-manager/config && ../../../$(KUSTOMIZE) edit set image quay.io/open-cluster-management/registration-operator:latest=$(OPERATOR_IMAGE_NAME)
	$(KUSTOMIZE) build deploy/cluster-manager/config | $(KUBECTL) apply -f -
	mv deploy/cluster-manager/config/kustomization.yaml.tmp deploy/cluster-manager/config/kustomization.yaml

apply-hub-cr:
	$(SED_CMD) -e "s,quay.io/open-cluster-management/registration,$(REGISTRATION_IMAGE)," -e "s,quay.io/open-cluster-management/work,$(WORK_IMAGE)," -e "s,quay.io/open-cluster-management/placement,$(PLACEMENT_IMAGE)," -e "s,quay.io/open-cluster-management/addon-manager,$(ADDON_MANAGER_IMAGE)," deploy/cluster-manager/config/samples/operator_open-cluster-management_clustermanagers.cr.yaml | $(KUBECTL) apply -f -

test-e2e: deploy-hub deploy-spoke-operator run-e2e

run-e2e: cluster-ip bootstrap-secret
	go test -c ./test/e2e
	./e2e.test -test.v -ginkgo.v -deploy-klusterlet=true -nil-executor-validating=true

clean-hub: clean-hub-cr clean-hub-operator

clean-spoke: clean-spoke-cr clean-spoke-operator

clean-e2e:
	$(RM) ./e2e.test

cluster-ip:
	$(eval HUB_CONTEXT := $(shell $(KUBECTL) config current-context --kubeconfig $(HUB_KUBECONFIG)))
	$(eval HUB_CLUSTER_IP := $(shell $(KUBECTL) get svc kubernetes -n default -o jsonpath="{.spec.clusterIP}" --kubeconfig $(HUB_KUBECONFIG)))
	$(KUBECTL) config set clusters.$(HUB_CONTEXT).server https://$(HUB_CLUSTER_IP) --kubeconfig $(HUB_KUBECONFIG)

bootstrap-secret:
	cp $(HUB_KUBECONFIG) deploy/klusterlet/config/samples/bootstrap/hub-kubeconfig
	$(KUBECTL) get ns open-cluster-management-agent; if [ $$? -ne 0 ] ; then $(KUBECTL) create ns open-cluster-management-agent; fi
	$(KUSTOMIZE) build deploy/klusterlet/config/samples/bootstrap | $(KUBECTL) apply -f -

deploy-spoke-operator: ensure-kustomize
	cp deploy/klusterlet/config/kustomization.yaml deploy/klusterlet/config/kustomization.yaml.tmp
	cd deploy/klusterlet/config && ../../../$(KUSTOMIZE) edit set image quay.io/open-cluster-management/registration-operator:latest=$(OPERATOR_IMAGE_NAME)
	$(KUSTOMIZE) build deploy/klusterlet/config | $(KUBECTL) apply -f -
	mv deploy/klusterlet/config/kustomization.yaml.tmp deploy/klusterlet/config/kustomization.yaml

apply-spoke-cr: bootstrap-secret
	$(KUSTOMIZE) build deploy/klusterlet/config/samples \
	| $(SED_CMD) -e "s,quay.io/open-cluster-management/registration,$(REGISTRATION_IMAGE)," -e "s,quay.io/open-cluster-management/work,$(WORK_IMAGE)," -e "s,cluster1,$(MANAGED_CLUSTER_NAME)," \
	| $(KUBECTL) apply -f -

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

# hosted targets are to deploy on hosted mode, it is not used in e2e test yet.

HOSTED_CLUSTER_MANAGER_NAME?=cluster-manager
EXTERNAL_HUB_KUBECONFIG?=./.external-hub-kubeconfig
EXTERNAL_MANAGED_KUBECONFIG?=./.external-managed-kubeconfig

# In hosted mode, hub-kubeconfig used in managedcluster should be the same as the external-hub-kubeconfig
hub-kubeconfig-hosted:
	cat $(EXTERNAL_HUB_KUBECONFIG) > $(HUB_KUBECONFIG)

deploy-hub-hosted: deploy-hub-operator apply-hub-cr-hosted hub-kubeconfig-hosted

deploy-spoke-hosted: deploy-spoke-operator apply-spoke-cr-hosted

apply-hub-cr-hosted: external-hub-secret
	$(SED_CMD) -e "s,quay.io/open-cluster-management/registration,$(REGISTRATION_IMAGE)," -e "s,quay.io/open-cluster-management/work,$(WORK_IMAGE)," -e "s,quay.io/open-cluster-management/placement,$(PLACEMENT_IMAGE)," -e "s,quay.io/open-cluster-management/addon-manager,$(ADDON_MANAGER_IMAGE)," deploy/cluster-manager/config/samples/operator_open-cluster-management_clustermanagers_hosted.cr.yaml | $(KUBECTL) apply -f -

clean-spoke-hosted: clean-spoke-cr-hosted clean-spoke-operator

external-hub-secret:
	cp $(EXTERNAL_HUB_KUBECONFIG) deploy/cluster-manager/config/samples/cluster-manager/external-hub-kubeconfig
	$(KUBECTL) get ns $(HOSTED_CLUSTER_MANAGER_NAME); if [ $$? -ne 0 ] ; then $(KUBECTL) create ns $(HOSTED_CLUSTER_MANAGER_NAME); fi
	$(KUSTOMIZE) build deploy/cluster-manager/config/samples/cluster-manager | $(SED_CMD) -e "s,cluster-manager,$(HOSTED_CLUSTER_MANAGER_NAME)," | $(KUBECTL) apply -f -

external-managed-secret:
	cp $(EXTERNAL_MANAGED_KUBECONFIG) deploy/klusterlet/config/samples/managedcluster/external-managed-kubeconfig
	$(KUBECTL) get ns $(KLUSTERLET_NAME); if [ $$? -ne 0 ] ; then $(KUBECTL) create ns $(KLUSTERLET_NAME); fi
	$(KUSTOMIZE) build deploy/klusterlet/config/samples/managedcluster | $(SED_CMD) -e "s,namespace: klusterlet,namespace: $(KLUSTERLET_NAME)," | $(KUBECTL) apply -f -

bootstrap-secret-hosted:
	cp $(HUB_KUBECONFIG) deploy/klusterlet/config/samples/bootstrap/hub-kubeconfig
	$(KUBECTL) get ns $(KLUSTERLET_NAME); if [ $$? -ne 0 ] ; then $(KUBECTL) create ns $(KLUSTERLET_NAME); fi
	$(KUSTOMIZE) build deploy/klusterlet/config/samples/bootstrap | $(SED_CMD) -e "s,namespace: open-cluster-management-agent,namespace: $(KLUSTERLET_NAME)," | $(KUBECTL) apply -f -

apply-spoke-cr-hosted: bootstrap-secret-hosted external-managed-secret
	$(KUSTOMIZE) build deploy/klusterlet/config/samples | $(SED_CMD) -e "s,mode: Default,mode: Hosted," -e "s,quay.io/open-cluster-management/registration,$(REGISTRATION_IMAGE)," -e "s,quay.io/open-cluster-management/work,$(WORK_IMAGE)," -e "s,cluster1,$(MANAGED_CLUSTER_NAME)," -e "s,name: klusterlet,name: $(KLUSTERLET_NAME)," -r | $(KUBECTL) apply -f -

clean-hub-cr-hosted:
	$(KUBECTL) delete managedcluster --all --ignore-not-found
	$(KUSTOMIZE) build deploy/cluster-manager/config/samples | $(SED_CMD) -e "s,cluster-manager,$(HOSTED_CLUSTER_MANAGER_NAME)," | $(KUBECTL) delete --ignore-not-found -f -
	$(KUSTOMIZE) build deploy/cluster-manager/config/samples/cluster-manager | $(SED_CMD) -e "s,cluster-manager,$(HOSTED_CLUSTER_MANAGER_NAME)," | $(KUBECTL) delete --ignore-not-found -f -

clean-spoke-cr-hosted:
	$(KUSTOMIZE) build deploy/klusterlet/config/samples | $(KUBECTL) delete --ignore-not-found -f -
	$(KUSTOMIZE) build deploy/klusterlet/config/samples/bootstrap | $(SED_CMD) -e "s,namespace: open-cluster-management-agent,namespace: $(KLUSTERLET_NAME)," | $(KUBECTL) delete --ignore-not-found -f -
	$(KUSTOMIZE) build deploy/klusterlet/config/samples/managedcluster | $(KUBECTL) delete --ignore-not-found -f -
