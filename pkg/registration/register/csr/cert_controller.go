package csr

import (
	"context"
	"crypto/tls"
	"crypto/x509/pkix"
	"fmt"
	"math/rand"
	"reflect"
	"time"

	"github.com/openshift/library-go/pkg/controller/factory"
	"github.com/openshift/library-go/pkg/operator/events"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	corev1informers "k8s.io/client-go/informers/core/v1"
	corev1client "k8s.io/client-go/kubernetes/typed/core/v1"
	certutil "k8s.io/client-go/util/cert"
	"k8s.io/client-go/util/keyutil"
	"k8s.io/klog/v2"

	"open-cluster-management.io/ocm/pkg/registration/register"
)

// ControllerResyncInterval is exposed so that integration tests can crank up the constroller sync speed.
var ControllerResyncInterval = 5 * time.Minute

type secretOption struct {
	// SecretNamespace is the namespace of the secret containing client certificate.
	secretNamespace string
	// SecretName is the name of the secret containing client certificate. The secret will be created if
	// it does not exist.
	secretName string
	// AdditonalSecretData contains data that will be added into the secret
	additionalSecretData map[string][]byte
}

// clientCertificateController implements the common logic of hub client certification creation/rotation. It
// creates a client certificate and rotates it before it becomes expired by using csrs. The client
// certificate generated is stored in a specific secret with the keys below:
// 1). tls.key: tls key file
// 2). tls.crt: tls cert file
type clientCertificateController struct {
	secretOption
	CSROption
	controllerName string

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

	managementCoreClient corev1client.CoreV1Interface
	statusUpdater        register.StatusUpdateFunc
}

// NewClientCertificateController return an instance of clientCertificateController
func NewClientCertificateController(
	secretNamespace string,
	secretName string,
	additionalSecretData map[string][]byte,
	csrOption CSROption,
	statusUpdater register.StatusUpdateFunc,
	managementSecretInformer corev1informers.SecretInformer,
	managementCoreClient corev1client.CoreV1Interface,
	recorder events.Recorder,
	controllerName string,
) factory.Controller {
	c := clientCertificateController{
		secretOption: secretOption{
			secretName:           secretName,
			secretNamespace:      secretNamespace,
			additionalSecretData: additionalSecretData,
		},
		CSROption:            csrOption,
		controllerName:       controllerName,
		statusUpdater:        statusUpdater,
		managementCoreClient: managementCoreClient,
	}

	return factory.New().
		WithFilteredEventsInformersQueueKeyFunc(func(obj runtime.Object) string {
			return factory.DefaultQueueKey
		}, func(obj interface{}) bool {
			accessor, err := meta.Accessor(obj)
			if err != nil {
				return false
			}
			// only enqueue a specific secret
			if accessor.GetNamespace() == c.secretNamespace && accessor.GetName() == c.secretName {
				return true
			}
			return false
		}, managementSecretInformer.Informer()).
		WithFilteredEventsInformersQueueKeyFunc(func(obj runtime.Object) string {
			return factory.DefaultQueueKey
		}, c.EventFilterFunc, csrOption.CSRControl.Informer()).
		WithSync(c.sync).
		ResyncEvery(ControllerResyncInterval).
		ToController(controllerName, recorder)
}

func (c *clientCertificateController) sync(ctx context.Context, syncCtx factory.SyncContext) error {
	logger := klog.FromContext(ctx)
	// get secret containing client certificate
	secret, err := c.managementCoreClient.Secrets(c.secretNamespace).Get(ctx, c.secretName, metav1.GetOptions{})
	switch {
	case apierrors.IsNotFound(err):
		secret = &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: c.secretNamespace,
				Name:      c.secretName,
			},
		}
	case err != nil:
		return fmt.Errorf("unable to get secret %q: %w", c.secretNamespace+"/"+c.secretName, err)
	}

	// reconcile pending csr if exists
	if len(c.csrName) > 0 {
		// build a secret data map if the csr is approved
		newSecretConfig, err := func() (map[string][]byte, error) {
			// skip if there is no ongoing csr
			if len(c.csrName) == 0 {
				return nil, fmt.Errorf("no ongoing csr")
			}

			// skip if csr is not approved yet
			isApproved, err := c.CSROption.CSRControl.isApproved(c.csrName)
			if err != nil {
				return nil, err
			}
			if !isApproved {
				return nil, nil
			}

			// skip if csr is not issued
			certData, err := c.CSROption.CSRControl.getIssuedCertificate(c.csrName)
			if err != nil {
				return nil, err
			}
			if len(certData) == 0 {
				return nil, nil
			}

			logger.V(4).Info("Sync csr", "name", c.csrName)
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
			if updateErr := c.statusUpdater(ctx, metav1.Condition{
				Type:    "ClusterCertificateRotated",
				Status:  metav1.ConditionFalse,
				Reason:  "ClientCertificateUpdateFailed",
				Message: fmt.Sprintf("Failed to rotated client certificate %v", err),
			}); updateErr != nil {
				return updateErr
			}
			return err
		}
		if len(newSecretConfig) == 0 {
			return nil
		}
		// append additional data into client certificate secret
		for k, v := range c.additionalSecretData {
			newSecretConfig[k] = v
		}
		secret.Data = newSecretConfig
		// save the changes into secret
		if err := saveSecret(c.managementCoreClient, c.secretNamespace, secret); err != nil {
			if updateErr := c.statusUpdater(ctx, metav1.Condition{
				Type:    "ClusterCertificateRotated",
				Status:  metav1.ConditionFalse,
				Reason:  "ClientCertificateUpdateFailed",
				Message: fmt.Sprintf("Failed to rotated client certificate %v", err),
			}); updateErr != nil {
				return updateErr
			}
			return err
		}

		notBefore, notAfter, err := getCertValidityPeriod(secret)

		cond := metav1.Condition{
			Type:    "ClusterCertificateRotated",
			Status:  metav1.ConditionTrue,
			Reason:  "ClientCertificateUpdated",
			Message: fmt.Sprintf("client certificate rotated starting from %v to %v", *notBefore, *notAfter),
		}

		if err != nil {
			cond = metav1.Condition{
				Type:    "ClusterCertificateRotated",
				Status:  metav1.ConditionFalse,
				Reason:  "ClientCertificateUpdateFailed",
				Message: fmt.Sprintf("Failed to rotated client certificate %v", err),
			}
		}
		if updateErr := c.statusUpdater(ctx, cond); updateErr != nil {
			return updateErr
		}

		if err != nil {
			c.reset()
			return err
		}

		syncCtx.Recorder().Eventf("ClientCertificateCreated", "A new client certificate for %s is available", c.controllerName)
		c.reset()
		return nil
	}

	// create a csr to request new client certificate if
	// a. there is no valid client certificate issued for the current cluster/agent;
	// b. client certificate is sensitive to the additional secret data and the data changes;
	// c. client certificate exists and has less than a random percentage range from 20% to 25% of its life remaining;
	shouldCreate, err := shouldCreateCSR(
		logger,
		c.controllerName,
		secret,
		syncCtx.Recorder(),
		c.Subject,
		c.additionalSecretData)
	if err != nil {
		return err
	}
	if !shouldCreate {
		return nil
	}

	shouldHalt := c.CSROption.HaltCSRCreation()
	if shouldHalt {
		if updateErr := c.statusUpdater(ctx, metav1.Condition{
			Type:    "ClusterCertificateRotated",
			Status:  metav1.ConditionFalse,
			Reason:  "ClientCertificateUpdateFailed",
			Message: "Stop creating csr since there are too many csr created already on hub",
		}); updateErr != nil {
			return updateErr
		}
		syncCtx.Recorder().Eventf("ClientCertificateCreationHalted", "Stop creating csr since there are too many csr created already on hub", c.controllerName)
		return nil
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
		csrData, err := certutil.MakeCSR(privateKey, c.Subject, c.DNSNames, nil)
		if err != nil {
			return keyData, "", fmt.Errorf("unable to generate certificate request: %w", err)
		}
		createdCSRName, err := c.CSROption.CSRControl.create(ctx, syncCtx.Recorder(), c.ObjectMeta, csrData, c.SignerName, c.ExpirationSeconds)
		if err != nil {
			return keyData, "", err
		}
		return keyData, createdCSRName, nil
	}()
	if err != nil {
		if updateErr := c.statusUpdater(ctx, metav1.Condition{
			Type:    "ClusterCertificateRotated",
			Status:  metav1.ConditionFalse,
			Reason:  "ClientCertificateUpdateFailed",
			Message: fmt.Sprintf("Failed to create CSR %v", err),
		}); updateErr != nil {
			return updateErr
		}
		return err
	}

	c.keyData = keyData
	c.csrName = createdCSRName
	return nil
}

func saveSecret(spokeCoreClient corev1client.CoreV1Interface, secretNamespace string, secret *corev1.Secret) error {
	var err error
	if secret.ResourceVersion == "" {
		_, err = spokeCoreClient.Secrets(secretNamespace).Create(context.Background(), secret, metav1.CreateOptions{})
		return err
	}
	_, err = spokeCoreClient.Secrets(secretNamespace).Update(context.Background(), secret, metav1.UpdateOptions{})
	return err
}

func (c *clientCertificateController) reset() {
	c.csrName = ""
	c.keyData = nil
}

func shouldCreateCSR(
	logger klog.Logger,
	controllerName string,
	secret *corev1.Secret,
	recorder events.Recorder,
	subject *pkix.Name,
	additionalSecretData map[string][]byte) (bool, error) {
	// create a csr to request new client certificate if
	// a.there is no valid client certificate issued for the current cluster/agent
	valid, err := isCertificateValid(logger, secret.Data[TLSCertFile], subject)
	if err != nil {
		recorder.Eventf("CertificateValidationFailed", "Failed to validate client certificate for %s: %v", controllerName, err)
		return true, nil
	}
	if !valid {
		recorder.Eventf("NoValidCertificateFound", "No valid client certificate for %s is found. Bootstrap is required", controllerName)
		return true, nil
	}

	// b.client certificate is sensitive to the additional secret data and the data changes
	if err := hasAdditionalSecretData(additionalSecretData, secret); err != nil {
		recorder.Eventf("AdditonalSecretDataChanged", "The additional secret data is changed for %v. Re-create the client certificate for %s", err, controllerName)
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
	recorder.Eventf("CertificateRotationStarted",
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

		if !reflect.DeepEqual(v, value) {
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
