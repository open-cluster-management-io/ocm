KUBECTL?=kubectl
OLM_NAMESPACE?=olm
OLM_VERSION?=v0.25.0

install-olm: ensure-operator-sdk
	$(KUBECTL) get crds | grep clusterserviceversion ; if [ $$? -ne 0 ] ; then $(OPERATOR_SDK) olm install --version $(OLM_VERSION); fi
	$(KUBECTL) get ns open-cluster-management ; if [ $$? -ne 0 ] ; then $(KUBECTL) create ns open-cluster-management ; fi

deploy-hub-operator-olm: install-olm
	$(OPERATOR_SDK) run packagemanifests deploy/cluster-manager/olm-catalog/ --namespace open-cluster-management --version $(CSV_VERSION) --install-mode OwnNamespace --timeout=10m

clean-hub-olm: ensure-operator-sdk
	$(KUBECTL) delete -f deploy/cluster-manager/config/samples/operator_open-cluster-management_clustermanagers.cr.yaml --ignore-not-found
	$(OPERATOR_SDK) cleanup cluster-manager --namespace open-cluster-management --timeout 10m

deploy-spoke-operator-olm: install-olm
	$(OPERATOR_SDK) run packagemanifests deploy/klusterlet/olm-catalog/ --namespace open-cluster-management --version $(CSV_VERSION) --install-mode OwnNamespace --timeout=10m

clean-spoke-olm: ensure-operator-sdk
	$(KUBECTL) delete -f deploy/klusterlet/config/samples/operator_open-cluster-management_klusterlets.cr.yaml --ignore-not-found
	$(OPERATOR_SDK) cleanup klusterlet --namespace open-cluster-management --timeout 10m

# generate a released csv bundle files, the released version should be set to RELEASED_CSV_VERSION.
release-csv: ensure-operator-sdk
	# generate released csv bundle files
	cd deploy/cluster-manager && ../../$(OPERATOR_SDK) generate bundle --version $(RELEASED_CSV_VERSION) --package cluster-manager --channels stable --default-channel stable --input-dir config --output-dir olm-catalog/$(RELEASED_CSV_VERSION)
	cd deploy/klusterlet && ../../$(OPERATOR_SDK) generate bundle --version $(RELEASED_CSV_VERSION) --package klusterlet --channels stable --default-channel stable --input-dir config --output-dir olm-catalog/$(RELEASED_CSV_VERSION)

	# update the image in the csv
	$(SED_CMD) -e "s,quay.io/open-cluster-management/registration-operator:latest,quay.io/open-cluster-management/registration-operator:v$(RELEASED_CSV_VERSION),"\
    -e "s,quay.io/open-cluster-management/registration:latest,quay.io/open-cluster-management/registration:v$(RELEASED_CSV_VERSION),"\
    -e "s,quay.io/open-cluster-management/work:latest,quay.io/open-cluster-management/work:v$(RELEASED_CSV_VERSION),"\
    -e "s,quay.io/open-cluster-management/placement:latest,quay.io/open-cluster-management/placement:v$(RELEASED_CSV_VERSION),"\
    -e "s,quay.io/open-cluster-management/addon-manager:latest,quay.io/open-cluster-management/addon-manager:v$(RELEASED_CSV_VERSION),"\
    -i deploy/cluster-manager/olm-catalog/$(RELEASED_CSV_VERSION)/manifests/cluster-manager.clusterserviceversion.yaml\
    -i deploy/klusterlet/olm-catalog/$(RELEASED_CSV_VERSION)/manifests/klusterlet.clusterserviceversion.yaml

	# delete bundle.Dockerfile since we do not use it to build image.
	rm ./deploy/cluster-manager/bundle.Dockerfile
	rm ./deploy/klusterlet/bundle.Dockerfile
