FROM docker.io/openshift/origin-release:golang-1.15 AS builder
WORKDIR /go/src/github.com/open-cluster-management/work
COPY . .
ENV GO_PACKAGE github.com/open-cluster-management/work

RUN make build --warn-undefined-variables
RUN make build-e2e --warn-undefined-variables

FROM registry.access.redhat.com/ubi8/ubi-minimal:latest
ENV USER_UID=10001

COPY --from=builder /go/src/github.com/open-cluster-management/work/work /
COPY --from=builder /go/src/github.com/open-cluster-management/work/e2e.test /
RUN microdnf update && microdnf clean all

USER ${USER_UID}
