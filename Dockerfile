FROM docker.io/openshift/origin-release:golang-1.13 AS builder
WORKDIR /go/src/github.com/open-cluster-management/registration
COPY . .
ENV GO_PACKAGE github.com/open-cluster-management/registration

RUN curl -O -L https://github.com/kubernetes-sigs/kustomize/releases/download/v2.0.0/kustomize_2.0.0_linux_amd64; mv kustomize_2.0.0_linux_amd64 kustomize; chmod u+x kustomize; mv kustomize /usr/bin/kustomize

RUN make build --warn-undefined-variables

FROM registry.access.redhat.com/ubi8/ubi-minimal:8.1-398

COPY --from=builder /go/src/github.com/open-cluster-management/registration/registration /
