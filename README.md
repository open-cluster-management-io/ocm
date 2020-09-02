# Providing content to managed clusters

Support a primitive that enables resources to be applied to a managed cluster.

## Community, discussion, contribution, and support

Check the [CONTRIBUTING Doc](CONTRIBUTING.md) for how to contribute to the repo.

<!--

You can reach the maintainers of this project at:

- [#xxx on Slack](https://slack.com/signin?redir=%2Fmessages%2Fxxx)

-->

------

## Getting Started

### Prerequisites

These instructions assume:

- You have a running kubernetes cluster
- You have `KUBECONFIG` environment variable set to a kubeconfig file giving you cluster-admin role on that cluster

### Deploy Work Agent

1. Run `make deploy-work-agent`

2. To clean the environment, run `make clean-work-agent`

## Security Response

If you've found a security issue that you'd like to disclose confidentially please contact Red Hat's Product Security team. Details at https://access.redhat.com/security/team/contact

<!--
## XXX References

If you have any further question about xxx, please refer to
[XXX help documentation](docs/xxx_help.md) for further information.
-->