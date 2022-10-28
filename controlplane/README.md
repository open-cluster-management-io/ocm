# Get Started

```
$ cd standalonecontrolplane

# clean environment, build binary, run
$ make all

```
The kubeconfig file is stored in `.ocmconfig/cert/`

Then open another terminal to test:
```
$ kubectl --kubeconfig .ocmconfig/cert/kube-aggregator.kubeconfig api-resources
```
