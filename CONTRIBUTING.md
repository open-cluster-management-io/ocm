**Table of Contents**

- [Contributing guidelines](#contributing-guidelines)
    - [Terms](#terms)
    - [Certificate of Origin](#certificate-of-origin)
    - [DCO Sign Off](#dco-sign-off)
    - [Code of Conduct](#code-of-conduct)
    - [Contributing a patch](#contributing-a-patch)
    - [Issue and pull request management](#issue-and-pull-request-management)

# Contributing guidelines

The ocm repo contains 4 core components:
* registration
* placement
* work
* registration-operator

Before 0.11.0, the 4 components has independent repos, now we are going through [the task of consolidating code](https://github.com/open-cluster-management-io/OCM/issues/128) to merge all code into this repo to gain all kinds of benifits.

We're contiously working on migrate code from `/staging` folder to outside. Finally, the `/staging` folder will be empty and removed.

Here is a table of the migration status:

| Component | Status |
| --- | --- |
| registration | staging |
| placement | staging |
| work | staging |
| registration-operator | staging |

Status:
* staging: the component is having all code in `staging` folder.
* mix: the component is having code in both `staging` and outside.
* done: the component is having all code outside `staging` folder and done of the migration.

If a component is in the `staging` status, you need to open project in the `/staging` folder and contribute there.

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
2. Fork the desired repository, then develop and test your code changes.
3. Submit a pull request.

## Issue and pull request management

Anyone can comment on issues and submit reviews for pull requests. In order to be assigned an issue or pull request, you can leave a `/assign <your Github ID>` comment on the issue or pull request.
