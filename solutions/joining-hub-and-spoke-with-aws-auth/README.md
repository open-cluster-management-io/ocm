# Join Hub and Spoke using AWS-based authentication

This guide provides list of steps to join AWS EKS clusters as Hub and Spoke using OCM.

# Background

This feature allows you to run OCM natively on EKS with AWS IAM based authentication. More details, on why and how, can be found in [enhancement proposal](https://github.com/open-cluster-management-io/enhancements/blob/main/enhancements/sig-architecture/105-aws-iam-registration/README.md).

>  **Note:**
> 
> This solution uses [AWS IRSA](https://docs.aws.amazon.com/eks/latest/userguide/iam-roles-for-service-accounts.html) for out going authentication from within an EKS cluster to AWS, and [EKS access entries](https://docs.aws.amazon.com/eks/latest/userguide/access-entries.html) for incoming authentication from outside world to inside the EKS cluster.

The hub and spoke can be joined using AWS IAM based authentication by running following steps:

1. Login to hub and spoke clusters in 2 separate shell sessions with admin access to aws as well as EKS cluster and set following env vars in both.
    > **Note:** The md5 hash is generated as described [here](https://github.com/open-cluster-management-io/enhancements/blob/main/enhancements/sig-architecture/105-aws-iam-registration/README.md?plain=1#L249). [This](https://www.md5hashgenerator.com/) website can be used to generate md5 hash for these steps.
   ```bash
    export HUB_CLUSTER_NAME=<hub-cluster-name>
    export SPOKE_CLUSTER_NAME=<hub-cluster-name>
    export HUB_ACCOUNT_ID=<hub-cluster-account-id>
    export SPOKE_ACCOUNT_ID=<spoke-cluster-account-id>
    export HUB_ROLE_NAME=ocm-hub-<md5hash>
    export SPOKE_ROLE_NAME=ocm-managed-cluster-<md5hash>
    export IDENTITY_CREATOR_ROLE_NAME=<hub-cluster-name>_managed-cluster-identity-creator
    export HUB_POLICY_NAME=<IAM-policy-created-on-hub-IAM-for-hub>
    export SPOKE_POLICY_NAME=<IAM-policy-created-on-spoke-IAM-for-spoke>
    export HUB_OIDC_PROVIDER_ID=<spoke-cluster-AWS-oidc-provider>
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
    export IDENTITY_CREATOR_ROLE_NAME=hub_managed-cluster-identity-creator
    export HUB_POLICY_NAME=hub
    export SPOKE_POLICY_NAME=spoke
    export HUB_OIDC_PROVIDER_ID=AE6D831CFA22823093D17E608EE0048C 
    export SPOKE_OIDC_PROVIDER_ID=BE6D831CFA22823093D17E608EE0048C
    export HUB_REGION=us-west-2
    export SPOKE_REGION=us-west-2
   ```

2. Login to spoke AWS account and create prerequisite IAM role, policy and tags on spoke IAM. Go to the folder containing this markdown file in this [repository](https://github.com/open-cluster-management-io/ocm/tree/main/solutions/joining-hub-and-spoke-with-aws-auth-manually) and run following commands:
   ```bash
   sed -e "s/PROVIDER_ID/$SPOKE_OIDC_PROVIDER_ID/g" -e "s/ACCOUNT_ID/$SPOKE_ACCOUNT_ID/g" -e "s/REGION/$SPOKE_REGION/g" templates/Template-Spoke-Role-Trust-Policy.json > templates/Spoke-Role-Trust-Policy.json
   aws iam create-role --role-name $SPOKE_ROLE_NAME --assume-role-policy-document file://templates/Spoke-Role-Trust-Policy.json

   sed -e "s/ACCOUNT_ID/$HUB_ACCOUNT_ID/g" -e "s/ROLE_NAME/$HUB_ROLE_NAME/g" templates/Template-Spoke-Role-Permission-Policy.json > templates/Spoke-Role-Permission-Policy.json
   aws iam put-role-policy --role-name $SPOKE_ROLE_NAME --policy-name $SPOKE_POLICY_NAME --policy-document file://templates/Spoke-Role-Permission-Policy.json

   aws iam tag-role --role-name $SPOKE_ROLE_NAME --tags '[{"Key":"hub_cluster_account_id", "Value":"'$HUB_ACCOUNT_ID'"},{"Key":"hub_cluster_name", "Value":"'$HUB_CLUSTER_NAME'"},{"Key":"managed_cluster_account_id", "Value":"'$SPOKE_ACCOUNT_ID'"},{"Key":"managed_cluster_name", "Value":"'$SPOKE_CLUSTER_NAME'"}]'
   ```

3. Login into hub aws account and create identity-creator hub role using following script:
   ```shell
   sed -e "s/HUB_ACCOUNT_ID/$HUB_ACCOUNT_ID/g" -e "s/HUB_REGION/$HUB_REGION/g" -e "s/HUB_OIDC_PROVIDER_ID/$HUB_OIDC_PROVIDER_ID/g" templates/Template-Identity-Creator-Trust-Policy.json > templates/Identity-Creator-Trust-Policy.json
   aws iam create-role --role-name $IDENTITY_CREATOR_ROLE_NAME --assume-role-policy-document file://templates/Identity-Creator-Trust-Policy.json

   sed -e "s/HUB_ACCOUNT_ID/$HUB_ACCOUNT_ID/g" -e "s/HUB_REGION/$HUB_REGION/g" templates/Template-Identity-Creator-Permission-Policy.json > templates/Identity-Creator-Permission-Policy.json
   aws iam put-role-policy --role-name $IDENTITY_CREATOR_ROLE_NAME --policy-name $HUB_POLICY_NAME --policy-document file://templates/Identity-Creator-Permission-Policy.json
   ```

4. Login to hub EKS clusters and initialize hub:
   ```shell
   clusteradm init --registration-drivers awsirsa \
   --feature-gates ManagedClusterAutoApproval=true \
   --auto-approved-arn-patterns "arn:aws:eks:us-west-2:123412341234:cluster/.*" --wait
   
   # export hub apiserver url and token from the output of above command
   # it will be used by spoke
   export TOKEN=""
   export HUB_API_SERVER="https://C66946AA519C4818E2189CE5A9324551.wk7.us-west-2.eks.amazonaws.com"
   ``` 

5. Login to spoke EKS clusters and join with hub, note the new command line options on second line:
   ```shell
   clusteradm join --hub-token $TOKEN --hub-apiserver $HUB_API_SERVER \
   --wait --cluster-name $SPOKE_CLUSTER_NAME --singleton \
   --registration-auth awsirsa \
   --hub-cluster-arn arn:aws:eks:$HUB_REGION:"$HUB_ACCOUNT_ID":cluster/$HUB_CLUSTER_NAME
   ```

6. Accept the ManagedCluster registration request on hub cluster:
   ```shell
   clusteradm accept --cluster $SPOKE_CLUSTER_NAME
   ```

7. Create a sample manifestwork in hub. Confirm that resources are pushed to spoke.

# Using EKS Pod Identity instead of IRSA

The `awsirsa` registration driver can also get its AWS credentials from [EKS Pod Identity](https://docs.aws.amazon.com/eks/latest/userguide/pod-identities.html). With Pod Identity, neither cluster needs an IAM OIDC provider. Configure both the hub and the spoke to use it.

Set `credentialSource: PodIdentity` on both CRs. The operators then stop annotating the OCM service accounts with `eks.amazonaws.com/role-arn`. That annotation makes EKS inject web identity credentials, which take precedence over Pod Identity in the AWS default credential chain. If the annotation is already there, for example on an existing IRSA setup, the operators remove it and restart the affected pods.

The default is `IRSA`, so existing setups keep working unchanged.

1. Install the [EKS Pod Identity Agent](https://docs.aws.amazon.com/eks/latest/userguide/pod-id-agent-setup.html) add-on on the hub and spoke clusters.

2. On the spoke, create `$SPOKE_ROLE_NAME` as in step 2 above, but use the Pod Identity trust policy. The permission policy and the four role tags are still required, because the hub role's trust policy checks the tags through `aws:PrincipalTag` conditions:
   ```bash
   aws iam create-role --role-name $SPOKE_ROLE_NAME --assume-role-policy-document file://templates/Template-Pod-Identity-Trust-Policy.json
   # then run the put-role-policy and tag-role commands from step 2
   ```
   Associate the role with the agent service accounts. In `Default` mode these are `klusterlet-registration-sa` and `klusterlet-work-sa`. In `Singleton` mode only `klusterlet-work-sa` is used.
   ```bash
   for sa in klusterlet-registration-sa klusterlet-work-sa; do
     aws eks create-pod-identity-association --cluster-name $SPOKE_CLUSTER_NAME --region $SPOKE_REGION \
       --namespace open-cluster-management-agent --service-account $sa \
       --role-arn arn:aws:iam::$SPOKE_ACCOUNT_ID:role/$SPOKE_ROLE_NAME
   done
   ```

3. On the hub, create `$IDENTITY_CREATOR_ROLE_NAME` as in step 3 above, but use the Pod Identity trust policy, then associate it with `registration-controller-sa`:
   ```bash
   aws iam create-role --role-name $IDENTITY_CREATOR_ROLE_NAME --assume-role-policy-document file://templates/Template-Pod-Identity-Trust-Policy.json
   # then run the put-role-policy command from step 3
   aws eks create-pod-identity-association --cluster-name $HUB_CLUSTER_NAME --region $HUB_REGION \
     --namespace open-cluster-management-hub --service-account registration-controller-sa \
     --role-arn arn:aws:iam::$HUB_ACCOUNT_ID:role/$IDENTITY_CREATOR_ROLE_NAME
   ```

4. Initialize the hub and join the spoke as in steps 4 and 5, then set `credentialSource` on both CRs:
   ```shell
   # hub
   kubectl patch clustermanager cluster-manager --type json -p \
     '[{"op":"add","path":"/spec/registrationConfiguration/registrationDrivers/0/awsirsa/credentialSource","value":"PodIdentity"}]'

   # spoke
   kubectl patch klusterlet klusterlet --type merge -p \
     '{"spec":{"registrationConfiguration":{"registrationDriver":{"awsIrsa":{"credentialSource":"PodIdentity"}}}}}'
   ```
   The resulting configuration looks like this:
   ```yaml
   # ClusterManager
   spec:
     registrationConfiguration:
       registrationDrivers:
       - authType: awsirsa
         awsirsa:
           hubClusterArn: arn:aws:eks:us-west-2:123456789012:cluster/hub
           credentialSource: PodIdentity
   ---
   # Klusterlet
   spec:
     registrationConfiguration:
       registrationDriver:
         authType: awsirsa
         awsIrsa:
           hubClusterArn: arn:aws:eks:us-west-2:123456789012:cluster/hub
           managedClusterArn: arn:aws:eks:us-west-2:567890123456:cluster/spoke
           credentialSource: PodIdentity
   ```
   Adjust the `registrationDrivers` index if `awsirsa` is not the first driver.

5. Continue with steps 6 and 7.
