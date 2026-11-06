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
   <a id="spoke-role-tags"></a>
   > **Note:** The four tags are required. The hub role's trust policy checks them through `aws:PrincipalTag` conditions, so without them the spoke cannot assume the hub role. If you manage the spoke role with IaC, set exactly these keys and values:
   >
   > | Key | Value |
   > |---|---|
   > | `hub_cluster_account_id` | `$HUB_ACCOUNT_ID` |
   > | `hub_cluster_name` | `$HUB_CLUSTER_NAME` |
   > | `managed_cluster_account_id` | `$SPOKE_ACCOUNT_ID` |
   > | `managed_cluster_name` | `$SPOKE_CLUSTER_NAME` |

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

   # the spoke needs the hub CA to join (see step 5); copy hub-ca.crt to the spoke session
   aws eks describe-cluster --name $HUB_CLUSTER_NAME --region $HUB_REGION \
     --query cluster.certificateAuthority.data --output text | base64 -d > hub-ca.crt
   ``` 

5. Login to spoke EKS clusters and join with hub, note the new command line options on second line. EKS doesn't publish the `kube-public/cluster-info` ConfigMap that `clusteradm join` normally reads the hub CA from, so pass the `hub-ca.crt` retrieved in step 4 with `--ca-file`. Without it, the join fails with `x509: certificate signed by unknown authority`.
   ```shell
   clusteradm join --hub-token $TOKEN --hub-apiserver $HUB_API_SERVER --ca-file hub-ca.crt \
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

Set `credentialSource: PodIdentity` on both CRs. The operators then stop annotating the OCM service accounts with `eks.amazonaws.com/role-arn`, so the service accounts no longer depend on IRSA. If the annotation is already there, for example on an existing IRSA setup, the operators remove it and restart the affected pods.

The default is `IRSA`, so existing setups keep working unchanged. While a Pod Identity association exists for a service account, EKS gives its pods Pod Identity credentials even if the IRSA annotation is present. To switch back to IRSA, set `credentialSource: IRSA`, delete the associations, and restart the OCM pods.

1. Install the [EKS Pod Identity Agent](https://docs.aws.amazon.com/eks/latest/userguide/pod-id-agent-setup.html) add-on on the hub and spoke clusters.

2. On the spoke, create `$SPOKE_ROLE_NAME` as in step 2 above, but use the Pod Identity trust policy. The trust policy only accepts the agent service accounts in `open-cluster-management-agent` on `$SPOKE_CLUSTER_NAME`, using the session tags that Pod Identity adds. The permission policy and the [four role tags](#spoke-role-tags) are still required:
   ```bash
   sed -e "s/SPOKE_REGION/$SPOKE_REGION/g" -e "s/SPOKE_ACCOUNT_ID/$SPOKE_ACCOUNT_ID/g" -e "s/SPOKE_CLUSTER_NAME/$SPOKE_CLUSTER_NAME/g" \
     templates/Template-Spoke-Pod-Identity-Trust-Policy.json > templates/Spoke-Pod-Identity-Trust-Policy.json
   aws iam create-role --role-name $SPOKE_ROLE_NAME --assume-role-policy-document file://templates/Spoke-Pod-Identity-Trust-Policy.json
   # then run the put-role-policy and tag-role commands from step 2
   ```
   Associate the role with the agent service accounts. In `Default` mode these are `klusterlet-registration-sa` and `klusterlet-work-sa`. In `Singleton` mode only `klusterlet-work-sa` is used.

   Keep session tags enabled, which is the default. Don't pass `--disable-session-tags`: the trust policy relies on the tags to restrict which pods can use the role, so associations without them are denied. The tags are transitive, so the hub role's trust policy also grants `sts:TagSession` to the spoke role.
   ```bash
   for sa in klusterlet-registration-sa klusterlet-work-sa; do
     aws eks create-pod-identity-association --cluster-name $SPOKE_CLUSTER_NAME --region $SPOKE_REGION \
       --namespace open-cluster-management-agent --service-account $sa \
       --role-arn arn:aws:iam::$SPOKE_ACCOUNT_ID:role/$SPOKE_ROLE_NAME
   done
   ```

3. On the hub, create `$IDENTITY_CREATOR_ROLE_NAME` as in step 3 above, but use the Pod Identity trust policy, which only accepts `registration-controller-sa` in `open-cluster-management-hub` on `$HUB_CLUSTER_NAME`. Then associate the role with that service account:
   ```bash
   sed -e "s/HUB_REGION/$HUB_REGION/g" -e "s/HUB_ACCOUNT_ID/$HUB_ACCOUNT_ID/g" -e "s/HUB_CLUSTER_NAME/$HUB_CLUSTER_NAME/g" \
     templates/Template-Identity-Creator-Pod-Identity-Trust-Policy.json > templates/Identity-Creator-Pod-Identity-Trust-Policy.json
   aws iam create-role --role-name $IDENTITY_CREATOR_ROLE_NAME --assume-role-policy-document file://templates/Identity-Creator-Pod-Identity-Trust-Policy.json
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

Creating a Pod Identity association requires `iam:PassRole` on the role. Grant `iam:PassRole` for `$SPOKE_ROLE_NAME` and `$IDENTITY_CREATOR_ROLE_NAME` only to the administrators who set up OCM. The trust policies limit which service accounts can use the roles, and `iam:PassRole` limits who can create the associations.

# Pre-provisioned hub resources (`resourceManagement: UseExisting`)

By default (`resourceManagement: Managed`), the hub creates an `ocm-hub-<md5hash>` IAM role and an EKS access entry for each accepted cluster, and deletes both when the cluster is removed. This is why the identity-creator role needs IAM write permissions.

With `resourceManagement: UseExisting`, the hub never creates, changes, or deletes IAM roles or access entries. You provision both ahead of time, for example with IaC. The hub only calls `eks:DescribeAccessEntry` to check that the access entry exists and maps to the expected username and group. If the check fails, the error shows up on the ManagedCluster's `HubAccepted` condition. This works with both IRSA and Pod Identity for `registration-controller-sa`.

For each managed cluster, create:

- **The hub role** `ocm-hub-<md5hash>` in the hub account. `<md5hash>` is the lowercase hex md5 of `<hubAccountId>#<hubClusterName>#<spokeAccountId>#<spokeClusterName>`, the same suffix the spoke role uses:
  ```bash
  printf '%s' "$HUB_ACCOUNT_ID#$HUB_CLUSTER_NAME#$SPOKE_ACCOUNT_ID#$SPOKE_CLUSTER_NAME" | md5sum   # macOS: md5 -q
  ```
  Its trust policy must match [`templates/Template-Hub-Role-Trust-Policy.json`](templates/Template-Hub-Role-Trust-Policy.json), which is what the hub applies in `Managed` mode. The role needs no permission policy.
  ```bash
  sed -e "s/SPOKE_ACCOUNT_ID/$SPOKE_ACCOUNT_ID/g" -e "s/HUB_ACCOUNT_ID/$HUB_ACCOUNT_ID/g" \
    -e "s/HUB_CLUSTER_NAME/$HUB_CLUSTER_NAME/g" -e "s/SPOKE_CLUSTER_NAME/$SPOKE_CLUSTER_NAME/g" \
    -e "s/ROLE_NAME/$SPOKE_ROLE_NAME/g" templates/Template-Hub-Role-Trust-Policy.json > templates/Hub-Role-Trust-Policy.json
  aws iam create-role --role-name $HUB_ROLE_NAME --assume-role-policy-document file://templates/Hub-Role-Trust-Policy.json
  ```
  The trust policy allows `sts:AssumeRole` and `sts:TagSession`. Spokes that use Pod Identity keep session tags on, as described [above](#using-eks-pod-identity-instead-of-irsa), and those tags are transitive, so the hub role must grant `sts:TagSession`.

  IAM stores the spoke role's unique ID in the trust policy. If the spoke role is deleted and recreated, re-apply the hub role's trust policy. In `Managed` mode the hub does this itself.
- **The access entry** on the hub cluster, of type `STANDARD`, with username `<spokeClusterName>`, Kubernetes group `open-cluster-management:<spokeClusterName>`, and no access policy:
  ```bash
  aws eks create-access-entry --cluster-name $HUB_CLUSTER_NAME --region $HUB_REGION \
    --principal-arn arn:aws:iam::$HUB_ACCOUNT_ID:role/$HUB_ROLE_NAME \
    --username $SPOKE_CLUSTER_NAME --kubernetes-groups open-cluster-management:$SPOKE_CLUSTER_NAME
  ```

In step 3, use [`templates/Template-Identity-Creator-Permission-Policy-UseExisting.json`](templates/Template-Identity-Creator-Permission-Policy-UseExisting.json) for the identity-creator role. It grants only `eks:DescribeAccessEntry`, scoped to the hub cluster's access entries:
```bash
sed -e "s/HUB_ACCOUNT_ID/$HUB_ACCOUNT_ID/g" -e "s/HUB_REGION/$HUB_REGION/g" -e "s/HUB_CLUSTER_NAME/$HUB_CLUSTER_NAME/g" \
  templates/Template-Identity-Creator-Permission-Policy-UseExisting.json > templates/Identity-Creator-Permission-Policy.json
```

Then set `resourceManagement` on the ClusterManager:
```yaml
spec:
  registrationConfiguration:
    registrationDrivers:
    - authType: awsirsa
      awsirsa:
        hubClusterArn: arn:aws:eks:us-west-2:123456789012:cluster/hub
        resourceManagement: UseExisting
```

Things to keep in mind:

- The `tags` setting is ignored in this mode. Tag the role and access entry yourself.
- Deleting a ManagedCluster or setting `hubAcceptsClient: false` still removes the hub-side RBAC, but it leaves the IAM role and access entry in place. Revoke AWS-level access through your IaC.
- Clusters that joined in `Managed` mode keep the role and access entry the hub created for them. After you switch to `UseExisting`, the hub no longer deletes them, so import them into your IaC or delete them yourself.
