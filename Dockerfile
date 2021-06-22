FROM docker.io/openshift/origin-release:golang-1.16 AS builder
WORKDIR /go/src/open-cluster-management.io/work
COPY . .
ENV GO_PACKAGE open-cluster-management.io/work

RUN make build --warn-undefined-variables
RUN make build-e2e --warn-undefined-variables

FROM registry.access.redhat.com/ubi8/ubi-minimal:latest
ENV USER_UID=10001

COPY --from=builder /go/src/open-cluster-management.io/work/work /
COPY --from=builder /go/src/open-cluster-management.io/work/e2e.test /
RUN microdnf update && microdnf clean all

USER ${USER_UID}
