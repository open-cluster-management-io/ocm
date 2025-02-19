package aws_irsa

import (
	"context"
	"fmt"
	"os"
	"path"
	"strings"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/eks"
	ekstypes "github.com/aws/aws-sdk-go-v2/service/eks/types"
	"github.com/aws/aws-sdk-go-v2/service/iam"
	iamtypes "github.com/aws/aws-sdk-go-v2/service/iam/types"
	"github.com/aws/smithy-go/middleware"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	kubefake "k8s.io/client-go/kubernetes/fake"
	clienttesting "k8s.io/client-go/testing"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/klog/v2"

	"open-cluster-management.io/ocm/manifests"
	commonhelper "open-cluster-management.io/ocm/pkg/common/helpers"
	testingcommon "open-cluster-management.io/ocm/pkg/common/testing"
	testinghelpers "open-cluster-management.io/ocm/pkg/registration/helpers/testing"
	"open-cluster-management.io/ocm/pkg/registration/register"
)

const (
	testNamespace  = "testns"
	testAgentName  = "testagent"
	testSecretName = "testsecret"
	testIrsaName   = "testirsa"
)

// var commonName = fmt.Sprintf("%s%s:%s", user.SubjectPrefix, testinghelpers.TestManagedClusterName, testAgentName)

func TestProcess(t *testing.T) {
	cases := []struct {
		name                string
		queueKey            string
		secret              *corev1.Secret
		approvedIrsaRequest *testinghelpers.TestIrsaRequest
		keyDataExpected     bool
		irsaNameExpected    bool
		expectedCondition   *metav1.Condition
		validateActions     func(t *testing.T, hubActions []clienttesting.Action, secret *corev1.Secret)
	}{
		{},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			ctrl := &mockAWSIRSAControl{}
			var irsas []runtime.Object

			hubKubeClient := kubefake.NewSimpleClientset(irsas...)

			additionalSecretData := map[string][]byte{
				register.ClusterNameFile: []byte(testinghelpers.TestManagedClusterName),
				register.AgentNameFile:   []byte(testAgentName),
			}

			awsOption := &AWSOption{
				AWSIRSAControl: ctrl,
			}

			driver := &AWSIRSADriver{}

			if c.approvedIrsaRequest != nil {
				driver.name = testIrsaName
			}

			syncCtx := testingcommon.NewFakeSyncContext(t, "test")

			klog.Info(hubKubeClient, additionalSecretData, awsOption, syncCtx)

		})
	}
}

var _ AWSIRSAControl = &mockAWSIRSAControl{}

//TODO: Uncomment the below once required in the aws irsa authentication implementation
/*
func conditionEqual(expected, actual *metav1.Condition) bool {
 	if expected == nil && actual == nil {
 		return true
 	}

 	if expected == nil || actual == nil {
 		return false
 	}

 	if expected.Type != actual.Type {
 		return false
 	}

 	if string(expected.Status) != string(actual.Status) {
 		return false
 	}

 	return true
 }
func (m *mockAWSIRSAControl) create(
	_ context.Context, _ events.Recorder, objMeta metav1.ObjectMeta, _ []byte, _ string, _ *int32) (string, error) {
	mockIrsa := &unstructured.Unstructured{}
	_, err := m.awsIrsaClient.Invokes(clienttesting.CreateActionImpl{
		ActionImpl: clienttesting.ActionImpl{
			Verb: "create",
		},
		Object: mockIrsa,
	}, nil)
	return objMeta.Name + rand.String(4), err
}
*/
type mockAWSIRSAControl struct {
	approved      bool
	eksKubeConfig []byte
	awsIrsaClient *clienttesting.Fake
}

func (m *mockAWSIRSAControl) isApproved(name string) (bool, error) {
	_, err := m.awsIrsaClient.Invokes(clienttesting.GetActionImpl{
		ActionImpl: clienttesting.ActionImpl{
			Verb: "get",
			//Resource: schema.GroupVersionResource.GroupVersion().WithResource("aws"),
		},
		Name: name,
	}, nil)

	return m.approved, err
}

func (m *mockAWSIRSAControl) generateEKSKubeConfig(name string) ([]byte, error) {
	_, err := m.awsIrsaClient.Invokes(clienttesting.GetActionImpl{
		ActionImpl: clienttesting.ActionImpl{
			Verb: "get",
			//Resource: certificates.SchemeGroupVersion.WithResource("certificatesigningrequests"),
		},
		Name: name,
	}, nil)
	return m.eksKubeConfig, err
}

func (m *mockAWSIRSAControl) Informer() cache.SharedIndexInformer {
	panic("implement me")
}

func TestIsHubKubeConfigValidFunc(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "testvalidhubclientconfig")
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	defer os.RemoveAll(tempDir)

	cert1 := testinghelpers.NewTestCert("system:open-cluster-management:cluster1:agent1", 60*time.Second)
	//cert2 := testinghelpers.NewTestCert("test", 60*time.Second)

	kubeconfig := testinghelpers.NewKubeconfig("c1", "https://127.0.0.1:6001", "", "", nil, nil, nil)

	cases := []struct {
		name               string
		clusterName        string
		agentName          string
		kubeconfig         []byte
		bootstapKubeconfig []byte
		tlsCert            []byte
		tlsKey             []byte
		isValid            bool
	}{
		{
			name:    "no kubeconfig",
			isValid: false,
		},
		{
			name:               "context cluster changes",
			clusterName:        "cluster1",
			agentName:          "agent1",
			kubeconfig:         kubeconfig,
			bootstapKubeconfig: testinghelpers.NewKubeconfig("c2", "https://127.0.0.1:6001", "", "", nil, nil, nil),
			tlsKey:             cert1.Key,
			tlsCert:            cert1.Cert,
			isValid:            false,
		},
		{
			name:               "hub server url changes",
			clusterName:        "cluster1",
			agentName:          "agent1",
			kubeconfig:         kubeconfig,
			bootstapKubeconfig: testinghelpers.NewKubeconfig("c1", "https://127.0.0.2:6001", "", "", nil, nil, nil),
			tlsKey:             cert1.Key,
			tlsCert:            cert1.Cert,
			isValid:            false,
		},
		{
			name:               "proxy url changes",
			clusterName:        "cluster1",
			agentName:          "agent1",
			kubeconfig:         kubeconfig,
			bootstapKubeconfig: testinghelpers.NewKubeconfig("c1", "https://127.0.0.1:6001", "https://127.0.0.1:3129", "", nil, nil, nil),
			tlsKey:             cert1.Key,
			tlsCert:            cert1.Cert,
			isValid:            false,
		},
		{
			name:               "ca bundle changes",
			clusterName:        "cluster1",
			agentName:          "agent1",
			kubeconfig:         kubeconfig,
			bootstapKubeconfig: testinghelpers.NewKubeconfig("c1", "https://127.0.0.1:6001", "", "", []byte("test"), nil, nil),
			tlsKey:             cert1.Key,
			tlsCert:            cert1.Cert,
			isValid:            false,
		},
		{
			name:               "ca changes",
			clusterName:        "cluster1",
			agentName:          "agent1",
			kubeconfig:         kubeconfig,
			bootstapKubeconfig: testinghelpers.NewKubeconfig("c1", "https://127.0.0.1:6001", "", "/etc/ca.crt", nil, nil, nil),
			tlsKey:             cert1.Key,
			tlsCert:            cert1.Cert,
			isValid:            false,
		},
		{
			name:               "valid hub client config",
			clusterName:        "cluster1",
			agentName:          "agent1",
			kubeconfig:         kubeconfig,
			bootstapKubeconfig: testinghelpers.NewKubeconfig("c1", "https://127.0.0.1:6001", "", "", nil, nil, nil),
			tlsKey:             cert1.Key,
			tlsCert:            cert1.Cert,
			isValid:            true,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			driver := NewAWSIRSADriver("", "", "", "")
			secretOption := register.SecretOption{
				ClusterName:       c.clusterName,
				AgentName:         c.agentName,
				HubKubeconfigDir:  tempDir,
				HubKubeconfigFile: path.Join(tempDir, "kubeconfig"),
			}
			if c.kubeconfig != nil {
				testinghelpers.WriteFile(path.Join(tempDir, "kubeconfig"), c.kubeconfig)
			}
			if c.tlsKey != nil {
				testinghelpers.WriteFile(path.Join(tempDir, "tls.key"), c.tlsKey)
			}
			if c.tlsCert != nil {
				testinghelpers.WriteFile(path.Join(tempDir, "tls.crt"), c.tlsCert)
			}
			if c.bootstapKubeconfig != nil {
				bootstrapKubeconfig, err := clientcmd.Load(c.bootstapKubeconfig)
				if err != nil {
					t.Fatal(err)
				}
				secretOption.BootStrapKubeConfig = bootstrapKubeconfig
			}

			valid, err := register.IsHubKubeConfigValidFunc(driver, secretOption)(context.TODO())
			if err != nil {
				t.Errorf("unexpected error: %v", err)
			}
			if c.isValid != valid {
				t.Errorf("expect %t, but %t", c.isValid, valid)
			}
		})
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
			name: "test invalid hubclusrterarn passed during join.",
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
