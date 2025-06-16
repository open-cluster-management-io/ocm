package aws_irsa

import (
	"context"
	"fmt"
	"os"
	"reflect"
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
	operatorv1 "open-cluster-management.io/api/operator/v1"

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
						operatorv1.ClusterAnnotationsKeyPrefix + "/" + ManagedClusterArn:           "arn:aws:eks:us-west-2:123456789012:cluster/managed-cluster1",
						operatorv1.ClusterAnnotationsKeyPrefix + "/" + ManagedClusterIAMRoleSuffix: "7f8141296c75f2871e3d030f85c35692",
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
						operatorv1.ClusterAnnotationsKeyPrefix + "/" + ManagedClusterArn:           "arn:aws:eks:us-west-2:123456789012:cluster/managed-cluster2",
						operatorv1.ClusterAnnotationsKeyPrefix + "/" + ManagedClusterIAMRoleSuffix: "7f8141296c75f2871e3d030f85c35692",
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
						operatorv1.ClusterAnnotationsKeyPrefix + "/" + ManagedClusterArn:           "arn:aws:eks:us-west-1:123456789012:cluster/managed-cluster3",
						operatorv1.ClusterAnnotationsKeyPrefix + "/" + ManagedClusterIAMRoleSuffix: "7f8141296c75f2871e3d030f85c35692",
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
						operatorv1.ClusterAnnotationsKeyPrefix + "/" + ManagedClusterArn:           "arn:aws:eks:us-west-2:999999999999:cluster/managed-cluster4",
						operatorv1.ClusterAnnotationsKeyPrefix + "/" + ManagedClusterIAMRoleSuffix: "7f8141296c75f2871e3d030f85c35692",
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
						operatorv1.ClusterAnnotationsKeyPrefix + "/" + ManagedClusterArn:           "XXXXXXarn:aws:eks:us-west-2:123456789012:cluster/managed-cluster5",
						operatorv1.ClusterAnnotationsKeyPrefix + "/" + ManagedClusterIAMRoleSuffix: "7f8141296c75f2871e3d030f85c35692",
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
						operatorv1.ClusterAnnotationsKeyPrefix + "/" + ManagedClusterArn:           "",
						operatorv1.ClusterAnnotationsKeyPrefix + "/" + ManagedClusterIAMRoleSuffix: "7f8141296c75f2871e3d030f85c35692",
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
		}, []string{},
		false,
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
	}, []string{}, false)
	if err == nil {
		t.Errorf("Error expected")
	}
}

func TestRenderTemplate(t *testing.T) {
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
	trustPolicy, _ := renderTemplate(trustPolicyTemplatePath, data)

	TPfilebuf, TPerr := manifests.ManagedClusterPolicyManifestFiles.ReadFile(trustPolicyTemplatePath)
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

	if trustPolicy != TrustPolicy {
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
			name: "test delete IAM Role",
			args: args{
				ctx:                context.Background(),
				withAPIOptionsFunc: mockSuccessfulDeletionBehaviour,
			},
			managedClusterAnnotations: map[string]string{
				operatorv1.ClusterAnnotationsKeyPrefix + "/" + ManagedClusterIAMRoleSuffix: "960c4e56c25ba0b571ddcdaa7edc943e",
				operatorv1.ClusterAnnotationsKeyPrefix + "/" + ManagedClusterArn:           "arn:aws:eks:us-west-2:123456789012:cluster/spoke-cluster",
			},
			want:    nil,
			wantErr: false,
		},
		{
			name: "test delete IAM Role with NoSuchEntity in DeleteRole",
			args: args{
				ctx: context.Background(),
				withAPIOptionsFunc: func(stack *middleware.Stack) error {
					err := mockSuccessfulDeletionBehaviour(stack)
					if err != nil {
						return err
					}

					return stack.Finalize.Add(
						middleware.FinalizeMiddlewareFunc(
							"DeleteRoleMock3",
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
				operatorv1.ClusterAnnotationsKeyPrefix + "/" + ManagedClusterIAMRoleSuffix: "960c4e56c25ba0b571ddcdaa7edc943e",
				operatorv1.ClusterAnnotationsKeyPrefix + "/" + ManagedClusterArn:           "arn:aws:eks:us-west-2:123456789012:cluster/spoke-cluster",
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

			roleName, _, err := getRoleNameAndArn(tt.args.ctx, managedCluster, cfg)
			if err != nil {
				t.Errorf("Error getting role name")
				return
			}
			err = deleteIAMRole(tt.args.ctx, cfg, roleName)
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
						operatorv1.ClusterAnnotationsKeyPrefix + "/" + ManagedClusterArn:           "arn:aws:eks:us-west-2:123456789012:cluster/managed-cluster1",
						operatorv1.ClusterAnnotationsKeyPrefix + "/" + ManagedClusterIAMRoleSuffix: "7f8141296c75f2871e3d030f85c35692",
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

			awsIrsaHubDriver, err := NewAWSIRSAHubDriver(context.Background(), "arn:aws:eks:us-west-2:123456789012:cluster/hub-cluster", []string{}, []string{}, false)
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

func TestCreateIAMRole(t *testing.T) {
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
			name: "test create IAM Role",
			args: args{
				ctx: context.Background(),
				withAPIOptionsFunc: func(stack *middleware.Stack) error {
					return stack.Finalize.Add(
						middleware.FinalizeMiddlewareFunc(
							"CreateRoleMock",
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
								return middleware.FinalizeOutput{}, middleware.Metadata{}, nil
							},
						),
						middleware.Before,
					)
				},
			},
			managedClusterAnnotations: map[string]string{
				operatorv1.ClusterAnnotationsKeyPrefix + "/" + ManagedClusterIAMRoleSuffix: "960c4e56c25ba0b571ddcdaa7edc943e",
				operatorv1.ClusterAnnotationsKeyPrefix + "/" + ManagedClusterArn:           "arn:aws:eks:us-west-2:123456789012:cluster/spoke-cluster",
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
								return middleware.FinalizeOutput{}, middleware.Metadata{}, nil
							},
						),
						middleware.Before,
					)
				},
			},
			managedClusterAnnotations: map[string]string{
				operatorv1.ClusterAnnotationsKeyPrefix + "/" + ManagedClusterIAMRoleSuffix: "test",
				operatorv1.ClusterAnnotationsKeyPrefix + "/" + ManagedClusterArn:           "arn:aws:eks:us-west-2:123456789012:cluster/spoke-cluster",
			},
			want:    fmt.Errorf("HubClusterARN provided during join by ManagedCluster spoke-cluster is different from the current hub cluster"),
			wantErr: true,
		},
		{
			name: "test create IAM Role with EntityAlreadyExists in CreateRole",
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
								return middleware.FinalizeOutput{}, middleware.Metadata{}, nil
							},
						),
						middleware.Before,
					)
				},
			},
			managedClusterAnnotations: map[string]string{
				operatorv1.ClusterAnnotationsKeyPrefix + "/" + ManagedClusterIAMRoleSuffix: "960c4e56c25ba0b571ddcdaa7edc943e",
				operatorv1.ClusterAnnotationsKeyPrefix + "/" + ManagedClusterArn:           "arn:aws:eks:us-west-2:123456789012:cluster/spoke-cluster",
			},
			want:    nil,
			wantErr: false,
		},
		{
			name: "test create IAM Role with error in CreateRole",
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
				operatorv1.ClusterAnnotationsKeyPrefix + "/" + ManagedClusterIAMRoleSuffix: "960c4e56c25ba0b571ddcdaa7edc943e",
				operatorv1.ClusterAnnotationsKeyPrefix + "/" + ManagedClusterArn:           "arn:aws:eks:us-west-2:123456789012:cluster/spoke-cluster",
			},
			want:    fmt.Errorf("operation error IAM: CreateRole, failed to create IAM role"),
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
			tags := []string{}

			_, _, err = createIAMRoleAndPolicy(tt.args.ctx, HubClusterArn, managedCluster, cfg, tags)
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
				operatorv1.ClusterAnnotationsKeyPrefix + "/" + ManagedClusterIAMRoleSuffix: "960c4e56c25ba0b571ddcdaa7edc943e",
				operatorv1.ClusterAnnotationsKeyPrefix + "/" + ManagedClusterArn:           "arn:aws:eks:us-west-2:123456789012:cluster/spoke-cluster",
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
				operatorv1.ClusterAnnotationsKeyPrefix + "/" + ManagedClusterIAMRoleSuffix: "960c4e56c25ba0b571ddcdaa7edc943e",
				operatorv1.ClusterAnnotationsKeyPrefix + "/" + ManagedClusterArn:           "arn:aws:eks:us-west-2:123456789012:cluster/spoke-cluster",
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
			tags := []string{}

			err = createAccessEntry(tt.args.ctx, eksClient, principalArn, hubClusterName, managedClusterName, tags)
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
func TestCreateTags(t *testing.T) {
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
		tags                      []string
	}{
		{
			name: "test create IAM Role and Policy with Tags",
			args: args{
				ctx: context.Background(),
				withAPIOptionsFunc: func(stack *middleware.Stack) error {
					return stack.Finalize.Add(
						middleware.FinalizeMiddlewareFunc(
							"CreateRoleAndPolicyWithTagsMock",
							func(ctx context.Context, input middleware.FinalizeInput, handler middleware.FinalizeHandler) (middleware.FinalizeOutput, middleware.Metadata, error) {
								operationName := middleware.GetOperationName(ctx)
								if operationName == "CreateRole" {
									return middleware.FinalizeOutput{
										Result: &iam.CreateRoleOutput{Role: &iamtypes.Role{
											RoleName: aws.String("TestRole"),
											Arn:      aws.String("arn:aws:iam::123456789012:role/TestRole"),
											Tags: []iamtypes.Tag{
												{
													Key:   aws.String("product:v1:tenant:app-name"),
													Value: aws.String("My-App"),
												},
												{
													Key:   aws.String("product:v1:tenant:created-by"),
													Value: aws.String("Team-1"),
												},
											},
										},
										},
									}, middleware.Metadata{}, nil
								}
								if operationName == "CreatePolicy" {
									return middleware.FinalizeOutput{
										Result: &iam.CreatePolicyOutput{Policy: &iamtypes.Policy{
											PolicyName: aws.String("TestPolicy"),
											Arn:        aws.String("arn:aws:iam::123456789012:role/TestPolicy"),
											Tags: []iamtypes.Tag{
												{
													Key:   aws.String("product:v1:tenant:app-name"),
													Value: aws.String("My-App"),
												},
												{
													Key:   aws.String("product:v1:tenant:created-by"),
													Value: aws.String("Team-1"),
												},
											},
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
			tags:    []string{"product:v1:tenant:app-name=My-App", "product:v1:tenant:created-by=Team-1"},
		},
		{
			name: "test create IAM Role and Policy with invalid Tag with key beginning with aws",
			args: args{
				ctx: context.Background(),
				withAPIOptionsFunc: func(stack *middleware.Stack) error {
					return stack.Finalize.Add(
						middleware.FinalizeMiddlewareFunc(
							"CreateRoleWithInvalidTagBeginsAwsMock",
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
			tags:    []string{"aws:invalid:tag=invalid-tag"},
		},
		{
			name: "test create IAM Role and Policy with invalid Tag with empty key",
			args: args{
				ctx: context.Background(),
				withAPIOptionsFunc: func(stack *middleware.Stack) error {
					return stack.Finalize.Add(
						middleware.FinalizeMiddlewareFunc(
							"CreateRoleWithInvalidTagEmptyKeyMock",
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
			tags:    []string{"=emptykey"},
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

			_, _, err = createIAMRoleAndPolicy(tt.args.ctx, HubClusterArn, managedCluster, cfg, tt.tags)
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

func TestParseTagsForRolesAndPolicies(t *testing.T) {
	cases := []struct {
		name   string
		tags   []string
		result []iamtypes.Tag
		err    error
	}{
		{
			name: "Test Parsing Tags Correctly",
			tags: []string{"product:v1:tenant:app-name=My-App"},
			result: []iamtypes.Tag{
				{
					Key:   &[]string{"product:v1:tenant:app-name"}[0],
					Value: &[]string{"My-App"}[0],
				},
			},
			err: nil,
		},
		{
			name:   "Test Parsing Tags Incorrectly",
			tags:   []string{"product:v1:tenant:app-nameMy-App"},
			result: nil,
			err:    fmt.Errorf("missing value from tag"),
		},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			output, err := parseTagsForRolesAndPolicies(tt.tags)

			if !reflect.DeepEqual(output, tt.result) && err != tt.err {
				for idx := range output {
					t.Errorf("Expected error to be %#v, but got %#v", tt.err, err)
					t.Errorf("Expected {Key: %s, Value: %s}, but got {Key: %s, Value: %s}", *tt.result[idx].Key, *tt.result[idx].Value, *output[idx].Key, *output[idx].Value)
				}
			}
		})
	}
}

func TestParseTagsForAccessEntries(t *testing.T) {
	cases := []struct {
		name   string
		tags   []string
		result map[string]string
		err    error
	}{
		{
			name:   "Test Parsing Tags Correctly for access entries",
			tags:   []string{"product:v1:tenant:app-name=My-App"},
			result: map[string]string{"product:v1:tenant:app-name": "My-App"},
			err:    nil,
		},
		{
			name:   "Test Parsing Tags Incorrectly  access entries",
			tags:   []string{"product:v1:tenant:app-nameMy-App"},
			result: nil,
			err:    fmt.Errorf("missing value in the tag"),
		},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			output, err := parseTagsForAccessEntry(tt.tags)

			if !reflect.DeepEqual(output, tt.result) && err != tt.err {
				for key := range output {
					t.Errorf("Expected error to be %#v, but got %#v", tt.err, err)
					t.Errorf("Expected {Key: %s, Value: %s}, but got {Key: %s, Value: %s}", key, tt.result[key], key, output[key])
				}
			}
		})
	}
}
