package util

import (
	"context"
	"encoding/pem"
	"fmt"
	"os"
	"time"

	"gopkg.in/yaml.v2"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	kubeinformers "k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	certutil "k8s.io/client-go/util/cert"

	addonv1alpha1client "open-cluster-management.io/api/client/addon/clientset/versioned"
	addoninformers "open-cluster-management.io/api/client/addon/informers/externalversions"
	clusterv1client "open-cluster-management.io/api/client/cluster/clientset/versioned"
	clusterv1informers "open-cluster-management.io/api/client/cluster/informers/externalversions"
	workclientset "open-cluster-management.io/api/client/work/clientset/versioned"
	workinformers "open-cluster-management.io/api/client/work/informers/externalversions"
	clusterv1 "open-cluster-management.io/api/cluster/v1"
	"open-cluster-management.io/sdk-go/pkg/cloudevents/generic/options/cert"
	"open-cluster-management.io/sdk-go/pkg/cloudevents/generic/options/grpc"
	sdkgrpc "open-cluster-management.io/sdk-go/pkg/server/grpc"
	"open-cluster-management.io/sdk-go/test/integration/cloudevents/util"
)

type GRPCServerWorkHook struct {
	WorkClient    workclientset.Interface
	WorkInformers workinformers.SharedInformerFactory
}

func NewGRPCServerWorkHook(kubeConfig *rest.Config) (*GRPCServerWorkHook, error) {
	workClient, err := workclientset.NewForConfig(kubeConfig)
	if err != nil {
		return nil, err
	}

	return &GRPCServerWorkHook{
		WorkClient:    workClient,
		WorkInformers: workinformers.NewSharedInformerFactoryWithOptions(workClient, 30*time.Minute),
	}, nil
}

func (h *GRPCServerWorkHook) Run(ctx context.Context) {
	go h.WorkInformers.Start(ctx.Done())
}

type GRPCServerRegistrationHook struct {
	KubeClient       kubernetes.Interface
	ClusterClient    clusterv1client.Interface
	AddOnClient      addonv1alpha1client.Interface
	KubeInformers    kubeinformers.SharedInformerFactory
	ClusterInformers clusterv1informers.SharedInformerFactory
	AddOnInformers   addoninformers.SharedInformerFactory
}

func NewGRPCServerRegistrationHook(kubeConfig *rest.Config) (*GRPCServerRegistrationHook, error) {
	kubeClient, err := kubernetes.NewForConfig(kubeConfig)
	if err != nil {
		return nil, err
	}
	clusterClient, err := clusterv1client.NewForConfig(kubeConfig)
	if err != nil {
		return nil, err
	}
	addonClient, err := addonv1alpha1client.NewForConfig(kubeConfig)
	if err != nil {
		return nil, err
	}
	return &GRPCServerRegistrationHook{
		KubeClient:    kubeClient,
		ClusterClient: clusterClient,
		AddOnClient:   addonClient,
		KubeInformers: kubeinformers.NewSharedInformerFactoryWithOptions(kubeClient, 30*time.Minute,
			kubeinformers.WithTweakListOptions(func(listOptions *metav1.ListOptions) {
				selector := &metav1.LabelSelector{
					MatchExpressions: []metav1.LabelSelectorRequirement{
						{
							Key:      clusterv1.ClusterNameLabelKey,
							Operator: metav1.LabelSelectorOpExists,
						},
					},
				}
				listOptions.LabelSelector = metav1.FormatLabelSelector(selector)
			})),
		ClusterInformers: clusterv1informers.NewSharedInformerFactory(clusterClient, 30*time.Minute),
		AddOnInformers:   addoninformers.NewSharedInformerFactory(addonClient, 30*time.Minute),
	}, nil
}

func (h *GRPCServerRegistrationHook) Run(ctx context.Context) {
	go h.KubeInformers.Start(ctx.Done())
	go h.ClusterInformers.Start(ctx.Done())
	go h.AddOnInformers.Start(ctx.Done())
}

func CreateGRPCConfigs(configFileName string, port string) (string, *sdkgrpc.GRPCServerOptions, string, error) {
	serverCertPairs, err := util.NewServerCertPairs()
	if err != nil {
		return "", nil, "", err
	}

	if _, err := util.AppendCAToCertPool(serverCertPairs.CA); err != nil {
		return "", nil, "", err
	}

	clientCertPairs, err := util.SignClientCert(serverCertPairs.CA, serverCertPairs.CAKey, 24*time.Hour)
	if err != nil {
		return "", nil, "", err
	}

	caFile, err := util.WriteCertToTempFile(serverCertPairs.CA)
	if err != nil {
		return "", nil, "", err
	}
	caKeyFile, err := util.WriteKeyToTempFile(serverCertPairs.CAKey)
	if err != nil {
		return "", nil, "", err
	}
	serverCertFile, err := util.WriteCertToTempFile(serverCertPairs.ServerTLSCert.Leaf)
	if err != nil {
		return "", nil, "", err
	}
	serverKeyFile, err := util.WriteKeyToTempFile(serverCertPairs.ServerTLSCert.PrivateKey)
	if err != nil {
		return "", nil, "", err
	}

	serverOptions := sdkgrpc.NewGRPCServerOptions()
	serverOptions.ClientCAFile = caFile
	serverOptions.TLSCertFile = serverCertFile
	serverOptions.TLSKeyFile = serverKeyFile
	serverOptions.ServerBindPort = port

	config := &grpc.GRPCConfig{
		CertConfig: cert.CertConfig{
			CAData: pem.EncodeToMemory(&pem.Block{
				Type:  certutil.CertificateBlockType,
				Bytes: serverCertPairs.CA.Raw,
			}),
			ClientCertData: clientCertPairs.ClientCert,
			ClientKeyData:  clientCertPairs.ClientKey,
		},
		URL: fmt.Sprintf("127.0.0.1:%s", serverOptions.ServerBindPort),
	}

	configData, err := yaml.Marshal(config)
	if err != nil {
		return "", nil, "", err
	}

	if err := os.WriteFile(configFileName, configData, 0600); err != nil {
		return "", nil, "", err
	}

	return config.URL, serverOptions, caKeyFile, nil
}
