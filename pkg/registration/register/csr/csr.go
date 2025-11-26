package csr

import (
	"context"
	"crypto/tls"
	"crypto/x509/pkix"
	"errors"
	"fmt"
	"math/rand"
	"os"
	"path"
	"strings"
	"time"

	certificates "k8s.io/api/certificates/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"
	certutil "k8s.io/client-go/util/cert"
	"k8s.io/client-go/util/keyutil"
	"k8s.io/klog/v2"
	"k8s.io/utils/pointer"

	addonv1alpha1 "open-cluster-management.io/api/addon/v1alpha1"
	clusterv1 "open-cluster-management.io/api/cluster/v1"
	"open-cluster-management.io/sdk-go/pkg/basecontroller/events"
	"open-cluster-management.io/sdk-go/pkg/basecontroller/factory"

	"open-cluster-management.io/ocm/pkg/registration/hub/user"
	"open-cluster-management.io/ocm/pkg/registration/register"
)

const (
	// TLSKeyFile is the name of tls key file in kubeconfigSecret
	TLSKeyFile = "tls.key"
	// TLSCertFile is the name of the tls cert file in kubeconfigSecret
	TLSCertFile = "tls.crt"

	// ClusterCertificateRotatedCondition is a condition type that client certificate is rotated
	ClusterCertificateRotatedCondition = "ClusterCertificateRotated"

	indexByCluster = "indexByCluster"
	indexByAddon   = "indexByAddon"

	// TODO(qiujian16) expose it if necessary in the future.
	clusterCSRThreshold = 10
	addonCSRThreshold   = 10
)

type CSRDriver struct {
	// csrName is the name of csr created by controller and waiting for approval.
	csrName string

	// keyData is the private key data used to created a csr
	// csrName and keyData store the internal state of the controller. They are set after controller creates a new csr
	// and cleared once the csr is approved and processed by controller. There are 4 combination of their values:
	//   1. csrName empty, keyData empty: means we aren't trying to create a new client cert, our current one is valid
	//   2. csrName set, keyData empty: there was bug
	//   3. csrName set, keyData set: we are waiting for a new cert to be signed.
	//   4. csrName empty, keydata set: the CSR failed to create, this shouldn't happen, it's a bug.
	keyData []byte

	csrControl CSRControl

	// HaltCSRCreation halt the csr creation
	haltCSRCreation func() bool

	opt *Option

	csrOption *CSROption
}

func (c *CSRDriver) Process(
	ctx context.Context, controllerName string, secret *corev1.Secret, additionalSecretData map[string][]byte,
	recorder events.Recorder) (*corev1.Secret, *metav1.Condition, error) {
	logger := klog.FromContext(ctx)

	// reconcile pending csr if exists
	if len(c.csrName) > 0 {
		// build a secret data map if the csr is approved
		newSecretConfig, err := func() (map[string][]byte, error) {
			// skip if there is no ongoing csr
			if len(c.csrName) == 0 {
				return nil, fmt.Errorf("no ongoing csr")
			}

			// skip if csr is not approved yet
			isApproved, err := c.csrControl.IsApproved(c.csrName)
			if err != nil {
				return nil, err
			}
			if !isApproved {
				return nil, nil
			}

			// skip if csr is not issued
			certData, err := c.csrControl.GetIssuedCertificate(c.csrName)
			if err != nil {
				return nil, err
			}
			if len(certData) == 0 {
				return nil, nil
			}

			logger.Info("Sync csr", "name", c.csrName)
			// check if cert in csr status matches with the corresponding private key
			if c.keyData == nil {
				return nil, fmt.Errorf("no private key found for certificate in csr: %s", c.csrName)
			}
			_, err = tls.X509KeyPair(certData, c.keyData)
			if err != nil {
				return nil, fmt.Errorf("private key does not match with the certificate in csr: %s", c.csrName)
			}

			data := map[string][]byte{
				TLSCertFile: certData,
				TLSKeyFile:  c.keyData,
			}

			return data, nil
		}()

		if err != nil {
			c.reset()
			return secret, &metav1.Condition{
				Type:    "ClusterCertificateRotated",
				Status:  metav1.ConditionFalse,
				Reason:  "ClientCertificateUpdateFailed",
				Message: fmt.Sprintf("Failed to rotated client certificate %v", err),
			}, err
		}
		if len(newSecretConfig) == 0 {
			return nil, nil, nil
		}
		// append additional data into client certificate secret
		for k, v := range newSecretConfig {
			secret.Data[k] = v
		}

		notBefore, notAfter, err := getCertValidityPeriod(secret)

		cond := &metav1.Condition{
			Type:    "ClusterCertificateRotated",
			Status:  metav1.ConditionTrue,
			Reason:  "ClientCertificateUpdated",
			Message: fmt.Sprintf("client certificate rotated starting from %v to %v", *notBefore, *notAfter),
		}

		if err != nil {
			cond = &metav1.Condition{
				Type:    "ClusterCertificateRotated",
				Status:  metav1.ConditionFalse,
				Reason:  "ClientCertificateUpdateFailed",
				Message: fmt.Sprintf("Failed to rotated client certificate %v", err),
			}
		} else {
			recorder.Eventf(ctx, "ClientCertificateCreated", "A new client certificate for %s is available", controllerName)
		}
		c.reset()
		return secret, cond, err
	}

	// create a csr to request new client certificate if
	// a. there is no valid client certificate issued for the current cluster/agent;
	// b. client certificate is sensitive to the additional secret data and the data changes;
	// c. client certificate exists and has less than a random percentage range from 20% to 25% of its life remaining;
	shouldCreate, err := shouldCreateCSR(
		ctx,
		logger,
		controllerName,
		secret,
		recorder,
		c.csrOption.Subject,
		additionalSecretData)
	if err != nil {
		return secret, nil, err
	}
	if !shouldCreate {
		return nil, nil, nil
	}

	shouldHalt := c.haltCSRCreation()
	if shouldHalt {
		recorder.Eventf(ctx, "ClientCertificateCreationHalted",
			"Stop creating csr since there are too many csr created already on hub", controllerName)
		return nil, &metav1.Condition{
			Type:    "ClusterCertificateRotated",
			Status:  metav1.ConditionFalse,
			Reason:  "ClientCertificateUpdateFailed",
			Message: "Stop creating csr since there are too many csr created already on hub",
		}, nil
	}

	keyData, createdCSRName, err := func() ([]byte, string, error) {
		// create a new private key
		keyData, err := keyutil.MakeEllipticPrivateKeyPEM()
		if err != nil {
			return nil, "", err
		}

		privateKey, err := keyutil.ParsePrivateKeyPEM(keyData)
		if err != nil {
			return keyData, "", fmt.Errorf("invalid private key for certificate request: %w", err)
		}
		csrData, err := certutil.MakeCSR(privateKey, c.csrOption.Subject, c.csrOption.DNSNames, nil)
		if err != nil {
			return keyData, "", fmt.Errorf("unable to generate certificate request: %w", err)
		}

		// do not set expiration second if it is 0
		expirationSeconds := pointer.Int32(c.opt.ExpirationSeconds)
		if *expirationSeconds == 0 {
			expirationSeconds = nil
		}
		createdCSRName, err := c.csrControl.Create(
			ctx, recorder, c.csrOption.ObjectMeta, csrData, c.csrOption.SignerName, expirationSeconds)
		if err != nil {
			return keyData, "", err
		}
		return keyData, createdCSRName, nil
	}()
	if err != nil {
		return nil, &metav1.Condition{
			Type:    "ClusterCertificateRotated",
			Status:  metav1.ConditionFalse,
			Reason:  "ClientCertificateUpdateFailed",
			Message: fmt.Sprintf("Failed to create CSR %v", err),
		}, err
	}

	c.keyData = keyData
	c.csrName = createdCSRName
	return nil, nil, nil
}

func (c *CSRDriver) reset() {
	c.csrName = ""
	c.keyData = nil
}

func (c *CSRDriver) BuildKubeConfigFromTemplate(kubeConfig *clientcmdapi.Config) *clientcmdapi.Config {
	kubeConfig.AuthInfos = map[string]*clientcmdapi.AuthInfo{register.DefaultKubeConfigAuth: {
		ClientCertificate: TLSCertFile,
		ClientKey:         TLSKeyFile,
	}}

	return kubeConfig
}

func (c *CSRDriver) InformerHandler() (cache.SharedIndexInformer, factory.EventFilterFunc) {
	return c.csrControl.Informer(), c.csrOption.EventFilterFunc
}

func (c *CSRDriver) IsHubKubeConfigValid(ctx context.Context, secretOption register.SecretOption) (bool, error) {
	logger := klog.FromContext(ctx)
	keyPath := path.Join(secretOption.HubKubeconfigDir, TLSKeyFile)
	if _, err := os.Stat(keyPath); os.IsNotExist(err) {
		logger.V(4).Info("TLS key file not found", "keyPath", keyPath)
		return false, nil
	}

	certPath := path.Join(secretOption.HubKubeconfigDir, TLSCertFile)
	certData, err := os.ReadFile(path.Clean(certPath))
	if err != nil {
		logger.V(4).Info("Unable to load TLS cert file", "certPath", certPath)
		return false, nil
	}

	// only set when clustername/agentname are set
	if len(secretOption.ClusterName) > 0 && len(secretOption.AgentName) > 0 {
		// check if the tls certificate is issued for the current cluster/agent
		clusterNameInCert, agentNameInCert, err := GetClusterAgentNamesFromCertificate(certData)
		if err != nil {
			return false, nil
		}
		if secretOption.ClusterName != clusterNameInCert || secretOption.AgentName != agentNameInCert {
			logger.V(4).Info("Certificate in file is issued for different agent",
				"certPath", certPath,
				"issuedFor", fmt.Sprintf("%s:%s", clusterNameInCert, agentNameInCert),
				"expectedFor", fmt.Sprintf("%s:%s", secretOption.ClusterName, secretOption.AgentName))

			return false, nil
		}
	}

	return IsCertificateValid(logger, certData, nil)
}

func (c *CSRDriver) ManagedClusterDecorator(cluster *clusterv1.ManagedCluster) *clusterv1.ManagedCluster {
	return cluster
}

func (c *CSRDriver) Fork(addonName string, secretOption register.SecretOption) register.RegisterDriver {
	csrOption := &CSROption{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: fmt.Sprintf("addon-%s-%s-", secretOption.ClusterName, addonName),
			Labels: map[string]string{
				// the labels are only hints. Anyone could set/modify them.
				clusterv1.ClusterNameLabelKey: secretOption.ClusterName,
				addonv1alpha1.AddonLabelKey:   addonName,
			},
		},
		Subject:         secretOption.Subject,
		DNSNames:        []string{fmt.Sprintf("%s.addon.open-cluster-management.io", addonName)},
		SignerName:      secretOption.Signer,
		EventFilterFunc: createCSREventFilterFunc(secretOption.ClusterName, addonName, secretOption.Signer),
	}

	driver := &CSRDriver{
		csrOption:       csrOption,
		opt:             c.opt,
		csrControl:      c.csrControl,
		haltCSRCreation: haltAddonCSRCreationFunc(c.csrControl.Informer().GetIndexer(), secretOption.ClusterName, addonName),
	}

	return driver
}

func (c *CSRDriver) BuildClients(ctx context.Context, secretOption register.SecretOption, bootstrap bool) (*register.Clients, error) {
	logger := klog.FromContext(ctx)

	kubeConfig, err := register.KubeConfigFromSecretOption(secretOption, bootstrap)
	if err != nil {
		return nil, err
	}
	clients, err := register.BuildClientsFromConfig(kubeConfig, secretOption.ClusterName)
	if err != nil {
		return nil, err
	}

	kubeClient, err := kubernetes.NewForConfig(kubeConfig)
	if err != nil {
		return nil, err
	}
	kubeInformerFactory := informers.NewSharedInformerFactoryWithOptions(
		kubeClient,
		10*time.Minute,
		informers.WithTweakListOptions(func(listOptions *metav1.ListOptions) {
			listOptions.LabelSelector = fmt.Sprintf("%s=%s", clusterv1.ClusterNameLabelKey, secretOption.ClusterName)
		}),
	)
	csrControl, err := NewCSRControl(logger, kubeInformerFactory.Certificates(), kubeClient)
	if err != nil {
		return nil, fmt.Errorf("failed to create CSR control: %w", err)
	}

	err = c.SetCSRControl(csrControl, secretOption.ClusterName)
	if err != nil {
		return nil, err
	}
	return clients, nil
}

func (c *CSRDriver) SetCSRControl(csrControl CSRControl, clusterName string) error {
	if err := csrControl.Informer().AddIndexers(cache.Indexers{indexByCluster: indexByClusterFunc}); err != nil {
		return err
	}

	if err := csrControl.Informer().AddIndexers(cache.Indexers{indexByAddon: indexByAddonFunc}); err != nil {
		return err
	}

	c.csrControl = csrControl
	c.haltCSRCreation = haltCSRCreationFunc(csrControl.Informer().GetIndexer(), clusterName)
	return nil
}

var _ register.RegisterDriver = &CSRDriver{}
var _ register.AddonDriver = &CSRDriver{}

func NewCSRDriver(opt *Option, secretOpts register.SecretOption) (*CSRDriver, error) {
	signer := certificates.KubeAPIServerClientSignerName
	if secretOpts.Signer != "" {
		signer = secretOpts.Signer
	}

	// bootstrapKubeConfigFile is required when the signer is kubeclient
	if signer == certificates.KubeAPIServerClientSignerName && len(secretOpts.BootStrapKubeConfigFile) == 0 {
		return nil, errors.New("bootstrap-kubeconfig is required")
	}

	driver := &CSRDriver{
		opt: opt,
	}
	driver.csrOption = &CSROption{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: fmt.Sprintf("%s-", secretOpts.ClusterName),
			Labels: map[string]string{
				// the label is only an hint for cluster name. Anyone could set/modify it.
				clusterv1.ClusterNameLabelKey: secretOpts.ClusterName,
			},
		},
		Subject: &pkix.Name{
			Organization: []string{
				fmt.Sprintf("%s%s", user.SubjectPrefix, secretOpts.ClusterName),
				user.ManagedClustersGroup,
			},
			CommonName: fmt.Sprintf("%s%s:%s", user.SubjectPrefix, secretOpts.ClusterName, secretOpts.AgentName),
		},
		SignerName: signer,
		EventFilterFunc: func(obj interface{}) bool {
			accessor, err := meta.Accessor(obj)
			if err != nil {
				return false
			}
			labels := accessor.GetLabels()
			// only enqueue csr from a specific managed cluster
			if labels[clusterv1.ClusterNameLabelKey] != secretOpts.ClusterName {
				return false
			}

			// should not contain addon key
			_, ok := labels[addonv1alpha1.AddonLabelKey]
			if ok {
				return false
			}

			// only enqueue csr whose name starts with the cluster name
			return strings.HasPrefix(accessor.GetName(), fmt.Sprintf("%s-", secretOpts.ClusterName))
		},
	}

	return driver, nil
}

func shouldCreateCSR(
	ctx context.Context,
	logger klog.Logger,
	controllerName string,
	secret *corev1.Secret,
	recorder events.Recorder,
	subject *pkix.Name,
	additionalSecretData map[string][]byte) (bool, error) {
	// create a csr to request new client certificate if
	// a.there is no valid client certificate issued for the current cluster/agent
	valid, err := IsCertificateValid(logger, secret.Data[TLSCertFile], subject)
	if err != nil {
		recorder.Eventf(ctx, "CertificateValidationFailed", "Failed to validate client certificate for %s: %v", controllerName, err)
		return true, nil
	}
	if !valid {
		recorder.Eventf(ctx, "NoValidCertificateFound", "No valid client certificate for %s is found. Bootstrap is required", controllerName)
		return true, nil
	}

	// b.client certificate is sensitive to the additional secret data and the data changes
	if err := hasAdditionalSecretData(additionalSecretData, secret); err != nil {
		recorder.Eventf(ctx, "AdditonalSecretDataChanged", "The additional secret data is changed for %v. Re-create the client certificate for %s", err, controllerName)
		return true, nil
	}

	// c.client certificate exists and has less than a random percentage range from 20% to 25% of its life remaining
	notBefore, notAfter, err := getCertValidityPeriod(secret)
	if err != nil {
		return false, err
	}
	total := notAfter.Sub(*notBefore)
	remaining := time.Until(*notAfter)
	logger.V(4).Info("Client certificate for:", "name", controllerName, "time total", total,
		"remaining", remaining, "remaining/total", remaining.Seconds()/total.Seconds())
	threshold := jitter(0.2, 0.25)
	if remaining.Seconds()/total.Seconds() > threshold {
		// Do nothing if the client certificate is valid and has more than a random percentage range from 20% to 25% of its life remaining
		logger.V(4).Info("Client certificate for:", "name", controllerName, "time total", total,
			"remaining", remaining, "remaining/total", remaining.Seconds()/total.Seconds())
		return false, nil
	}
	recorder.Eventf(ctx, "CertificateRotationStarted",
		"The current client certificate for %s expires in %v. Start certificate rotation",
		controllerName, remaining.Round(time.Second))
	return true, nil
}

// hasAdditonalSecretData checks if the secret includes the expected additional secret data.
func hasAdditionalSecretData(additionalSecretData map[string][]byte, secret *corev1.Secret) error {
	for k, v := range additionalSecretData {
		value, ok := secret.Data[k]
		if !ok {
			return fmt.Errorf("key %q not found in secret %q", k, secret.Namespace+"/"+secret.Name)
		}

		if !equality.Semantic.DeepEqual(v, value) {
			return fmt.Errorf("key %q in secret %q does not match the expected value",
				k, secret.Namespace+"/"+secret.Name)
		}
	}
	return nil
}

func jitter(percentage float64, maxFactor float64) float64 {
	if maxFactor <= 0.0 {
		maxFactor = 1.0
	}
	newPercentage := percentage + percentage*rand.Float64()*maxFactor //#nosec G404
	return newPercentage
}

func indexByAddonFunc(obj interface{}) ([]string, error) {
	accessor, err := meta.Accessor(obj)
	if err != nil {
		return nil, err
	}

	cluster, ok := accessor.GetLabels()[clusterv1.ClusterNameLabelKey]
	if !ok {
		return []string{}, nil
	}

	addon, ok := accessor.GetLabels()[addonv1alpha1.AddonLabelKey]
	if !ok {
		return []string{}, nil
	}

	return []string{fmt.Sprintf("%s/%s", cluster, addon)}, nil
}

func indexByClusterFunc(obj interface{}) ([]string, error) {
	accessor, err := meta.Accessor(obj)
	if err != nil {
		return nil, err
	}

	cluster, ok := accessor.GetLabels()[clusterv1.ClusterNameLabelKey]
	if !ok {
		return []string{}, nil
	}

	// should not contain addon key
	if _, ok := accessor.GetLabels()[addonv1alpha1.AddonLabelKey]; ok {
		return []string{}, nil
	}

	return []string{cluster}, nil
}

func createCSREventFilterFunc(clusterName, addOnName, signerName string) factory.EventFilterFunc {
	return func(obj interface{}) bool {
		accessor, err := meta.Accessor(obj)
		if err != nil {
			return false
		}
		labels := accessor.GetLabels()
		// only enqueue csr from a specific managed cluster
		if labels[clusterv1.ClusterNameLabelKey] != clusterName {
			return false
		}
		// only enqueue csr created for a specific addon
		if labels[addonv1alpha1.AddonLabelKey] != addOnName {
			return false
		}

		// only enqueue csr with a specific signer name
		csr, ok := obj.(*certificates.CertificateSigningRequest)
		if !ok {
			return false
		}
		if len(csr.Spec.SignerName) == 0 {
			return false
		}
		if csr.Spec.SignerName != signerName {
			return false
		}
		return true
	}
}
