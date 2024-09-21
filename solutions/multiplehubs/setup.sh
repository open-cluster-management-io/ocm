# In this demo, we are going to use 3 clusters: hub1, hub2 and agent.
#
# We will enable MultipleHubs feature on agent, the agent will first be registered to hub1.
# Then we manually reject the registration from agent on hub1 by setting the managed cluster `hubAcceptsClient` to false.
# After that, we will observe that the agent will automatically register to hub2.
# Next, We set the managed cluster `hubAcceptsClient` to true on hub1 and delete the hub2, we will observe that the agent will automatically register back to hub1.

kind create cluster --name hub1
kind create cluster --name hub2
kind create cluster --name agent

# Init the hub1
clusteradm init --wait --bundle-version=latest --context kind-hub1
joinhub1=$(clusteradm get token --context kind-hub1 | grep clusteradm)

# Join the agent into the hub1
$(echo ${joinhub1} --bundle-version=latest --force-internal-endpoint-lookup --wait --context kind-agent | sed "s/<cluster_name>/agent/g")
clusteradm accept --context kind-hub1 --clusters agent --wait

# Init the hub2
clusteradm init --wait --bundle-version=latest --context kind-hub2

# Prepare the hub1, hub2 bootstrap kubeconfigs
kind get kubeconfig --name hub1 --internal > hub1.kubeconfig
kind get kubeconfig --name hub2 --internal > hub2.kubeconfig

# Save hub1, hub2 bootstrap kubeconfigs as secrets in the open-cluster-management-agent ns
kubectl --context kind-agent create secret generic hub1-kubeconfig -n open-cluster-management-agent --from-file=kubeconfig=hub1.kubeconfig
kubectl --context kind-agent create secret generic hub2-kubeconfig -n open-cluster-management-agent --from-file=kubeconfig=hub2.kubeconfig

# Enable AutoApproval on hub1, hub2
# The username is kubernetes-admin, which is the default admin user in kind clusters
kubectl --context kind-hub1 patch clustermanager cluster-manager --type=merge -p '{"spec":{"registrationConfiguration":{"featureGates":[
{"feature": "ManagedClusterAutoApproval", "mode": "Enable"}], "autoApproveUsers":["kubernetes-admin"]}}}'
kubectl --context kind-hub2 patch clustermanager cluster-manager --type=merge -p '{"spec":{"registrationConfiguration":{"featureGates":[
{"feature": "ManagedClusterAutoApproval", "mode": "Enable"}], "autoApproveUsers":["kubernetes-admin"]}}}'

# Enable MultipleHubs feature on agent
kubectl --context kind-agent patch klusterlet klusterlet --type=merge -p '{"spec":{"registrationConfiguration":{"featureGates":[
{"feature": "MultipleHubs", "mode": "Enable"}], "bootstrapKubeConfigs":{"type":"LocalSecrets", "localSecretsConfig":{"hubConnectionTimeoutSeconds":180,"kubeConfigSecrets":[{"name":"hub1-kubeconfig"},{"name":"hub2-kubeconfig"}]}}}}}'
