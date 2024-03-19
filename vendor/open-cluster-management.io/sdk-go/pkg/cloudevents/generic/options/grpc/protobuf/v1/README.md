# CloudEvent gRPC Protobuf Definitions

## Overview

This repository includes the protobuf message and RPC method definitions for CloudEvent gRPC service, along with the corresponding Go code generated from these definitions.

## Getting Started

### Prerequisites

Make sure you have the following tools installed:

- [Protocol Compiler (protoc)](https://grpc.io/docs/protoc-installation/)
- Go plugins for the protocol compiler:

```bash
$ go install google.golang.org/protobuf/cmd/protoc-gen-go
$ go install google.golang.org/grpc/cmd/protoc-gen-go-grpc
```

### Updating CloudEvent gRPC Service

1. Modify the `*.proto` files to reflect your desired changes.
2. Run the following command to update the generated code:

    ```bash
    go generate
    ```

    This step is crucial to ensure that your changes are applied to both the gRPC server and client stub.
