<!-- START doctoc generated TOC please keep comment here to allow auto update -->
<!-- DON'T EDIT THIS SECTION, INSTEAD RE-RUN doctoc TO UPDATE -->
**Table of Contents**  *generated with [DocToc](https://github.com/thlorenz/doctoc)*

- [Contributing guidelines](#contributing-guidelines)
  - [Contributions](#contributions)
  - [Certificate of Origin](#certificate-of-origin)
  - [Contributing A Patch](#contributing-a-patch)
  - [Issue and Pull Request Management](#issue-and-pull-request-management)
  - [Pre-check before submitting a PR](#pre-check-before-submitting-a-pr)
  - [Build images](#build-images)

<!-- END doctoc generated TOC please keep comment here to allow auto update -->

# Contributing guidelines

## Contributions

All contributions to the repository must be submitted under the terms of the [Apache Public License 2.0](https://www.apache.org/licenses/LICENSE-2.0).

## Certificate of Origin

By contributing to this project you agree to the Developer Certificate of
Origin (DCO). This document was created by the Linux Kernel community and is a
simple statement that you, as a contributor, have the legal right to make the
contribution. See the [DCO](DCO) file for details.

## Contributing A Patch

1. Submit an issue describing your proposed change to the repo in question.
1. The [repo owners](OWNERS) will respond to your issue promptly.
1. Fork the desired repo, develop and test your code changes.
1. Submit a pull request.

## Issue and Pull Request Management

Anyone may comment on issues and submit reviews for pull requests. However, in
order to be assigned an issue or pull request, you must be a member of the
[open-cluster-management](https://github.com/open-cluster-management-io) GitHub organization.

Repo maintainers can assign you an issue or pull request by leaving a
`/assign <your Github ID>` comment on the issue or pull request.

## Pre-check before submitting a PR

After your PR is ready to commit, please run following commands to check your code.

- verify your code

  ```shell
  make verify
  ```
  
- run the unit test
  
  ```shell
  make test
  ```

- run the integration test
  
  ```shell
  make test-integration
  ```

- run the end-to-end test, prepare a kind cluster and run the following command
  
  ```shell
  export KUBECONFIG={the kubeconfig of your kind cluster that will run the e2e test}

  make images

  kind load docker-image quay.io/open-cluster-management/registration --name {your kind cluster name}

  make test-e2e
  ```

## Build images

Make sure your code build passed.

```shell
make images
```

Now, you can follow the [getting started guide](./README.md#Quickstart) to work with the open-cluster-management registration repository.
