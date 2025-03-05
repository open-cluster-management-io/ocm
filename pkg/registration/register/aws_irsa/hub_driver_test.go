package aws_irsa

import (
	"context"
	"fmt"
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/aws/arn"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/eks"
	ekstypes "github.com/aws/aws-sdk-go-v2/service/eks/types"
	"github.com/aws/aws-sdk-go-v2/service/iam"
	iamtypes "github.com/aws/aws-sdk-go-v2/service/iam/types"
	"github.com/aws/smithy-go/middleware"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	clusterv1 "open-cluster-management.io/api/cluster/v1"

	"open-cluster-management.io/ocm/manifests"
	commonhelper "open-cluster-management.io/ocm/pkg/common/helpers"
	testinghelpers "open-cluster-management.io/ocm/pkg/registration/helpers/testing"
)

func TestAccept(t *testing.T) {
	cases := []struct {
		name       string
		cluster    *clusterv1.ManagedCluster
		isAccepted bool
	}{
		{
			name: "Accept cluster when managedcluster in list of patterns",
			cluster: &clusterv1.ManagedCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name: "managed-cluster1",
					Annotations: map[string]string{
						"agent.open-cluster-management.io/managed-cluster-arn":             "arn:aws:eks:us-west-2:123456789012:cluster/managed-cluster1",
						"agent.open-cluster-management.io/managed-cluster-iam-role-suffix": "7f8141296c75f2871e3d030f85c35692",
					},
				},
			},
			isAccepted: true,
		},
		{
			name: "Accept cluster when managedcluster in list of patterns",
			cluster: &clusterv1.ManagedCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name: "managed-cluster2",
					Annotations: map[string]string{
						"agent.open-cluster-management.io/managed-cluster-arn":             "arn:aws:eks:us-west-2:123456789012:cluster/managed-cluster2",
						"agent.open-cluster-management.io/managed-cluster-iam-role-suffix": "7f8141296c75f2871e3d030f85c35692",
					},
				},
			},
			isAccepted: true,
		},
		{
			name: "Accept cluster when managedcluster in list of patterns",
			cluster: &clusterv1.ManagedCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name: "managed-cluster3",
					Annotations: map[string]string{
						"agent.open-cluster-management.io/managed-cluster-arn":             "arn:aws:eks:us-west-1:123456789012:cluster/managed-cluster3",
						"agent.open-cluster-management.io/managed-cluster-iam-role-suffix": "7f8141296c75f2871e3d030f85c35692",
					},
				},
			},
			isAccepted: true,
		},
		{
			name: "Reject cluster when managedcluster not in list of patterns",
			cluster: &clusterv1.ManagedCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name: "managed-cluster4",
					Annotations: map[string]string{
						"agent.open-cluster-management.io/managed-cluster-arn":             "arn:aws:eks:us-west-2:999999999999:cluster/managed-cluster4",
						"agent.open-cluster-management.io/managed-cluster-iam-role-suffix": "7f8141296c75f2871e3d030f85c35692",
					},
				},
			},
			isAccepted: false,
		},
		{
			name: "Reject cluster when list of patterns has only a partial match",
			cluster: &clusterv1.ManagedCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name: "managed-cluster5",
					Annotations: map[string]string{
						"agent.open-cluster-management.io/managed-cluster-arn":             "XXXXXXarn:aws:eks:us-west-2:123456789012:cluster/managed-cluster5",
						"agent.open-cluster-management.io/managed-cluster-iam-role-suffix": "7f8141296c75f2871e3d030f85c35692",
					},
				},
			},
			isAccepted: false,
		},
		{
			name: "Reject cluster when cluster is empty",
			cluster: &clusterv1.ManagedCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name: "managed-cluster6",
					Annotations: map[string]string{
						"agent.open-cluster-management.io/managed-cluster-arn":             "",
						"agent.open-cluster-management.io/managed-cluster-iam-role-suffix": "7f8141296c75f2871e3d030f85c35692",
					},
				},
			},
			isAccepted: false,
		},
		{
			name: "Accept cluster for csr registration without pattern matching when annotation not present",
			cluster: &clusterv1.ManagedCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name: "managed-cluster7",
				},
			},
			isAccepted: true,
		},
	}
	awsIrsaHubDriver, err := NewAWSIRSAHubDriver(context.Background(), "arn:aws:eks:us-west-2:123456789012:cluster/hub-cluster",
		[]string{
			"arn:aws:eks:us-west-2:123456789012:cluster/.*",
			"arn:aws:eks:us-west-1:123456789012:cluster/.*",
		},
	)

	if err != nil {
		t.Errorf("Error not expected")
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			isAccepted := awsIrsaHubDriver.Accept(c.cluster)
			if c.isAccepted != isAccepted {
				t.Errorf("expect %t, but %t", c.isAccepted, isAccepted)
			}
		},
		)
	}
}

func TestNewDriverValidation(t *testing.T) {
	// Test with an invalid manager cluster approval pattern
	_, err := NewAWSIRSAHubDriver(context.Background(), "arn:aws:eks:us-west-2:123456789012:cluster/hub-cluster", []string{
		"arn:(aws:eks:us-west-2:123456789012:cluster/.*", // bad pattern
	})
	if err == nil {
		t.Errorf("Error expected")
	}
}

func TestRenderTemplates(t *testing.T) {
	templateFiles := []string{"managed-cluster-policy/AccessPolicy.tmpl", "managed-cluster-policy/TrustPolicy.tmpl"}
	data := map[string]interface{}{
		"hubClusterArn":               "arn:aws:iam::123456789012:cluster/hub-cluster",
		"managedClusterAccountId":     "123456789013",
		"managedClusterIamRoleSuffix": "",
		"hubAccountId":                "123456789012",
		"hubClusterName":              "hub-cluster",
		"managedClusterName":          "managed-cluster",
	}
	data["managedClusterIamRoleSuffix"] = commonhelper.Md5HashSuffix(
		data["hubAccountId"].(string),
		data["hubClusterName"].(string),
		data["managedClusterAccountId"].(string),
		data["managedClusterName"].(string),
	)
	renderedTemplates, _ := renderTemplates(templateFiles, data)

	APfilebuf, APerr := manifests.ManagedClusterPolicyManifestFiles.ReadFile("managed-cluster-policy/AccessPolicy.tmpl")
	if APerr != nil {
		t.Errorf("Templates not rendered as expected")
		return
	}
	contents := string(APfilebuf)
	AccessPolicy := strings.Replace(contents, "{{.hubClusterArn}}", data["hubClusterArn"].(string), 1)

	TPfilebuf, TPerr := manifests.ManagedClusterPolicyManifestFiles.ReadFile("managed-cluster-policy/TrustPolicy.tmpl")
	if TPerr != nil {
		t.Errorf("Templates not rendered as expected")
		return
	}
	contentstrust := string(TPfilebuf)

	replacer := strings.NewReplacer("{{.managedClusterAccountId}}", data["managedClusterAccountId"].(string),
		"{{.managedClusterIamRoleSuffix}}", data["managedClusterIamRoleSuffix"].(string),
		"{{.hubAccountId}}", data["hubAccountId"].(string),
		"{{.hubClusterName}}", data["hubClusterName"].(string),
		"{{.managedClusterAccountId}}", data["managedClusterAccountId"].(string),
		"{{.managedClusterName}}", data["managedClusterName"].(string))

	TrustPolicy := replacer.Replace(contentstrust)

	if len(renderedTemplates) != 2 {
		t.Errorf("Templates not rendered as expected")
		return
	}

	if renderedTemplates[0] != AccessPolicy {
		t.Errorf("AccessPolicy not rendered as expected")
		return
	}

	if renderedTemplates[1] != TrustPolicy {
		t.Errorf("TrustPolicy not rendered as expected")
		return
	}
}

func TestDeleteIAMRoleAndPolicy(t *testing.T) {
	type args struct {
		ctx                context.Context
		withAPIOptionsFunc func(*middleware.Stack) error
	}

	cases := []struct {
		name                      string
		args                      args
		managedClusterAnnotations map[string]string
		want                      error
		wantErr                   bool
	}{
		{
			name: "test delete IAM Role and policy",
			args: args{
				ctx:                context.Background(),
				withAPIOptionsFunc: mockSuccessfulDeletionBehaviour,
			},
			managedClusterAnnotations: map[string]string{
				"agent.open-cluster-management.io/managed-cluster-iam-role-suffix": "960c4e56c25ba0b571ddcdaa7edc943e",
				"agent.open-cluster-management.io/managed-cluster-arn":             "arn:aws:eks:us-west-2:123456789012:cluster/spoke-cluster",
			},
			want:    nil,
			wantErr: false,
		},
		{
			name: "test delete IAM Role and policy with NoSuchEntity in DeleteRole",
			args: args{
				ctx: context.Background(),
				withAPIOptionsFunc: func(stack *middleware.Stack) error {
					err := mockSuccessfulDeletionBehaviour(stack)
					if err != nil {
						return err
					}

					return stack.Finalize.Add(
						middleware.FinalizeMiddlewareFunc(
							"DeleteRoleOrDeletePolicyOrDetachPolicyMock3",
							func(ctx context.Context, input middleware.FinalizeInput, next middleware.FinalizeHandler) (middleware.FinalizeOutput, middleware.Metadata, error) {
								if middleware.GetOperationName(ctx) == "DetachRolePolicy" {
									return middleware.FinalizeOutput{
										Result: &iam.DetachRolePolicyOutput{},
									}, middleware.Metadata{}, fmt.Errorf("failed to detach IAM policy from role, NoSuchEntity")
								}
								return next.HandleFinalize(ctx, input)
							},
						),
						middleware.Before,
					)
				},
			},
			managedClusterAnnotations: map[string]string{
				"agent.open-cluster-management.io/managed-cluster-iam-role-suffix": "960c4e56c25ba0b571ddcdaa7edc943e",
				"agent.open-cluster-management.io/managed-cluster-arn":             "arn:aws:eks:us-west-2:123456789012:cluster/spoke-cluster",
			},
			want:    nil,
			wantErr: false,
		},
		{
			name: "test delete IAM Role and policy with NoSuchEntity in DeletePolicy",
			args: args{
				ctx: context.Background(),
				withAPIOptionsFunc: func(stack *middleware.Stack) error {
					err := mockSuccessfulDeletionBehaviour(stack)
					if err != nil {
						return err
					}

					return stack.Finalize.Add(
						middleware.FinalizeMiddlewareFunc(
							"DeleteRoleOrDeletePolicyOrDetachPolicyMock3",
							func(ctx context.Context, input middleware.FinalizeInput, next middleware.FinalizeHandler) (middleware.FinalizeOutput, middleware.Metadata, error) {
								if middleware.GetOperationName(ctx) == "DeletePolicy" {
									return middleware.FinalizeOutput{
										Result: nil,
									}, middleware.Metadata{}, fmt.Errorf("failed to delete IAM policy, NoSuchEntity")
								}
								return next.HandleFinalize(ctx, input)
							},
						),
						middleware.Before,
					)
				},
			},
			managedClusterAnnotations: map[string]string{
				"agent.open-cluster-management.io/managed-cluster-iam-role-suffix": "960c4e56c25ba0b571ddcdaa7edc943e",
				"agent.open-cluster-management.io/managed-cluster-arn":             "arn:aws:eks:us-west-2:123456789012:cluster/spoke-cluster",
			},
			want:    nil,
			wantErr: false,
		},
		{
			name: "test delete IAM Role and policy with NoSuchEntity in DeleteRole",
			args: args{
				ctx: context.Background(),
				withAPIOptionsFunc: func(stack *middleware.Stack) error {
					err := mockSuccessfulDeletionBehaviour(stack)
					if err != nil {
						return err
					}

					return stack.Finalize.Add(
						middleware.FinalizeMiddlewareFunc(
							"DeleteRoleOrDeletePolicyOrDetachPolicyMock3",
							func(ctx context.Context, input middleware.FinalizeInput, next middleware.FinalizeHandler) (middleware.FinalizeOutput, middleware.Metadata, error) {
								if middleware.GetOperationName(ctx) == "DeleteRole" {
									return middleware.FinalizeOutput{
										Result: nil,
									}, middleware.Metadata{}, fmt.Errorf("failed to delete IAM role, NoSuchEntity")
								}
								return next.HandleFinalize(ctx, input)
							},
						),
						middleware.Before,
					)
				},
			},
			managedClusterAnnotations: map[string]string{
				"agent.open-cluster-management.io/managed-cluster-iam-role-suffix": "960c4e56c25ba0b571ddcdaa7edc943e",
				"agent.open-cluster-management.io/managed-cluster-arn":             "arn:aws:eks:us-west-2:123456789012:cluster/spoke-cluster",
			},
			want:    nil,
			wantErr: false,
		},
	}

	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			os.Setenv("AWS_ACCESS_KEY_ID", "test")
			os.Setenv("AWS_SECRET_ACCESS_KEY", "test")
			os.Setenv("AWS_ACCOUNT_ID", "test")

			cfg, err := config.LoadDefaultConfig(
				tt.args.ctx,
				config.WithAPIOptions([]func(*middleware.Stack) error{tt.args.withAPIOptionsFunc}),
			)
			if err != nil {
				t.Fatal(err)
			}

			managedCluster := testinghelpers.NewManagedCluster()
			managedCluster.Annotations = tt.managedClusterAnnotations

			roleName, _, _, policyArn, err := getRoleAndPolicyArn(tt.args.ctx, managedCluster, cfg)
			if err != nil {
				t.Errorf("Error getting role and policy Arn.")
				return
			}
			err = deleteIAMRoleAndPolicy(tt.args.ctx, cfg, roleName, policyArn)
			if (err != nil) != tt.wantErr {
				t.Errorf("error = %#v, wantErr %#v", err, tt.wantErr)
				return
			}
			if tt.wantErr && err.Error() != tt.want.Error() {
				t.Errorf("err = %#v, want %#v", err, tt.want)
			}
		})
	}
}

func mockSuccessfulDeletionBehaviour(stack *middleware.Stack) error {
	isValidRoleName := regexp.MustCompile(`^[A-Za-z0-9_+=,.@-]+$`).MatchString
	isValidClusterName := regexp.MustCompile(`^[0-9A-Za-z][A-Za-z0-9-_]*$`).MatchString

	err := stack.Initialize.Add(
		middleware.InitializeMiddlewareFunc(
			"DeleteRoleOrDeletePolicyOrDetachPolicyMock1",
			func(ctx context.Context, input middleware.InitializeInput, next middleware.InitializeHandler) (middleware.InitializeOutput, middleware.Metadata, error) {
				switch v := input.Parameters.(type) {
				case *iam.DeleteRoleInput:
					if !isValidRoleName(*v.RoleName) {
						return middleware.InitializeOutput{Result: nil}, middleware.Metadata{}, fmt.Errorf("invalid role name")
					}
				case *iam.DeletePolicyInput:
					if !arn.IsARN(*v.PolicyArn) {
						return middleware.InitializeOutput{Result: nil}, middleware.Metadata{}, fmt.Errorf("invalid ARN")
					}

				case *eks.DeleteAccessEntryInput:
					if !isValidClusterName(*v.ClusterName) {
						return middleware.InitializeOutput{Result: nil}, middleware.Metadata{}, fmt.Errorf("invalid cluster name")
					}
					if !arn.IsARN(*v.PrincipalArn) {
						return middleware.InitializeOutput{Result: nil}, middleware.Metadata{}, fmt.Errorf("invalid ARN")
					}
				}

				return next.HandleInitialize(ctx, input)
			},
		),
		middleware.Before,
	)

	if err != nil {
		return err
	}

	err = stack.Finalize.Add(
		middleware.FinalizeMiddlewareFunc(
			"DeleteRoleOrDeletePolicyOrDetachPolicyMock2",
			func(ctx context.Context, input middleware.FinalizeInput, handler middleware.FinalizeHandler) (middleware.FinalizeOutput, middleware.Metadata, error) {
				operationName := middleware.GetOperationName(ctx)
				switch operationName {
				case "DeleteRole":
					return middleware.FinalizeOutput{
						Result: &iam.DeleteRoleOutput{},
					}, middleware.Metadata{}, nil
				case "DeletePolicy":
					return middleware.FinalizeOutput{
						Result: &iam.DeletePolicyOutput{},
					}, middleware.Metadata{}, nil
				case "DetachRolePolicy":
					return middleware.FinalizeOutput{
						Result: &iam.DetachRolePolicyOutput{},
					}, middleware.Metadata{}, nil
				case "DeleteAccessEntry":
					return middleware.FinalizeOutput{
						Result: &eks.DeleteAccessEntryOutput{},
					}, middleware.Metadata{}, nil
				}
				return middleware.FinalizeOutput{}, middleware.Metadata{}, nil
			},
		),
		middleware.Before,
	)

	if err != nil {
		return err
	}
	return nil
}

func TestDeleteAccessEntry(t *testing.T) {
	type args struct {
		ctx                context.Context
		withAPIOptionsFunc func(*middleware.Stack) error
	}

	cases := []struct {
		name           string
		hubClusterName string
		args           args
		want           error
		wantErr        bool
	}{
		{
			name:           "test delete Access Entry",
			hubClusterName: "hub",
			args: args{
				ctx:                context.Background(),
				withAPIOptionsFunc: mockSuccessfulDeletionBehaviour,
			},
			want:    nil,
			wantErr: false,
		},
		{
			name:           "test delete Access Entry error due to cluster name being an ARN",
			hubClusterName: "arn:aws:eks:us-west-2:123456789012:cluster/hub", // Not a cluster name, it is an ARN
			args: args{
				ctx:                context.Background(),
				withAPIOptionsFunc: mockSuccessfulDeletionBehaviour,
			},
			want:    fmt.Errorf("operation error EKS: DeleteAccessEntry, invalid cluster name"),
			wantErr: true,
		},
		{
			name:           "test delete Access Entry error",
			hubClusterName: "hub",
			args: args{
				ctx: context.Background(),
				withAPIOptionsFunc: func(stack *middleware.Stack) error {
					return stack.Finalize.Add(
						middleware.FinalizeMiddlewareFunc(
							"DeleteAccessEntryErrorMock",
							func(ctx context.Context, input middleware.FinalizeInput, handler middleware.FinalizeHandler) (middleware.FinalizeOutput, middleware.Metadata, error) {
								operationName := middleware.GetOperationName(ctx)
								if operationName == "DeleteAccessEntry" {
									return middleware.FinalizeOutput{
										Result: nil,
									}, middleware.Metadata{}, fmt.Errorf("failed to delete access entry")
								}
								return middleware.FinalizeOutput{}, middleware.Metadata{}, nil
							},
						),
						middleware.Before,
					)
				},
			},
			want:    fmt.Errorf("operation error EKS: DeleteAccessEntry, failed to delete access entry"),
			wantErr: true,
		},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			cfg, err := config.LoadDefaultConfig(
				tt.args.ctx,
				config.WithAPIOptions([]func(*middleware.Stack) error{tt.args.withAPIOptionsFunc}),
			)
			if err != nil {
				t.Fatal(err)
			}

			eksClient := eks.NewFromConfig(cfg)
			principalArn := "arn:aws:iam::123456789012:role/TestRole"

			err = deleteAccessEntry(tt.args.ctx, eksClient, principalArn, tt.hubClusterName)
			if (err != nil) != tt.wantErr {
				t.Errorf("error = %#v, wantErr %#v", err, tt.wantErr)
				return
			}
			if tt.wantErr && err.Error() != tt.want.Error() {
				t.Errorf("err = %#v, want %#v", err, tt.want)
			}
		})
	}
}

func TestCleanup(t *testing.T) {
	type args struct {
		ctx                context.Context
		withAPIOptionsFunc func(*middleware.Stack) error
	}

	cases := []struct {
		name    string
		cluster *clusterv1.ManagedCluster
		args    args
		want    error
		wantErr bool
	}{
		{
			name: "test Cleanup",
			cluster: &clusterv1.ManagedCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name: "managed-cluster",
					Annotations: map[string]string{
						"agent.open-cluster-management.io/managed-cluster-arn":             "arn:aws:eks:us-west-2:123456789012:cluster/managed-cluster1",
						"agent.open-cluster-management.io/managed-cluster-iam-role-suffix": "7f8141296c75f2871e3d030f85c35692",
					},
				},
			},
			args: args{
				ctx:                context.Background(),
				withAPIOptionsFunc: mockSuccessfulDeletionBehaviour,
			},
			want:    nil,
			wantErr: false,
		},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			cfg, err := config.LoadDefaultConfig(
				tt.args.ctx,
				config.WithAPIOptions([]func(*middleware.Stack) error{tt.args.withAPIOptionsFunc}),
			)
			if err != nil {
				t.Fatal(err)
			}

			awsIrsaHubDriver, err := NewAWSIRSAHubDriver(context.Background(), "arn:aws:eks:us-west-2:123456789012:cluster/hub-cluster", []string{})
			if err != nil {
				t.Errorf("error creating AWSIRSAHubDriver")
				return
			}
			awsIrsaHubDriver.(*AWSIRSAHubDriver).cfg = cfg

			err = awsIrsaHubDriver.Cleanup(tt.args.ctx, tt.cluster)
			if (err != nil) != tt.wantErr {
				t.Errorf("error = %#v, wantErr %#v", err, tt.wantErr)
				return
			}
			if tt.wantErr && err.Error() != tt.want.Error() {
				t.Errorf("err = %#v, want %#v", err, tt.want)
			}
		})
	}
}

func TestCreateIAMRoleAndPolicy(t *testing.T) {
	type args struct {
		ctx                context.Context
		withAPIOptionsFunc func(*middleware.Stack) error
	}

	cases := []struct {
		name                      string
		args                      args
		managedClusterAnnotations map[string]string
		want                      error
		wantErr                   bool
	}{
		{
			name: "test create IAM Role and policy",
			args: args{
				ctx: context.Background(),
				withAPIOptionsFunc: func(stack *middleware.Stack) error {
					return stack.Finalize.Add(
						middleware.FinalizeMiddlewareFunc(
							"CreateRoleOrCreatePolicyOrAttachPolicyMock",
							func(ctx context.Context, input middleware.FinalizeInput, handler middleware.FinalizeHandler) (middleware.FinalizeOutput, middleware.Metadata, error) {
								operationName := middleware.GetOperationName(ctx)
								if operationName == "CreateRole" {
									return middleware.FinalizeOutput{
										Result: &iam.CreateRoleOutput{Role: &iamtypes.Role{
											RoleName: aws.String("TestRole"),
											Arn:      aws.String("arn:aws:iam::123456789012:role/TestRole"),
										},
										},
									}, middleware.Metadata{}, nil
								}
								if operationName == "CreatePolicy" {
									return middleware.FinalizeOutput{
										Result: &iam.CreatePolicyOutput{Policy: &iamtypes.Policy{
											PolicyName: aws.String("TestPolicy"),
											Arn:        aws.String("arn:aws:iam::123456789012:role/TestPolicy"),
										},
										},
									}, middleware.Metadata{}, nil
								}
								if operationName == "AttachRolePolicy" {
									return middleware.FinalizeOutput{
										Result: &iam.AttachRolePolicyOutput{},
									}, middleware.Metadata{}, nil
								}
								return middleware.FinalizeOutput{}, middleware.Metadata{}, nil
							},
						),
						middleware.Before,
					)
				},
			},
			managedClusterAnnotations: map[string]string{
				"agent.open-cluster-management.io/managed-cluster-iam-role-suffix": "960c4e56c25ba0b571ddcdaa7edc943e",
				"agent.open-cluster-management.io/managed-cluster-arn":             "arn:aws:eks:us-west-2:123456789012:cluster/spoke-cluster",
			},
			want:    nil,
			wantErr: false,
		},
		{
			name: "test invalid hubclusterarn passed during join.",
			args: args{
				ctx: context.Background(),
				withAPIOptionsFunc: func(stack *middleware.Stack) error {
					return stack.Finalize.Add(
						middleware.FinalizeMiddlewareFunc(
							"InvalidHubclusterArnMock",
							func(ctx context.Context, input middleware.FinalizeInput, handler middleware.FinalizeHandler) (middleware.FinalizeOutput, middleware.Metadata, error) {
								operationName := middleware.GetOperationName(ctx)
								if operationName == "CreateRole" {
									return middleware.FinalizeOutput{
										Result: &iam.CreateRoleOutput{Role: &iamtypes.Role{
											RoleName: aws.String("TestRole"),
											Arn:      aws.String("arn:aws:iam::123456789012:role/TestRole"),
										},
										},
									}, middleware.Metadata{}, nil
								}
								if operationName == "CreatePolicy" {
									return middleware.FinalizeOutput{
										Result: &iam.CreatePolicyOutput{Policy: &iamtypes.Policy{
											PolicyName: aws.String("TestPolicy"),
											Arn:        aws.String("arn:aws:iam::123456789012:role/TestPolicy"),
										},
										},
									}, middleware.Metadata{}, nil
								}
								if operationName == "AttachRolePolicy" {
									return middleware.FinalizeOutput{
										Result: &iam.AttachRolePolicyOutput{},
									}, middleware.Metadata{}, nil
								}
								return middleware.FinalizeOutput{}, middleware.Metadata{}, nil
							},
						),
						middleware.Before,
					)
				},
			},
			managedClusterAnnotations: map[string]string{
				"agent.open-cluster-management.io/managed-cluster-iam-role-suffix": "test",
				"agent.open-cluster-management.io/managed-cluster-arn":             "arn:aws:eks:us-west-2:123456789012:cluster/spoke-cluster",
			},
			want:    fmt.Errorf("HubClusterARN provided during join by ManagedCluster spoke-cluster is different from the current hub cluster"),
			wantErr: true,
		},
		{
			name: "test create IAM Role and policy with EntityAlreadyExists in CreateRole",
			args: args{
				ctx: context.Background(),
				withAPIOptionsFunc: func(stack *middleware.Stack) error {
					return stack.Finalize.Add(
						middleware.FinalizeMiddlewareFunc(
							"CreateRoleEntityAlreadyExistsMock",
							func(ctx context.Context, input middleware.FinalizeInput, handler middleware.FinalizeHandler) (middleware.FinalizeOutput, middleware.Metadata, error) {
								operationName := middleware.GetOperationName(ctx)
								if operationName == "CreateRole" {
									return middleware.FinalizeOutput{
										Result: nil,
									}, middleware.Metadata{}, fmt.Errorf("failed to create IAM role, EntityAlreadyExists")
								}
								if operationName == "GetRole" {
									return middleware.FinalizeOutput{
										Result: &iam.GetRoleOutput{Role: &iamtypes.Role{
											RoleName: aws.String("TestRole"),
											Arn:      aws.String("arn:aws:iam::123456789012:role/TestRole"),
										},
										},
									}, middleware.Metadata{}, nil
								}
								if operationName == "CreatePolicy" {
									return middleware.FinalizeOutput{
										Result: &iam.CreatePolicyOutput{Policy: &iamtypes.Policy{
											PolicyName: aws.String("TestPolicy"),
											Arn:        aws.String("arn:aws:iam::123456789012:role/TestPolicy"),
										},
										},
									}, middleware.Metadata{}, nil
								}
								if operationName == "AttachRolePolicy" {
									return middleware.FinalizeOutput{
										Result: &iam.AttachRolePolicyOutput{},
									}, middleware.Metadata{}, nil
								}
								return middleware.FinalizeOutput{}, middleware.Metadata{}, nil
							},
						),
						middleware.Before,
					)
				},
			},
			managedClusterAnnotations: map[string]string{
				"agent.open-cluster-management.io/managed-cluster-iam-role-suffix": "960c4e56c25ba0b571ddcdaa7edc943e",
				"agent.open-cluster-management.io/managed-cluster-arn":             "arn:aws:eks:us-west-2:123456789012:cluster/spoke-cluster",
			},
			want:    nil,
			wantErr: false,
		},
		{
			name: "test create IAM Role and policy with error in CreateRole",
			args: args{
				ctx: context.Background(),
				withAPIOptionsFunc: func(stack *middleware.Stack) error {
					return stack.Finalize.Add(
						middleware.FinalizeMiddlewareFunc(
							"CreateRoleErrorMock",
							func(ctx context.Context, input middleware.FinalizeInput, handler middleware.FinalizeHandler) (middleware.FinalizeOutput, middleware.Metadata, error) {
								operationName := middleware.GetOperationName(ctx)
								if operationName == "CreateRole" {
									return middleware.FinalizeOutput{
										Result: nil,
									}, middleware.Metadata{}, fmt.Errorf("failed to create IAM role")
								}
								return middleware.FinalizeOutput{}, middleware.Metadata{}, nil
							},
						),
						middleware.Before,
					)
				},
			},
			managedClusterAnnotations: map[string]string{
				"agent.open-cluster-management.io/managed-cluster-iam-role-suffix": "960c4e56c25ba0b571ddcdaa7edc943e",
				"agent.open-cluster-management.io/managed-cluster-arn":             "arn:aws:eks:us-west-2:123456789012:cluster/spoke-cluster",
			},
			want:    fmt.Errorf("operation error IAM: CreateRole, failed to create IAM role"),
			wantErr: true,
		},
		{
			name: "test create IAM Role and policy with EntityAlreadyExists in CreatePolicy",
			args: args{
				ctx: context.Background(),
				withAPIOptionsFunc: func(stack *middleware.Stack) error {
					return stack.Finalize.Add(
						middleware.FinalizeMiddlewareFunc(
							"CreatePolicyEntityAlreadyExistsMock",
							func(ctx context.Context, input middleware.FinalizeInput, handler middleware.FinalizeHandler) (middleware.FinalizeOutput, middleware.Metadata, error) {
								operationName := middleware.GetOperationName(ctx)
								if operationName == "CreateRole" {
									return middleware.FinalizeOutput{
										Result: &iam.CreateRoleOutput{Role: &iamtypes.Role{
											RoleName: aws.String("TestRole"),
											Arn:      aws.String("arn:aws:iam::123456789012:role/TestRole"),
										},
										},
									}, middleware.Metadata{}, nil
								}
								if operationName == "CreatePolicy" {
									return middleware.FinalizeOutput{
										Result: nil,
									}, middleware.Metadata{}, fmt.Errorf("failed to create IAM policy, EntityAlreadyExists")
								}
								if operationName == "ListPolicies" {
									policies := []iamtypes.Policy{
										{
											PolicyName: aws.String("TestPolicy1"),
											Arn:        aws.String("arn:aws:iam::123456789012:role/TestPolicy1"),
										},
										// You can add more policies here if needed
										{
											PolicyName: aws.String("ocm-hub-960c4e56c25ba0b571ddcdaa7edc943e"),
											Arn:        aws.String("arn:aws:iam::123456789012:role/TestPolicy2"),
										},
									}
									return middleware.FinalizeOutput{
										Result: &iam.ListPoliciesOutput{Policies: policies},
									}, middleware.Metadata{}, nil
								}
								if operationName == "AttachRolePolicy" {
									return middleware.FinalizeOutput{
										Result: &iam.AttachRolePolicyOutput{},
									}, middleware.Metadata{}, nil
								}
								return middleware.FinalizeOutput{}, middleware.Metadata{}, nil
							},
						),
						middleware.Before,
					)
				},
			},
			managedClusterAnnotations: map[string]string{
				"agent.open-cluster-management.io/managed-cluster-iam-role-suffix": "960c4e56c25ba0b571ddcdaa7edc943e",
				"agent.open-cluster-management.io/managed-cluster-arn":             "arn:aws:eks:us-west-2:123456789012:cluster/spoke-cluster",
			},
			want:    nil,
			wantErr: false,
		},
		{
			name: "test create IAM Role and policy with error in CreatePolicy",
			args: args{
				ctx: context.Background(),
				withAPIOptionsFunc: func(stack *middleware.Stack) error {
					return stack.Finalize.Add(
						middleware.FinalizeMiddlewareFunc(
							"CreatePolicyErrorMock",
							func(ctx context.Context, input middleware.FinalizeInput, handler middleware.FinalizeHandler) (middleware.FinalizeOutput, middleware.Metadata, error) {
								operationName := middleware.GetOperationName(ctx)
								if operationName == "CreateRole" {
									return middleware.FinalizeOutput{
										Result: &iam.CreateRoleOutput{Role: &iamtypes.Role{
											RoleName: aws.String("TestRole"),
											Arn:      aws.String("arn:aws:iam::123456789012:role/TestRole"),
										},
										},
									}, middleware.Metadata{}, nil
								}
								if operationName == "CreatePolicy" {
									return middleware.FinalizeOutput{
										Result: nil,
									}, middleware.Metadata{}, fmt.Errorf("failed to create IAM policy")
								}
								return middleware.FinalizeOutput{}, middleware.Metadata{}, nil
							},
						),
						middleware.Before,
					)
				},
			},
			managedClusterAnnotations: map[string]string{
				"agent.open-cluster-management.io/managed-cluster-iam-role-suffix": "960c4e56c25ba0b571ddcdaa7edc943e",
				"agent.open-cluster-management.io/managed-cluster-arn":             "arn:aws:eks:us-west-2:123456789012:cluster/spoke-cluster",
			},
			want:    fmt.Errorf("operation error IAM: CreatePolicy, failed to create IAM policy"),
			wantErr: true,
		},
		{
			name: "test create IAM Role and policy with error in AttachRolePolicy",
			args: args{
				ctx: context.Background(),
				withAPIOptionsFunc: func(stack *middleware.Stack) error {
					return stack.Finalize.Add(
						middleware.FinalizeMiddlewareFunc(
							"AttachRolePolicyErrorMock",
							func(ctx context.Context, input middleware.FinalizeInput, handler middleware.FinalizeHandler) (middleware.FinalizeOutput, middleware.Metadata, error) {
								operationName := middleware.GetOperationName(ctx)
								if operationName == "CreateRole" {
									return middleware.FinalizeOutput{
										Result: &iam.CreateRoleOutput{Role: &iamtypes.Role{
											RoleName: aws.String("TestRole"),
											Arn:      aws.String("arn:aws:iam::123456789012:role/TestRole"),
										},
										},
									}, middleware.Metadata{}, nil
								}
								if operationName == "CreatePolicy" {
									return middleware.FinalizeOutput{
										Result: &iam.CreatePolicyOutput{Policy: &iamtypes.Policy{
											PolicyName: aws.String("TestPolicy"),
											Arn:        aws.String("arn:aws:iam::123456789012:role/TestPolicy"),
										},
										},
									}, middleware.Metadata{}, nil
								}
								if operationName == "AttachRolePolicy" {
									return middleware.FinalizeOutput{
										Result: nil,
									}, middleware.Metadata{}, fmt.Errorf("failed to attach policy to role")
								}
								return middleware.FinalizeOutput{}, middleware.Metadata{}, nil
							},
						),
						middleware.Before,
					)
				},
			},
			managedClusterAnnotations: map[string]string{
				"agent.open-cluster-management.io/managed-cluster-iam-role-suffix": "960c4e56c25ba0b571ddcdaa7edc943e",
				"agent.open-cluster-management.io/managed-cluster-arn":             "arn:aws:eks:us-west-2:123456789012:cluster/spoke-cluster",
			},
			want:    fmt.Errorf("operation error IAM: AttachRolePolicy, failed to attach policy to role"),
			wantErr: true,
		},
	}

	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			os.Setenv("AWS_ACCESS_KEY_ID", "test")
			os.Setenv("AWS_SECRET_ACCESS_KEY", "test")
			os.Setenv("AWS_ACCOUNT_ID", "test")

			cfg, err := config.LoadDefaultConfig(
				tt.args.ctx,
				config.WithAPIOptions([]func(*middleware.Stack) error{tt.args.withAPIOptionsFunc}),
			)
			if err != nil {
				t.Fatal(err)
			}

			HubClusterArn := "arn:aws:eks:us-west-2:123456789012:cluster/hub-cluster"

			managedCluster := testinghelpers.NewManagedCluster()
			managedCluster.Annotations = tt.managedClusterAnnotations

			_, _, err = createIAMRoleAndPolicy(tt.args.ctx, HubClusterArn, managedCluster, cfg)
			if (err != nil) != tt.wantErr {
				t.Errorf("error = %#v, wantErr %#v", err, tt.wantErr)
				return
			}
			if tt.wantErr && err.Error() != tt.want.Error() {
				t.Errorf("err = %#v, want %#v", err, tt.want)
			}
		})
	}
}

func TestCreateAccessEntries(t *testing.T) {
	type args struct {
		ctx                context.Context
		withAPIOptionsFunc func(*middleware.Stack) error
	}

	cases := []struct {
		name                      string
		args                      args
		managedClusterAnnotations map[string]string
		want                      error
		wantErr                   bool
	}{
		{
			name: "test CreateAccessEntry successful",
			args: args{
				ctx: context.Background(),
				withAPIOptionsFunc: func(stack *middleware.Stack) error {
					return stack.Finalize.Add(
						middleware.FinalizeMiddlewareFunc(
							"CreateAccessEntryMock",
							func(ctx context.Context, input middleware.FinalizeInput, handler middleware.FinalizeHandler) (middleware.FinalizeOutput, middleware.Metadata, error) {
								operationName := middleware.GetOperationName(ctx)
								if operationName == "CreateAccessEntry" {
									return middleware.FinalizeOutput{
										Result: &eks.CreateAccessEntryOutput{AccessEntry: &ekstypes.AccessEntry{
											AccessEntryArn: aws.String("arn:aws:eks::123456789012:access-entry/TestAccessEntry"),
										},
										},
									}, middleware.Metadata{}, nil
								}
								return middleware.FinalizeOutput{}, middleware.Metadata{}, nil
							},
						),
						middleware.Before,
					)
				},
			},
			managedClusterAnnotations: map[string]string{
				"agent.open-cluster-management.io/managed-cluster-iam-role-suffix": "960c4e56c25ba0b571ddcdaa7edc943e",
				"agent.open-cluster-management.io/managed-cluster-arn":             "arn:aws:eks:us-west-2:123456789012:cluster/spoke-cluster",
			},
			want:    nil,
			wantErr: false,
		},
		{
			name: "test CreateAccessEntry error",
			args: args{
				ctx: context.Background(),
				withAPIOptionsFunc: func(stack *middleware.Stack) error {
					return stack.Finalize.Add(
						middleware.FinalizeMiddlewareFunc(
							"CreateAccessEntryErrorMock",
							func(ctx context.Context, input middleware.FinalizeInput, handler middleware.FinalizeHandler) (middleware.FinalizeOutput, middleware.Metadata, error) {
								operationName := middleware.GetOperationName(ctx)
								if operationName == "CreateAccessEntry" {
									return middleware.FinalizeOutput{
										Result: nil,
									}, middleware.Metadata{}, fmt.Errorf("failed to create access entry")
								}
								return middleware.FinalizeOutput{}, middleware.Metadata{}, nil
							},
						),
						middleware.Before,
					)
				},
			},
			managedClusterAnnotations: map[string]string{
				"agent.open-cluster-management.io/managed-cluster-iam-role-suffix": "960c4e56c25ba0b571ddcdaa7edc943e",
				"agent.open-cluster-management.io/managed-cluster-arn":             "arn:aws:eks:us-west-2:123456789012:cluster/spoke-cluster",
			},
			want:    fmt.Errorf("operation error EKS: CreateAccessEntry, failed to create access entry"),
			wantErr: true,
		},
	}

	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			cfg, err := config.LoadDefaultConfig(
				tt.args.ctx,
				config.WithAPIOptions([]func(*middleware.Stack) error{tt.args.withAPIOptionsFunc}),
			)
			if err != nil {
				t.Fatal(err)
			}

			eksClient := eks.NewFromConfig(cfg)
			principalArn := "arn:aws:iam::123456789012:role/TestRole"
			hubClusterName := "hub"
			managedClusterName := "spoke"

			managedCluster := testinghelpers.NewManagedCluster()
			managedCluster.Annotations = tt.managedClusterAnnotations

			err = createAccessEntry(tt.args.ctx, eksClient, principalArn, hubClusterName, managedClusterName)
			if (err != nil) != tt.wantErr {
				t.Errorf("error = %#v, wantErr %#v", err, tt.wantErr)
				return
			}
			if tt.wantErr && err.Error() != tt.want.Error() {
				t.Errorf("err = %#v, want %#v", err, tt.want)
			}
		})
	}
}
