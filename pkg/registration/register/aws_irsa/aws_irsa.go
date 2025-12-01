package aws_irsa

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/cache"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"

	clusterv1 "open-cluster-management.io/api/cluster/v1"
	operatorv1 "open-cluster-management.io/api/operator/v1"
	"open-cluster-management.io/sdk-go/pkg/basecontroller/events"
	"open-cluster-management.io/sdk-go/pkg/basecontroller/factory"

	"open-cluster-management.io/ocm/pkg/common/helpers"
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

	awsIRSAControl AWSIRSAControl
}

func (c *AWSIRSADriver) Process(
	ctx context.Context, controllerName string, secret *corev1.Secret, additionalSecretData map[string][]byte,
	recorder events.Recorder) (*corev1.Secret, *metav1.Condition, error) {

	isApproved, err := c.awsIRSAControl.isApproved(c.name)
	if err != nil {
		return nil, nil, err
	}
	if !isApproved {
		return nil, nil, nil
	}

	recorder.Eventf(ctx, "EKSRegistrationRequestApproved", "An EKS registration request is approved for %s", controllerName)
	return secret, nil, nil
}

func (c *AWSIRSADriver) BuildKubeConfigFromTemplate(kubeConfig *clientcmdapi.Config) *clientcmdapi.Config {
	hubClusterAccountId, hubClusterName := helpers.GetAwsAccountIdAndClusterName(c.hubClusterArn)
	awsRegion := helpers.GetAwsRegion(c.hubClusterArn)
	kubeConfig.AuthInfos = map[string]*clientcmdapi.AuthInfo{register.DefaultKubeConfigAuth: {
		Exec: &clientcmdapi.ExecConfig{
			APIVersion: "client.authentication.k8s.io/v1beta1",
			Command:    "/awscli/dist/aws",
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

func (c *AWSIRSADriver) InformerHandler() (cache.SharedIndexInformer, factory.EventFilterFunc) {
	return c.awsIRSAControl.Informer(), nil
}

func (c *AWSIRSADriver) IsHubKubeConfigValid(ctx context.Context, secretOption register.SecretOption) (bool, error) {
	if secretOption.BootStrapKubeConfigFile == "" {
		return false, nil
	}
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

func (c *AWSIRSADriver) BuildClients(_ context.Context, secretOption register.SecretOption, bootstrap bool) (*register.Clients, error) {
	clients, err := register.BuildClientsFromSecretOption(secretOption, bootstrap)
	if err != nil {
		return nil, err
	}
	c.awsIRSAControl, err = NewAWSIRSAControl(clients.ClusterInformer, clients.ClusterClient)
	if err != nil {
		return nil, fmt.Errorf("failed to create AWS IRSA control: %w", err)
	}
	return clients, nil
}

func NewAWSIRSADriver(opt *AWSOption, secretOption register.SecretOption) register.RegisterDriver {
	return &AWSIRSADriver{
		managedClusterArn:        opt.ManagedClusterArn,
		managedClusterRoleSuffix: opt.ManagedClusterRoleSuffix,
		hubClusterArn:            opt.HubClusterArn,
		name:                     secretOption.ClusterName,
	}
}
