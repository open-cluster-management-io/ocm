package aws_irsa

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"net/url"
	"reflect"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/aws/retry"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/eks"
	"github.com/aws/aws-sdk-go-v2/service/eks/types"
	"github.com/aws/aws-sdk-go-v2/service/iam"
	iamtypes "github.com/aws/aws-sdk-go-v2/service/iam/types"
	"github.com/aws/smithy-go"
	"k8s.io/klog/v2"

	clusterv1 "open-cluster-management.io/api/cluster/v1"
	operatorv1 "open-cluster-management.io/api/operator/v1"

	"open-cluster-management.io/ocm/manifests"
	commonhelpers "open-cluster-management.io/ocm/pkg/common/helpers"
	"open-cluster-management.io/ocm/pkg/registration/register"
)

const (
	resourceNotFound        = "ResourceNotFoundException"
	errNoSuchEntity         = "NoSuchEntity"
	errEntityAlreadyExists  = "EntityAlreadyExists"
	trustPolicyTemplatePath = "managed-cluster-policy/TrustPolicy.tmpl"
)

type AWSIRSAHubDriver struct {
	hubClusterArn           string
	cfg                     aws.Config
	autoApprovedARNPatterns []*regexp.Regexp
	awsResourceTags         []string
	resourceManagement      operatorv1.AwsResourceManagement
}

func (a *AWSIRSAHubDriver) Accept(cluster *clusterv1.ManagedCluster) bool {
	if a.autoApprovedARNPatterns == nil {
		return true
	}
	if !a.allows(cluster) {
		return true
	}

	managedClusterArn := cluster.Annotations[operatorv1.ClusterAnnotationsKeyPrefix+"/"+ManagedClusterArn]
	for _, p := range a.autoApprovedARNPatterns {
		// Ensure the pattern matches the entire managed cluster ARN
		if p.FindString(managedClusterArn) == managedClusterArn && len(managedClusterArn) > 0 {
			return true
		}
	}
	return false
}

// Cleanup is run when the cluster is deleting or hubAcceptClient is set false
func (c *AWSIRSAHubDriver) Cleanup(ctx context.Context, managedCluster *clusterv1.ManagedCluster) error {
	_, isManagedClusterIamRoleSuffixPresent :=
		managedCluster.Annotations[operatorv1.ClusterAnnotationsKeyPrefix+"/"+ManagedClusterIAMRoleSuffix]
	_, isManagedClusterArnPresent := managedCluster.Annotations[operatorv1.ClusterAnnotationsKeyPrefix+"/"+ManagedClusterArn]

	logger := klog.FromContext(ctx)

	if !isManagedClusterArnPresent && !isManagedClusterIamRoleSuffixPresent {
		logger.V(4).Info("No Op Cleanup since managedcluster annotations are not present for awsirsa.")
		return nil
	}

	if c.resourceManagement == operatorv1.AwsResourceManagementUseExisting {
		logger.V(4).Info("Skipping cleanup of the IAM role and access entry because they are not managed by the hub",
			"ManagedCluster", managedCluster.Name)
		return nil
	}

	roleName, roleArn, err := getRoleNameAndArn(ctx, managedCluster, c.cfg, c.hubClusterArn)
	if err != nil {
		logger.V(4).Error(err, "Failed to getRoleNameAndArn")
		return err
	}

	eksClient := eks.NewFromConfig(c.cfg)
	_, hubClusterName := commonhelpers.GetAwsAccountIdAndClusterName(c.hubClusterArn)
	err = deleteAccessEntry(ctx, eksClient, roleArn, hubClusterName)
	if err != nil {
		return err
	}

	err = deleteIAMRole(ctx, c.cfg, roleName)
	if err != nil {
		return err
	}

	return nil
}

func (c *AWSIRSAHubDriver) Run(_ context.Context, _ int) {
	// noop
}

func (a *AWSIRSAHubDriver) allows(cluster *clusterv1.ManagedCluster) bool {
	_, isManagedClusterArnPresent := cluster.Annotations[operatorv1.ClusterAnnotationsKeyPrefix+"/"+ManagedClusterArn]
	_, isManagedClusterIAMRoleSuffixPresent := cluster.Annotations[operatorv1.ClusterAnnotationsKeyPrefix+"/"+ManagedClusterIAMRoleSuffix]
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
	if a.resourceManagement == operatorv1.AwsResourceManagementUseExisting {
		return verifyAccessEntry(ctx, eksClient, a.hubClusterArn, cluster)
	}
	hubClusterName, roleArn, err := createIAMRoleAndPolicy(ctx, a.hubClusterArn, cluster, a.cfg, a.awsResourceTags)
	if err != nil {
		return err
	}

	err = createAccessEntry(ctx, eksClient, roleArn, hubClusterName, cluster.Name, a.awsResourceTags)
	if err != nil {
		return err
	}

	return nil
}

// This function creates:
// 1. IAM Role and Trust Policy in the hub cluster IAM
// 2. Returns the hubClusterName and the roleArn to be used for Access Entry creation
func createIAMRoleAndPolicy(ctx context.Context, hubClusterArn string, managedCluster *clusterv1.ManagedCluster, cfg aws.Config,
	awsResourceTags []string) (string, string, error) {
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
		managedCluster.Annotations[operatorv1.ClusterAnnotationsKeyPrefix+"/"+ManagedClusterIAMRoleSuffix]
	managedClusterArn, isManagedClusterArnPresent := managedCluster.Annotations[operatorv1.ClusterAnnotationsKeyPrefix+"/"+ManagedClusterArn]

	hubAccountId, hubClusterName = commonhelpers.GetAwsAccountIdAndClusterName(hubClusterArn)
	managedClusterAccountId, managedClusterName = commonhelpers.GetAwsAccountIdAndClusterName(managedClusterArn)

	roleName, roleArn, err := getRoleNameAndArn(ctx, managedCluster, cfg, hubClusterArn)
	if err != nil {
		logger.V(4).Error(err, "Failed to getRoleNameAndArn")
		return hubClusterName, roleArn, err
	}

	if hubClusterArn != "" && isManagedClusterIamRoleSuffixPresent && isManagedClusterArnPresent {

		if err := validateIAMRoleSuffix(hubClusterArn, managedCluster); err != nil {
			return hubClusterName, roleArn, err
		}

		data := map[string]interface{}{
			"hubClusterArn":               hubClusterArn,
			"managedClusterAccountId":     managedClusterAccountId,
			"managedClusterPartition":     commonhelpers.GetAwsPartition(managedClusterArn),
			"managedClusterIamRoleSuffix": managedClusterIamRoleSuffix,
			"hubAccountId":                hubAccountId,
			"hubClusterName":              hubClusterName,
			"managedClusterName":          managedClusterName,
		}
		trustPolicy, err := renderTemplate(trustPolicyTemplatePath, data)
		if err != nil {
			logger.V(4).Error(err, "Failed to render template while creating IAM role and policy for ManagedCluster", "ManagedCluster", managedClusterName)
			return hubClusterName, roleArn, err
		}

		parsedTags, err := parseTagsForRolesAndPolicies(awsResourceTags)

		if err != nil {
			logger.V(4).Error(err, "Failed to parse tags for AWS roles and policies", "ManagedCluster", managedClusterName)
			return hubClusterName, roleArn, err
		}

		createRoleOutput, err = iamClient.CreateRole(ctx, &iam.CreateRoleInput{
			RoleName:                 aws.String(roleName),
			AssumeRolePolicyDocument: aws.String(trustPolicy),
			Tags:                     parsedTags,
		})
		if err != nil {
			if !(strings.Contains(err.Error(), errEntityAlreadyExists)) {
				logger.V(4).Error(err, "Failed to create IAM role %s for ManagedCluster", "IAMRole", roleName, "ManagedCluster", managedClusterName)
				return hubClusterName, roleArn, err
			}
			// The role name is stable, but the trust stored on an existing role may not be: IAM resolves
			// the principal to the role's unique ID, so a managed-cluster role that was deleted and
			// recreated no longer matches it. Reconcile the trust policy instead of assuming it is right.
			logger.V(4).Info("IAM role already exists for ManagedCluster, reconciling its trust policy", "IAMRole", roleName, "ManagedCluster", managedClusterName)
			if err := reconcileTrustPolicy(ctx, iamClient, roleName, trustPolicy, managedClusterName); err != nil {
				return hubClusterName, roleArn, err
			}
		} else {
			logger.V(4).Info("Role created successfully for ManagedCluster", "IAMRole", *createRoleOutput.Role.Arn, "ManagedCluster", managedClusterName)
		}
	}
	return hubClusterName, roleArn, nil
}

// validateIAMRoleSuffix rejects a ManagedCluster whose role suffix was computed against a different hub.
func validateIAMRoleSuffix(hubClusterArn string, managedCluster *clusterv1.ManagedCluster) error {
	managedClusterIamRoleSuffix := managedCluster.Annotations[operatorv1.ClusterAnnotationsKeyPrefix+"/"+ManagedClusterIAMRoleSuffix]
	managedClusterArn := managedCluster.Annotations[operatorv1.ClusterAnnotationsKeyPrefix+"/"+ManagedClusterArn]

	hubAccountId, hubClusterName := commonhelpers.GetAwsAccountIdAndClusterName(hubClusterArn)
	managedClusterAccountId, managedClusterName := commonhelpers.GetAwsAccountIdAndClusterName(managedClusterArn)

	hash := commonhelpers.Md5HashSuffix(hubAccountId, hubClusterName, managedClusterAccountId, managedClusterName)
	if hash != managedClusterIamRoleSuffix {
		return fmt.Errorf("HubClusterARN provided during join by ManagedCluster %s is different from the current hub cluster", managedClusterName)
	}
	return nil
}

// verifyAccessEntry checks that a pre-provisioned access entry maps the hub role to the identity the hub
// grants RBAC to. Without this check a missing or misconfigured entry only shows up as an authentication
// or authorization failure on the managed cluster.
func verifyAccessEntry(ctx context.Context, eksClient *eks.Client, hubClusterArn string, managedCluster *clusterv1.ManagedCluster) error {
	logger := klog.FromContext(ctx)

	if hubClusterArn == "" {
		return fmt.Errorf("--hub-cluster-arn is required when AWS resource management is %s",
			operatorv1.AwsResourceManagementUseExisting)
	}
	if err := validateIAMRoleSuffix(hubClusterArn, managedCluster); err != nil {
		return err
	}

	hubAccountId, hubClusterName := commonhelpers.GetAwsAccountIdAndClusterName(hubClusterArn)
	roleName := fmt.Sprintf("ocm-hub-%s", managedCluster.Annotations[operatorv1.ClusterAnnotationsKeyPrefix+"/"+ManagedClusterIAMRoleSuffix])
	roleArn := commonhelpers.BuildIamRoleArn(commonhelpers.GetAwsPartition(hubClusterArn), hubAccountId, roleName)
	expectedUsername := managedCluster.Name
	expectedGroup := fmt.Sprintf("open-cluster-management:%s", managedCluster.Name)

	output, err := eksClient.DescribeAccessEntry(ctx, &eks.DescribeAccessEntryInput{
		ClusterName:  aws.String(hubClusterName),
		PrincipalArn: aws.String(roleArn),
	})
	if err != nil {
		var notFound *types.ResourceNotFoundException
		if errors.As(err, &notFound) {
			return fmt.Errorf("access entry for principal %s not found on EKS cluster %s; with AWS resource management %s it must be "+
				"pre-created with username %q and Kubernetes group %q",
				roleArn, hubClusterName, operatorv1.AwsResourceManagementUseExisting, expectedUsername, expectedGroup)
		}
		return fmt.Errorf("failed to describe access entry for principal %s on EKS cluster %s: %w", roleArn, hubClusterName, err)
	}

	var username string
	var groups []string
	if output.AccessEntry != nil {
		username = aws.ToString(output.AccessEntry.Username)
		groups = output.AccessEntry.KubernetesGroups
	}
	if username != expectedUsername || !slices.Contains(groups, expectedGroup) {
		return fmt.Errorf("access entry for principal %s on EKS cluster %s has username %q and Kubernetes groups %v; "+
			"expected username %q and Kubernetes group %q", roleArn, hubClusterName, username, groups, expectedUsername, expectedGroup)
	}

	logger.V(4).Info("Pre-provisioned access entry verified", "AccessEntry", aws.ToString(output.AccessEntry.AccessEntryArn),
		"ManagedCluster", managedCluster.Name)
	return nil
}

// reconcileTrustPolicy makes sure an existing hub role trusts the managed cluster role that
// TrustPolicy.tmpl renders. Errors are returned rather than logged so the hub controller surfaces
// them on the ManagedCluster's HubAccepted condition.
//
// AccessDenied is the exception. Identity-creator policies written for v1.4.0 and earlier do not
// grant iam:GetRole or iam:UpdateAssumeRolePolicy, and failing here would mark every existing
// cluster not accepted after a hub upgrade. Those clusters joined with a trust policy that already
// works for IRSA, so the driver keeps it and logs what permission is missing.
func reconcileTrustPolicy(ctx context.Context, iamClient *iam.Client, roleName, desiredTrustPolicy, managedClusterName string) error {
	logger := klog.FromContext(ctx)

	getRoleOutput, err := iamClient.GetRole(ctx, &iam.GetRoleInput{RoleName: aws.String(roleName)})
	if isAccessDenied(err) {
		logTrustPolicySkipped(logger, err, "iam:GetRole", roleName, managedClusterName)
		return nil
	}
	if err != nil {
		return fmt.Errorf("failed to get IAM role %s to verify its trust policy for ManagedCluster %s: %w", roleName, managedClusterName, err)
	}

	var currentTrustPolicy string
	if getRoleOutput.Role != nil {
		currentTrustPolicy = aws.ToString(getRoleOutput.Role.AssumeRolePolicyDocument)
	}
	matches, err := trustPolicyMatches(currentTrustPolicy, desiredTrustPolicy)
	if err != nil {
		return fmt.Errorf("failed to compare trust policy of IAM role %s for ManagedCluster %s: %w", roleName, managedClusterName, err)
	}
	if matches {
		logger.V(4).Info("Trust policy of existing IAM role is up to date", "IAMRole", roleName, "ManagedCluster", managedClusterName)
		return nil
	}

	logger.Info("Trust policy of existing IAM role does not match the expected trust policy, updating it",
		"IAMRole", roleName, "ManagedCluster", managedClusterName)
	_, err = iamClient.UpdateAssumeRolePolicy(ctx, &iam.UpdateAssumeRolePolicyInput{
		RoleName:       aws.String(roleName),
		PolicyDocument: aws.String(desiredTrustPolicy),
	})
	if isAccessDenied(err) {
		logTrustPolicySkipped(logger, err, "iam:UpdateAssumeRolePolicy", roleName, managedClusterName)
		return nil
	}
	if err != nil {
		return fmt.Errorf("failed to update trust policy of IAM role %s for ManagedCluster %s: %w", roleName, managedClusterName, err)
	}
	logger.Info("Trust policy of existing IAM role updated", "IAMRole", roleName, "ManagedCluster", managedClusterName)
	return nil
}

func isAccessDenied(err error) bool {
	var apiErr smithy.APIError
	if !errors.As(err, &apiErr) {
		return false
	}
	code := apiErr.ErrorCode()
	return code == "AccessDenied" || code == "AccessDeniedException"
}

func logTrustPolicySkipped(logger klog.Logger, err error, permission, roleName, managedClusterName string) {
	logger.Error(err, "Skipping trust policy reconcile of existing IAM role: the hub's identity-creator role lacks "+
		permission+". Existing IRSA clusters keep working; add iam:GetRole and iam:UpdateAssumeRolePolicy to the "+
		"identity-creator policy so recreated managed-cluster roles and EKS Pod Identity spokes are trusted.",
		"IAMRole", roleName, "ManagedCluster", managedClusterName)
}

// trustPolicyMatches compares the trust policy IAM stores on a role with the one the driver wants.
// GetRole returns the document URL-encoded and IAM does not preserve formatting, so the comparison
// is structural. A stored document that cannot be decoded counts as a mismatch: updating it is the
// way back to a known state. The desired document is rendered by the driver, so failing to parse it
// is a bug and is reported as an error.
func trustPolicyMatches(current, desired string) (bool, error) {
	var desiredDoc interface{}
	if err := json.Unmarshal([]byte(desired), &desiredDoc); err != nil {
		return false, fmt.Errorf("desired trust policy is not valid JSON: %w", err)
	}

	decoded, err := url.QueryUnescape(current)
	if err != nil {
		return false, nil
	}
	var currentDoc interface{}
	if err := json.Unmarshal([]byte(decoded), &currentDoc); err != nil {
		return false, nil
	}
	return reflect.DeepEqual(currentDoc, desiredDoc), nil
}

func renderTemplate(argTemplate string, data interface{}) (args string, err error) {
	var t *template.Template
	var filebytes []byte
	filebytes, err = manifests.ManagedClusterPolicyManifestFiles.ReadFile(argTemplate)
	if err != nil {
		return
	}
	contents := string(filebytes)
	t, err = template.New(contents).Parse(contents)
	if err != nil {
		return
	}

	buf := &bytes.Buffer{}
	err = t.Execute(buf, data)
	if err != nil {
		return
	}
	args = buf.String()

	return
}

// This function creates access entry which allow access to an IAM role from outside the cluster
func createAccessEntry(ctx context.Context, eksClient *eks.Client, roleArn string, hubClusterName string, managedClusterName string,
	awsResourceTags []string) error {
	logger := klog.FromContext(ctx)
	tagsForAccessEntry, err := parseTagsForAccessEntry(awsResourceTags)
	if err != nil {
		logger.V(4).Error(err, "Failed to parse tags during AWS access entry creation", "ManagedCluster", managedClusterName)
		return err
	}

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

func deleteIAMRole(ctx context.Context, cfg aws.Config, roleName string) error {
	logger := klog.FromContext(ctx)

	iamClient := iam.NewFromConfig(cfg)

	_, err := iamClient.DeleteRole(ctx, &iam.DeleteRoleInput{
		RoleName: &roleName,
	})
	if err != nil {
		if !strings.Contains(err.Error(), errNoSuchEntity) {
			logger.V(4).Error(err, "Failed to delete Role", "Role", roleName)
			return err
		}
	} else {
		logger.V(4).Info("Role deleted successfully", "Role", roleName)
	}

	return nil
}

// The IAM role lives in the hub cluster's account, so its ARN has to be built in the
// same partition as hubClusterArn.
func getRoleNameAndArn(ctx context.Context, managedCluster *clusterv1.ManagedCluster, cfg aws.Config,
	hubClusterArn string) (string, string, error) {
	logger := klog.FromContext(ctx)

	managedClusterIamRoleSuffix :=
		managedCluster.Annotations[operatorv1.ClusterAnnotationsKeyPrefix+"/"+ManagedClusterIAMRoleSuffix]

	roleName := fmt.Sprintf("ocm-hub-%s", managedClusterIamRoleSuffix)

	creds, err := cfg.Credentials.Retrieve(ctx)
	if err != nil {
		logger.V(4).Error(err, "Failed to get IAM Credentials")
		return "", "", err
	}
	awsAccountId := creds.AccountID
	// Container credentials (e.g. EKS Pod Identity) only carry the account ID if the endpoint returns it.
	if awsAccountId == "" {
		awsAccountId, _ = commonhelpers.GetAwsAccountIdAndClusterName(hubClusterArn)
	}
	roleArn := commonhelpers.BuildIamRoleArn(commonhelpers.GetAwsPartition(hubClusterArn), awsAccountId, roleName)
	return roleName, roleArn, err
}

func deleteAccessEntry(ctx context.Context, eksClient *eks.Client, roleArn string, hubClusterName string) error {
	logger := klog.FromContext(ctx)

	params := &eks.DeleteAccessEntryInput{
		ClusterName:  aws.String(hubClusterName),
		PrincipalArn: aws.String(roleArn),
	}

	_, err := eksClient.DeleteAccessEntry(ctx, params)
	if err != nil {
		if strings.Contains(err.Error(), resourceNotFound) {
			logger.V(4).Error(err, "Access Entry already deleted for HubClusterName", "HubClusterName", hubClusterName)
			return nil
		}
		logger.V(4).Error(err, "Failed to delete Access Entry for HubClusterName", "HubClusterName", hubClusterName)
		return err
	} else {
		logger.V(4).Info("Access Entry deleted successfully for HubClusterName", "HubClusterName", hubClusterName)
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

func NewAWSIRSAHubDriver(ctx context.Context, hubClusterArn string, autoApprovedIdentityPatterns []string,
	awsResourceTags []string, resourceManagement string) (register.HubDriver, error) {
	logger := klog.FromContext(ctx)

	management := operatorv1.AwsResourceManagement(resourceManagement)
	switch management {
	case "":
		management = operatorv1.AwsResourceManagementManaged
	case operatorv1.AwsResourceManagementManaged, operatorv1.AwsResourceManagementUseExisting:
	default:
		return nil, fmt.Errorf("invalid AWS resource management %q, must be %s or %s", resourceManagement,
			operatorv1.AwsResourceManagementManaged, operatorv1.AwsResourceManagementUseExisting)
	}

	cfg, err := config.LoadDefaultConfig(ctx)
	if err != nil {
		logger.Error(err, "Failed to load aws config")
		return nil, err
	}

	compiledPatterns := make([]*regexp.Regexp, len(autoApprovedIdentityPatterns))
	for i, s := range autoApprovedIdentityPatterns {
		p, err := regexp.Compile(s)
		if err != nil {
			return nil, fmt.Errorf("failed to process auto approval ARN pattern: %w", err)
		}
		compiledPatterns[i] = p
	}

	awsIRSADriverForHub := &AWSIRSAHubDriver{
		hubClusterArn:           hubClusterArn,
		cfg:                     cfg,
		autoApprovedARNPatterns: compiledPatterns,
		awsResourceTags:         awsResourceTags,
		resourceManagement:      management,
	}

	return awsIRSADriverForHub, nil
}
