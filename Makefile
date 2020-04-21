
all: build
.PHONY: all

# Include the library makefile
include $(addprefix ./vendor/github.com/openshift/build-machinery-go/make/, \
	golang.mk \
	targets/openshift/deps.mk \
	targets/openshift/images.mk \
)

clean:
	$(RM) ./work
.PHONY: clean

GO_TEST_PACKAGES :=./pkg/... ./cmd/...
