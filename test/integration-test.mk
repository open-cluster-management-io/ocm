TEST_TMP :=/tmp

export KUBEBUILDER_ASSETS ?=$(TEST_TMP)/kubebuilder/bin

K8S_VERSION ?=1.16.4
KB_TOOLS_ARCHIVE_NAME :=kubebuilder-tools-$(K8S_VERSION)-$(GOHOSTOS)-$(GOHOSTARCH).tar.gz
KB_TOOLS_ARCHIVE_PATH := $(TEST_TMP)/$(KB_TOOLS_ARCHIVE_NAME)

ensure-test-env:
ifeq "" "$(wildcard $(KUBEBUILDER_ASSETS))"
	$(info Downloading kubebuilder-tools into '$(KUBEBUILDER_ASSETS)')
	mkdir -p '$(KUBEBUILDER_ASSETS)'
	curl -s -f -L https://storage.googleapis.com/kubebuilder-tools/$(KB_TOOLS_ARCHIVE_NAME) -o '$(KB_TOOLS_ARCHIVE_PATH)'
	tar -C '$(KUBEBUILDER_ASSETS)' --strip-components=2 -zvxf '$(KB_TOOLS_ARCHIVE_PATH)'
else
	$(info Using existing kubebuilder-tools from "$(KUBEBUILDER_ASSETS)")
endif
.PHONY: ensure-test-env

clean-test-env:
	$(RM) '$(KB_TOOLS_ARCHIVE_PATH)'
	rm -rf $(TEST_TMP)/kubebuilder
	$(RM) ./integration.test
.PHONY: clean-test-env

clean: clean-test-env

test-integration: ensure-test-env
	go test -c ./test/integration
	./integration.test -ginkgo.slowSpecThreshold=15 -ginkgo.v -ginkgo.failFast
.PHONY: test-integration