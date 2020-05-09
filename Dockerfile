FROM docker.io/openshift/origin-release:golang-1.13 AS builder
WORKDIR /go/src/github.com/open-cluster-management/work
COPY . .
ENV GO_PACKAGE github.com/open-cluster-management/nucleus
RUN make build --warn-undefined-variables

FROM registry.access.redhat.com/ubi8/ubi-minimal:8.1-398

COPY --from=builder /go/src/github.com/open-cluster-management/nucleus/nucleus /
