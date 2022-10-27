package kubecontroller

import (
	"context"
	"fmt"

	"k8s.io/controller-manager/controller"
	"k8s.io/klog/v2"
	"k8s.io/kubernetes/pkg/controller/certificates/approver"
	"k8s.io/kubernetes/pkg/controller/certificates/cleaner"
	"k8s.io/kubernetes/pkg/controller/certificates/rootcacertpublisher"
	"k8s.io/kubernetes/pkg/controller/certificates/signer"
	csrsigningconfig "k8s.io/kubernetes/pkg/controller/certificates/signer/config"
)

func startCSRSigningController(ctx context.Context, controllerContext ControllerContext) (controller.Interface, bool, error) {
	missingSingleSigningFile := controllerContext.ComponentConfig.CSRSigningController.ClusterSigningCertFile == "" || controllerContext.ComponentConfig.CSRSigningController.ClusterSigningKeyFile == ""
	if missingSingleSigningFile && !anySpecificFilesSet(controllerContext.ComponentConfig.CSRSigningController) {
		klog.V(2).Info("skipping CSR signer controller because no csr cert/key was specified")
		return nil, false, nil
	}
	if !missingSingleSigningFile && anySpecificFilesSet(controllerContext.ComponentConfig.CSRSigningController) {
		return nil, false, fmt.Errorf("cannot specify default and per controller certs at the same time")
	}

	c := controllerContext.ClientBuilder.ClientOrDie("certificate-controller")
	csrInformer := controllerContext.InformerFactory.Certificates().V1().CertificateSigningRequests()
	certTTL := controllerContext.ComponentConfig.CSRSigningController.ClusterSigningDuration.Duration

	if kubeAPIServerSignerCertFile, kubeAPIServerSignerKeyFile := getKubeAPIServerClientSignerFiles(controllerContext.ComponentConfig.CSRSigningController); len(kubeAPIServerSignerCertFile) > 0 || len(kubeAPIServerSignerKeyFile) > 0 {
		kubeAPIServerClientSigner, err := signer.NewKubeAPIServerClientCSRSigningController(c, csrInformer, kubeAPIServerSignerCertFile, kubeAPIServerSignerKeyFile, certTTL)
		if err != nil {
			return nil, false, fmt.Errorf("failed to start kubernetes.io/kube-apiserver-client certificate controller: %v", err)
		}
		go kubeAPIServerClientSigner.Run(ctx, 5)
	} else {
		klog.V(2).Infof("skipping CSR signer controller %q because specific files were specified for other signers and not this one.", "kubernetes.io/kube-apiserver-client")
	}

	if legacyUnknownSignerCertFile, legacyUnknownSignerKeyFile := getLegacyUnknownSignerFiles(controllerContext.ComponentConfig.CSRSigningController); len(legacyUnknownSignerCertFile) > 0 || len(legacyUnknownSignerKeyFile) > 0 {
		legacyUnknownSigner, err := signer.NewLegacyUnknownCSRSigningController(c, csrInformer, legacyUnknownSignerCertFile, legacyUnknownSignerKeyFile, certTTL)
		if err != nil {
			return nil, false, fmt.Errorf("failed to start kubernetes.io/legacy-unknown certificate controller: %v", err)
		}
		go legacyUnknownSigner.Run(ctx, 5)
	} else {
		klog.V(2).Infof("skipping CSR signer controller %q because specific files were specified for other signers and not this one.", "kubernetes.io/legacy-unknown")
	}

	return nil, true, nil
}

func areKubeAPIServerClientSignerFilesSpecified(config csrsigningconfig.CSRSigningControllerConfiguration) bool {
	// if only one is specified, it will error later during construction
	return len(config.KubeAPIServerClientSignerConfiguration.CertFile) > 0 || len(config.KubeAPIServerClientSignerConfiguration.KeyFile) > 0
}

func areLegacyUnknownSignerFilesSpecified(config csrsigningconfig.CSRSigningControllerConfiguration) bool {
	// if only one is specified, it will error later during construction
	return len(config.LegacyUnknownSignerConfiguration.CertFile) > 0 || len(config.LegacyUnknownSignerConfiguration.KeyFile) > 0
}

func anySpecificFilesSet(config csrsigningconfig.CSRSigningControllerConfiguration) bool {
	return areKubeAPIServerClientSignerFilesSpecified(config) ||
		areLegacyUnknownSignerFilesSpecified(config)
}

func getKubeAPIServerClientSignerFiles(config csrsigningconfig.CSRSigningControllerConfiguration) (string, string) {
	// if any cert/key is set for specific CSR signing loops, then the --cluster-signing-{cert,key}-file are not used for any CSR signing loop.
	if anySpecificFilesSet(config) {
		return config.KubeAPIServerClientSignerConfiguration.CertFile, config.KubeAPIServerClientSignerConfiguration.KeyFile
	}
	return config.ClusterSigningCertFile, config.ClusterSigningKeyFile
}

func getLegacyUnknownSignerFiles(config csrsigningconfig.CSRSigningControllerConfiguration) (string, string) {
	// if any cert/key is set for specific CSR signing loops, then the --cluster-signing-{cert,key}-file are not used for any CSR signing loop.
	if anySpecificFilesSet(config) {
		return config.LegacyUnknownSignerConfiguration.CertFile, config.LegacyUnknownSignerConfiguration.KeyFile
	}
	return config.ClusterSigningCertFile, config.ClusterSigningKeyFile
}

func startCSRApprovingController(ctx context.Context, controllerContext ControllerContext) (controller.Interface, bool, error) {
	approver := approver.NewCSRApprovingController(
		controllerContext.ClientBuilder.ClientOrDie("certificate-controller"),
		controllerContext.InformerFactory.Certificates().V1().CertificateSigningRequests(),
	)
	go approver.Run(ctx, 5)

	return nil, true, nil
}

func startCSRCleanerController(ctx context.Context, controllerContext ControllerContext) (controller.Interface, bool, error) {
	cleaner := cleaner.NewCSRCleanerController(
		controllerContext.ClientBuilder.ClientOrDie("certificate-controller").CertificatesV1().CertificateSigningRequests(),
		controllerContext.InformerFactory.Certificates().V1().CertificateSigningRequests(),
	)
	go cleaner.Run(ctx, 1)
	return nil, true, nil
}

func startRootCACertPublisher(ctx context.Context, controllerContext ControllerContext) (controller.Interface, bool, error) {
	var (
		rootCA []byte
		err    error
	)
	if controllerContext.ComponentConfig.SAController.RootCAFile != "" {
		if rootCA, err = readCA(controllerContext.ComponentConfig.SAController.RootCAFile); err != nil {
			return nil, true, fmt.Errorf("error parsing root-ca-file at %s: %v", controllerContext.ComponentConfig.SAController.RootCAFile, err)
		}
	} else {
		rootCA = controllerContext.ClientBuilder.ConfigOrDie("root-ca-cert-publisher").CAData
	}

	sac, err := rootcacertpublisher.NewPublisher(
		controllerContext.InformerFactory.Core().V1().ConfigMaps(),
		controllerContext.InformerFactory.Core().V1().Namespaces(),
		controllerContext.ClientBuilder.ClientOrDie("root-ca-cert-publisher"),
		rootCA,
	)
	if err != nil {
		return nil, true, fmt.Errorf("error creating root CA certificate publisher: %v", err)
	}
	go sac.Run(ctx, 1)
	return nil, true, nil
}
