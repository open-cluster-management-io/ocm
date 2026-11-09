FROM golang:1.26 AS build

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -o /out/apiserver ./cmd/apiserver

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/apiserver /apiserver
USER 65532:65532
ENTRYPOINT ["/apiserver"]
