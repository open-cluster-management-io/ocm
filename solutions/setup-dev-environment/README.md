# Setup dev environment by kind

This scripts is to setup an OCM developer environment in your local machine with 3 kind clusters. The script will bootstrap 3 kind clusters on your local machine: hub, cluster1, and cluster2. The hub is used as the control plane of the OCM, and the other two clusters are registered on the hub as the managed clusters.

## Prerequisite

[kind](https://kind.sigs.k8s.io) must be installed on your local machine. The Kubernetes version must be >= 1.19, see [kind user guide](https://kind.sigs.k8s.io/docs/user/quick-start/#creating-a-cluster) for more details.

Download and install [clusteradm](https://github.com/open-cluster-management-io/clusteradm/releases). For Linux OS, run the following commands:

```
wget -qO- https://github.com/open-cluster-management-io/clusteradm/releases/latest/download/clusteradm_linux_amd64.tar.gz | sudo tar -xvz -C /usr/local/bin/

sudo chmod +x /usr/local/bin/clusteradm
```

## Setup the clusters

Run `./local-up.sh`, you will see two clusters registered on the hub cluster.

```
NAME       HUB ACCEPTED   MANAGED CLUSTER URLS      JOINED   AVAILABLE   AGE
cluster1   true           https://127.0.0.1:45325   True     True        116s
cluster2   true           https://127.0.0.1:45325   True     True        98s
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
    ```
    $ cat /proc/sys/fs/inotify/max_user_watches
    $ cat /proc/sys/fs/inotify/max_user_instances
    ``` 
* To temporarily increase the limits
    ```
    $ sudo sysctl fs.inotify.max_user_instances=8192
    $ sudo sysctl fs.inotify.max_user_watches=524288
    $ sudo sysctl -p
    ``` 
* To permanently increase the limits
    ```
    $ sudo echo "fs.inotify.max_user_watches=1024" >> /etc/sysctl.conf
    $ sudo echo "fs.inotify.max_user_instances=1024" >> /etc/sysctl.conf
    $ sudo sysctl -p /etc/sysctl.conf #reloads system settings to apply changes
    ``` 
Once you've increased the limits, delete the clusters already created and try again:
```
$ kind delete clusters hub cluster1 cluster2
$ ./setup-ocm.sh
```


</details> 