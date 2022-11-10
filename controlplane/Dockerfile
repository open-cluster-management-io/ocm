FROM golang:1.18 AS builder

ARG OS=linux
ARG ARCH=amd64
ENV DIRPATH /go/src/open-cluster-management.io/OCM/controlplane
WORKDIR ${DIRPATH}

COPY . .

RUN apt-get update && apt-get install net-tools && make vendor 
RUN GOOS=${OS} \
    GOARCH=${ARCH} \
    make build


FROM registry.access.redhat.com/ubi8/ubi-minimal:latest
ENV USER_UID=10001

COPY --from=builder /go/src/open-cluster-management.io/OCM/controlplane/bin/ocm-controlplane /

USER ${USER_UID}
