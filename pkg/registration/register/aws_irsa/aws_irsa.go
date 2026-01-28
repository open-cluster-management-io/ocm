package aws_irsa

import (
	"context"
	"fmt"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"
	"k8s.io/klog/v2"

	clusterv1 "open-cluster-management.io/api/cluster/v1"
	operatorv1 "open-cluster-management.io/api/operator/v1"
	"open-cluster-management.io/sdk-go/pkg/basecontroller/events"
	"open-cluster-management.io/sdk-go/pkg/basecontroller/factory"

	"open-cluster-management.io/ocm/pkg/common/helpers"
	"open-cluster-management.io/ocm/pkg/registration/register"
	"open-cluster-management.io/ocm/pkg/registration/register/csr"
	"open-cluster-management.io/ocm/pkg/registration/register/token"
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

	// addonClients holds the addon clients and informers
	addonClients *register.AddOnClients

	// tokenControl is used for token-based addon authentication
	tokenControl token.TokenControl

	// csrControl is used for CSR-based addon authentication
	csrControl csr.CSRControl
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

func (c *AWSIRSADriver) BuildClients(ctx context.Context, secretOption register.SecretOption, bootstrap bool) (*register.Clients, error) {
	clients, err := register.BuildClientsFromSecretOption(secretOption, bootstrap)
	if err != nil {
		return nil, err
	}
	c.awsIRSAControl, err = NewAWSIRSAControl(clients.ClusterInformer, clients.ClusterClient)
	if err != nil {
		return nil, fmt.Errorf("failed to create AWS IRSA control: %w", err)
	}

	// Store addon clients and initialize controls for addon authentication after bootstrap
	if !bootstrap {
		c.addonClients = &register.AddOnClients{
			AddonClient:   clients.AddonClient,
			AddonInformer: clients.AddonInformer,
		}

		kubeConfig, err := register.KubeConfigFromSecretOption(secretOption, bootstrap)
		if err != nil {
			return nil, err
		}
		kubeClient, err := kubernetes.NewForConfig(kubeConfig)
		if err != nil {
			return nil, err
		}
		c.tokenControl = token.NewTokenControl(kubeClient.CoreV1())

		// Initialize CSR control for CSR-based addon authentication
		logger := klog.FromContext(ctx)
		kubeInformerFactory := informers.NewSharedInformerFactoryWithOptions(
			kubeClient,
			10*time.Minute,
			informers.WithTweakListOptions(func(listOptions *metav1.ListOptions) {
				listOptions.LabelSelector = fmt.Sprintf("%s=%s", clusterv1.ClusterNameLabelKey, secretOption.ClusterName)
			}),
		)
		csrControl, err := csr.NewCSRControl(logger, kubeInformerFactory.Certificates(), kubeClient)
		if err != nil {
			return nil, fmt.Errorf("failed to create CSR control: %w", err)
		}
		c.csrControl = csrControl
	}

	return clients, nil
}

func (c *AWSIRSADriver) Fork(addonName string, authConfig register.AddonAuthConfig, secretOption register.SecretOption) (register.RegisterDriver, error) {
	// Check if token-based authentication should be used (shared helper)
	tokenDriver, err := token.TryForkTokenDriver(addonName, authConfig, secretOption, c.tokenControl, c.addonClients)
	if err != nil {
		return nil, err
	}
	if tokenDriver != nil {
		return tokenDriver, nil
	}

	// For CSR driver, create a CSR-based driver for addon authentication
	// This handles:
	// - CustomSigner type (secretOption.Signer != KubeAPIServerClientSignerName)
	// - KubeClient type with CSR authentication

	// Get CSR configuration from AddonAuthConfig (type-safe interface)
	csrConfig := authConfig.GetCSRConfiguration()
	if csrConfig == nil {
		return nil, fmt.Errorf("CSR configuration is nil for addon %s", addonName)
	}

	// Note: tokenControl is not set for addon CSR drivers since they use CSR-based auth
	return csr.NewCSRDriverForAddOn(addonName, csrConfig, secretOption, c.csrControl), nil
}

func NewAWSIRSADriver(opt *AWSOption, secretOption register.SecretOption) register.RegisterDriver {
	return &AWSIRSADriver{
		managedClusterArn:        opt.ManagedClusterArn,
		managedClusterRoleSuffix: opt.ManagedClusterRoleSuffix,
		hubClusterArn:            opt.HubClusterArn,
		name:                     secretOption.ClusterName,
	}
}

var _ register.RegisterDriver = &AWSIRSADriver{}
var _ register.AddonDriverFactory = &AWSIRSADriver{}
