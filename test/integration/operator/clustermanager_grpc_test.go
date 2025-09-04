package operator

import (
	"context"
	"fmt"

	"github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"
	"github.com/openshift/library-go/pkg/operator/resource/resourceapply"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/client-go/kubernetes"

	operatorclient "open-cluster-management.io/api/client/operator/clientset/versioned"
	operatorapiv1 "open-cluster-management.io/api/operator/v1"

	"open-cluster-management.io/ocm/pkg/operator/helpers"
	"open-cluster-management.io/ocm/test/integration/util"
)

var _ = ginkgo.Describe("ClusterManager with GRPC registration enabled", ginkgo.Ordered, ginkgo.Label("grpc-test"), func() {
	var cancel context.CancelFunc

	var hubGRPCServerClusterRole = fmt.Sprintf("open-cluster-management:%s-grpc-server", clusterManagerName)
	var hubGRPCServerClusterRoleBinding = fmt.Sprintf("open-cluster-management:%s-grpc-server", clusterManagerName)
	var hubGRPCServerService = fmt.Sprintf("%s-grpc-server", clusterManagerName)
	var hubGRPCServerDeployment = fmt.Sprintf("%s-grpc-server", clusterManagerName)
	var hubGRPCServerServiceAccount = "grpc-server-sa"
	var hubGRPCServerSecret = "grpc-server-serving-cert" //#nosec G101
	var hubRegistrationDeployment = fmt.Sprintf("%s-registration-controller", clusterManagerName)

	ginkgo.Context("Deploy and clean hub component with default mode", func() {
		ginkgo.BeforeEach(func() {
			var ctx context.Context
			ctx, cancel = context.WithCancel(context.Background())
			go startHubOperator(ctx, operatorapiv1.InstallModeDefault)
		})

		ginkgo.AfterEach(func() {
			// delete deployment for clustermanager here so tests are not impacted with each other
			err := kubeClient.AppsV1().Deployments(hubNamespace).DeleteCollection(context.TODO(), metav1.DeleteOptions{}, metav1.ListOptions{})
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
			if cancel != nil {
				cancel()
			}
		})

		ginkgo.It("should deploy and clean hub component successfully", func() {
			ginkgo.By("Enable grpc auth for clustermanager", func() {
				gomega.Eventually(func() error {
					return enableGRPCAuth(operatorClient, clusterManagerName, "user1", "user2")
				}, eventuallyTimeout, eventuallyInterval).ShouldNot(gomega.HaveOccurred())
			})

			ginkgo.By("Checking related resources created", func() {
				assertGRPCServerResources(kubeClient, hubNamespace,
					hubGRPCServerClusterRole, hubGRPCServerClusterRoleBinding, hubGRPCServerService,
					hubGRPCServerServiceAccount, hubGRPCServerDeployment, hubGRPCServerSecret,
					hubRegistrationDeployment,
					[]string{
						"--grpc-ca-file=/var/run/secrets/hub/grpc/certs/tls.crt",
						"--grpc-key-file=/var/run/secrets/hub/grpc/certs/tls.key",
						"--auto-approved-grpc-users=user1,user2",
					})
			})

			ginkgo.By("Remove grpc auth from clustermanager", func() {
				gomega.Eventually(func() error {
					return disableGRPCAuth(operatorClient, clusterManagerName)
				}, eventuallyTimeout, eventuallyInterval).ShouldNot(gomega.HaveOccurred())
			})

			ginkgo.By("Checking related resources deleted", func() {
				assertNoGRPCServerResources(kubeClient, hubNamespace,
					hubGRPCServerClusterRole, hubGRPCServerClusterRoleBinding, hubGRPCServerService,
					hubGRPCServerServiceAccount, hubGRPCServerDeployment, hubGRPCServerSecret,
					hubRegistrationDeployment)
			})
		})
	})

	ginkgo.Context("Deploy and clean hub component with hosted mode", func() {
		ginkgo.BeforeEach(func() {
			var ctx context.Context
			ctx, cancel = context.WithCancel(context.Background())

			recorder := util.NewIntegrationTestEventRecorder("integration")

			// Create the hosted hub namespace
			ns := &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: hubNamespaceHosted,
				},
			}
			_, _, err := resourceapply.ApplyNamespace(ctx, hostedKubeClient.CoreV1(), recorder, ns)
			gomega.Expect(err).To(gomega.BeNil())

			// Create the external hub kubeconfig secret
			// #nosec G101
			hubKubeconfigSecret := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      helpers.ExternalHubKubeConfig,
					Namespace: hubNamespaceHosted,
				},
				Data: map[string][]byte{
					"kubeconfig": util.NewKubeConfig(hostedRestConfig),
				},
			}
			_, _, err = resourceapply.ApplySecret(ctx, hostedKubeClient.CoreV1(), recorder, hubKubeconfigSecret)
			gomega.Expect(err).To(gomega.BeNil())

			go startHubOperator(ctx, operatorapiv1.InstallModeHosted)
		})

		ginkgo.AfterEach(func() {
			// delete deployment for clustermanager here so tests are not impacted with each other
			err := hostedKubeClient.AppsV1().Deployments(hubNamespaceHosted).DeleteCollection(context.TODO(), metav1.DeleteOptions{}, metav1.ListOptions{})
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
			if cancel != nil {
				cancel()
			}
		})

		ginkgo.It("should deploy and clean hub component successfully", func() {
			ginkgo.By("Enable grpc auth for clustermanager", func() {
				gomega.Eventually(func() error {
					return enableGRPCAuth(hostedOperatorClient, clusterManagerName)
				}, eventuallyTimeout, eventuallyInterval).ShouldNot(gomega.HaveOccurred())
			})

			ginkgo.By("Checking related resources created", func() {
				assertGRPCServerResources(hostedKubeClient, hubNamespaceHosted,
					hubGRPCServerClusterRole, hubGRPCServerClusterRoleBinding, hubGRPCServerService,
					hubGRPCServerServiceAccount, hubGRPCServerDeployment, hubGRPCServerSecret,
					hubRegistrationDeployment,
					[]string{
						"--grpc-ca-file=/var/run/secrets/hub/grpc/certs/tls.crt",
						"--grpc-key-file=/var/run/secrets/hub/grpc/certs/tls.key",
					})
			})

			ginkgo.By("Remove grpc auth from clustermanager", func() {
				gomega.Eventually(func() error {
					return disableGRPCAuth(hostedOperatorClient, clusterManagerName)
				}, eventuallyTimeout, eventuallyInterval).ShouldNot(gomega.HaveOccurred())
			})

			ginkgo.By("Checking related resources deleted", func() {
				assertNoGRPCServerResources(hostedKubeClient, hubNamespaceHosted,
					hubGRPCServerClusterRole, hubGRPCServerClusterRoleBinding, hubGRPCServerService,
					hubGRPCServerServiceAccount, hubGRPCServerDeployment, hubGRPCServerSecret,
					hubRegistrationDeployment)
			})
		})
	})
})

func assertGRPCServerResources(kubeClient kubernetes.Interface, hubNamespace string,
	hubGRPCServerClusterRole, hubGRPCServerClusterRoleBinding, hubGRPCServerService,
	hubGRPCServerServiceAccount, hubGRPCServerDeployment, hubGRPCServerSecret,
	hubRegistrationDeployment string,
	expectedArgs []string) {
	gomega.Eventually(func() error {
		if _, err := kubeClient.CoreV1().Namespaces().Get(context.Background(), hubNamespace, metav1.GetOptions{}); err != nil {
			return err
		}
		return nil
	}, eventuallyTimeout, eventuallyInterval).Should(gomega.BeNil())

	gomega.Eventually(func() error {
		if _, err := kubeClient.RbacV1().ClusterRoles().Get(context.Background(), hubGRPCServerClusterRole, metav1.GetOptions{}); err != nil {
			return err
		}
		return nil
	}, eventuallyTimeout, eventuallyInterval).Should(gomega.BeNil())

	gomega.Eventually(func() error {
		if _, err := kubeClient.RbacV1().ClusterRoleBindings().Get(context.Background(), hubGRPCServerClusterRoleBinding, metav1.GetOptions{}); err != nil {
			return err
		}
		return nil
	}, eventuallyTimeout, eventuallyInterval).Should(gomega.BeNil())

	gomega.Eventually(func() error {
		if _, err := kubeClient.CoreV1().Services(hubNamespace).Get(context.Background(), hubGRPCServerService, metav1.GetOptions{}); err != nil {
			return err
		}
		return nil
	}, eventuallyTimeout, eventuallyInterval).Should(gomega.BeNil())

	gomega.Eventually(func() error {
		if _, err := kubeClient.CoreV1().ServiceAccounts(hubNamespace).Get(context.Background(), hubGRPCServerServiceAccount, metav1.GetOptions{}); err != nil {
			return err
		}
		return nil
	}, eventuallyTimeout, eventuallyInterval).Should(gomega.BeNil())

	gomega.Eventually(func() error {
		if _, err := kubeClient.AppsV1().Deployments(hubNamespace).Get(context.Background(), hubGRPCServerDeployment, metav1.GetOptions{}); err != nil {
			return err
		}
		return nil
	}, eventuallyTimeout, eventuallyInterval).Should(gomega.BeNil())

	gomega.Eventually(func() error {
		if _, err := kubeClient.CoreV1().Secrets(hubNamespace).Get(context.Background(), hubGRPCServerSecret, metav1.GetOptions{}); err != nil {
			return err
		}
		return nil
	}, eventuallyTimeout, eventuallyInterval).Should(gomega.BeNil())

	// ensure the grpc configuration was added to the registration controller
	gomega.Eventually(func() error {
		deploy, err := kubeClient.AppsV1().Deployments(hubNamespace).Get(context.Background(), hubRegistrationDeployment, metav1.GetOptions{})
		if err != nil {
			return err
		}

		if !hasGRPCServerSinger(deploy) {
			return fmt.Errorf("no grpc-server-singer")
		}

		args := sets.New(deploy.Spec.Template.Spec.Containers[0].Args...)
		for _, arg := range expectedArgs {
			if !args.Has(arg) {
				return fmt.Errorf("expected arg %s not found", arg)
			}
		}

		return nil
	}, eventuallyTimeout, eventuallyInterval).Should(gomega.BeNil())
}

func assertNoGRPCServerResources(kubeClient kubernetes.Interface, hubNamespace string,
	hubGRPCServerClusterRole, hubGRPCServerClusterRoleBinding, hubGRPCServerService,
	hubGRPCServerServiceAccount, hubGRPCServerDeployment, hubGRPCServerSecret,
	hubRegistrationDeployment string) {
	gomega.Eventually(func() error {
		_, err := kubeClient.RbacV1().ClusterRoles().Get(context.Background(), hubGRPCServerClusterRole, metav1.GetOptions{})
		if err != nil {
			if errors.IsNotFound(err) {
				return nil
			}
			return err
		}
		return fmt.Errorf("resource %s still exists", hubGRPCServerClusterRole)
	}, eventuallyTimeout, eventuallyInterval).Should(gomega.BeNil())

	gomega.Eventually(func() error {
		_, err := kubeClient.RbacV1().ClusterRoleBindings().Get(context.Background(), hubGRPCServerClusterRoleBinding, metav1.GetOptions{})
		if err != nil {
			if errors.IsNotFound(err) {
				return nil
			}
			return err
		}
		return fmt.Errorf("resource %s still exists", hubGRPCServerClusterRoleBinding)
	}, eventuallyTimeout, eventuallyInterval).Should(gomega.BeNil())

	gomega.Eventually(func() error {
		_, err := kubeClient.CoreV1().Services(hubNamespace).Get(context.Background(), hubGRPCServerService, metav1.GetOptions{})
		if err != nil {
			if errors.IsNotFound(err) {
				return nil
			}
			return err
		}
		return fmt.Errorf("resource %s still exists", hubGRPCServerService)
	}, eventuallyTimeout, eventuallyInterval).Should(gomega.BeNil())

	gomega.Eventually(func() error {
		_, err := kubeClient.CoreV1().ServiceAccounts(hubNamespace).Get(context.Background(), hubGRPCServerServiceAccount, metav1.GetOptions{})
		if err != nil {
			if errors.IsNotFound(err) {
				return nil
			}
			return err
		}
		return fmt.Errorf("resource %s still exists", hubGRPCServerServiceAccount)
	}, eventuallyTimeout, eventuallyInterval).Should(gomega.BeNil())

	gomega.Eventually(func() error {
		_, err := kubeClient.AppsV1().Deployments(hubNamespace).Get(context.Background(), hubGRPCServerDeployment, metav1.GetOptions{})
		if err != nil {
			if errors.IsNotFound(err) {
				return nil
			}
			return err
		}
		return fmt.Errorf("resource %s still exists", hubGRPCServerDeployment)
	}, eventuallyTimeout, eventuallyInterval).Should(gomega.BeNil())

	gomega.Eventually(func() error {
		_, err := kubeClient.CoreV1().Secrets(hubNamespace).Get(context.Background(), hubGRPCServerSecret, metav1.GetOptions{})
		if err != nil {
			if errors.IsNotFound(err) {
				return nil
			}
			return err
		}
		return fmt.Errorf("resource %s still exists", hubGRPCServerSecret)
	}, eventuallyTimeout, eventuallyInterval).Should(gomega.BeNil())

	// ensure the grpc configuration was removed from the registration controller
	gomega.Eventually(func() error {
		deploy, err := kubeClient.AppsV1().Deployments(hubNamespace).Get(context.Background(), hubRegistrationDeployment, metav1.GetOptions{})
		if err != nil {
			return err
		}

		if hasGRPCServerSinger(deploy) {
			return fmt.Errorf("grpc-server-singer still exists")
		}

		return nil
	}, eventuallyTimeout, eventuallyInterval).Should(gomega.BeNil())
}

func enableGRPCAuth(operatorClient operatorclient.Interface, clusterManagerName string,
	autoApprovedIdentities ...string) error {
	clusterManager, err := operatorClient.OperatorV1().ClusterManagers().Get(context.Background(),
		clusterManagerName, metav1.GetOptions{})
	if err != nil {
		return err
	}

	clusterManager.Spec.RegistrationConfiguration = &operatorapiv1.RegistrationHubConfiguration{
		RegistrationDrivers: []operatorapiv1.RegistrationDriverHub{
			{
				AuthType: operatorapiv1.CSRAuthType,
			},
			{
				AuthType: operatorapiv1.GRPCAuthType,
			},
		},
	}
	if len(autoApprovedIdentities) != 0 {
		clusterManager.Spec.RegistrationConfiguration = &operatorapiv1.RegistrationHubConfiguration{
			RegistrationDrivers: []operatorapiv1.RegistrationDriverHub{
				{
					AuthType: operatorapiv1.CSRAuthType,
				},
				{
					AuthType: operatorapiv1.GRPCAuthType,
					GRPC: &operatorapiv1.GRPCRegistrationConfig{
						AutoApprovedIdentities: autoApprovedIdentities,
					},
				},
			},
		}
	}
	_, err = operatorClient.OperatorV1().ClusterManagers().Update(context.Background(),
		clusterManager, metav1.UpdateOptions{})
	return err
}

func disableGRPCAuth(operatorClient operatorclient.Interface, clusterManagerName string) error {
	clusterManager, err := operatorClient.OperatorV1().ClusterManagers().Get(context.Background(),
		clusterManagerName, metav1.GetOptions{})
	if err != nil {
		return err
	}

	clusterManager.Spec.RegistrationConfiguration = nil
	_, err = operatorClient.OperatorV1().ClusterManagers().Update(context.Background(),
		clusterManager, metav1.UpdateOptions{})
	return err
}

func hasGRPCServerSinger(deploy *appsv1.Deployment) bool {
	for _, v := range deploy.Spec.Template.Spec.Volumes {
		if v.Name == "grpc-server-singer" {
			return true
		}
	}
	return false
}
