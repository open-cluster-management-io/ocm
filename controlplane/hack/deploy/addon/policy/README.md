# Install policy add-on 

1. Install manifests on the standalone cluster

```bash
kubectl apply -k deploy/addon/policy/hub --kubeconfig=<standalone-kubeconfig>
```

2. Install manifests on the hosting cluster

```bash
cd deploy/addon/policy/manager && kustomize edit set namespace $HUB_NAME

kubectl apply -k deploy/addon/policy/manager 
```