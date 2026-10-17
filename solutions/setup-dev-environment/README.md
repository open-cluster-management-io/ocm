# Setup dev environment by kind

This script sets up an OCM developer environment on your local machine with three kind clusters: hub, cluster1, and cluster2. The hub is used as the control plane of the OCM and is also registered as a managed cluster named local-cluster. The other two clusters are registered on the hub as managed clusters.

## Prerequisite

[kind](https://kind.sigs.k8s.io) must be installed on your local machine. The Kubernetes version must be >= 1.25, see [kind user guide](https://kind.sigs.k8s.io/docs/user/quick-start/#creating-a-cluster) for more details.

Download and install [clusteradm](https://github.com/open-cluster-management-io/clusteradm/releases). For Linux OS, run the following commands:

```bash
wget -qO- https://github.com/open-cluster-management-io/clusteradm/releases/latest/download/clusteradm_linux_amd64.tar.gz | sudo tar -xvz -C /usr/local/bin/

sudo chmod +x /usr/local/bin/clusteradm
```

For other platforms (macOS, arm64), download the appropriate binary from the [clusteradm releases page](https://github.com/open-cluster-management-io/clusteradm/releases).

## Configuration

| Environment variable | Default | Purpose |
| --- | --- | --- |
| `HUB`, `CLUSTER1`, `CLUSTER2` | `hub`, `cluster1`, `cluster2` | Names of the three kind clusters. The hub is also registered as `local-cluster`. Existing names cause an error. |
| `BUILD_IMAGES` | `false` | `true` builds all five OCM images from this checkout and loads them into all three clusters. Otherwise, clusteradm selects its default released images. |
| `IMAGE_REGISTRY` | `quay.io/open-cluster-management` | Image name prefix in local build mode. No registry push is needed. |
| `IMAGE_TAG` | `local` in local build mode | Tag shared by the locally built images. `latest` is rejected so Pods use the loaded images. |
| `CONTAINER_CLI` | Docker, falling back to Podman | Container runtime for local builds and kind. An explicit `KIND_EXPERIMENTAL_PROVIDER` is honored when `CONTAINER_CLI` is unset. |
| `HUB_FEATURE_GATES` | clusteradm defaults | Comma-separated feature gates for the hub, for example `ClusterProfile=true`. |
| `SPOKE_FEATURE_GATES` | clusteradm defaults | Comma-separated feature gates for every managed cluster, including the hub's agent, for example `ClusterClaim=false`. |

Local build mode additionally requires Go and make. It uses the kind nodes'
architecture for both the container images and Go binaries, and applies this
checkout's operator RBAC to match the locally built operators. All clusters remain
available after the script finishes or fails.

## Setup the clusters

Run `./local-up.sh`, you will see three clusters registered on the hub cluster.

```text
NAME            HUB ACCEPTED   MANAGED CLUSTER URLS      JOINED   AVAILABLE   AGE
cluster1        true           https://127.0.0.1:45325   True     True        116s
cluster2        true           https://127.0.0.1:45325   True     True        98s
local-cluster   true           https://127.0.0.1:45325   True     True        78s
```

<details>
<summary>Known issue when running the setup script on a Linux environment</summary>

You may run into this issue when trying to create multiple clusters
```
Creating cluster "cluster2" ...
✓ Ensuring node image (kindest/node:v1.26.3) 🖼
✗ Preparing nodes 📦  
ERROR: failed to create cluster: could not find a log line that matches "Reached target .*Multi-User System.*|detected cgroup v1
```
This might be caused by kernel limits such as number of open files, inotifiy watches, etc.
To solve this, try increasing your `max_user_instances` and `max_user_watches`:

* To see the current limits
    ```bash
    cat /proc/sys/fs/inotify/max_user_watches
    cat /proc/sys/fs/inotify/max_user_instances
    ``` 
* To temporarily increase the limits
    ```bash
    sudo sysctl fs.inotify.max_user_instances=8192
    sudo sysctl fs.inotify.max_user_watches=524288
    ``` 
* To permanently increase the limits
    ```bash
    echo "fs.inotify.max_user_watches=524288" | sudo tee -a /etc/sysctl.conf
    echo "fs.inotify.max_user_instances=8192" | sudo tee -a /etc/sysctl.conf
    sudo sysctl -p /etc/sysctl.conf
    ``` 
Once you've increased the limits, delete the clusters already created and try again:
```bash
kind delete clusters hub cluster1 cluster2
./local-up.sh
```


</details>
