# Resource Usage Collect Addon Helm Chart

This Helm chart installs the Resource Usage Collect Addon for Open Cluster Management (OCM). The addon collects resource usage information from managed clusters and reports it to the hub cluster.

## Prerequisites

- Open Cluster Management (OCM) installed
- Helm 3.x
- kubectl configured to access your cluster
- A managed cluster registered with OCM

## Installation

### From Local Chart

```bash
# Clone the repository
git clone https://github.com/open-cluster-management-io/ocm.git
cd ocm/solutions/kueue-admission-check/resource-usage-collect-addon-helm

# Install the chart
helm install resource-usage-collect-addon . \
  --namespace open-cluster-management-addon \
  --create-namespace
```

### From Chart Archive

```bash
# Package the chart
helm package .

# Install from the package
helm install resource-usage-collect-addon resource-usage-collect-addon-0.1.0.tgz \
  --namespace open-cluster-management-addon \
  --create-namespace
```

## Configuration

The following table lists the configurable parameters of the Resource Usage Collect Addon chart and their default values.

| Parameter | Description | Default |
|-----------|-------------|---------|
| `global.image.repository` | Container image repository | `quay.io/open-cluster-management` |
| `global.image.tag` | Container image tag | `latest` |
| `global.image.pullPolicy` | Container image pull policy | `IfNotPresent` |
| `addon.name` | Name of the addon | `resource-usage-collect` |
| `addon.displayName` | Display name of the addon | `Resource Usage Collect` |
| `addon.description` | Description of the addon | `Collects resource usage metrics from managed clusters` |
| `addon.namespace` | Namespace where the addon will be installed | `open-cluster-management-addon` |
| `placement.name` | Name of the placement | `resource-usage-collect-placement` |
| `placement.namespace` | Namespace of the placement | `open-cluster-management-addon` |
| `agent.replicas` | Number of agent replicas | `1` |
| `agent.resources.requests.cpu` | CPU request for the agent | `100m` |
| `agent.resources.requests.memory` | Memory request for the agent | `128Mi` |
| `agent.resources.limits.cpu` | CPU limit for the agent | `500m` |
| `agent.resources.limits.memory` | Memory limit for the agent | `512Mi` |
| `rbac.create` | Whether to create RBAC resources | `true` |

## Migration from Direct YAML Installation

If you're currently using the direct YAML installation method from `setup-env.sh`, follow these steps to migrate to the Helm chart:

1. **Backup your current configuration**:
   ```bash
   kubectl get clustermanagementaddon resource-usage-collect -n open-cluster-management-addon -o yaml > backup.yaml
   ```

2. **Uninstall the current version**:
   ```bash
   kubectl delete -f https://raw.githubusercontent.com/open-cluster-management-io/addon-contrib/main/resource-usage-collect-addon/manifests/clustermanagementaddon.yaml
   ```

3. **Install using Helm**:
   ```bash
   helm install resource-usage-collect-addon . \
     --namespace open-cluster-management-addon \
     --create-namespace
   ```

4. **Verify the installation**:
   ```bash
   helm list -n open-cluster-management-addon
   kubectl get clustermanagementaddon resource-usage-collect -n open-cluster-management-addon
   ```

## Testing

The chart includes a test suite that verifies the installation of all components. To run the tests:

```bash
helm test resource-usage-collect-addon -n open-cluster-management-addon
```

## Uninstallation

To uninstall/delete the deployment:

```bash
helm uninstall resource-usage-collect-addon -n open-cluster-management-addon
```

## Contributing

Please read [CONTRIBUTING.md](CONTRIBUTING.md) for details on our code of conduct and the process for submitting pull requests.

## License

This project is licensed under the Apache License 2.0 - see the [LICENSE](LICENSE) file for details.

## Integration with Kueue

This addon is a key component in the OCM-Kueue integration workflow:

1. **Resource Collection**: The addon collects real-time resource usage metrics from managed clusters
2. **Metric Storage**: Metrics are stored and made available through the OCM API
3. **Kueue Integration**: Kueue uses these metrics to:
   - Make intelligent workload placement decisions
   - Ensure workloads are scheduled on clusters with sufficient resources
   - Prevent resource overallocation
   - Optimize cluster utilization

## Migration from Direct YAML Installation

If you're currently using the direct YAML installation method from `setup-env.sh`, follow these steps to migrate to the Helm chart:

1. **Backup your current configuration**:
   ```bash
   kubectl get clustermanagementaddon resource-usage-collect -n open-cluster-management-addon -o yaml > backup.yaml
   ```

2. **Uninstall the current version**:
   ```bash
   kubectl delete -f https://raw.githubusercontent.com/open-cluster-management-io/addon-contrib/main/resource-usage-collect-addon/manifests/clustermanagementaddon.yaml
   ```

3. **Install using Helm**:
   ```bash
   helm install resource-usage-collect-addon . \
     --namespace open-cluster-management-addon \
     --create-namespace
   ```

4. **Verify the installation**:
   ```bash
   helm list -n open-cluster-management-addon
   kubectl get clustermanagementaddon resource-usage-collect -n open-cluster-management-addon
   ```

## Testing

The chart includes a test suite that verifies the installation of all components. To run the tests:

```bash
helm test resource-usage-collect-addon -n open-cluster-management-addon
```

## Uninstallation

To uninstall/delete the deployment:

```bash
helm uninstall resource-usage-collect-addon -n open-cluster-management-addon
```

## Contributing

Please read [CONTRIBUTING.md](CONTRIBUTING.md) for details on our code of conduct and the process for submitting pull requests.

## License

This project is licensed under the Apache License 2.0 - see the [LICENSE](LICENSE) file for details. 