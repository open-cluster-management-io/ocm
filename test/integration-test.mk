ENSURE_ENVTEST_SCRIPT := https://raw.githubusercontent.com/open-cluster-management-io/sdk-go/main/ci/envtest/ensure-envtest.sh

.PHONY: envtest-setup
envtest-setup:
	$(eval export KUBEBUILDER_ASSETS=$(shell curl -fsSL $(ENSURE_ENVTEST_SCRIPT) | bash))
	@echo "KUBEBUILDER_ASSETS=$(KUBEBUILDER_ASSETS)"

clean-integration-test:
	$(RM) ./*integration.test
.PHONY: clean-integration-test

clean: clean-integration-test

build-work-integration:
	go test -c ./test/integration/work -o ./work-integration.test

test-registration-integration: envtest-setup
	go test -c ./test/integration/registration -o ./registration-integration.test -mod=vendor
	./registration-integration.test -ginkgo.slow-spec-threshold=15s -ginkgo.v -ginkgo.fail-fast ${ARGS} -v=5
.PHONY: test-registration-integration

test-work-integration: envtest-setup build-work-integration
	./work-integration.test -ginkgo.slow-spec-threshold=15s -ginkgo.v -ginkgo.fail-fast ${ARGS}
.PHONY: test-work-integration

test-placement-integration: envtest-setup
	go test -c ./test/integration/placement -o ./placement-integration.test
	./placement-integration.test -ginkgo.slow-spec-threshold=15s -ginkgo.v -ginkgo.fail-fast ${ARGS}
.PHONY: test-placement-integration

test-registration-operator-integration: envtest-setup
	go test -c ./test/integration/operator -o ./registration-operator-integration.test
	./registration-operator-integration.test -ginkgo.slow-spec-threshold=15s -ginkgo.v -ginkgo.fail-fast ${ARGS}
.PHONY: test-registration-operator-integration

test-addon-integration: envtest-setup
	go test -c ./test/integration/addon -o ./addon-integration.test
	./addon-integration.test -ginkgo.slow-spec-threshold=15s -ginkgo.v -ginkgo.fail-fast ${ARGS}
.PHONY: test-addon-integration

# In the cloud events scenario, skip the following tests
test-cloudevents-work-grpc-integration: envtest-setup build-work-integration
	./work-integration.test -ginkgo.slow-spec-threshold=15s -ginkgo.v -ginkgo.fail-fast \
		-test.driver=grpc \
		-v=4 ${ARGS}
.PHONY: test-cloudevents-work-grpc-integration

test-integration: test-registration-operator-integration test-registration-integration test-placement-integration test-work-integration test-addon-integration
.PHONY: test-integration
