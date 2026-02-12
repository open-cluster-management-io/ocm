K8S_VERSION ?=1.30.0

SETUP_ENVTEST ?=$(PERMANENT_TMP_GOPATH)/bin/setup-envtest
SETUP_ENVTEST_VERSION ?=v0.23.1
setup_envtest_gen_dir:=$(dir $(SETUP_ENVTEST))
SETUP_ENVTEST_ARCHOS:=$(GOHOSTOS)-$(GOHOSTARCH)

# download the kubebuilder-tools to get kube-apiserver binaries from it
ensure-kubebuilder-tools: ensure-setup-envtest
	$(info Setting up envtest binaries for Kubernetes $(K8S_VERSION))
	@$(eval export KUBEBUILDER_ASSETS := $(shell $(SETUP_ENVTEST) use $(K8S_VERSION) --bin-dir $(shell pwd)/$(PERMANENT_TMP_GOPATH)/kubebuilder/bin -p path))
	@echo "KUBEBUILDER_ASSETS=$(KUBEBUILDER_ASSETS)"
.PHONY: ensure-kubebuilder-tools

ensure-setup-envtest:
ifeq "" "$(wildcard $(SETUP_ENVTEST))"
	$(info Downloading setup-envtest $(SETUP_ENVTEST_VERSION) into '$(SETUP_ENVTEST)')
	mkdir -p '$(setup_envtest_gen_dir)'
	curl -s -f -L https://github.com/kubernetes-sigs/controller-runtime/releases/download/$(SETUP_ENVTEST_VERSION)/setup-envtest-$(SETUP_ENVTEST_ARCHOS) -o '$(SETUP_ENVTEST)'
	chmod +x '$(SETUP_ENVTEST)';
else
	$(info Using existing setup-envtest from "$(SETUP_ENVTEST)")
endif
.PHONY: ensure-setup-envtest

clean-integration-test:
	chmod -R u+w $(PERMANENT_TMP_GOPATH)/kubebuilder 2>/dev/null || true
	rm -rf $(PERMANENT_TMP_GOPATH)/kubebuilder
	$(RM) '$(SETUP_ENVTEST)'
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
