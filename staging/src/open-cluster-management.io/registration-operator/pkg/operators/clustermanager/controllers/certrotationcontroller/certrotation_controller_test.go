package certrotationcontroller

import (
	"context"
	"testing"
	"time"

	"github.com/openshift/library-go/pkg/controller/factory"
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
	"open-cluster-management.io/registration-operator/pkg/helpers"
	testinghelper "open-cluster-management.io/registration-operator/pkg/helpers/testing"
)

const (
	testClusterManagerNameDefault = "testclustermanager-default"
	testClusterManagerNameHosted  = "testclustermanager-hosted"
)

var secretNames = []string{signerSecret, helpers.RegistrationWebhookSecret, helpers.WorkWebhookSecret}

func newClusterManager(name string, mode operatorapiv1.InstallMode) *operatorapiv1.ClusterManager {
	return &operatorapiv1.ClusterManager{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
		Spec: operatorapiv1.ClusterManagerSpec{
			RegistrationImagePullSpec: "testregistration",
			DeployOption: operatorapiv1.ClusterManagerDeployOption{
				Mode: mode,
			},
		},
	}
}

type validateFunc func(t *testing.T, kubeClient kubernetes.Interface, err error)

func TestCertRotation(t *testing.T) {
	cases := []struct {
		name            string
		clusterManagers []*operatorapiv1.ClusterManager
		existingObjects []runtime.Object
		queueKey        string
		validate        validateFunc
	}{
		{
			name:     "Sync All clustermanagers when no clustermanager created yet",
			queueKey: factory.DefaultQueueKey,
			validate: func(t *testing.T, kubeClient kubernetes.Interface, err error) {
				if err != nil {
					t.Fatalf("expected no error, but get %q", err)
				}
				secretList, err := kubeClient.CoreV1().Secrets("").List(context.Background(), metav1.ListOptions{})
				if err != nil {
					t.Fatalf("expected no error when list secret:%s", err)
				}
				if len(secretList.Items) > 0 {
					t.Fatal("expected no secret created")
				}
			},
		},
		{
			name: "Sync one clustermanager when the namespace not created yet",
			clusterManagers: []*operatorapiv1.ClusterManager{
				newClusterManager(testClusterManagerNameDefault, operatorapiv1.InstallModeDefault),
			},
			queueKey: testClusterManagerNameDefault,
			validate: func(t *testing.T, kubeClient kubernetes.Interface, err error) {
				if err == nil {
					t.Fatalf("expected an error")
				}
				secretList, err := kubeClient.CoreV1().Secrets("").List(context.Background(), metav1.ListOptions{})
				if err != nil {
					t.Fatalf("expected no error when list secret:%s", err)
				}
				if len(secretList.Items) > 0 {
					t.Fatal("expected no secret created")
				}
			},
		},
		{
			name: "Sync one clustermanager when there are two clustermanager",
			clusterManagers: []*operatorapiv1.ClusterManager{
				newClusterManager(testClusterManagerNameDefault, operatorapiv1.InstallModeDefault),
				newClusterManager(testClusterManagerNameHosted, operatorapiv1.InstallModeHosted),
			},
			existingObjects: []runtime.Object{
				&corev1.Namespace{
					ObjectMeta: metav1.ObjectMeta{
						Name: helpers.ClusterManagerNamespace(testClusterManagerNameDefault, operatorapiv1.InstallModeDefault),
					},
				},
				&corev1.Namespace{
					ObjectMeta: metav1.ObjectMeta{
						Name: helpers.ClusterManagerNamespace(testClusterManagerNameHosted, operatorapiv1.InstallModeHosted),
					},
				},
			},
			queueKey: testClusterManagerNameDefault,
			validate: func(t *testing.T, kubeClient kubernetes.Interface, err error) {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				assertResourcesExistAndValid(t, kubeClient, helpers.ClusterManagerNamespace(testClusterManagerNameDefault, operatorapiv1.InstallModeDefault))
				assertResourcesNotExist(t, kubeClient, helpers.ClusterManagerNamespace(testClusterManagerNameHosted, operatorapiv1.InstallModeHosted))
			},
		},
		{
			name: "Sync all clustermanagaers",
			clusterManagers: []*operatorapiv1.ClusterManager{
				newClusterManager(testClusterManagerNameDefault, operatorapiv1.InstallModeDefault),
				newClusterManager(testClusterManagerNameHosted, operatorapiv1.InstallModeHosted),
			},
			existingObjects: []runtime.Object{
				&corev1.Namespace{
					ObjectMeta: metav1.ObjectMeta{
						Name: helpers.ClusterManagerNamespace(testClusterManagerNameDefault, operatorapiv1.InstallModeDefault),
					},
				},
				&corev1.Namespace{
					ObjectMeta: metav1.ObjectMeta{
						Name: helpers.ClusterManagerNamespace(testClusterManagerNameHosted, operatorapiv1.InstallModeHosted),
					},
				},
			},
			queueKey: factory.DefaultQueueKey,
			validate: func(t *testing.T, kubeClient kubernetes.Interface, err error) {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				assertResourcesExistAndValid(t, kubeClient, helpers.ClusterManagerNamespace(testClusterManagerNameDefault, operatorapiv1.InstallModeDefault))
				assertResourcesExistAndValid(t, kubeClient, helpers.ClusterManagerNamespace(testClusterManagerNameHosted, operatorapiv1.InstallModeHosted))
			},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			kubeClient := fakekube.NewSimpleClientset(c.existingObjects...)
			kubeInformer := kubeinformers.NewSharedInformerFactory(kubeClient, 5*time.Minute)
			clusterManagers := []runtime.Object{}
			for i := range c.clusterManagers {
				clusterManagers = append(clusterManagers, c.clusterManagers[i])
			}
			operatorClient := fakeoperatorclient.NewSimpleClientset(clusterManagers...)
			operatorInformers := operatorinformers.NewSharedInformerFactory(operatorClient, 5*time.Minute)
			clusterManagerStore := operatorInformers.Operator().V1().ClusterManagers().Informer().GetStore()
			for _, clusterManager := range clusterManagers {
				if err := clusterManagerStore.Add(clusterManager); err != nil {
					t.Fatal(err)
				}
			}

			syncContext := testinghelper.NewFakeSyncContext(t, c.queueKey)
			recorder := syncContext.Recorder()

			controller := NewCertRotationController(kubeClient, kubeInformer.Core().V1().Secrets(), kubeInformer.Core().V1().ConfigMaps(), operatorInformers.Operator().V1().ClusterManagers(), recorder)

			err := controller.Sync(context.TODO(), syncContext)
			c.validate(t, kubeClient, err)
		})
	}
}

func assertResourcesExistAndValid(t *testing.T, kubeClient kubernetes.Interface, namespace string) {
	configmap, err := kubeClient.CoreV1().ConfigMaps(namespace).Get(context.Background(), "ca-bundle-configmap", metav1.GetOptions{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	for _, name := range secretNames {
		secret, err := kubeClient.CoreV1().Secrets(namespace).Get(context.Background(), name, metav1.GetOptions{})
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

func assertResourcesNotExist(t *testing.T, kubeClient kubernetes.Interface, namespace string) {
	_, err := kubeClient.CoreV1().ConfigMaps(namespace).Get(context.Background(), "ca-bundle-configmap", metav1.GetOptions{})
	if !errors.IsNotFound(err) {
		t.Fatalf("expect configmap not found, but get err: %v", err)
	}

	for _, name := range secretNames {
		_, err := kubeClient.CoreV1().Secrets(namespace).Get(context.Background(), name, metav1.GetOptions{})
		if !errors.IsNotFound(err) {
			t.Fatalf("expect secret not found, but get err: %v", err)
		}
	}
}
