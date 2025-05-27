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
