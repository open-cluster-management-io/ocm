FROM registry.ci.openshift.org/open-cluster-management/builder:go1.17-linux AS builder
WORKDIR /go/src/open-cluster-management.io/registration-operator
COPY . .
ENV GO_PACKAGE open-cluster-management.io/registration-operator
RUN make build --warn-undefined-variables

FROM registry.access.redhat.com/ubi8/ubi-minimal:latest
ENV USER_UID=10001

COPY --from=builder /go/src/open-cluster-management.io/registration-operator/registration-operator /
RUN microdnf update && microdnf clean all

USER ${USER_UID}
