# Manually join Hub and Spoke using AWS-based authentication

This guide provides list of manual steps on how we can manually join AWS EKS clusters as Hub and Spoke using OCM.

# Background

We are working on a new feature to run OCM natively on EKS with AWS IAM based authentication. More details, on why and how, can be found in [enhancement proposal](https://github.com/open-cluster-management-io/enhancements/blob/main/enhancements/sig-architecture/105-aws-iam-registration/README.md). This purpose of this document is to:
- Share with community, 
- sthe proof of design proposed in [enhancement proposal](https://github.com/open-cluster-management-io/enhancements/blob/main/enhancements/sig-architecture/105-aws-iam-registration/README.md)
- Get early feedback


>  **Note:**
> 
> This solution uses [AWS IRSA](https://docs.aws.amazon.com/eks/latest/userguide/iam-roles-for-service-accounts.html) for out going authentication from within an EKS cluster to AWS, and [EKS access entries](https://docs.aws.amazon.com/eks/latest/userguide/access-entries.html) for incoming authentication from outside world to inside the EKS cluster.

While the implementation of this feature is in progress, in the hub and spoke side components. The hub and spoke can be joined using AWS IAM based authentication by running following steps to manipulate hub and spoke components manually:

1. Login to hub and spoke clusters in 2 separate shell sessions with admin access to aws as well as EKS cluster and set following env vars in both.
    > **Note:** The md5 hash is generated as described [here](https://github.com/open-cluster-management-io/enhancements/blob/main/enhancements/sig-architecture/105-aws-iam-registration/README.md?plain=1#L249). [This](https://www.md5hashgenerator.com/) website can be used to generate md5 hash for these steps.
   ```bash
    export HUB_CLUSTER_NAME=<hub-cluster-name>
    export SPOKE_CLUSTER_NAME=<hub-cluster-name>
    export HUB_ACCOUNT_ID=<hub-cluster-account-id>
    export SPOKE_ACCOUNT_ID=<spoke-cluster-account-id>
    export HUB_ROLE_NAME=ocm-hub-<md5hash>
    export SPOKE_ROLE_NAME=ocm-managed-cluster-<md5hash>
    export HUB_POLICY_NAME=<IAM-policy-created-on-hub-IAM-for-spoke>
    export SPOKE_POLICY_NAME=<IAM-policy-created-on-spoke-IAM-for-spoke>
    export SPOKE_OIDC_PROVIDER_ID=<spoke-cluster-AWS-oidc-provider>
    export HUB_REGION=<hub-cluster-region>
    export SPOKE_REGION=<spoke-cluster-region>
   ```
   
   e.g.
   ```bash
    export HUB_CLUSTER_NAME=hub
    export SPOKE_CLUSTER_NAME=spoke
    export HUB_ACCOUNT_ID=123456789012
    export SPOKE_ACCOUNT_ID=567890123456
    export HUB_ROLE_NAME=ocm-hub-138c525cf3b7f74ba3b945e7847792cc
    export SPOKE_ROLE_NAME=ocm-managed-cluster-138c525cf3b7f74ba3b945e7847792cc
    export HUB_POLICY_NAME=hub
    export SPOKE_POLICY_NAME=spoke
    export SPOKE_OIDC_PROVIDER_ID=BE6D831CFA22823093D17E608EE0048C
    export HUB_REGION=us-west-2
    export SPOKE_REGION=us-west-2
   ```

2. Login to spoke AWS account and create IAM role, policy and tags on spoke IAM. Go to the folder containing this markdown file in this [repository](https://github.com/open-cluster-management-io/open-cluster-management-io.github.io) and run following commands:
   ```bash
   sed -e "s/PROVIDER_ID/$SPOKE_OIDC_PROVIDER_ID/g" -e "s/ACCOUNT_ID/$SPOKE_ACCOUNT_ID/g" -e "s/REGION/$SPOKE_REGION/g" templates/Template-Spoke-Role-Trust-Policy.json > templates/Spoke-Role-Trust-Policy.json
   aws iam create-role --role-name $SPOKE_ROLE_NAME --assume-role-policy-document file://templates/Spoke-Role-Trust-Policy.json

   sed -e "s/ACCOUNT_ID/$HUB_ACCOUNT_ID/g" -e "s/ROLE_NAME/$HUB_ROLE_NAME/g" templates/Template-Spoke-Role-Permission-Policy.json > templates/Spoke-Role-Permission-Policy.json
   aws iam put-role-policy --role-name $SPOKE_ROLE_NAME --policy-name $SPOKE_POLICY_NAME --policy-document file://templates/Spoke-Role-Permission-Policy.json

   aws iam tag-role --role-name $SPOKE_ROLE_NAME --tags '[{"Key":"hub_cluster_account_id", "Value":"'$HUB_ACCOUNT_ID'"},{"Key":"hub_cluster_name", "Value":"'$HUB_CLUSTER_NAME'"},{"Key":"managed_cluster_account_id", "Value":"'$SPOKE_ACCOUNT_ID'"},{"Key":"managed_cluster_name", "Value":"'$SPOKE_CLUSTER_NAME'"}]'
   ```

3. Login to hub EKS clusters and initialize hub:
   ```shell
   clusteradm init  --wait
   
   # export hub apiserver url and token from the output of above command
   # it will be used by spoke
   export TOKEN=""
   export HUB_API_SERVER="https://C66946AA519C4818E2189CE5A9324551.wk7.us-west-2.eks.amazonaws.com"
   ``` 

4. Login to spoke EKS clusters and join with hub, note the new command line options on second line:
   ```shell
   clusteradm join --hub-token $TOKEN --hub-apiserver $HUB_API_SERVER --wait --cluster-name $SPOKE_CLUSTER_NAME --singleton \
   --bundle-version latest --registration-auth awsirsa --hub-cluster-arn arn:aws:eks:$HUB_REGION:"$HUB_ACCOUNT_ID":cluster/$HUB_CLUSTER_NAME
   ```

5. Making aws-cli available in klusterlet-agent:
   ```shell
   # Scaling down klusterlet operator 
   kubectl -n open-cluster-management patch deployment klusterlet --type='json' -p='[{"op": "replace", "path": "/spec/replicas", "value":0}]'
   
   # Prepare klusterlet-agent to host aws-cli
   # Adds initContainer to klusterlet-agent deployment
   # Add aws-cli to PATH
   
   kubectl -n open-cluster-management-agent patch deployment klusterlet-agent --type='json' -p='[
     {
       "op": "add",
       "path": "/spec/template/spec/volumes/-",
       "value": {
         "name": "awscli",
         "emptyDir": {}
       }
     },
     {
       "op": "add",
       "path": "/spec/template/spec/containers/0/volumeMounts/-",
       "value": {
         "name": "awscli",
         "mountPath": "/awscli"
       }
     },
     {
       "op": "add",
       "path": "/spec/template/spec/initContainers",
       "value": [
         {
           "name": "load-awscli",
           "image": "amazon/aws-cli:latest",
           "command": ["cp", "-vr", "/usr/local/aws-cli/v2/current/dist", "/awscli"],
           "volumeMounts": [
             {
               "name": "awscli",
               "mountPath": "/awscli"
             }
           ]
         }
       ]
     },
     {
       "op": "add",
       "path": "/spec/template/spec/containers/0/env/-",
       "value": {
         "name": "PATH",
         "value": "/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin:/awscli/dist"
       }
     }
   ]'

   ```

6. Login into hub aws account and create hub role using following script:
   ```shell
   sed -e "s/ROLE_NAME/$SPOKE_ROLE_NAME/g" -e "s/SPOKE_ACCOUNT_ID/$SPOKE_ACCOUNT_ID/g" -e "s/HUB_ACCOUNT_ID/$HUB_ACCOUNT_ID/g" -e "s/HUB_CLUSTER_NAME/$HUB_CLUSTER_NAME/g" -e "s/SPOKE_CLUSTER_NAME/$SPOKE_CLUSTER_NAME/g" templates/Template-Hub-Role-Trust-Policy.json > templates/Hub-Role-Trust-Policy.json
   aws iam create-role --role-name $HUB_ROLE_NAME --assume-role-policy-document file://templates/Hub-Role-Trust-Policy.json
   
   sed -e "s/REGION/$HUB_REGION/g" -e "s/ACCOUNT_ID/$HUB_ACCOUNT_ID/g" -e "s/CLUSTER_NAME/$HUB_CLUSTER_NAME/g" templates/Template-Hub-Role-Permission-Policy.json > templates/Hub-Role-Permission-Policy.json
   aws iam put-role-policy --role-name $HUB_ROLE_NAME --policy-name $HUB_POLICY_NAME --policy-document file://templates/Hub-Role-Permission-Policy.json
   ```

7. Accept the ManagedCluster registration request on hub cluster:
   ```shell
   clusteradm accept --cluster $SPOKE_CLUSTER_NAME
   ```
   > Note: Ignore the following error as there is no CSR in aws registration flow:
   > 
   > Error: no csr is approved yet for cluster `spoke-cluster-name`

8. Create access entry on hub EKS cluster using the below commands:
   ```shell 
   aws eks list-access-entries --cluster $HUB_CLUSTER_NAME --region=$HUB_REGION
   aws eks create-access-entry --cluster-name $HUB_CLUSTER_NAME --region=$HUB_REGION --principal-arn arn:aws:iam::"$HUB_ACCOUNT_ID":role/$HUB_ROLE_NAME --username $SPOKE_CLUSTER_NAME --kubernetes-groups open-cluster-management:$SPOKE_CLUSTER_NAME
   aws eks list-access-entries --cluster $HUB_CLUSTER_NAME --region=$HUB_REGION | grep -i $HUB_ROLE_NAME
   ```

9. Generate the secret called `hub-kubeconfig-secret` in `open-cluster-management-agent` namespace using above kubeconfig:
   ```shell
   HUB_KUBECONFIG=$(aws eks update-kubeconfig --name $HUB_CLUSTER_NAME --kubeconfig /awscli/kubeconfig.kubeconfig --role-arn arn:aws:iam::"$HUB_ACCOUNT_ID":role/$HUB_ROLE_NAME --dry-run)
   
   AGENT_NAME_ENCODED=$(kubectl get klusterlet klusterlet -o jsonpath='{.metadata.uid}' | tr -d '\n' | base64 | tr -d '\n')
   SPOKE_CLUSTER_NAME_ENCODED=$(echo -n "$SPOKE_CLUSTER_NAME" | base64 | tr -d '\n')
   HUB_KUBECONFIG_ENCODED=$(echo -n "$HUB_KUBECONFIG" | base64 | tr -d '\n')
   HUB_KUBECONFIG_ENCODED_ESCAPED=$(printf '%s' "$HUB_KUBECONFIG_ENCODED" | sed 's/[&/\|]/\\&/g')
   
   sed -e "s|\${AGENT_NAME_ENCODED}|$AGENT_NAME_ENCODED|g" \
       -e "s|\${SPOKE_CLUSTER_NAME_ENCODED}|$SPOKE_CLUSTER_NAME_ENCODED|g" \
       -e "s|\${HUB_KUBECONFIG_ENCODED}|$HUB_KUBECONFIG_ENCODED_ESCAPED|g" \
       templates/Template-hub-kubeconfig-secret.yaml > hubKubeconfigSecret.yaml
   
   # Updating the clusterName to "hub" to make it same as bootstrap-kubeconfig
   # to pass a validation in ocm. Install yq, if missing, for following command
   NEW_CLUSTER_NAME="hub"
   yq eval "
   (.clusters[].name = \"${NEW_CLUSTER_NAME}\") |
   (.contexts[].context.cluster = \"${NEW_CLUSTER_NAME}\") |
   del(.users[].user.exec.env)
   " -i "hubKubeconfigSecret.yaml"
       
   kubectl apply -f hubKubeconfigSecret.yaml
   ```

10. Create a sample manifestwork in hub. Confirm that resources are pushed to spoke.
