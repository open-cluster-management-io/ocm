
all: build
.PHONY: all

# Include the library makefile
include $(addprefix ./vendor/github.com/openshift/build-machinery-go/make/, \
	golang.mk \
	targets/openshift/deps.mk \
	targets/openshift/images.mk \
	targets/openshift/bindata.mk \
)

$(call add-bindata,hub,./manifests/hub/...,bindata,bindata,./pkg/operators/hub/bindata/bindata.go)
$(call add-bindata,spoke,./manifests/spoke/...,bindata,bindata,./pkg/operators/spoke/bindata/bindata.go)

copy-crd:
	cp ./vendor/github.com/open-cluster-management/api/cluster/v1/*.yaml ./manifests/hub/
	cp ./vendor/github.com/open-cluster-management/api/work/v1/*.yaml ./manifests/hub/

update-all: copy-crd update-bindata-hub update-bindata-spoke

clean:
	$(RM) ./nucleus
.PHONY: clean

GO_TEST_PACKAGES :=./pkg/... ./cmd/...
