FROM golang:1.17 AS builder
WORKDIR /go/src/open-cluster-management.io/addon-framework
COPY . .
ENV GO_PACKAGE open-cluster-management.io/addon-framework

RUN make build --warn-undefined-variables

FROM registry.access.redhat.com/ubi8/ubi-minimal:latest
COPY --from=builder /go/src/open-cluster-management.io/addon-framework/helloworld /
COPY --from=builder /go/src/open-cluster-management.io/addon-framework/helloworld_helm /
COPY --from=builder /go/src/open-cluster-management.io/addon-framework/helloworld_hosted /

RUN microdnf update && microdnf clean all
