package certrotationcontroller

import (
	"context"
	"fmt"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	kubeinformers "k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	fakekube "k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/util/cert"

	fakeoperatorclient "open-cluster-management.io/api/client/operator/clientset/versioned/fake"
	operatorinformers "open-cluster-management.io/api/client/operator/informers/externalversions"
	operatorapiv1 "open-cluster-management.io/api/operator/v1"
	"open-cluster-management.io/registration-operator/pkg/certrotation"
	"open-cluster-management.io/registration-operator/pkg/helpers"
	testinghelper "open-cluster-management.io/registration-operator/pkg/helpers/testing"
)

const testClusterManagerName = "testclustermanager"

var secretNames = []string{"signer-key-pair-secret", "serving-cert-key-pair-secret"}

func newClusterManager() *operatorapiv1.ClusterManager {
	return &operatorapiv1.ClusterManager{
		ObjectMeta: metav1.ObjectMeta{
			Name: testClusterManagerName,
		},
		Spec: operatorapiv1.ClusterManagerSpec{
			RegistrationImagePullSpec: "testregistration",
		},
	}
}

type validateFunc func(t *testing.T, kubeClient kubernetes.Interface, err error)

func TestCertRotation(t *testing.T) {
	cases := []struct {
		name            string
		clusterManager  *operatorapiv1.ClusterManager
		existingObjects []runtime.Object
		validate        validateFunc
	}{
		{
			name: "no cluster manager",
			validate: func(t *testing.T, kubeClient kubernetes.Interface, err error) {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				assertNoSecretCreated(t, kubeClient)
			},
		},
		{
			name:           "no namespace",
			clusterManager: newClusterManager(),
			validate: func(t *testing.T, kubeClient kubernetes.Interface, err error) {
				if err == nil {
					t.Fatalf("expected an error")
				}
				assertNoSecretCreated(t, kubeClient)
			},
		},
		{
			name:           "rotate cert",
			clusterManager: newClusterManager(),
			existingObjects: []runtime.Object{
				&corev1.Namespace{
					ObjectMeta: metav1.ObjectMeta{
						Name: helpers.ClusterManagerNamespace,
					},
				},
			},
			validate: func(t *testing.T, kubeClient kubernetes.Interface, err error) {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				assertSecretsExistAndValid(t, kubeClient)
			},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			kubeClient := fakekube.NewSimpleClientset(c.existingObjects...)
			kubeInformer := kubeinformers.NewSharedInformerFactory(kubeClient, 5*time.Minute)
			clusterManagers := []runtime.Object{}
			if c.clusterManager != nil {
				clusterManagers = append(clusterManagers, c.clusterManager)
			}
			operatorClient := fakeoperatorclient.NewSimpleClientset(clusterManagers...)
			operatorInformers := operatorinformers.NewSharedInformerFactory(operatorClient, 5*time.Minute)
			clusterManagerStore := operatorInformers.Operator().V1().ClusterManagers().Informer().GetStore()
			for _, clusterManager := range clusterManagers {
				clusterManagerStore.Add(clusterManager)
			}

			syncContext := testinghelper.NewFakeSyncContext(t, "")
			recorder := syncContext.Recorder()

			signingRotation := certrotation.SigningRotation{
				Namespace:        helpers.ClusterManagerNamespace,
				Name:             "signer-key-pair-secret",
				SignerNamePrefix: "test-signer",
				Validity:         time.Hour * 1,
				Lister:           kubeInformer.Core().V1().Secrets().Lister(),
				Client:           kubeClient.CoreV1(),
				EventRecorder:    recorder,
			}

			caBundleRotation := certrotation.CABundleRotation{
				Namespace:     helpers.ClusterManagerNamespace,
				Name:          "ca-bundle-configmap",
				Lister:        kubeInformer.Core().V1().ConfigMaps().Lister(),
				Client:        kubeClient.CoreV1(),
				EventRecorder: recorder,
			}
			targetRotations := []certrotation.TargetRotation{
				{
					Namespace:     helpers.ClusterManagerNamespace,
					Name:          "serving-cert-key-pair-secret",
					Validity:      time.Minute * 30,
					HostNames:     []string{fmt.Sprintf("service1.%s.svc", helpers.ClusterManagerNamespace)},
					Lister:        kubeInformer.Core().V1().Secrets().Lister(),
					Client:        kubeClient.CoreV1(),
					EventRecorder: recorder,
				},
			}

			controller := &certRotationController{
				signingRotation:      signingRotation,
				caBundleRotation:     caBundleRotation,
				targetRotations:      targetRotations,
				kubeClient:           kubeClient,
				clusterManagerLister: operatorInformers.Operator().V1().ClusterManagers().Lister(),
			}

			err := controller.sync(context.TODO(), syncContext)
			c.validate(t, kubeClient, err)
		})
	}
}

func assertNoSecretCreated(t *testing.T, kubeClient kubernetes.Interface) {
	for _, name := range secretNames {
		_, err := kubeClient.CoreV1().Secrets(helpers.ClusterManagerNamespace).Get(context.Background(), name, metav1.GetOptions{})
		if err == nil {
			t.Fatalf("unexpected secret %q", name)
		}
		if !errors.IsNotFound(err) {
			t.Fatalf("unexpected error: %v", err)
		}
	}
}

func assertSecretsExistAndValid(t *testing.T, kubeClient kubernetes.Interface) {
	configmap, err := kubeClient.CoreV1().ConfigMaps(helpers.ClusterManagerNamespace).Get(context.Background(), "ca-bundle-configmap", metav1.GetOptions{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	for _, name := range secretNames {
		secret, err := kubeClient.CoreV1().Secrets(helpers.ClusterManagerNamespace).Get(context.Background(), name, metav1.GetOptions{})
		if errors.IsNotFound(err) {
			t.Fatalf("secret not found: %v", name)
		}
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		certificates, err := cert.ParseCertsPEM(secret.Data["tls.crt"])
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(certificates) == 0 {
			t.Fatalf("no certificate found")
		}

		now := time.Now()
		certificate := certificates[0]
		if now.After(certificate.NotAfter) {
			t.Fatalf("invalid NotAfter: %s", name)
		}
		if now.Before(certificate.NotBefore) {
			t.Fatalf("invalid NotBefore: %s", name)
		}

		if name == "signer-key-pair-secret" {
			continue
		}

		// ensure signing cert of serving certs in the ca bundle configmap
		caCerts, err := cert.ParseCertsPEM([]byte(configmap.Data["ca-bundle.crt"]))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		found := false
		for _, caCert := range caCerts {
			if certificate.Issuer.CommonName != caCert.Subject.CommonName {
				continue
			}
			if now.After(caCert.NotAfter) {
				t.Fatalf("invalid NotAfter of ca: %s", name)
			}
			if now.Before(caCert.NotBefore) {
				t.Fatalf("invalid NotBefore of ca: %s", name)
			}
			found = true
			break
		}
		if !found {
			t.Fatalf("no issuer found: %s", name)
		}
	}
}
