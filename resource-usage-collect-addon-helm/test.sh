#!/bin/bash

set -e

# Install the chart with test values
echo "Installing the chart with test values..."
helm install resource-usage-collect-addon . \
  --namespace open-cluster-management-addon \
  --create-namespace \
  -f test-values.yaml

# Wait for resources to be created
echo "Waiting for resources to be created..."
sleep 10

# Run the tests
echo "Running the tests..."
helm test resource-usage-collect-addon -n open-cluster-management-addon

# Uninstall the chart
echo "Uninstalling the chart..."
helm uninstall resource-usage-collect-addon -n open-cluster-management-addon

echo "Tests completed successfully!" 