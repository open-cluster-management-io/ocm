#!/bin/bash

# Get the number of tests to run from the first command line argument
n=$1

# Check if the test count was provided
if [[ -z "$n" ]]; then
    echo "Usage: $0 <number-of-tests>"
    exit 1
fi

# Create the directory to store test results with timestamp
mkdir -p "_output/flaky-error-test"

# Loop to run the tests n times
for (( i=1; i<=n; i++ ))
do
    echo "Running test $i of $n..."
    # Run the test command and capture the output
    test_output=$(make test-integration 2>&1)
    test_exit_code=$?

    # Determine test result and update the respective list
    if [ $test_exit_code -eq 0 ]; then
        echo "Test $i passed"
    else
        echo "Test $i failed"
        timestamp=$(date +"%Y%m%d-%H%M%S")
        output_file="_output/flaky-error-test/integration_test_$timestamp_${i}.output"
        echo "$test_output" > "$output_file"
        echo "Test $i result stored in $output_file"
        exit 1
    fi
done
