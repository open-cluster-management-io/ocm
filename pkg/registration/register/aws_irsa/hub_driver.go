package aws_irsa

import (
	"bytes"
	"context"
	"fmt"
	"html/template"
	"log"
	"regexp"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/aws/retry"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/eks"
	"github.com/aws/aws-sdk-go-v2/service/eks/types"
	"github.com/aws/aws-sdk-go-v2/service/iam"
	"k8s.io/klog/v2"
	clusterv1 "open-cluster-management.io/api/cluster/v1"
	v1 "open-cluster-management.io/api/cluster/v1"
	"open-cluster-management.io/ocm/manifests"
	commonhelpers "open-cluster-management.io/ocm/pkg/common/helpers"
	"open-cluster-management.io/ocm/pkg/registration/register"
)

type AWSIRSAHubDriver struct {
	hubClusterArn           string
	cfg                     aws.Config
	autoApprovalAwsPatterns []string
}

func (a *AWSIRSAHubDriver) allowAccept(ctx context.Context, cluster *clusterv1.ManagedCluster) (bool, error) {
	managedClusterArn := cluster.Annotations["agent.open-cluster-management.io/managed-cluster-arn"]
	managedClusterIAMRoleSuffix := cluster.Annotations["agent.open-cluster-management.io/managed-cluster-iam-role-suffix"]
	if managedClusterIAMRoleSuffix != "" && managedClusterArn != "" {
		for _, AutoApprovalAwsPattern := range a.autoApprovalAwsPatterns {
			re, err := regexp.Compile(AutoApprovalAwsPattern)
			if err != nil {
				fmt.Println("Failed to process the approved arn pattern for aws irsa auto approval", err)
				return false, err
			}
			if re.MatchString(managedClusterArn) {
				return true, nil
				// TODO
			} else {
				log.Println("Managed cluster does not match any allowed patterns")
			}
		}
	}
	return false, nil
}

func (a *AWSIRSAHubDriver) Accept(ctx context.Context, cluster *clusterv1.ManagedCluster) (bool, error) {
	log.Println(a.autoApprovalAwsPatterns)
	log.Println(cluster.Annotations["agent.open-cluster-management.io/managed-cluster-arn"])
	if a.autoApprovalAwsPatterns == nil {
		log.Println("managed cluster auto accept as no patterns present.")
		return true, nil
	}
	allowAccept, err := a.allowAccept(ctx, cluster)
	if err != nil {
		fmt.Println("Failed to process the approved arn pattern for aws irsa auto approval", err)
		return false, err
	}
	if err == nil && allowAccept {
		log.Println("managed cluster auto accepted after pattern matching.")
		return true, nil
	}
	log.Println("managed cluster not auto accepted for aws_irsa.")
	return false, nil
}

func (a *AWSIRSAHubDriver) Cleanup(ctx context.Context, cluster *clusterv1.ManagedCluster) error {
	// No Op
	return nil
}

func (a *AWSIRSAHubDriver) Run(ctx context.Context, workers int) {
	// No Op
}

func (a *AWSIRSAHubDriver) allows(cluster *clusterv1.ManagedCluster) bool {
	_, isManagedClusterArnPresent := cluster.Annotations["agent.open-cluster-management.io/managed-cluster-arn"]
	_, isManagedClusterIAMRoleSuffixPresent := cluster.Annotations["agent.open-cluster-management.io/managed-cluster-iam-role-suffix"]
	if isManagedClusterArnPresent && isManagedClusterIAMRoleSuffixPresent {
		return true
	}
	return false
}

func (a *AWSIRSAHubDriver) CreatePermissions(ctx context.Context, cluster *clusterv1.ManagedCluster) error {
	if !a.allows(cluster) {
		return nil
	}
	klog.Infof("ManagedCluster %s is joined using aws-irsa registration-auth", cluster.Name)

	// Create an EKS client
	eksClient := eks.NewFromConfig(a.cfg)
	hubClusterName, roleArn, err := createIAMRoleAndPolicy(ctx, a.hubClusterArn, cluster, a.cfg)
	if err != nil {
		return err
	}

	err = createAccessEntry(ctx, eksClient, roleArn, hubClusterName, cluster.Name)
	if err != nil {
		return err
	}

	return nil
}

// This function creates:
// 1. IAM Role and Policy in the hub cluster IAM
// 2. Returns the hubClusterName and the roleArn to be used for Access Entry creation
func createIAMRoleAndPolicy(ctx context.Context, hubClusterArn string, managedCluster *v1.ManagedCluster, cfg aws.Config) (string, string, error) {
	var managedClusterIamRoleSuffix string
	var createRoleOutput *iam.CreateRoleOutput
	var hubClusterName string
	var managedClusterName string
	var hubAccountId string
	var managedClusterAccountId string

	// Create an IAM client
	iamClient := iam.NewFromConfig(cfg)

	managedClusterIamRoleSuffix, isManagedClusterIamRoleSuffixPresent :=
		managedCluster.Annotations["agent.open-cluster-management.io/managed-cluster-iam-role-suffix"]
	managedClusterArn, isManagedClusterArnPresent := managedCluster.Annotations["agent.open-cluster-management.io/managed-cluster-arn"]

	roleName := fmt.Sprintf("ocm-hub-%s", managedClusterIamRoleSuffix)
	policyName := roleName
	creds, err := cfg.Credentials.Retrieve(ctx)
	if err != nil {
		klog.Errorf("Failed to get IAM Credentials")
		return hubClusterName, "", err
	}
	awsAccountId := creds.AccountID
	roleArn := fmt.Sprintf("arn:aws:iam::%s:role/%s", awsAccountId, roleName)
	policyArn := fmt.Sprintf("arn:aws:iam::%s:policy/%s", awsAccountId, policyName)
	hubAccountId, hubClusterName = commonhelpers.GetAwsAccountIdAndClusterName(hubClusterArn)
	managedClusterAccountId, managedClusterName = commonhelpers.GetAwsAccountIdAndClusterName(managedClusterArn)

	if hubClusterArn != "" && isManagedClusterIamRoleSuffixPresent && isManagedClusterArnPresent {

		// Check if hash is the same
		hash := commonhelpers.Md5HashSuffix(hubAccountId, hubClusterName, managedClusterAccountId, managedClusterName)
		if hash != managedClusterIamRoleSuffix {
			err := fmt.Errorf("HubClusterARN provided during join by ManagedCluster %s is different from the current hub cluster", managedClusterName)
			return hubClusterName, roleArn, err
		}

		templateFiles := []string{"managed-cluster-policy/AccessPolicy.tmpl", "managed-cluster-policy/TrustPolicy.tmpl"}
		data := map[string]interface{}{
			"hubClusterArn":               hubClusterArn,
			"managedClusterAccountId":     managedClusterAccountId,
			"managedClusterIamRoleSuffix": managedClusterIamRoleSuffix,
			"hubAccountId":                hubAccountId,
			"hubClusterName":              hubClusterName,
			"managedClusterName":          managedClusterName,
		}
		renderedTemplates, err := renderTemplates(templateFiles, data)
		if err != nil {
			klog.Errorf("Failed to render templates while creating IAM role and policy for ManagedCluster %s", managedClusterName)
			return hubClusterName, roleArn, err
		}

		createRoleOutput, err = iamClient.CreateRole(ctx, &iam.CreateRoleInput{
			RoleName:                 aws.String(roleName),
			AssumeRolePolicyDocument: aws.String(renderedTemplates[1]),
		})
		if err != nil {
			// Ignore error when role already exists as we will always create the same role
			if !(strings.Contains(err.Error(), "EntityAlreadyExists")) {
				klog.Errorf("Failed to create IAM role %s for ManagedCluster %s", roleName, managedClusterName)
				return hubClusterName, roleArn, err
			} else {
				klog.Infof("Ignore IAM role %s creation error for ManagedCluster %s as it already exists", roleName, managedClusterName)
			}
		} else {
			klog.Infof("Role %s created successfully for ManagedCluster %s", *createRoleOutput.Role.Arn, managedClusterName)
		}

		createPolicyResult, err := iamClient.CreatePolicy(ctx, &iam.CreatePolicyInput{
			PolicyDocument: aws.String(renderedTemplates[0]),
			PolicyName:     aws.String(policyName),
		})
		if err != nil {
			if !(strings.Contains(err.Error(), "EntityAlreadyExists")) {
				klog.Errorf("Failed to create IAM Policy %s for ManagedCluster %s", policyName, managedClusterName)
				return hubClusterName, roleArn, err
			} else {
				klog.Infof("Ignore IAM policy %s creation error for ManagedCluster %s as it already exists", policyName, managedClusterName)
			}
		} else {
			klog.Infof("Policy %s created successfully for ManagedCluster %s", *createPolicyResult.Policy.Arn, managedClusterName)
		}

		_, err = iamClient.AttachRolePolicy(ctx, &iam.AttachRolePolicyInput{
			PolicyArn: aws.String(policyArn),
			RoleName:  aws.String(roleName),
		})
		if err != nil {
			klog.Errorf("Unable to attach policy %s to role %s for ManagedCluster %s", policyArn, roleName, managedClusterName)
			return hubClusterName, roleArn, err
		} else {
			klog.Infof("Successfully attached IAM Policy %s to Role %s for ManagedCluster %s", policyArn, roleName, managedClusterName)
		}
	}
	return hubClusterName, roleArn, nil
}

func renderTemplates(argTemplates []string, data interface{}) (args []string, err error) {
	var t *template.Template
	var filebytes []byte
	for _, arg := range argTemplates {
		filebytes, err = manifests.ManagedClusterPolicyManifestFiles.ReadFile(arg)
		if err != nil {
			args = nil
			return
		}
		contents := string(filebytes)
		t, err = template.New(contents).Parse(contents)
		if err != nil {
			args = nil
			return
		}

		buf := &bytes.Buffer{}
		err = t.Execute(buf, data)
		if err != nil {
			args = nil
			return
		}
		args = append(args, buf.String())
	}

	return
}

// This function creates access entry which allow access to an IAM role from outside the cluster
func createAccessEntry(ctx context.Context, eksClient *eks.Client, roleArn string, hubClusterName string, managedClusterName string) error {
	params := &eks.CreateAccessEntryInput{
		ClusterName:      aws.String(hubClusterName),
		PrincipalArn:     aws.String(roleArn),
		Username:         aws.String(managedClusterName),
		KubernetesGroups: []string{fmt.Sprintf("open-cluster-management:%s", managedClusterName)},
	}

	createAccessEntryOutput, err := eksClient.CreateAccessEntry(ctx, params, func(opts *eks.Options) {
		opts.Retryer = retry.AddWithErrorCodes(retry.NewStandard(func(o *retry.StandardOptions) {
			o.MaxAttempts = 10
			o.Backoff = retry.NewExponentialJitterBackoff(100 * time.Second)
		}), (*types.InvalidParameterException)(nil).ErrorCode())
	})
	if err != nil {
		if !(strings.Contains(err.Error(), "ResourceInUseException")) {
			klog.Errorf("Failed to create Access entry for managed cluster %s", managedClusterName)
			return err
		} else {
			klog.Infof("Ignore Access Entry creation for managed cluster %s as it is already in use", managedClusterName)
		}
	} else {
		klog.Infof("Access entry %s created successfully for managed cluster %s", *createAccessEntryOutput.AccessEntry.AccessEntryArn, managedClusterName)
	}
	return nil
}

func NewAWSIRSAHubDriver(ctx context.Context, hubClusterArn string, AutoApprovalAwsPatterns []string) (register.HubDriver, error) {
	logger := klog.FromContext(ctx)
	cfg, err := config.LoadDefaultConfig(ctx)
	if err != nil {
		logger.Error(err, "Failed to load aws config")
		return nil, err
	}
	awsIRSADriverForHub := &AWSIRSAHubDriver{
		hubClusterArn:           hubClusterArn,
		cfg:                     cfg,
		autoApprovalAwsPatterns: AutoApprovalAwsPatterns,
	}
	return awsIRSADriverForHub, nil
}
