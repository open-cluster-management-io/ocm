#!/bin/bash

# Check if there are enough arguments
if [ "$#" -lt 1 ]; then
    echo "Usage: $0 <RUN_TIMES> [KlusterletDeployMode]"
    exit 1
fi

# Read command line arguments
# Example usage: IMAGE_TAG=e2e sh hack/e2e_flaky_error_test.sh 10 no
RUN_TIMES=$1
BUILD_IMAGES=${2:-yes} # Default to 'yes' if the third argument is not provided
KLUSTERLET_DEPLOY_MODE=${3:-Default} # Use Default if the second argument is not provided

# Conditionally build images for testing
if [ "$BUILD_IMAGES" = "yes" ]; then
    IMAGE_TAG=$IMAGE_TAG make images
fi

# Create the directory to store test results with timestamp
mkdir -p "_output/flaky-error-test"

# Loop to run tests
for (( i=1; i<=RUN_TIMES; i++ ))
do
    # Create a kind cluster
    kind create cluster --name=e2e

    # Load test images into the kind cluster
    kind load docker-image --name=e2e quay.io/open-cluster-management/registration-operator:$IMAGE_TAG
    kind load docker-image --name=e2e quay.io/open-cluster-management/registration:$IMAGE_TAG
    kind load docker-image --name=e2e quay.io/open-cluster-management/work:$IMAGE_TAG
    kind load docker-image --name=e2e quay.io/open-cluster-management/placement:$IMAGE_TAG
    kind load docker-image --name=e2e quay.io/open-cluster-management/addon-manager:$IMAGE_TAG

    # This is for addon-manager test: /workspaces/OCM/test/e2e/manifests/addon/addon_template.yaml
    docker pull quay.io/open-cluster-management/addon-examples:latest
    kind load docker-image --name=e2e quay.io/open-cluster-management/addon-examples:latest

    kind get kubeconfig --name=e2e > .kubeconfig

    echo "Running e2e test iteration $((i+1))/$RUN_TIMES with KlusterletDeployMode=$KLUSTERLET_DEPLOY_MODE"
    test_output=$(IMAGE_TAG=$IMAGE_TAG KLUSTERLET_DEPLOY_MODE=$KLUSTERLET_DEPLOY_MODE KUBECONFIG=.kubeconfig make test-e2e 2>&1)
    test_exit_code=$?

    echo "$test_output"

    # Determine test result and update the respective list
    if [ $test_exit_code -eq 0 ]; then
        echo "Test $i passed"
    else
        echo "Test $i failed"
        timestamp=$(date +"%Y%m%d-%H%M%S")
        output_file="_output/flaky-error-test/e2e_test_$timestamp_${i}.output"
        echo "$test_output" > "$output_file"
        echo "Exiting due to failure."
        exit 1
    fi

    # If the test is successful, delete clusters
    kind delete cluster --name=e2e
done

echo "All tests completed successfully."
