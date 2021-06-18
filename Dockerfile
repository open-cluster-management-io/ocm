FROM registry.ci.openshift.org/open-cluster-management/builder:go1.16-linux AS builder
WORKDIR /go/src/github.com/open-cluster-management/registration-operator
COPY . .
ENV GO_PACKAGE github.com/open-cluster-management/registration-operator
RUN make build --warn-undefined-variables

FROM registry.access.redhat.com/ubi8/ubi-minimal:latest
ENV USER_UID=10001

COPY --from=builder /go/src/github.com/open-cluster-management/registration-operator/registration-operator /
RUN microdnf update && microdnf clean all

USER ${USER_UID}
