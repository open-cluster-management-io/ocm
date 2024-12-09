package aws_irsa

import (
	"context"
	"fmt"
	operatorv1 "open-cluster-management.io/api/operator/v1"

	"github.com/openshift/library-go/pkg/controller/factory"
	"github.com/openshift/library-go/pkg/operator/events"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/tools/cache"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"
	"k8s.io/klog/v2"

	clusterv1 "open-cluster-management.io/api/cluster/v1"

	//operatorv1 "open-cluster-management.io/api/operator/v1"

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
	name string
}

func (c *AWSIRSADriver) Process(
	ctx context.Context, controllerName string, secret *corev1.Secret, additionalSecretData map[string][]byte,
	recorder events.Recorder, opt any) (*corev1.Secret, *metav1.Condition, error) {
	logger := klog.FromContext(ctx)

	awsOption, ok := opt.(*AWSOption)
	if !ok {
		return nil, nil, fmt.Errorf("option type is not correct")
	}

	// TODO: skip if registration request is not accepted yet, that is the required condition is missing on ManagedCluster CR
	isApproved, err := awsOption.AWSIRSAControl.isApproved(c.name)
	if err != nil {
		return nil, nil, err
	}
	if !isApproved {
		return nil, nil, nil
	}

	// TODO: Generate kubeconfig if the request is accepted
	eksKubeConfigData, err := awsOption.AWSIRSAControl.generateEKSKubeConfig(c.name)
	if err != nil {
		return nil, nil, err
	}
	if len(eksKubeConfigData) == 0 {
		return nil, nil, nil
	}

	secret.Data["kubeconfig"] = eksKubeConfigData
	logger.Info("Store kubeconfig into the secret.")

	recorder.Eventf("EKSHubKubeconfigCreated", "A new eks hub Kubeconfig for %s is available", controllerName)
	//return secret, cond, err
	return secret, nil, err
}

//TODO: Uncomment the below once required in the aws irsa authentication implementation

/*
func (c *AWSIRSADriver) reset() {
	c.name = ""
}
*/

func (c *AWSIRSADriver) BuildKubeConfigFromTemplate(kubeConfig *clientcmdapi.Config) *clientcmdapi.Config {
	kubeConfig.AuthInfos = map[string]*clientcmdapi.AuthInfo{register.DefaultKubeConfigAuth: {
		ClientCertificate: TLSCertFile,
		ClientKey:         TLSKeyFile,
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

func (c *AWSIRSADriver) ManagedClusterDecorator(cluster *clusterv1.ManagedCluster, clusterAnnotations map[string]string, managedClusterArn string, managedClusterRoleSuffix string) *clusterv1.ManagedCluster {
	if clusterAnnotations == nil {
		clusterAnnotations = map[string]string{}
	}
	clusterAnnotations[operatorv1.ClusterAnnotationsKeyPrefix+"/"+ManagedClusterArn] = managedClusterArn
	clusterAnnotations[operatorv1.ClusterAnnotationsKeyPrefix+"/"+ManagedClusterIAMRoleSuffix] = managedClusterRoleSuffix
	return cluster
}

//func (c *AWSIRSADriver) AddClusterAnnotations(clusterAnnotations map[string]string, managedClusterArn string, managedClusterRoleSuffix string) {
//	if clusterAnnotations == nil {
//		clusterAnnotations = map[string]string{}
//	}
//
//	clusterAnnotations[operatorv1.ClusterAnnotationsKeyPrefix+"/"+ManagedClusterArn] = managedClusterArn
//	clusterAnnotations[operatorv1.ClusterAnnotationsKeyPrefix+"/"+ManagedClusterIAMRoleSuffix] = managedClusterRoleSuffix
//}

func NewAWSIRSADriver() register.RegisterDriver {
	return &AWSIRSADriver{}
}
