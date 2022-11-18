# Install managed-serviceaccount add-on 

1. Install manifests on the standalone cluster

```bash
kubectl apply -k deploy/addon/managed-serviceaccount/hub --kubeconfig=<standalone-kubeconfig>
```

2. Install manifests on the hosting cluster

```bash
cd deploy/addon/managed-serviceaccount/manager && kustomize edit set namespace $HUB_NAME

kubectl apply -k deploy/addon/managed-serviceaccount/manager 
```