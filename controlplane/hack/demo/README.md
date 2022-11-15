# Get started 

## Prerequisites
1. Connect to an OpenShift cluster
2. Download [demo-magic.sh](https://raw.githubusercontent.com/paxtonhare/demo-magic/master/demo-magic.sh) and save it to this directory.
3. export HOST_POSTFIX environment variable to the route domain of your OpenShift cluster.

## Install

```bash
    ./next-generation.sh 2 (the number of control planes)
```

## Clean up

```bash
    ./next-generation.sh 2 clean
```