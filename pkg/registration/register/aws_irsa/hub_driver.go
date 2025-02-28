package aws_irsa

import (
	"bytes"
	"context"
	"fmt"
	"html/template"
	"regexp"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/aws/retry"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/eks"
	"github.com/aws/aws-sdk-go-v2/service/eks/types"
	"github.com/aws/aws-sdk-go-v2/service/iam"
	iamtypes "github.com/aws/aws-sdk-go-v2/service/iam/types"
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
	autoApprovedARNPatterns []*regexp.Regexp
	tags                    []string
}

func (a *AWSIRSAHubDriver) Accept(cluster *clusterv1.ManagedCluster) bool {
	if a.autoApprovedARNPatterns == nil {
		return true
	}
	if !a.allows(cluster) {
		return true
	}

	managedClusterArn := cluster.Annotations["agent.open-cluster-management.io/managed-cluster-arn"]
	for _, p := range a.autoApprovedARNPatterns {
		// Ensure the pattern matches the entire managed cluster ARN
		if p.FindString(managedClusterArn) == managedClusterArn && len(managedClusterArn) > 0 {
			return true
		}
	}
	return false
}

// Cleanup is run when the cluster is deleting or hubAcceptClient is set false
func (c *AWSIRSAHubDriver) Cleanup(_ context.Context, _ *clusterv1.ManagedCluster) error {
	// noop
	return nil
}

func (c *AWSIRSAHubDriver) Run(_ context.Context, _ int) {
	// noop
}

func (a *AWSIRSAHubDriver) allows(cluster *clusterv1.ManagedCluster) bool {
	_, isManagedClusterArnPresent := cluster.Annotations["agent.open-cluster-management.io/managed-cluster-arn"]
	_, isManagedClusterIAMRoleSuffixPresent := cluster.Annotations["agent.open-cluster-management.io/managed-cluster-iam-role-suffix"]
	return isManagedClusterArnPresent && isManagedClusterIAMRoleSuffixPresent
}

func (a *AWSIRSAHubDriver) CreatePermissions(ctx context.Context, cluster *clusterv1.ManagedCluster) error {
	logger := klog.FromContext(ctx)
	if !a.allows(cluster) {
		return nil
	}
	logger.V(4).Info("ManagedCluster is joined using aws-irsa registration-auth", "ManagedCluster", cluster.Name)

	// Create an EKS client
	eksClient := eks.NewFromConfig(a.cfg)
	hubClusterName, roleArn, err := createIAMRoleAndPolicy(ctx, a.hubClusterArn, cluster, a.cfg, a.tags)
	if err != nil {
		return err
	}

	err = createAccessEntry(ctx, eksClient, roleArn, hubClusterName, cluster.Name, a.tags)
	if err != nil {
		return err
	}

	return nil
}

// This function creates:
// 1. IAM Role and Policy in the hub cluster IAM
// 2. Returns the hubClusterName and the roleArn to be used for Access Entry creation
func createIAMRoleAndPolicy(ctx context.Context, hubClusterArn string, managedCluster *v1.ManagedCluster, cfg aws.Config, tags []string) (string, string, error) {
	logger := klog.FromContext(ctx)
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
			logger.V(4).Error(err, "Failed to render templates while creating IAM role and policy for ManagedCluster", "ManagedCluster", managedClusterName)
			return hubClusterName, roleArn, err
		}

		parsedTags, err := parseTagsForRolesAndPolicies(tags)

		createRoleOutput, err = iamClient.CreateRole(ctx, &iam.CreateRoleInput{
			RoleName:                 aws.String(roleName),
			AssumeRolePolicyDocument: aws.String(renderedTemplates[1]),
			Tags:                     parsedTags,
		})
		if err != nil {
			// Ignore error when role already exists as we will always create the same role
			if !(strings.Contains(err.Error(), "EntityAlreadyExists")) {
				logger.V(4).Error(err, "Failed to create IAM role %s for ManagedCluster", "IAMRole", roleName, "ManagedCluster", managedClusterName)
				return hubClusterName, roleArn, err
			} else {
				logger.V(4).Info("Ignore IAM role creation error for ManagedCluster as it already exists", "IAMRole", roleName, "ManagedCluster", managedClusterName)
			}
		} else {
			logger.V(4).Info("Role created successfully for ManagedCluster", "IAMRole", *createRoleOutput.Role.Arn, "ManagedCluster", managedClusterName)
		}

		createPolicyResult, err := iamClient.CreatePolicy(ctx, &iam.CreatePolicyInput{
			PolicyDocument: aws.String(renderedTemplates[0]),
			PolicyName:     aws.String(policyName),
		})
		if err != nil {
			if !(strings.Contains(err.Error(), "EntityAlreadyExists")) {
				logger.V(4).Error(err, "Failed to create IAM Policy for ManagedCluster", "IAMPolicy", policyName, "ManagedCluster", managedClusterName)
				return hubClusterName, roleArn, err
			} else {
				logger.V(4).Info("Ignore IAM policy creation error for ManagedCluster as it already exists", "IAMPolicy", policyName, "ManagedCluster", managedClusterName)
			}
		} else {
			logger.V(4).Info("Policy created successfully for ManagedCluster", "IAMPolicy", *createPolicyResult.Policy.Arn, "ManagedCluster", managedClusterName)
		}

		_, err = iamClient.AttachRolePolicy(ctx, &iam.AttachRolePolicyInput{
			PolicyArn: aws.String(policyArn),
			RoleName:  aws.String(roleName),
		})
		if err != nil {
			logger.V(4).Error(err, "Unable to attach policy to role for ManagedCluster",
				"IAMPolicy", policyName, "IAMRole", roleName, "ManagedCluster", managedClusterName)
			return hubClusterName, roleArn, err
		} else {
			logger.V(4).Info("Successfully attached IAM Policy to Role for ManagedCluster",
				"IAMPolicy", policyName, "IAMRole", roleName, "ManagedCluster", managedClusterName)
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
func createAccessEntry(ctx context.Context, eksClient *eks.Client, roleArn string, hubClusterName string, managedClusterName string, tags []string) error {
	logger := klog.FromContext(ctx)
	tagsForAccessEntry, err := parseTagsForAccessEntry(tags)
	params := &eks.CreateAccessEntryInput{
		ClusterName:      aws.String(hubClusterName),
		PrincipalArn:     aws.String(roleArn),
		Username:         aws.String(managedClusterName),
		KubernetesGroups: []string{fmt.Sprintf("open-cluster-management:%s", managedClusterName)},
		Tags:             tagsForAccessEntry,
	}

	createAccessEntryOutput, err := eksClient.CreateAccessEntry(ctx, params, func(opts *eks.Options) {
		opts.Retryer = retry.AddWithErrorCodes(retry.NewStandard(func(o *retry.StandardOptions) {
			o.MaxAttempts = 10
			o.Backoff = retry.NewExponentialJitterBackoff(100 * time.Second)
		}), (*types.InvalidParameterException)(nil).ErrorCode())
	})
	if err != nil {
		if !(strings.Contains(err.Error(), "ResourceInUseException")) {
			logger.V(4).Error(err, "Failed to create Access entry for managed cluster", "ManagedCluster", managedClusterName)
			return err
		} else {
			logger.V(4).Info("Ignore Access Entry creation for managed cluster as it is already in use",
				"ManagedCluster", managedClusterName)
		}
	} else {
		logger.V(4).Info("Access entry created successfully for managed cluster",
			"AccessEntry", *createAccessEntryOutput.AccessEntry.AccessEntryArn, "ManagedCluster", managedClusterName)
	}
	return nil
}

func parseTagsForAccessEntry(tags []string) (map[string]string, error) {

	parsedTags := map[string]string{}
	for _, tag := range tags {
		splitTag := strings.Split(tag, "=")
		if len(splitTag) != 2 {
			return nil, fmt.Errorf("missing value in the tag")
		}
		key, value := splitTag[0], splitTag[1]
		parsedTags[key] = value
	}
	return parsedTags, nil
}

func parseTagsForRolesAndPolicies(tags []string) ([]iamtypes.Tag, error) {

	var parsedTags []iamtypes.Tag

	for _, tag := range tags {
		splitTag := strings.Split(tag, "=")
		if len(splitTag) != 2 {
			return nil, fmt.Errorf("missing value from tag")
		}
		key, value := splitTag[0], splitTag[1]
		parsedTags = append(parsedTags, iamtypes.Tag{
			Key:   &key,
			Value: &value,
		})
	}
	return parsedTags, nil
}

func NewAWSIRSAHubDriver(ctx context.Context, hubClusterArn string, autoApprovedIdentityPatterns []string, tags []string) (register.HubDriver, error) {
	logger := klog.FromContext(ctx)
	cfg, err := config.LoadDefaultConfig(ctx)
	if err != nil {
		logger.Error(err, "Failed to load aws config")
		return nil, err
	}

	compiledPatterns := make([]*regexp.Regexp, len(autoApprovedIdentityPatterns))
	for i, s := range autoApprovedIdentityPatterns {
		p, err := regexp.Compile(s)
		if err != nil {
			return nil, fmt.Errorf("Failed to process auto approval ARN pattern: %w", err)
		}
		compiledPatterns[i] = p
	}

	awsIRSADriverForHub := &AWSIRSAHubDriver{
		hubClusterArn:           hubClusterArn,
		cfg:                     cfg,
		autoApprovedARNPatterns: compiledPatterns,
		tags:                    tags,
	}

	return awsIRSADriverForHub, nil
}
