# Changing OCM CRDs and API

In order change Custom Resource Definitions and the API in [ocm](https://github.com/open-cluster-management-io/ocm), one needs to make changes to the [api](https://github.com/open-cluster-management-io/api) repository and then use them in the [ocm](https://github.com/open-cluster-management-io/ocm) repository. This document aims to walk you through these steps with an example.

## Prerequisites
Clone a fork of both ocm and api to your machine
```shell
git clone git@github.com:<your-username>/api.git
git clone git@github.com:<your-username>/ocm.git
```

## Make your API changes
Make your changes in your api repository. For example, let's say you wanted to change the `ClientCertExpirationSeconds` to be `ClientCertExpirationMinutes` for some reason:

### Make your changes in the API repository
```go
type RegistrationConfiguration struct {
	// clientCertExpirationMinutes represents the minutes of a client certificate to expire. If it is not set or 0, the default
	// duration minutes will be set by the hub cluster. If the value is larger than the max signing duration minutes set on
	// the hub cluster, the max signing duration minutes will be set.
	// +optional
	ClientCertExpirationMinutes int32 `json:"clientCertExpirationMinutes,omitempty"`
    ...
```
In your api repository, run the following to generate CRDs and DeepCopy functions
```shell
user@fedora:~/api$ make update
```

You'll notice that a few files such as `operator/v1/zz_generated.deepcopy.go` and `operator/v1/0000_00_operator.open-cluster-management.io_klusterlets.crd.yaml` have been changed to reflect the changes in the API and CRD.
### Sync API updates with ocm repository

In order to use the new API, you'll have to sync them to your ocm repository. In your ocm repository, go to your go.mod file and replace `open-cluster-management.io/api` with your own updated version. This can be done by using the `replace` directive. At the end of the go.mod file, add:

```go
replace open-cluster-management.io/api v0.16.1 => /home/<user>/api //path to my local api repo w/ changes
```

Then, in your ocm repository, run the following:
```shell
user@fedora:~/ocm$ go mod tidy
user@fedora:~/ocm$ go mod vendor
user@fedora:~/ocm$ make update
```