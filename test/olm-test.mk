KUBECTL?=kubectl
OLM_NAMESPACE?=olm
OLM_VERSION?=v0.25.0

install-olm: ensure-operator-sdk
	$(KUBECTL) get crds | grep clusterserviceversion ; if [ $$? -ne 0 ] ; then $(OPERATOR_SDK) olm install --version $(OLM_VERSION); fi
	$(KUBECTL) get ns open-cluster-management ; if [ $$? -ne 0 ] ; then $(KUBECTL) create ns open-cluster-management ; fi

deploy-hub-operator-olm: install-olm
	$(OPERATOR_SDK) run packagemanifests deploy/cluster-manager/olm-catalog/cluster-manager/ --namespace open-cluster-management --version $(CSV_VERSION) --install-mode OwnNamespace --timeout=10m

clean-hub-olm: ensure-operator-sdk
	$(KUBECTL) delete -f deploy/cluster-manager/config/samples/operator_open-cluster-management_clustermanagers.cr.yaml --ignore-not-found
	$(OPERATOR_SDK) cleanup cluster-manager --namespace open-cluster-management --timeout 10m

deploy-spoke-operator-olm: install-olm
	$(OPERATOR_SDK) run packagemanifests deploy/klusterlet/olm-catalog/klusterlet/ --namespace open-cluster-management --version $(CSV_VERSION) --install-mode OwnNamespace --timeout=10m

clean-spoke-olm: ensure-operator-sdk
	$(KUBECTL) delete -f deploy/klusterlet/config/samples/operator_open-cluster-management_klusterlets.cr.yaml --ignore-not-found
	$(OPERATOR_SDK) cleanup klusterlet --namespace open-cluster-management --timeout 10m
