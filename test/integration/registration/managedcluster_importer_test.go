package registration_test

import (
	"fmt"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/openshift/api"
	"github.com/openshift/library-go/pkg/operator/resource/resourceapply"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	"k8s.io/apimachinery/pkg/util/rand"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/dynamic"

	operatorclient "open-cluster-management.io/api/client/operator/clientset/versioned"
	clusterv1 "open-cluster-management.io/api/cluster/v1"
	operatorv1 "open-cluster-management.io/api/operator/v1"
	"open-cluster-management.io/sdk-go/pkg/basecontroller/events"

	commonrecorder "open-cluster-management.io/ocm/pkg/common/recorder"
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

var _ = Describe("Cluster Auto Importer", func() {
	var managedClusterName string
	var dynamicClient dynamic.Interface
	var operatorClient operatorclient.Interface
	BeforeEach(func(ctx SpecContext) {
		suffix := rand.String(5)
		managedClusterName = fmt.Sprintf("managedcluster-%s", suffix)

		By("Create bootstrap token")
		clusterManagerConfig := chart.NewDefaultClusterManagerChartConfig()
		clusterManagerConfig.CreateBootstrapSA = true
		clusterManagerConfig.CreateNamespace = true
		crdObjs, rawObjs, err := chart.RenderClusterManagerChart(ctx, clusterManagerConfig, "open-cluster-management")
		manifests := append(crdObjs, rawObjs...)
		Expect(err).NotTo(HaveOccurred())
		recorder := commonrecorder.NewEventsRecorderWrapper(ctx,
			events.NewContextualLoggingEventRecorder("importer-test"))
		for _, manifest := range manifests {
			requiredObj, _, err := genericCodec.Decode(manifest, nil, nil)
			Expect(err).NotTo(HaveOccurred())
			switch t := requiredObj.(type) {
			case *corev1.Namespace:
				_, _, err = resourceapply.ApplyNamespace(ctx, kubeClient.CoreV1(), recorder, t)
				Expect(err).NotTo(HaveOccurred())
			case *corev1.ServiceAccount:
				_, _, err = resourceapply.ApplyServiceAccount(ctx, kubeClient.CoreV1(), recorder, t)
				Expect(err).NotTo(HaveOccurred())
			case *rbacv1.ClusterRole:
				_, _, err = resourceapply.ApplyClusterRole(ctx, kubeClient.RbacV1(), recorder, t)
				Expect(err).NotTo(HaveOccurred())
			case *rbacv1.ClusterRoleBinding:
				_, _, err = resourceapply.ApplyClusterRoleBinding(ctx, kubeClient.RbacV1(), recorder, t)
				Expect(err).NotTo(HaveOccurred())
			}
		}

		dynamicClient, err = dynamic.NewForConfig(spokeCfg)
		Expect(err).NotTo(HaveOccurred())

		operatorClient, err = operatorclient.NewForConfig(spokeCfg)
		Expect(err).NotTo(HaveOccurred())
	})

	Context("Cluster API importer", func() {
		JustBeforeEach(func(ctx SpecContext) {
			cluster := &clusterv1.ManagedCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name: managedClusterName,
				},
				Spec: clusterv1.ManagedClusterSpec{
					HubAcceptsClient: true,
				},
			}
			_, err := clusterClient.ClusterV1().ManagedClusters().Create(ctx, cluster, metav1.CreateOptions{})
			Expect(err).NotTo(HaveOccurred())

			// Wait for managed cluster namespace to be created
			namespaceGVR := schema.GroupVersionResource{Version: "v1", Resource: "namespaces"}
			Eventually(func() error {
				_, err := dynamicClient.Resource(namespaceGVR).Get(ctx, managedClusterName, metav1.GetOptions{})
				return err
			}, eventuallyTimeout, eventuallyInterval).ShouldNot(HaveOccurred())
		})

		JustAfterEach(func(ctx SpecContext) {
			err := clusterClient.ClusterV1().ManagedClusters().Delete(ctx, managedClusterName, metav1.DeleteOptions{})
			if !errors.IsNotFound(err) {
				Expect(err).NotTo(HaveOccurred())
			}
		})

		It("Should import CAPI cluster", func(ctx SpecContext) {
			By("create cluster import config secret")
			configSecret := util.GetClusterImportConfigSecret(managedClusterName)
			_, err := kubeClient.CoreV1().Secrets(managedClusterName).Create(ctx, configSecret, metav1.CreateOptions{})
			Expect(err).NotTo(HaveOccurred())

			By("Create CAPI cluster")
			capiCluster := testingcommon.NewUnstructured(
				"cluster.x-k8s.io/v1beta1", "Cluster", managedClusterName, managedClusterName)
			_, err = dynamicClient.Resource(capi.ClusterAPIGVR).Namespace(managedClusterName).Create(
				ctx, capiCluster, metav1.CreateOptions{})
			Expect(err).NotTo(HaveOccurred())

			Eventually(func() error {
				spokeCluster, err := util.GetManagedCluster(clusterClient, managedClusterName)
				if err != nil {
					return err
				}
				if !meta.IsStatusConditionFalse(
					spokeCluster.Status.Conditions, importer.ManagedClusterConditionImported) {
					return fmt.Errorf("cluster should have error when imported")
				}
				return nil
			}, eventuallyTimeout, eventuallyInterval).ShouldNot(HaveOccurred())

			By("Create secret")
			capiSecret := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      managedClusterName + "-kubeconfig",
					Namespace: managedClusterName,
				},
				Data: map[string][]byte{
					"value": util.NewKubeConfig(spokeCfg),
				},
			}
			_, err = kubeClient.CoreV1().Secrets(managedClusterName).Create(ctx, capiSecret, metav1.CreateOptions{})
			Expect(err).NotTo(HaveOccurred())

			// trigger the capi cluster resource to reconcile again
			Eventually(func() error {
				capiCluster, err := dynamicClient.Resource(capi.ClusterAPIGVR).Namespace(managedClusterName).Get(
					ctx, managedClusterName, metav1.GetOptions{})
				if err != nil {
					return err
				}
				if err := unstructured.SetNestedField(capiCluster.Object, map[string]interface{}{
					"phase": "Provisioned",
				}, "status"); err != nil {
					return err
				}
				_, err = dynamicClient.Resource(capi.ClusterAPIGVR).Namespace(managedClusterName).UpdateStatus(
					ctx, capiCluster, metav1.UpdateOptions{})
				if err != nil {
					return err
				}
				return nil
			}, eventuallyTimeout, eventuallyInterval).ShouldNot(HaveOccurred())

			Eventually(func() error {
				spokeCluster, err := util.GetManagedCluster(clusterClient, managedClusterName)
				if err != nil {
					return err
				}
				if !meta.IsStatusConditionTrue(
					spokeCluster.Status.Conditions, importer.ManagedClusterConditionImported) {
					By(fmt.Sprintf("condition is %v", spokeCluster.Status.Conditions))
					return fmt.Errorf("cluster should have imported")
				}
				_, err = operatorClient.OperatorV1().Klusterlets().Get(
					ctx, "klusterlet", metav1.GetOptions{})
				if err != nil {
					return err
				}
				return nil
			}, eventuallyTimeout, eventuallyInterval).ShouldNot(HaveOccurred())

			err = dynamicClient.Resource(capi.ClusterAPIGVR).Namespace(managedClusterName).Delete(
				ctx, managedClusterName, metav1.DeleteOptions{})
			Expect(err).NotTo(HaveOccurred())
			err = kubeClient.CoreV1().Secrets(managedClusterName).Delete(
				ctx, managedClusterName+"-kubeconfig", metav1.DeleteOptions{})
			Expect(err).NotTo(HaveOccurred())
		})
	})
})
