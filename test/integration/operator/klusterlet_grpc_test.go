package operator

import (
	"context"
	"fmt"

	"github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/rand"
	"k8s.io/apimachinery/pkg/util/sets"

	operatorapiv1 "open-cluster-management.io/api/operator/v1"
)

var _ = ginkgo.Describe("Klusterlet using grpc auth", func() {
	var cancel context.CancelFunc
	var klusterlet *operatorapiv1.Klusterlet
	var klusterletNamespace string
	var registrationDeploymentName string
	var workDeploymentName string
	var deploymentName string

	ginkgo.Context("Default mode", func() {
		ginkgo.BeforeEach(func() {
			var ctx context.Context

			klusterletNamespace = fmt.Sprintf("open-cluster-management-%s", rand.String(6))
			ns := &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: klusterletNamespace,
				},
			}
			_, err := kubeClient.CoreV1().Namespaces().Create(context.Background(), ns, metav1.CreateOptions{})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())

			klusterlet = &operatorapiv1.Klusterlet{
				ObjectMeta: metav1.ObjectMeta{
					Name: fmt.Sprintf("klusterlet-%s", rand.String(6)),
				},
				Spec: operatorapiv1.KlusterletSpec{
					RegistrationImagePullSpec: "quay.io/open-cluster-management/registration",
					WorkImagePullSpec:         "quay.io/open-cluster-management/work",
					ClusterName:               "testcluster",
					Namespace:                 klusterletNamespace,
					RegistrationConfiguration: &operatorapiv1.RegistrationConfiguration{
						RegistrationDriver: operatorapiv1.RegistrationDriver{
							AuthType: "grpc",
						},
					},
				},
			}

			registrationDeploymentName = fmt.Sprintf("%s-registration-agent", klusterlet.Name)
			workDeploymentName = fmt.Sprintf("%s-work-agent", klusterlet.Name)

			ctx, cancel = context.WithCancel(context.Background())
			go startKlusterletOperator(ctx)
		})

		ginkgo.AfterEach(func() {
			err := operatorClient.OperatorV1().Klusterlets().Delete(context.Background(), klusterlet.Name, metav1.DeleteOptions{})
			gomega.Expect(err).NotTo(gomega.HaveOccurred())

			err = kubeClient.CoreV1().Namespaces().Delete(context.Background(), klusterletNamespace, metav1.DeleteOptions{})
			gomega.Expect(err).NotTo(gomega.HaveOccurred())

			if cancel != nil {
				cancel()
			}
		})

		ginkgo.It("should have expected resource created successfully", func() {
			_, err := operatorClient.OperatorV1().Klusterlets().Create(context.Background(), klusterlet, metav1.CreateOptions{})
			gomega.Expect(err).NotTo(gomega.HaveOccurred())

			gomega.Eventually(func() error {
				deploy, err := kubeClient.AppsV1().Deployments(klusterletNamespace).Get(context.Background(), registrationDeploymentName, metav1.GetOptions{})
				if err != nil {
					return err
				}

				if !isGRPCEnabled(deploy) {
					return fmt.Errorf("GRPC is not enabled in deployment %s", registrationDeploymentName)
				}

				return nil
			}, eventuallyTimeout, eventuallyInterval).ShouldNot(gomega.HaveOccurred())

			gomega.Eventually(func() error {
				deploy, err := kubeClient.AppsV1().Deployments(klusterletNamespace).Get(context.Background(), workDeploymentName, metav1.GetOptions{})
				if err != nil {
					return err
				}

				if !isGRPCEnabled(deploy) {
					return fmt.Errorf("GRPC is not enabled in deployment %s", workDeploymentName)
				}

				return nil
			}, eventuallyTimeout, eventuallyInterval).ShouldNot(gomega.HaveOccurred())
		})
	})

	ginkgo.Context("Singleton mode", func() {
		ginkgo.BeforeEach(func() {
			var ctx context.Context

			klusterletNamespace = fmt.Sprintf("open-cluster-management-%s", rand.String(6))
			ns := &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: klusterletNamespace,
				},
			}
			_, err := kubeClient.CoreV1().Namespaces().Create(context.Background(), ns, metav1.CreateOptions{})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())

			klusterlet = &operatorapiv1.Klusterlet{
				ObjectMeta: metav1.ObjectMeta{
					Name: fmt.Sprintf("klusterlet-%s", rand.String(6)),
				},
				Spec: operatorapiv1.KlusterletSpec{
					ImagePullSpec: "quay.io/open-cluster-management/registration-operator",
					ClusterName:   "testcluster",
					Namespace:     klusterletNamespace,
					RegistrationConfiguration: &operatorapiv1.RegistrationConfiguration{
						RegistrationDriver: operatorapiv1.RegistrationDriver{
							AuthType: "grpc",
						},
					},
					DeployOption: operatorapiv1.KlusterletDeployOption{
						Mode: operatorapiv1.InstallModeSingleton,
					},
				},
			}

			deploymentName = fmt.Sprintf("%s-agent", klusterlet.Name)

			ctx, cancel = context.WithCancel(context.Background())
			go startKlusterletOperator(ctx)
		})

		ginkgo.AfterEach(func() {
			err := operatorClient.OperatorV1().Klusterlets().Delete(context.Background(), klusterlet.Name, metav1.DeleteOptions{})
			gomega.Expect(err).NotTo(gomega.HaveOccurred())

			err = kubeClient.CoreV1().Namespaces().Delete(context.Background(), klusterletNamespace, metav1.DeleteOptions{})
			gomega.Expect(err).NotTo(gomega.HaveOccurred())

			if cancel != nil {
				cancel()
			}
		})

		ginkgo.It("should have expected resource created successfully", func() {
			_, err := operatorClient.OperatorV1().Klusterlets().Create(context.Background(), klusterlet, metav1.CreateOptions{})
			gomega.Expect(err).NotTo(gomega.HaveOccurred())

			gomega.Eventually(func() error {
				deploy, err := kubeClient.AppsV1().Deployments(klusterletNamespace).Get(context.Background(), deploymentName, metav1.GetOptions{})
				if err != nil {
					return err
				}

				if !isGRPCEnabled(deploy) {
					return fmt.Errorf("GRPC is not enabled in deployment %s", deploymentName)
				}

				return nil
			}, eventuallyTimeout, eventuallyInterval).ShouldNot(gomega.HaveOccurred())
		})
	})
})

func isGRPCEnabled(deploy *appsv1.Deployment) bool {
	for _, c := range deploy.Spec.Template.Spec.Containers {
		args := sets.New(c.Args...)
		if c.Name == "registration-controller" {
			return args.Has("--registration-auth=grpc") &&
				args.Has("--grpc-bootstrap-config=/spoke/bootstrap/config.yaml") &&
				args.Has("--grpc-config=/spoke/hub-kubeconfig/config.yaml")
		}

		if c.Name == "klusterlet-manifestwork-agent" {
			return args.Has("--workload-source-driver=grpc") &&
				args.Has("--workload-source-config=/spoke/hub-kubeconfig/config.yaml") &&
				args.Has("--cloudevents-client-id=testcluster-work-agent")
		}

		if c.Name == "klusterlet-agent" {
			return args.Has("--registration-auth=grpc") &&
				args.Has("--grpc-bootstrap-config=/spoke/bootstrap/config.yaml") &&
				args.Has("--grpc-config=/spoke/hub-kubeconfig/config.yaml") &&
				args.Has("--workload-source-driver=grpc") &&
				args.Has("--workload-source-config=/spoke/hub-kubeconfig/config.yaml") &&
				args.Has("--cloudevents-client-id=testcluster-klusterlet-agent")
		}
	}
	return false
}
