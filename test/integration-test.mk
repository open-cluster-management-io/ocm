TEST_TMP :=/tmp

export KUBEBUILDER_ASSETS ?=$(TEST_TMP)/kubebuilder/bin

K8S_VERSION ?=1.30.0
KB_TOOLS_ARCHIVE_NAME :=kubebuilder-tools-$(K8S_VERSION)-$(GOHOSTOS)-$(GOHOSTARCH).tar.gz
KB_TOOLS_ARCHIVE_PATH := $(TEST_TMP)/$(KB_TOOLS_ARCHIVE_NAME)

# download the kubebuilder-tools to get kube-apiserver binaries from it
ensure-kubebuilder-tools:
ifeq "" "$(wildcard $(KUBEBUILDER_ASSETS))"
	$(info Downloading kube-apiserver into '$(KUBEBUILDER_ASSETS)')
	mkdir -p '$(KUBEBUILDER_ASSETS)'
	curl -s -f -L https://storage.googleapis.com/kubebuilder-tools/$(KB_TOOLS_ARCHIVE_NAME) -o '$(KB_TOOLS_ARCHIVE_PATH)'
	tar -C '$(KUBEBUILDER_ASSETS)' --strip-components=2 -zvxf '$(KB_TOOLS_ARCHIVE_PATH)'
else
	$(info Using existing kube-apiserver from "$(KUBEBUILDER_ASSETS)")
endif
.PHONY: ensure-kubebuilder-tools

clean-integration-test:
	$(RM) '$(KB_TOOLS_ARCHIVE_PATH)'
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
