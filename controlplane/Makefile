BINARYDIR := bin

all: clean vendor build run
.PHONY: all

run:
	hack/start-ocm-controlplane.sh
.PHONY: run

# the script will automatically start a exteral etcd
run-with-external-etcd:
	hack/start-ocm-controlplane.sh false
.PHONY: run-with-external-etcd

build: 
	$(shell if [ ! -e $(BINARYDIR) ];then mkdir -p $(BINARYDIR); fi)
	go build -o bin/ocm-controlplane cmd/main.go 
.PHONY: build

clean:
	rm -rf bin .ocmconfig
.PHONY: clean

vendor: 
	go mod tidy 
	go mod vendor
.PHONY: vendor

update:
	bash -x hack/crd-update/copy-crds.sh
.PHONY: update