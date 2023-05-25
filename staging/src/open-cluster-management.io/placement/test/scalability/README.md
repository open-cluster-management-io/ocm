# Scalability test

## Motivation

Add test cases for scalability, help developers to measure the impact of their fixes on scalability.

## Design Details

### Create a placement with many managedclusters

This test case is used to check whether a placement with many managedclusters can be created within expected time or not. One placement with many managedclusters will be created, and there will be enough candidate managedclusters to ensure there will be competing among these candidates.

### Create/update many placements with many managedclusters

This test case is used to check whether many placements, each with many managedclusters, can be created/updated within expected time or not. Placements with many managedclusters will be created first to check time cost for creating, and then the count of managedclusters will be updated to update the placements, this is used to check the time cost for updating.
