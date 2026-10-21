.PHONY: test-cluster-inventory-conformance
test-cluster-inventory-conformance:
	bash test/conformance/test-existing-inventory.sh

.PHONY: test-cluster-inventory-conformance-kind
test-cluster-inventory-conformance-kind:
	bash test/conformance/setup-and-test-kind.sh

.PHONY: clean-cluster-inventory-conformance-kind
clean-cluster-inventory-conformance-kind:
	bash test/conformance/delete-kind-clusters.sh
