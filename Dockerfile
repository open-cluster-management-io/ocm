FROM docker.io/openshift/origin-release:golang-1.15 AS builder
WORKDIR /go/src/github.com/open-cluster-management/placement
COPY . .
ENV GO_PACKAGE github.com/open-cluster-management/placement

RUN make build --warn-undefined-variables

FROM registry.access.redhat.com/ubi8/ubi-minimal:latest
ENV USER_UID=10001
COPY --from=builder /go/src/github.com/open-cluster-management/placement/placement /
RUN microdnf update && microdnf clean all

USER ${USER_UID}
