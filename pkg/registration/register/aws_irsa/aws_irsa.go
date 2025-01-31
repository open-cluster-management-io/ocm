package aws_irsa

import (
	"bytes"
	"context"
	"fmt"
	"html/template"
	"log"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/aws/retry"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/eks"
	"github.com/aws/aws-sdk-go-v2/service/eks/types"
	"github.com/aws/aws-sdk-go-v2/service/iam"
	"github.com/openshift/library-go/pkg/controller/factory"
	"github.com/openshift/library-go/pkg/operator/events"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/tools/cache"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"

	clusterv1 "open-cluster-management.io/api/cluster/v1"
	v1 "open-cluster-management.io/api/cluster/v1"
	operatorv1 "open-cluster-management.io/api/operator/v1"

	"open-cluster-management.io/ocm/manifests"
	"open-cluster-management.io/ocm/pkg/common/helpers"
	commonhelpers "open-cluster-management.io/ocm/pkg/common/helpers"
	"open-cluster-management.io/ocm/pkg/registration/register"
)

//TODO: Remove these constants in once we have the function fully implemented for the AWSIRSADriver

const (
	// TLSKeyFile is the name of tls key file in kubeconfigSecret
	TLSKeyFile = "tls.key"
	// TLSCertFile is the name of the tls cert file in kubeconfigSecret
	TLSCertFile                 = "tls.crt"
	ManagedClusterArn           = "managed-cluster-arn"
	ManagedClusterIAMRoleSuffix = "managed-cluster-iam-role-suffix"
)

type AWSIRSADriver struct {
	name                     string
	managedClusterArn        string
	hubClusterArn            string
	managedClusterRoleSuffix string
}

func (c *AWSIRSADriver) Process(
	ctx context.Context, controllerName string, secret *corev1.Secret, additionalSecretData map[string][]byte,
	recorder events.Recorder, opt any) (*corev1.Secret, *metav1.Condition, error) {

	awsOption, ok := opt.(*AWSOption)
	if !ok {
		return nil, nil, fmt.Errorf("option type is not correct")
	}

	isApproved, err := awsOption.AWSIRSAControl.isApproved(c.name)
	if err != nil {
		return nil, nil, err
	}
	if !isApproved {
		return nil, nil, nil
	}

	recorder.Eventf("EKSRegistrationRequestApproved", "An EKS registration request is approved for %s", controllerName)
	return secret, nil, nil
}

func (c *AWSIRSADriver) BuildKubeConfigFromTemplate(kubeConfig *clientcmdapi.Config) *clientcmdapi.Config {
	hubClusterAccountId, hubClusterName := helpers.GetAwsAccountIdAndClusterName(c.hubClusterArn)
	awsRegion := helpers.GetAwsRegion(c.hubClusterArn)
	kubeConfig.AuthInfos = map[string]*clientcmdapi.AuthInfo{register.DefaultKubeConfigAuth: {
		Exec: &clientcmdapi.ExecConfig{
			APIVersion: "client.authentication.k8s.io/v1beta1",
			Command:    "aws",
			Args: []string{
				"--region",
				awsRegion,
				"eks",
				"get-token",
				"--cluster-name",
				hubClusterName,
				"--output",
				"json",
				"--role",
				fmt.Sprintf("arn:aws:iam::%s:role/ocm-hub-%s", hubClusterAccountId, c.managedClusterRoleSuffix),
			},
		},
	}}
	return kubeConfig
}

func (c *AWSIRSADriver) InformerHandler(option any) (cache.SharedIndexInformer, factory.EventFilterFunc) {
	awsOption, ok := option.(*AWSOption)
	if !ok {
		utilruntime.Must(fmt.Errorf("option type is not correct"))
	}
	return awsOption.AWSIRSAControl.Informer(), awsOption.EventFilterFunc
}

func (c *AWSIRSADriver) IsHubKubeConfigValid(ctx context.Context, secretOption register.SecretOption) (bool, error) {
	// TODO: implement the logic to validate the kubeconfig
	return true, nil
}

func (c *AWSIRSADriver) ManagedClusterDecorator(cluster *clusterv1.ManagedCluster) *clusterv1.ManagedCluster {
	if cluster.Annotations == nil {
		cluster.Annotations = make(map[string]string)
	}
	cluster.Annotations[operatorv1.ClusterAnnotationsKeyPrefix+"/"+ManagedClusterArn] = c.managedClusterArn
	cluster.Annotations[operatorv1.ClusterAnnotationsKeyPrefix+"/"+ManagedClusterIAMRoleSuffix] = c.managedClusterRoleSuffix
	return cluster
}

func NewAWSIRSADriver(managedClusterArn string, managedClusterRoleSuffix string, hubClusterArn string, name string) register.RegisterDriver {
	return &AWSIRSADriver{
		managedClusterArn:        managedClusterArn,
		managedClusterRoleSuffix: managedClusterRoleSuffix,
		hubClusterArn:            hubClusterArn,
		name:                     name,
	}
}

type AWSIRSADriverForHub struct {
	hubClusterArn string
}

func (a *AWSIRSADriverForHub) allows(cluster *clusterv1.ManagedCluster) bool {
	_, isManagedClusterArnPresent := cluster.Annotations["agent.open-cluster-management.io/managed-cluster-arn"]
	_, isManagedClusterIAMRoleSuffixPresent := cluster.Annotations["agent.open-cluster-management.io/managed-cluster-iam-role-suffix"]
	if isManagedClusterArnPresent && isManagedClusterIAMRoleSuffixPresent {
		return true
	}
	return false
}

func (a *AWSIRSADriverForHub) CreatePermissions(ctx context.Context, cluster *clusterv1.ManagedCluster) error {

	if !a.allows(cluster) {
		return nil
	}
	log.Printf("ManagedCluster %s is joined using aws-irsa registration-auth", cluster.Name)

	//Creating config for aws
	cfg, err := config.LoadDefaultConfig(ctx)
	if err != nil {
		log.Printf("Failed to load aws config %v", err)
	}

	// Create an EKS client
	eksClient := eks.NewFromConfig(cfg)
	hubClusterName, roleArn, err := CreateIAMRoleAndPolicy(ctx, a.hubClusterArn, cluster, cfg)
	if err != nil {
		log.Printf("Failed to create IAM Roles and Policies %v", err)
		return err
	}

	err = createAccessEntry(ctx, eksClient, roleArn, hubClusterName, cluster.Name)
	if err != nil {
		log.Printf("Failed to create Access Entries for aws irsa %v", err)
	}

	return nil
}

// This function creates:
// 1. IAM Roles and Policies in the hub cluster IAM
// 2. Returns the hubclustername and the roleArn to be used for Access Entry creation
func CreateIAMRoleAndPolicy(ctx context.Context, hubClusterArn string, managedCluster *v1.ManagedCluster, cfg aws.Config) (string, string, error) {
	var managedClusterIamRoleSuffix string
	var getRoleOutput *iam.GetRoleOutput
	var createRoleOutput *iam.CreateRoleOutput
	var hubClusterName string
	var managedClusterName string
	var roleArn string
	var hubAccountId string
	var managedClusterAccountId string

	// Create an IAM client
	iamClient := iam.NewFromConfig(cfg)

	managedClusterIamRoleSuffix, isManagedClusterIamRoleSuffixPresent :=
		managedCluster.Annotations["agent.open-cluster-management.io/managed-cluster-iam-role-suffix"]
	managedClusterArn, isManagedClusterArnPresent := managedCluster.Annotations["agent.open-cluster-management.io/managed-cluster-arn"]

	if hubClusterArn != "" && isManagedClusterIamRoleSuffixPresent && isManagedClusterArnPresent {

		managedClusterAccountId, managedClusterName = commonhelpers.GetAwsAccountIdAndClusterName(managedClusterArn)
		hubAccountId, hubClusterName = commonhelpers.GetAwsAccountIdAndClusterName(hubClusterArn)
		// Define the role name and trust policy
		roleName := "ocm-hub-" + managedClusterIamRoleSuffix
		policyName := "ocm-hub-" + managedClusterIamRoleSuffix

		// Check if hash is the same
		hash := commonhelpers.Md5HashSuffix(hubAccountId, hubClusterName, managedClusterAccountId, managedClusterName)
		if hash != managedClusterIamRoleSuffix {
			err := fmt.Errorf("HubClusterARN provided during join is different from the current hub cluster")
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
			log.Printf("Failed to render templates while creating IAM role and policy %v", err)
			return hubClusterName, roleArn, err
		}

		createRoleOutput, err = iamClient.CreateRole(ctx, &iam.CreateRoleInput{
			RoleName:                 aws.String(roleName),
			AssumeRolePolicyDocument: aws.String(renderedTemplates[1]),
		})
		if err != nil {
			// Ignore error when role already exists as we will always create the same role
			if !(strings.Contains(err.Error(), "EntityAlreadyExists")) {
				log.Printf("Failed to create IAM role: %v\n", err)
				return hubClusterName, roleArn, err
			} else {
				log.Printf("Ignore IAM role creation error as entity already exists")
				getRoleOutput, err = iamClient.GetRole(ctx, &iam.GetRoleInput{
					RoleName: aws.String(roleName),
				})
				if err != nil {
					log.Printf("Failed to get IAM role: %v\n", err)
					return hubClusterName, roleArn, err
				}
			}
		} else {
			log.Printf("Role created successfully: %s\n", *createRoleOutput.Role.Arn)
		}

		createPolicyResult, err := iamClient.CreatePolicy(ctx, &iam.CreatePolicyInput{
			PolicyDocument: aws.String(renderedTemplates[0]),
			PolicyName:     aws.String(policyName),
		})

		creds, err := cfg.Credentials.Retrieve(ctx)
		if err != nil {
			log.Printf("Failed to get IAM Credentials: %v\n", err)
			return hubClusterName, roleArn, err
		}
		awsAccountId := creds.AccountID
		policyArn := fmt.Sprintf("arn:aws:iam::%s:policy/%s", awsAccountId, policyName)

		if err != nil {
			if !(strings.Contains(err.Error(), "EntityAlreadyExists")) {
				log.Printf("Failed to create IAM Policy:%s %v\n", err, roleName)
				return hubClusterName, roleArn, err
			} else {
				log.Printf("Ignore IAM policy creation error as entity already exists %v", err)
			}
		} else {
			log.Printf("Policy created successfully: %s\n", *createPolicyResult.Policy.Arn)
		}

		if createPolicyResult != nil {
			policyArn = *createPolicyResult.Policy.Arn
		}

		_, err = iamClient.AttachRolePolicy(ctx, &iam.AttachRolePolicyInput{
			PolicyArn: aws.String(policyArn),
			RoleName:  aws.String(roleName),
		})
		if err != nil {
			log.Printf("Couldn't attach policy %v to role %v. Here's why: %v\n", policyArn, roleName, err)
			return hubClusterName, roleArn, err
		}
		if getRoleOutput != nil {
			roleArn = *getRoleOutput.Role.Arn
		} else {
			roleArn = *createRoleOutput.Role.Arn
		}
		log.Printf("Created IAM Role %v and policy %v. \n", roleName, policyArn)

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
		KubernetesGroups: []string{"open-cluster-management:" + managedClusterName},
	}

	createAccessEntryOutput, err := eksClient.CreateAccessEntry(ctx, params, func(opts *eks.Options) {
		opts.Retryer = retry.AddWithErrorCodes(retry.NewStandard(func(o *retry.StandardOptions) {
			o.MaxAttempts = 10
			o.Backoff = retry.NewExponentialJitterBackoff(100 * time.Second)
		}), (*types.InvalidParameterException)(nil).ErrorCode())
	})
	if err != nil {
		if !(strings.Contains(err.Error(), "EntityAlreadyExists")) {
			log.Printf("Failed to create Access entry for the managed cluster %v because of %v\n", managedClusterName, err)
			return err
		} else {
			log.Printf("Ignore Access entry creation error as entity already exists")
		}
	}
	log.Printf("Access entry created successfully: %s\n", *createAccessEntryOutput.AccessEntry.AccessEntryArn)
	return nil
}

func NewAWSIRSADriverForHub(hubClusterArn string) register.RegisterDriverForHub {
	awsIRSADriverForHub := &AWSIRSADriverForHub{
		hubClusterArn: hubClusterArn,
	}
	return awsIRSADriverForHub
}
