TEST_TMP :=/tmp

export KUBEBUILDER_ASSETS ?=$(TEST_TMP)/kubebuilder/bin

K8S_VERSION ?=1.30.0
SETUP_ENVTEST := $(shell go env GOPATH)/bin/setup-envtest

# download the kubebuilder-tools to get kube-apiserver binaries from it
ensure-kubebuilder-tools:
ifeq "" "$(wildcard $(KUBEBUILDER_ASSETS))"
	$(info Downloading kube-apiserver into '$(KUBEBUILDER_ASSETS)')
	mkdir -p '$(KUBEBUILDER_ASSETS)'
ifeq "" "$(wildcard $(SETUP_ENVTEST))"
	$(info Installing setup-envtest into '$(SETUP_ENVTEST)')
	go install sigs.k8s.io/controller-runtime/tools/setup-envtest@release-0.22
endif
	ENVTEST_K8S_PATH=$$($(SETUP_ENVTEST) use $(K8S_VERSION) --bin-dir $(KUBEBUILDER_ASSETS) -p path); \
	if [ -z "$$ENVTEST_K8S_PATH" ]; then \
		echo "Error: setup-envtest returned empty path"; \
		exit 1; \
	fi; \
	if [ ! -d "$$ENVTEST_K8S_PATH" ]; then \
		echo "Error: setup-envtest path does not exist: $$ENVTEST_K8S_PATH"; \
		exit 1; \
	fi; \
	cp -r $$ENVTEST_K8S_PATH/* $(KUBEBUILDER_ASSETS)/
else
	$(info Using existing kube-apiserver from "$(KUBEBUILDER_ASSETS)")
endif
.PHONY: ensure-kubebuilder-tools

clean-integration-test:
	rm -rf $(TEST_TMP)/kubebuilder
	$(RM) ./*integration.test
.PHONY: clean-integration-test

clean: clean-integration-test

build-work-integration:
	go test -c ./test/integration/work -o ./work-integration.test

test-registration-integration: ensure-kubebuilder-tools
	go test -c ./test/integration/registration -o ./registration-integration.test -mod=vendor
	./registration-integration.test -ginkgo.slow-spec-threshold=15s -ginkgo.v -ginkgo.fail-fast ${ARGS} -v=5
.PHONY: test-registration-integration

test-work-integration: ensure-kubebuilder-tools build-work-integration
	./work-integration.test -ginkgo.slow-spec-threshold=15s -ginkgo.v -ginkgo.fail-fast ${ARGS}
.PHONY: test-work-integration

test-placement-integration: ensure-kubebuilder-tools
	go test -c ./test/integration/placement -o ./placement-integration.test
	./placement-integration.test -ginkgo.slow-spec-threshold=15s -ginkgo.v -ginkgo.fail-fast ${ARGS}
.PHONY: test-placement-integration

test-registration-operator-integration: ensure-kubebuilder-tools
	go test -c ./test/integration/operator -o ./registration-operator-integration.test
	./registration-operator-integration.test -ginkgo.slow-spec-threshold=15s -ginkgo.v -ginkgo.fail-fast ${ARGS}
.PHONY: test-registration-operator-integration

test-addon-integration: ensure-kubebuilder-tools
	go test -c ./test/integration/addon -o ./addon-integration.test
	./addon-integration.test -ginkgo.slow-spec-threshold=15s -ginkgo.v -ginkgo.fail-fast ${ARGS}
.PHONY: test-addon-integration

# In the cloud events scenario, skip the following tests
# - unmanaged_appliedwork_test.go, this test mainly focus on switching the hub kube-apiserver
# - manifestworkreplicaset_test.go, this test needs to update the work status with the hub work client,
#   cloud events work client does not support it. (TODO) may add e2e to for mwrs.
test-cloudevents-work-mqtt-integration: ensure-kubebuilder-tools build-work-integration
	./work-integration.test -ginkgo.slow-spec-threshold=15s -ginkgo.v -ginkgo.fail-fast \
		-ginkgo.skip-file manifestworkreplicaset_test.go \
		-ginkgo.skip-file unmanaged_appliedwork_test.go \
		-ginkgo.skip-file manifestworkgarbagecollection_test.go \
		-test.driver=mqtt \
		-v=4 ${ARGS}
.PHONY: test-cloudevents-work-mqtt-integration

# In the cloud events scenario, skip the following tests
test-cloudevents-work-grpc-integration: ensure-kubebuilder-tools build-work-integration
	./work-integration.test -ginkgo.slow-spec-threshold=15s -ginkgo.v -ginkgo.fail-fast \
		-test.driver=grpc \
		-v=4 ${ARGS}
.PHONY: test-cloudevents-work-grpc-integration

test-integration: test-registration-operator-integration test-registration-integration test-placement-integration test-work-integration test-addon-integration
.PHONY: test-integration

test-cloudevents-integration: test-cloudevents-work-mqtt-integration test-cloudevents-work-grpc-integration
.PHONY: test-cloudevents-integration
