#!/bin/bash

# Check if there are enough arguments
if [ "$#" -lt 1 ]; then
    echo "Usage: $0 <RUN_TIMES> [KlusterletDeployMode]"
    exit 1
fi

# Read command line arguments
RUN_TIMES=$1
KLUSTERLET_DEPLOY_MODE=${2:-Default} # Use Default if the second argument is not provided

# Build images for testing
IMAGE_TAG=e2e make images build

# Create the directory to store test results with timestamp
mkdir -p "_output/flaky-error-test"

# Loop to run tests
for (( i=0; i<RUN_TIMES; i++ ))
do
    # Create a kind cluster
    kind create cluster --name=e2e

    # Load test images into the kind cluster
    kind load docker-image --name=e2e quay.io/open-cluster-management/registration-operator:e2e
    kind load docker-image --name=e2e quay.io/open-cluster-management/registration:e2e
    kind load docker-image --name=e2e quay.io/open-cluster-management/work:e2e
    kind load docker-image --name=e2e quay.io/open-cluster-management/placement:e2e
    kind load docker-image --name=e2e quay.io/open-cluster-management/addon-manager:e2e

    echo "Running e2e test iteration $((i+1))/$RUN_TIMES with KlusterletDeployMode=$KLUSTERLET_DEPLOY_MODE"
    test_output=$(IMAGE_TAG=e2e KLUSTERLET_DEPLOY_MODE=$KLUSTERLET_DEPLOY_MODE make test-e2e 2>&1)
    test_exit_code=$?

    # Determine test result and update the respective list
    if [ $test_exit_code -eq 0 ]; then
        echo "Test $i passed"
    else
        echo "Test $i failed"
        timestamp=$(date +"%Y%m%d-%H%M%S")
        output_file="_output/flaky-error-test/e2e_test_$timestamp_${i}.output"
        echo "Exiting due to failure."
        exit 1
    fi

    # If the test is successful, delete clusters
    kind delete cluster --name=e2e
done

echo "All tests completed successfully."