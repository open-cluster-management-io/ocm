# Install work-manager add-on 

1. Install manifests on the standalone cluster

```bash 
kubectl apply -k deploy/addon/work-manager/hub --kubeconfig=<standalone-kubeconfig>
```

2. Install manifests on the hosting cluster

```bash
cd deploy/addon/work-manager/manager && kustomize edit set namespace $HUB_NAME
kubectl apply -k work-manager/manager 
```