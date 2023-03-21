SHELL :=/bin/bash

IMAGE_REGISTRY?=quay.io/open-cluster-management
IMAGE_TAG?=latest

build-registration:
	go build -o registration ./cmd/registration

image-registration:
	docker build \
		-f build/Dockerfile.registration \
		-t $(IMAGE_REGISTRY)/registration:$(IMAGE_TAG) .

build-work:
	go build -o work ./cmd/work

image-work:
	docker build \
		-f build/Dockerfile.work \
		-t $(IMAGE_REGISTRY)/work:$(IMAGE_TAG) .

build-placement:
	go build -o placement ./cmd/placement

image-placement:
	docker build \
		-f build/Dockerfile.placement \
		-t $(IMAGE_REGISTRY)/placement:$(IMAGE_TAG) .

build-registration-operator:
	go build -o registration-operator ./cmd/registration-operator

image-registration-operator:
	docker build \
		-f build/Dockerfile.registration-operator \
		-t $(IMAGE_REGISTRY)/registration-operator:$(IMAGE_TAG) .