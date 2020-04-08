
all: build
.PHONY: all

# Include the library makefile
include $(addprefix ./vendor/github.com/openshift/build-machinery-go/make/, \
	golang.mk \
	targets/openshift/deps.mk \
	targets/openshift/images.mk \
	targets/openshift/bindata.mk \
)

$(call add-bindata,spokecluster,./pkg/hub/spokecluster/manifests/...,bindata,bindata,./pkg/hub/spokecluster/bindata/bindata.go)
$(call add-bindata,csr,./pkg/hub/csr/manifests/...,bindata,bindata,./pkg/hub/csr/bindata/bindata.go)

clean:
	$(RM) ./registration
.PHONY: clean

GO_TEST_PACKAGES :=./pkg/... ./cmd/...
