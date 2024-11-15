**Table of Contents**

- [Contributing guidelines](#contributing-guidelines)
  - [Terms](#terms)
  - [Certificate of Origin](#certificate-of-origin)
  - [DCO Sign Off](#dco-sign-off)
  - [Code of Conduct](#code-of-conduct)
  - [Contributing a patch](#contributing-a-patch)
  - [Setting up your dev environment, coding, and debugging](#setting-up-your-dev-environment-coding-and-debugging)
    - [Prerequisites](#prerequisites)
    - [Setting up dev environment](#setting-up-dev-environment)
    - [Building Images](#building-images)
    - [Testing the changes in the kind cluster](#testing-the-changes-in-the-kind-cluster)
    - [Integration tests](#integration-tests)
    - [E2E tests](#e2e-tests)
  - [Issue and pull request management](#issue-and-pull-request-management)
- [References](#references)

# Contributing guidelines

The ocm repo contains 5 core components:
* registration
* placement
* work
* registration-operator
* addon-manager

## Terms

All contributions to the repository must be submitted under the terms of the [Apache Public License 2.0](https://www.apache.org/licenses/LICENSE-2.0).

## Certificate of Origin

By contributing to this project, you agree to the Developer Certificate of Origin (DCO). This document was created by the Linux Kernel community and is a simple statement that you, as a contributor, have the legal right to make the contribution. See the [DCO](DCO) file for details.

## DCO Sign Off

You must sign off your commit to state that you certify the [DCO](DCO). To certify your commit for DCO, add a line like the following at the end of your commit message:

```
Signed-off-by: John Smith <john@example.com>
```

This can be done with the `--signoff` option to `git commit`. See the [Git documentation](https://git-scm.com/docs/git-commit#Documentation/git-commit.txt--s) for details.

## Code of Conduct

The Open Cluster Management project has adopted the CNCF Code of Conduct. Refer to our [Community Code of Conduct](CODE_OF_CONDUCT.md) for details.

## Contributing a patch

1. Submit an issue describing your proposed change to the repository in question. The repository owners will respond to your issue promptly.
2. Fork the desired repository, then [develop and test](#setting-up-your-dev-environment-coding-and-debugging) your code changes.
3. Submit a pull request.

## Setting up your dev environment, coding, and debugging

### Prerequisites

- [Golang](https://go.dev/doc/install)
- Container Engin: [Docker](https://docs.docker.com/engine/install/) or [Podman](https://podman.io/docs/installation)
- [Kind](https://kind.sigs.k8s.io/docs/user/quick-start/#installation)
- [kubectl](https://kubernetes.io/docs/tasks/tools/)

### Setting up dev environment

For development purposes, we can use the [kind](https://kind.sigs.k8s.io/docs/user/quick-start/#installation) tool to
create a local Kubernetes cluster and deploy the Open Cluster Management components.

1. Create a kind cluster:

```bash
kind create cluster
```

2. Deploy the Open Cluster Management components referring to the [Setup hub and managed clusters](https://open-cluster-management.io/getting-started/quick-start) guide on the kind cluster created in the previous step. Summary the key steps are:

    1. Install the clusteradm CLI tool:
    ```bash
    curl -sL https://raw.githubusercontent.com/open-cluster-management/clusteradm/main/hack/install.sh | bash
    ```
    2. Init the control plane:
    ```bash
    clusteradm init --wait --bundle-version="latest"
    ```
    3. Join the hub cluster as a managed cluster(self management):
    ```bash
    clusteradm join --hub-token <hub-token> --hub-apiserver <hub-apiserver> --wait --cluster-name cluster1 --force-internal-endpoint-lookup --bundle-version="latest"
    ```
    4. Access the hub cluster:
    ```bash
    clusteradm accept --clusters cluster1
    ```

3. Check the Open Cluster Management components are running:

```bash
╰─# kubectl get clustermanager cluster-manager
NAME              AGE
cluster-manager   7h36m

╰─# kubectl get managedcluster
NAME       HUB ACCEPTED   MANAGED CLUSTER URLS              JOINED   AVAILABLE   AGE
cluster1   true           https://kind-control-plane:6443   True     True        7h36m

╰─# kubectl get deployment -A | grep open-cluster-management
open-cluster-management-agent   klusterlet-registration-agent             # register the managed cluster
open-cluster-management-agent   klusterlet-work-agent                     # distribute resources to the managed cluster
open-cluster-management-hub     cluster-manager-addon-manager-controller  # manage the addons
open-cluster-management-hub     cluster-manager-placement-controller      # manage the placement
open-cluster-management-hub     cluster-manager-registration-controller   # manage the registration
open-cluster-management-hub     cluster-manager-registration-webhook      # validate managedcluster/managedclusterset
open-cluster-management-hub     cluster-manager-work-webhook              # validate manifestwork
open-cluster-management         cluster-manager                           # watch clustermanager and deploy the hub components
open-cluster-management         klusterlet                                # watch managedcluster and deploy the managed cluster components
```

Note: if klustelet is deployed in Singleton mode(`clusteradm join --hub-token <hub-token> --hub-apiserver <hub-apiserver> --wait --cluster-name cluster1 --force-internal-endpoint-lookup --bundle-version="latest" --singleton=true`), the `klusterlet-registration-agent`
 and `klusterlet-work-agent` will be replaced by one deployment `klusterlet-agent`.

Once the Open Cluster Management components are deployed, you can start developing and testing your changes.

### Building Images

There are 5 core images that are built from the ocm repository: `registration`, `placement`, `work`,
`registration-operator`, and `addon-manager`.

To build all these images, you can run the following command:

```bash
# Replace the IMAGE_REGISTRY and IMAGE_TAG with your desired values
IMAGE_REGISTRY=quay.io/open-cluster-management IMAGE_TAG=test make images
```

To build a specific image, you can run the following command:

```bash
# the <image-name> can be one of the following: registration, placement, work, registration-operator, addon-manager
IMAGE_REGISTRY=quay.io/open-cluster-management IMAGE_TAG=test make image-<image-name>
```

After building the images, you can push them to the registry by docker/podman push, if you do not have a registry,
 you can use the command `kind load docker-image quay.io/open-cluster-management/<image-name>:test` to load the
  images into the kind cluster.

### Testing the changes in the kind cluster

After building and pushing the images, you can test the changes in the kind cluster.

1. Test the operators(deployment `cluster-manager` or `klusterlet` in the `open-cluster-management` namespace) changes
by replacing the registration-operator image for the operators deployments:

```bash
kubectl set image -n open-cluster-management deployment/cluster-manager registration-operator=$(IMAGE_REGISTRY)/registration-operator:$(IMAGE_TAG)
kubectl set image -n open-cluster-management deployment/klusterlet klusterlet=$(IMAGE_REGISTRY)/registration-operator:$(IMAGE_TAG)
```

2. Test the hub side components changes by replacing the image fields in the clustermanager spec:

```bash
kubectl edit clustermanager cluster-manager
```

3. Test the managed cluster side components changes by replacing the image fields in the klusterlet spec:

```bash
kubectl edit klusterlet klusterlet
```

### Integration tests

The integration tests are written in the [test/integration](test/integration) directory. They start a kubernetes
api server locally with [controller-runtime](https://book.kubebuilder.io/reference/envtest), and run the tests against
the local api server.

To run the integration tests, you can use the following command:

```bash
make test-integration # run all the integration tests
make test-<component>-integration # run the integration tests for a specific component
```

### E2E tests

The E2E tests are written in the [test/e2e](test/e2e) directory. You can run the E2E tests:
- referring to the steps described in the [e2e workflow](.github/workflows/e2e.yml) file
- or by the [act](https://github.com/nektos/act) command: `act -j e2e`

## Issue and pull request management

Anyone can comment on issues and submit reviews for pull requests. In order to be assigned an issue or pull request, you can leave a `/assign <your Github ID>` comment on the issue or pull request.

# References

Please refer to [the Contribution Guide](https://open-cluster-management.io/contribute/)
