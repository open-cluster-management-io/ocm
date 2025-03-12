package registration_test

import (
	"context"
	"fmt"
	"time"

	"github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"
	"github.com/openshift/api"
	"github.com/openshift/library-go/pkg/operator/events"
	"github.com/openshift/library-go/pkg/operator/resource/resourceapply"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	"k8s.io/apimachinery/pkg/util/rand"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/dynamic"
	clocktesting "k8s.io/utils/clock/testing"

	operatorclient "open-cluster-management.io/api/client/operator/clientset/versioned"
	clusterv1 "open-cluster-management.io/api/cluster/v1"
	operatorv1 "open-cluster-management.io/api/operator/v1"

	testingcommon "open-cluster-management.io/ocm/pkg/common/testing"
	"open-cluster-management.io/ocm/pkg/operator/helpers/chart"
	"open-cluster-management.io/ocm/pkg/registration/hub/importer"
	"open-cluster-management.io/ocm/pkg/registration/hub/importer/providers/capi"
	"open-cluster-management.io/ocm/test/integration/util"
)

var (
	genericScheme = runtime.NewScheme()
	genericCodecs = serializer.NewCodecFactory(genericScheme)
	genericCodec  = genericCodecs.UniversalDeserializer()
)

func init() {
	utilruntime.Must(api.InstallKube(genericScheme))
	utilruntime.Must(apiextensionsv1.AddToScheme(genericScheme))
	utilruntime.Must(operatorv1.Install(genericScheme))
}

var _ = ginkgo.Describe("Cluster Auto Importer", func() {
	var managedClusterName string
	var dynamicClient dynamic.Interface
	var operatorClient operatorclient.Interface
	ginkgo.BeforeEach(func() {
		suffix := rand.String(5)
		managedClusterName = fmt.Sprintf("managedcluster-%s", suffix)

		ginkgo.By("Create bootstrap token")
		clusterManagerConfig := chart.NewDefaultClusterManagerChartConfig()
		clusterManagerConfig.CreateBootstrapSA = true
		clusterManagerConfig.CreateNamespace = true
		crdObjs, rawObjs, err := chart.RenderClusterManagerChart(clusterManagerConfig, "open-cluster-management")
		manifests := append(crdObjs, rawObjs...)
		gomega.Expect(err).NotTo(gomega.HaveOccurred())
		recorder := events.NewInMemoryRecorder("importer-testing", clocktesting.NewFakePassiveClock(time.Now()))
		for _, manifest := range manifests {
			requiredObj, _, err := genericCodec.Decode(manifest, nil, nil)
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
			switch t := requiredObj.(type) {
			case *corev1.Namespace:
				_, _, err = resourceapply.ApplyNamespace(context.TODO(), kubeClient.CoreV1(), recorder, t)
				gomega.Expect(err).NotTo(gomega.HaveOccurred())
			case *corev1.ServiceAccount:
				_, _, err = resourceapply.ApplyServiceAccount(context.TODO(), kubeClient.CoreV1(), recorder, t)
				gomega.Expect(err).NotTo(gomega.HaveOccurred())
			case *rbacv1.ClusterRole:
				_, _, err = resourceapply.ApplyClusterRole(context.TODO(), kubeClient.RbacV1(), recorder, t)
				gomega.Expect(err).NotTo(gomega.HaveOccurred())
			case *rbacv1.ClusterRoleBinding:
				_, _, err = resourceapply.ApplyClusterRoleBinding(context.TODO(), kubeClient.RbacV1(), recorder, t)
				gomega.Expect(err).NotTo(gomega.HaveOccurred())
			}
		}

		dynamicClient, err = dynamic.NewForConfig(spokeCfg)
		gomega.Expect(err).NotTo(gomega.HaveOccurred())

		operatorClient, err = operatorclient.NewForConfig(spokeCfg)
		gomega.Expect(err).NotTo(gomega.HaveOccurred())
	})

	ginkgo.Context("Cluster API importer", func() {
		ginkgo.JustBeforeEach(func() {
			cluster := &clusterv1.ManagedCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name: managedClusterName,
				},
				Spec: clusterv1.ManagedClusterSpec{
					HubAcceptsClient: true,
				},
			}
			_, err := clusterClient.ClusterV1().ManagedClusters().Create(context.TODO(), cluster, metav1.CreateOptions{})
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
		})

		ginkgo.JustAfterEach(func() {
			err := clusterClient.ClusterV1().ManagedClusters().Delete(context.TODO(), managedClusterName, metav1.DeleteOptions{})
			if !errors.IsNotFound(err) {
				gomega.Expect(err).NotTo(gomega.HaveOccurred())
			}
		})

		ginkgo.It("Should import CAPI cluster", func() {
			ginkgo.By("Create CAPI cluster")
			capiCluster := testingcommon.NewUnstructured(
				"cluster.x-k8s.io/v1beta1", "Cluster", managedClusterName, managedClusterName)
			_, err := dynamicClient.Resource(capi.ClusterAPIGVR).Namespace(managedClusterName).Create(
				context.TODO(), capiCluster, metav1.CreateOptions{})
			gomega.Expect(err).NotTo(gomega.HaveOccurred())

			gomega.Eventually(func() error {
				spokeCluster, err := util.GetManagedCluster(clusterClient, managedClusterName)
				if err != nil {
					return err
				}
				if !meta.IsStatusConditionFalse(
					spokeCluster.Status.Conditions, importer.ManagedClusterConditionImported) {
					return fmt.Errorf("cluster should have error when imported")
				}
				return nil
			}, eventuallyTimeout, eventuallyInterval).ShouldNot(gomega.HaveOccurred())

			ginkgo.By("Create secret")
			capiSecret := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      managedClusterName + "-kubeconfig",
					Namespace: managedClusterName,
				},
				Data: map[string][]byte{
					"value": util.NewKubeConfig(spokeCfg),
				},
			}
			_, err = kubeClient.CoreV1().Secrets(managedClusterName).Create(context.TODO(), capiSecret, metav1.CreateOptions{})
			gomega.Expect(err).NotTo(gomega.HaveOccurred())

			// trigger the capi cluster resource to reconcile again
			gomega.Eventually(func() error {
				capiCluster, err := dynamicClient.Resource(capi.ClusterAPIGVR).Namespace(managedClusterName).Get(
					context.TODO(), managedClusterName, metav1.GetOptions{})
				if err != nil {
					return err
				}
				unstructured.SetNestedField(capiCluster.Object, map[string]interface{}{
					"phase": "Provisioned",
				}, "status")
				_, err = dynamicClient.Resource(capi.ClusterAPIGVR).Namespace(managedClusterName).UpdateStatus(
					context.TODO(), capiCluster, metav1.UpdateOptions{})
				if err != nil {
					return err
				}
				return nil
			}, eventuallyTimeout, eventuallyInterval).ShouldNot(gomega.HaveOccurred())

			gomega.Eventually(func() error {
				spokeCluster, err := util.GetManagedCluster(clusterClient, managedClusterName)
				if err != nil {
					return err
				}
				if !meta.IsStatusConditionTrue(
					spokeCluster.Status.Conditions, importer.ManagedClusterConditionImported) {
					ginkgo.By(fmt.Sprintf("condition is %v", spokeCluster.Status.Conditions))
					return fmt.Errorf("cluster should have imported")
				}
				_, err = operatorClient.OperatorV1().Klusterlets().Get(
					context.TODO(), "klusterlet", metav1.GetOptions{})
				if err != nil {
					return err
				}
				return nil
			}, eventuallyTimeout, eventuallyInterval).ShouldNot(gomega.HaveOccurred())

			err = dynamicClient.Resource(capi.ClusterAPIGVR).Namespace(managedClusterName).Delete(
				context.TODO(), managedClusterName, metav1.DeleteOptions{})
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
			err = kubeClient.CoreV1().Secrets(managedClusterName).Delete(
				context.TODO(), managedClusterName+"-kubeconfig", metav1.DeleteOptions{})
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
		})
	})
})
