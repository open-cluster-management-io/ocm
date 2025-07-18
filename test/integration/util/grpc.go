package util

import (
	"context"
	"encoding/pem"
	"fmt"
	"os"
	"time"

	"gopkg.in/yaml.v2"
	"k8s.io/client-go/rest"
	certutil "k8s.io/client-go/util/cert"

	workclientset "open-cluster-management.io/api/client/work/clientset/versioned"
	workinformers "open-cluster-management.io/api/client/work/informers/externalversions"
	"open-cluster-management.io/sdk-go/pkg/cloudevents/generic/options/cert"
	"open-cluster-management.io/sdk-go/pkg/cloudevents/generic/options/grpc"
	grpcoptions "open-cluster-management.io/sdk-go/pkg/cloudevents/server/grpc/options"
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

func CreateGRPCConfigs(configFileName string) (string, *grpcoptions.GRPCServerOptions, error) {
	serverCertPairs, err := util.NewServerCertPairs()
	if err != nil {
		return "", nil, err
	}

	if _, err := util.AppendCAToCertPool(serverCertPairs.CA); err != nil {
		return "", nil, err
	}

	clientCertPairs, err := util.SignClientCert(serverCertPairs.CA, serverCertPairs.CAKey, 24*time.Hour)
	if err != nil {
		return "", nil, err
	}

	caFile, err := util.WriteCertToTempFile(serverCertPairs.CA)
	if err != nil {
		return "", nil, err
	}
	serverCertFile, err := util.WriteCertToTempFile(serverCertPairs.ServerTLSCert.Leaf)
	if err != nil {
		return "", nil, err
	}
	serverKeyFile, err := util.WriteKeyToTempFile(serverCertPairs.ServerTLSCert.PrivateKey)
	if err != nil {
		return "", nil, err
	}

	serverOptions := grpcoptions.NewGRPCServerOptions()
	serverOptions.ClientCAFile = caFile
	serverOptions.TLSCertFile = serverCertFile
	serverOptions.TLSKeyFile = serverKeyFile

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
		return "", nil, err
	}

	if err := os.WriteFile(configFileName, configData, 0600); err != nil {
		return "", nil, err
	}

	return config.URL, serverOptions, nil
}
