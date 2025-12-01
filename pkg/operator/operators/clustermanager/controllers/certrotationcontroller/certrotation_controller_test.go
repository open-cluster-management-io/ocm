package certrotationcontroller

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/runtime"
	kubeinformers "k8s.io/client-go/informers"
	corev1informers "k8s.io/client-go/informers/core/v1"
	"k8s.io/client-go/kubernetes"
	fakekube "k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/util/cert"

	fakeoperatorclient "open-cluster-management.io/api/client/operator/clientset/versioned/fake"
	operatorinformers "open-cluster-management.io/api/client/operator/informers/externalversions"
	operatorapiv1 "open-cluster-management.io/api/operator/v1"
	"open-cluster-management.io/sdk-go/pkg/basecontroller/factory"
	"open-cluster-management.io/sdk-go/pkg/certrotation"

	testingcommon "open-cluster-management.io/ocm/pkg/common/testing"
	"open-cluster-management.io/ocm/pkg/operator/helpers"
)

const (
	testClusterManagerNameDefault = "testclustermanager-default"
	testClusterManagerNameHosted  = "testclustermanager-hosted"
)

var secretNames = []string{helpers.SignerSecret, helpers.RegistrationWebhookSecret, helpers.WorkWebhookSecret}

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
			RegistrationConfiguration: &operatorapiv1.RegistrationHubConfiguration{
				RegistrationDrivers: []operatorapiv1.RegistrationDriverHub{
					{
						AuthType: operatorapiv1.GRPCAuthType,
					},
					{
						AuthType: operatorapiv1.CSRAuthType,
					},
				},
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
			name: "Sync all clustermanagers",
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

			newOnTermInformer := func(name string) kubeinformers.SharedInformerFactory {
				return kubeinformers.NewSharedInformerFactoryWithOptions(kubeClient, 5*time.Minute,
					kubeinformers.WithTweakListOptions(func(options *metav1.ListOptions) {
						options.FieldSelector = fields.OneTermEqualSelector("metadata.name", name).String()
					}))
			}

			secretInformers := map[string]corev1informers.SecretInformer{
				helpers.SignerSecret:              newOnTermInformer(helpers.SignerSecret).Core().V1().Secrets(),
				helpers.RegistrationWebhookSecret: newOnTermInformer(helpers.RegistrationWebhookSecret).Core().V1().Secrets(),
				helpers.WorkWebhookSecret:         newOnTermInformer(helpers.WorkWebhookSecret).Core().V1().Secrets(),
				helpers.GRPCServerSecret:          newOnTermInformer(helpers.GRPCServerSecret).Core().V1().Secrets(),
			}

			configmapInformer := newOnTermInformer(helpers.CaBundleConfigmap).Core().V1().ConfigMaps()

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

			syncContext := testingcommon.NewFakeSyncContext(t, c.queueKey)

			controller := NewCertRotationController(kubeClient, secretInformers, configmapInformer, operatorInformers.Operator().V1().ClusterManagers())

			err := controller.Sync(context.TODO(), syncContext, c.queueKey)
			c.validate(t, kubeClient, err)
		})
	}
}

func TestCertRotationGRPCAuth(t *testing.T) {
	namespace := helpers.ClusterManagerNamespace(testClusterManagerNameDefault, operatorapiv1.InstallModeDefault)

	cases := []struct {
		name                  string
		clusterManager        *operatorapiv1.ClusterManager
		updatedClusterManager *operatorapiv1.ClusterManager
		existingObjects       []runtime.Object
		validate              func(t *testing.T, kubeClient kubernetes.Interface, controller *certRotationController)
	}{
		{
			name: "Enable GRPC",
			clusterManager: func() *operatorapiv1.ClusterManager {
				cm := newClusterManager(testClusterManagerNameDefault, operatorapiv1.InstallModeDefault)
				cm.Spec.RegistrationConfiguration = &operatorapiv1.RegistrationHubConfiguration{
					RegistrationDrivers: []operatorapiv1.RegistrationDriverHub{
						{
							AuthType: operatorapiv1.CSRAuthType,
						},
					},
				}
				return cm
			}(),
			updatedClusterManager: func() *operatorapiv1.ClusterManager {
				return newClusterManager(testClusterManagerNameDefault, operatorapiv1.InstallModeDefault)
			}(),
			existingObjects: []runtime.Object{
				&corev1.Namespace{
					ObjectMeta: metav1.ObjectMeta{
						Name: namespace,
					},
				},
			},
			validate: func(t *testing.T, kubeClient kubernetes.Interface, controller *certRotationController) {
				// Check that GRPC server secret was created after update
				secret, err := kubeClient.CoreV1().Secrets(namespace).Get(context.Background(), helpers.GRPCServerSecret, metav1.GetOptions{})
				if err != nil {
					t.Fatalf("expected grpc server secret to be created after update, but got error: %v", err)
				}

				// Verify the secret has the expected certificate fields
				if _, ok := secret.Data["tls.crt"]; !ok {
					t.Fatalf("expected tls.crt in secret data")
				}
				if _, ok := secret.Data["tls.key"]; !ok {
					t.Fatalf("expected tls.key in secret data")
				}

				// Check that rotation was added to the map
				cmRotations, ok := controller.rotationMap[testClusterManagerNameDefault]
				if !ok {
					t.Fatalf("expected rotations to exist in map")
				}
				if _, ok := cmRotations.targetRotations[helpers.GRPCServerSecret]; !ok {
					t.Fatalf("expected grpc server rotation to be added after update, %v", cmRotations)
				}
			},
		},
		{
			name: "Disable GRPC",
			clusterManager: func() *operatorapiv1.ClusterManager {
				return newClusterManager(testClusterManagerNameDefault, operatorapiv1.InstallModeDefault)
			}(),
			updatedClusterManager: func() *operatorapiv1.ClusterManager {
				cm := newClusterManager(testClusterManagerNameDefault, operatorapiv1.InstallModeDefault)
				cm.Spec.RegistrationConfiguration = &operatorapiv1.RegistrationHubConfiguration{
					RegistrationDrivers: []operatorapiv1.RegistrationDriverHub{
						{
							AuthType: operatorapiv1.CSRAuthType,
						},
					},
				}
				return cm
			}(),
			existingObjects: []runtime.Object{
				&corev1.Namespace{
					ObjectMeta: metav1.ObjectMeta{
						Name: namespace,
					},
				},
			},
			validate: func(t *testing.T, kubeClient kubernetes.Interface, controller *certRotationController) {
				// Check that GRPC server secret was deleted after update
				_, err := kubeClient.CoreV1().Secrets(namespace).Get(context.Background(), helpers.GRPCServerSecret, metav1.GetOptions{})
				if !errors.IsNotFound(err) {
					t.Fatalf("expected GRPC server secret to be deleted after update, but got error: %v", err)
				}

				// Check that rotation was removed from the map
				cmRotations, ok := controller.rotationMap[testClusterManagerNameDefault]
				if !ok {
					t.Fatalf("expected rotations to exist in map")
				}
				if _, ok := cmRotations.targetRotations[helpers.GRPCServerSecret]; ok {
					t.Fatalf("expected grpc server rotation to be removed after update, %v", cmRotations)
				}
			},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			kubeClient := fakekube.NewSimpleClientset(c.existingObjects...)

			newOnTermInformer := func(name string) kubeinformers.SharedInformerFactory {
				return kubeinformers.NewSharedInformerFactoryWithOptions(kubeClient, 5*time.Minute,
					kubeinformers.WithTweakListOptions(func(options *metav1.ListOptions) {
						options.FieldSelector = fields.OneTermEqualSelector("metadata.name", name).String()
					}))
			}

			secretInformers := map[string]corev1informers.SecretInformer{
				helpers.SignerSecret:              newOnTermInformer(helpers.SignerSecret).Core().V1().Secrets(),
				helpers.RegistrationWebhookSecret: newOnTermInformer(helpers.RegistrationWebhookSecret).Core().V1().Secrets(),
				helpers.WorkWebhookSecret:         newOnTermInformer(helpers.WorkWebhookSecret).Core().V1().Secrets(),
				helpers.GRPCServerSecret:          newOnTermInformer(helpers.GRPCServerSecret).Core().V1().Secrets(),
			}

			configmapInformer := newOnTermInformer(helpers.CaBundleConfigmap).Core().V1().ConfigMaps()

			operatorClient := fakeoperatorclient.NewSimpleClientset(c.clusterManager)
			operatorInformers := operatorinformers.NewSharedInformerFactory(operatorClient, 5*time.Minute)
			clusterManagerStore := operatorInformers.Operator().V1().ClusterManagers().Informer().GetStore()
			if err := clusterManagerStore.Add(c.clusterManager); err != nil {
				t.Fatal(err)
			}

			syncContext := testingcommon.NewFakeSyncContext(t, testClusterManagerNameDefault)

			// Create the controller to check the rotation map
			controller := &certRotationController{
				rotationMap:          make(map[string]rotations),
				kubeClient:           kubeClient,
				secretInformers:      secretInformers,
				configMapInformer:    configmapInformer,
				clusterManagerLister: operatorInformers.Operator().V1().ClusterManagers().Lister(),
			}

			// First sync with initial configuration
			if err := controller.sync(context.TODO(), syncContext, testClusterManagerNameDefault); err != nil {
				t.Fatal(err)
			}

			// update the cluster manager and sync again
			if err := clusterManagerStore.Update(c.updatedClusterManager); err != nil {
				t.Fatal(err)
			}
			if err := controller.sync(context.TODO(), syncContext, testClusterManagerNameDefault); err != nil {
				t.Fatal(err)
			}

			c.validate(t, kubeClient, controller)
		})
	}
}

func TestCertRotationGRPCServerHostNames(t *testing.T) {
	namespace := helpers.ClusterManagerNamespace(testClusterManagerNameDefault, operatorapiv1.InstallModeDefault)

	cases := []struct {
		name                string
		clusterManager      *operatorapiv1.ClusterManager
		existingObjects     []runtime.Object
		validate            func(t *testing.T, kubeClient kubernetes.Interface, controller *certRotationController)
		expectedErrorSubstr string
	}{
		{
			name: "GRPC with LoadBalancer endpoint type and IP",
			clusterManager: func() *operatorapiv1.ClusterManager {
				cm := newClusterManager(testClusterManagerNameDefault, operatorapiv1.InstallModeDefault)
				cm.Spec.ServerConfiguration = &operatorapiv1.ServerConfiguration{
					EndpointsExposure: []operatorapiv1.EndpointExposure{
						{
							Protocol: operatorapiv1.GRPCAuthType,
							GRPC: &operatorapiv1.Endpoint{
								Type: operatorapiv1.EndpointTypeLoadBalancer,
							},
						},
					},
				}
				return cm
			}(),
			existingObjects: []runtime.Object{
				&corev1.Namespace{
					ObjectMeta: metav1.ObjectMeta{
						Name: namespace,
					},
				},
				&corev1.Service{
					ObjectMeta: metav1.ObjectMeta{
						Name:      testClusterManagerNameDefault + "-grpc-server",
						Namespace: namespace,
					},
					Status: corev1.ServiceStatus{
						LoadBalancer: corev1.LoadBalancerStatus{
							Ingress: []corev1.LoadBalancerIngress{
								{
									IP: "192.168.1.100",
								},
							},
						},
					},
				},
			},
			validate: func(t *testing.T, kubeClient kubernetes.Interface, controller *certRotationController) {
				// Check that the GRPC rotation was added with LoadBalancer IP in hostnames
				rotations, ok := controller.rotationMap[testClusterManagerNameDefault]
				if !ok {
					t.Fatalf("expected rotations to exist in map")
				}

				var grpcRotation *certrotation.TargetRotation
				for _, rotation := range rotations.targetRotations {
					if rotation.Name == helpers.GRPCServerSecret {
						grpcRotation = &rotation
						break
					}
				}

				if grpcRotation == nil {
					t.Fatalf("expected GRPC rotation to exist")
				}

				// Should have default service hostname and LoadBalancer IP
				expectedHostnames := []string{
					fmt.Sprintf("%s-grpc-server.%s.svc", testClusterManagerNameDefault, namespace),
					"192.168.1.100",
				}
				if len(grpcRotation.HostNames) != len(expectedHostnames) {
					t.Fatalf("expected %d hostnames, got %d", len(expectedHostnames), len(grpcRotation.HostNames))
				}
				for i, hostname := range expectedHostnames {
					if grpcRotation.HostNames[i] != hostname {
						t.Errorf("expected hostname[%d] to be %s, got %s", i, hostname, grpcRotation.HostNames[i])
					}
				}
			},
		},
		{
			name: "GRPC with LoadBalancer endpoint type and Hostname",
			clusterManager: func() *operatorapiv1.ClusterManager {
				cm := newClusterManager(testClusterManagerNameDefault, operatorapiv1.InstallModeDefault)
				cm.Spec.ServerConfiguration = &operatorapiv1.ServerConfiguration{
					EndpointsExposure: []operatorapiv1.EndpointExposure{
						{
							Protocol: operatorapiv1.GRPCAuthType,
							GRPC: &operatorapiv1.Endpoint{
								Type: operatorapiv1.EndpointTypeLoadBalancer,
							},
						},
					},
				}
				return cm
			}(),
			existingObjects: []runtime.Object{
				&corev1.Namespace{
					ObjectMeta: metav1.ObjectMeta{
						Name: namespace,
					},
				},
				&corev1.Service{
					ObjectMeta: metav1.ObjectMeta{
						Name:      testClusterManagerNameDefault + "-grpc-server",
						Namespace: namespace,
					},
					Status: corev1.ServiceStatus{
						LoadBalancer: corev1.LoadBalancerStatus{
							Ingress: []corev1.LoadBalancerIngress{
								{
									Hostname: "grpc.example.com",
								},
							},
						},
					},
				},
			},
			validate: func(t *testing.T, kubeClient kubernetes.Interface, controller *certRotationController) {
				rotations, ok := controller.rotationMap[testClusterManagerNameDefault]
				if !ok {
					t.Fatalf("expected rotations to exist in map")
				}

				var grpcRotation *certrotation.TargetRotation
				for _, rotation := range rotations.targetRotations {
					if rotation.Name == helpers.GRPCServerSecret {
						grpcRotation = &rotation
						break
					}
				}

				if grpcRotation == nil {
					t.Fatalf("expected GRPC rotation to exist")
				}

				// Should have default service hostname and LoadBalancer hostname
				expectedHostnames := []string{
					fmt.Sprintf("%s-grpc-server.%s.svc", testClusterManagerNameDefault, namespace),
					"grpc.example.com",
				}
				if len(grpcRotation.HostNames) != len(expectedHostnames) {
					t.Fatalf("expected %d hostnames, got %d", len(expectedHostnames), len(grpcRotation.HostNames))
				}
				for i, hostname := range expectedHostnames {
					if grpcRotation.HostNames[i] != hostname {
						t.Errorf("expected hostname[%d] to be %s, got %s", i, hostname, grpcRotation.HostNames[i])
					}
				}
			},
		},
		{
			name: "GRPC with Hostname endpoint type",
			clusterManager: func() *operatorapiv1.ClusterManager {
				cm := newClusterManager(testClusterManagerNameDefault, operatorapiv1.InstallModeDefault)
				cm.Spec.ServerConfiguration = &operatorapiv1.ServerConfiguration{
					EndpointsExposure: []operatorapiv1.EndpointExposure{
						{
							Protocol: operatorapiv1.GRPCAuthType,
							GRPC: &operatorapiv1.Endpoint{
								Type: operatorapiv1.EndpointTypeHostname,
								Hostname: &operatorapiv1.HostnameConfig{
									Host: "custom.grpc.example.com",
								},
							},
						},
					},
				}
				return cm
			}(),
			existingObjects: []runtime.Object{
				&corev1.Namespace{
					ObjectMeta: metav1.ObjectMeta{
						Name: namespace,
					},
				},
			},
			validate: func(t *testing.T, kubeClient kubernetes.Interface, controller *certRotationController) {
				rotations, ok := controller.rotationMap[testClusterManagerNameDefault]
				if !ok {
					t.Fatalf("expected rotations to exist in map")
				}

				var grpcRotation *certrotation.TargetRotation
				for _, rotation := range rotations.targetRotations {
					if rotation.Name == helpers.GRPCServerSecret {
						grpcRotation = &rotation
						break
					}
				}

				if grpcRotation == nil {
					t.Fatalf("expected GRPC rotation to exist")
				}

				// Should have default service hostname and custom hostname
				expectedHostnames := []string{
					fmt.Sprintf("%s-grpc-server.%s.svc", testClusterManagerNameDefault, namespace),
					"custom.grpc.example.com",
				}
				if len(grpcRotation.HostNames) != len(expectedHostnames) {
					t.Fatalf("expected %d hostnames, got %d", len(expectedHostnames), len(grpcRotation.HostNames))
				}
				for i, hostname := range expectedHostnames {
					if grpcRotation.HostNames[i] != hostname {
						t.Errorf("expected hostname[%d] to be %s, got %s", i, hostname, grpcRotation.HostNames[i])
					}
				}
			},
		},
		{
			name: "GRPC with loadBalancer but service not found",
			clusterManager: func() *operatorapiv1.ClusterManager {
				cm := newClusterManager(testClusterManagerNameDefault, operatorapiv1.InstallModeDefault)
				cm.Spec.ServerConfiguration = &operatorapiv1.ServerConfiguration{
					EndpointsExposure: []operatorapiv1.EndpointExposure{
						{
							Protocol: operatorapiv1.GRPCAuthType,
							GRPC: &operatorapiv1.Endpoint{
								Type: operatorapiv1.EndpointTypeLoadBalancer,
							},
						},
					},
				}
				return cm
			}(),
			existingObjects: []runtime.Object{
				&corev1.Namespace{
					ObjectMeta: metav1.ObjectMeta{
						Name: namespace,
					},
				},
			},
			expectedErrorSubstr: "failed to find service",
			validate: func(t *testing.T, kubeClient kubernetes.Interface, controller *certRotationController) {
				// Service not found error should prevent GRPC rotation from being added
				cmRotations, ok := controller.rotationMap[testClusterManagerNameDefault]
				if !ok {
					t.Fatalf("expected rotations to exist in map")
				}

				// GRPC rotation should not be added due to error
				if _, ok := cmRotations.targetRotations[helpers.GRPCServerSecret]; ok {
					t.Fatalf("expected GRPC rotation not to be added due to service not found error")
				}
			},
		},
		{
			name: "GRPC with LoadBalancer but no ingress status",
			clusterManager: func() *operatorapiv1.ClusterManager {
				cm := newClusterManager(testClusterManagerNameDefault, operatorapiv1.InstallModeDefault)
				cm.Spec.ServerConfiguration = &operatorapiv1.ServerConfiguration{
					EndpointsExposure: []operatorapiv1.EndpointExposure{
						{
							Protocol: operatorapiv1.GRPCAuthType,
							GRPC: &operatorapiv1.Endpoint{
								Type: operatorapiv1.EndpointTypeLoadBalancer,
							},
						},
					},
				}
				return cm
			}(),
			existingObjects: []runtime.Object{
				&corev1.Namespace{
					ObjectMeta: metav1.ObjectMeta{
						Name: namespace,
					},
				},
				&corev1.Service{
					ObjectMeta: metav1.ObjectMeta{
						Name:      testClusterManagerNameDefault + "-grpc-server",
						Namespace: namespace,
					},
					Status: corev1.ServiceStatus{
						LoadBalancer: corev1.LoadBalancerStatus{
							Ingress: []corev1.LoadBalancerIngress{},
						},
					},
				},
			},
			expectedErrorSubstr: "failed to find ingress",
			validate: func(t *testing.T, kubeClient kubernetes.Interface, controller *certRotationController) {
				// No ingress status error should prevent GRPC rotation from being added
				cmRotations, ok := controller.rotationMap[testClusterManagerNameDefault]
				if !ok {
					t.Fatalf("expected rotations to exist in map")
				}

				// GRPC rotation should not be added due to error
				if _, ok := cmRotations.targetRotations[helpers.GRPCServerSecret]; ok {
					t.Fatalf("expected GRPC rotation not to be added due to no ingress status error")
				}
			},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			kubeClient := fakekube.NewSimpleClientset(c.existingObjects...)

			newOnTermInformer := func(name string) kubeinformers.SharedInformerFactory {
				return kubeinformers.NewSharedInformerFactoryWithOptions(kubeClient, 5*time.Minute,
					kubeinformers.WithTweakListOptions(func(options *metav1.ListOptions) {
						options.FieldSelector = fields.OneTermEqualSelector("metadata.name", name).String()
					}))
			}

			secretInformers := map[string]corev1informers.SecretInformer{
				helpers.SignerSecret:              newOnTermInformer(helpers.SignerSecret).Core().V1().Secrets(),
				helpers.RegistrationWebhookSecret: newOnTermInformer(helpers.RegistrationWebhookSecret).Core().V1().Secrets(),
				helpers.WorkWebhookSecret:         newOnTermInformer(helpers.WorkWebhookSecret).Core().V1().Secrets(),
				helpers.GRPCServerSecret:          newOnTermInformer(helpers.GRPCServerSecret).Core().V1().Secrets(),
			}

			configmapInformer := newOnTermInformer(helpers.CaBundleConfigmap).Core().V1().ConfigMaps()

			operatorClient := fakeoperatorclient.NewSimpleClientset(c.clusterManager)
			operatorInformers := operatorinformers.NewSharedInformerFactory(operatorClient, 5*time.Minute)
			clusterManagerStore := operatorInformers.Operator().V1().ClusterManagers().Informer().GetStore()
			if err := clusterManagerStore.Add(c.clusterManager); err != nil {
				t.Fatal(err)
			}

			syncContext := testingcommon.NewFakeSyncContext(t, testClusterManagerNameDefault)

			controller := &certRotationController{
				rotationMap:          make(map[string]rotations),
				kubeClient:           kubeClient,
				secretInformers:      secretInformers,
				configMapInformer:    configmapInformer,
				clusterManagerLister: operatorInformers.Operator().V1().ClusterManagers().Lister(),
			}

			// Sync the controller
			err := controller.sync(context.TODO(), syncContext, testClusterManagerNameDefault)

			// Check if we expect an error
			if c.expectedErrorSubstr != "" {
				if err == nil {
					t.Fatalf("expected error containing %q, but got no error", c.expectedErrorSubstr)
				}
				if !strings.Contains(err.Error(), c.expectedErrorSubstr) {
					t.Fatalf("expected error containing %q, but got: %v", c.expectedErrorSubstr, err)
				}
			}

			c.validate(t, kubeClient, controller)
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
