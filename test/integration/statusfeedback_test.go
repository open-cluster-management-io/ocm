package integration

import (
	"context"
	"fmt"
	"time"

	"github.com/onsi/ginkgo"
	"github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apiequality "k8s.io/apimachinery/pkg/api/equality"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	utilrand "k8s.io/apimachinery/pkg/util/rand"
	workapiv1 "open-cluster-management.io/api/work/v1"
	"open-cluster-management.io/work/pkg/spoke"
	"open-cluster-management.io/work/test/integration/util"
)

var _ = ginkgo.Describe("ManifestWork Status Feedback", func() {
	var o *spoke.WorkloadAgentOptions
	var cancel context.CancelFunc

	var work *workapiv1.ManifestWork
	var manifests []workapiv1.Manifest

	var err error

	ginkgo.BeforeEach(func() {
		o = spoke.NewWorkloadAgentOptions()
		o.HubKubeconfigFile = hubKubeconfigFileName
		o.SpokeClusterName = utilrand.String(5)
		o.StatusSyncInterval = 3 * time.Second

		ns := &corev1.Namespace{}
		ns.Name = o.SpokeClusterName
		_, err := spokeKubeClient.CoreV1().Namespaces().Create(context.Background(), ns, metav1.CreateOptions{})
		gomega.Expect(err).ToNot(gomega.HaveOccurred())

		var ctx context.Context
		ctx, cancel = context.WithCancel(context.Background())
		go startWorkAgent(ctx, o)

		// reset manifests
		manifests = nil
	})

	ginkgo.JustBeforeEach(func() {
		work = util.NewManifestWork(o.SpokeClusterName, "", manifests)
		gomega.Expect(err).ToNot(gomega.HaveOccurred())
	})

	ginkgo.AfterEach(func() {
		if cancel != nil {
			cancel()
		}
		err := spokeKubeClient.CoreV1().Namespaces().Delete(context.Background(), o.SpokeClusterName, metav1.DeleteOptions{})
		gomega.Expect(err).ToNot(gomega.HaveOccurred())
	})

	ginkgo.Context("Deployment Status feedback", func() {
		ginkgo.BeforeEach(func() {
			u, _, err := util.NewDeployment(o.SpokeClusterName, "deploy1", "sa")
			gomega.Expect(err).ToNot(gomega.HaveOccurred())
			manifests = append(manifests, util.ToManifest(u))
		})

		ginkgo.It("should return well known statuses", func() {
			work.Spec.ManifestConfigs = []workapiv1.ManifestConfigOption{
				{
					ResourceIdentifier: workapiv1.ResourceIdentifier{
						Group:     "apps",
						Resource:  "deployments",
						Namespace: o.SpokeClusterName,
						Name:      "deploy1",
					},
					FeedbackRules: []workapiv1.FeedbackRule{
						{
							Type: workapiv1.WellKnownStatusType,
						},
					},
				},
			}

			work, err = hubWorkClient.WorkV1().ManifestWorks(o.SpokeClusterName).Create(context.Background(), work, metav1.CreateOptions{})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())

			util.AssertWorkCondition(work.Namespace, work.Name, hubWorkClient, string(workapiv1.WorkApplied), metav1.ConditionTrue,
				[]metav1.ConditionStatus{metav1.ConditionTrue}, eventuallyTimeout, eventuallyInterval)
			util.AssertWorkCondition(work.Namespace, work.Name, hubWorkClient, string(workapiv1.WorkAvailable), metav1.ConditionTrue,
				[]metav1.ConditionStatus{metav1.ConditionTrue}, eventuallyTimeout, eventuallyInterval)

			// Update Deployment status on spoke
			gomega.Eventually(func() error {
				deploy, err := spokeKubeClient.AppsV1().Deployments(o.SpokeClusterName).Get(context.Background(), "deploy1", metav1.GetOptions{})
				if err != nil {
					return err
				}

				deploy.Status.AvailableReplicas = 2
				deploy.Status.Replicas = 3
				deploy.Status.ReadyReplicas = 2

				_, err = spokeKubeClient.AppsV1().Deployments(o.SpokeClusterName).UpdateStatus(context.Background(), deploy, metav1.UpdateOptions{})
				return err
			}, eventuallyTimeout, eventuallyInterval).ShouldNot(gomega.HaveOccurred())

			// Check if we get status of deployment on work api
			gomega.Eventually(func() error {
				work, err = hubWorkClient.WorkV1().ManifestWorks(o.SpokeClusterName).Get(context.Background(), work.Name, metav1.GetOptions{})
				if err != nil {
					return err
				}

				if len(work.Status.ResourceStatus.Manifests) != 1 {
					return fmt.Errorf("The size of resource status is not correct, expect to be 1 but got %d", len(work.Status.ResourceStatus.Manifests))
				}

				values := work.Status.ResourceStatus.Manifests[0].StatusFeedbacks.Values

				expectedValues := []workapiv1.FeedbackValue{
					{
						Name: "ReadyReplicas",
						Value: workapiv1.FieldValue{
							Type:    workapiv1.Integer,
							Integer: util.Int64Ptr(2),
						},
					},
					{
						Name: "Replicas",
						Value: workapiv1.FieldValue{
							Type:    workapiv1.Integer,
							Integer: util.Int64Ptr(3),
						},
					},
					{
						Name: "AvailableReplicas",
						Value: workapiv1.FieldValue{
							Type:    workapiv1.Integer,
							Integer: util.Int64Ptr(2),
						},
					},
				}
				if !apiequality.Semantic.DeepEqual(values, expectedValues) {
					return fmt.Errorf("Status feedback values are not correct, we got %v", values)
				}

				if !util.HaveManifestCondition(work.Status.ResourceStatus.Manifests, "StatusFeedbackSynced", []metav1.ConditionStatus{metav1.ConditionTrue}) {
					return fmt.Errorf("Status sync condition should be True")
				}

				return err
			}, eventuallyTimeout, eventuallyInterval).ShouldNot(gomega.HaveOccurred())

			// Update replica of deployment
			gomega.Eventually(func() error {
				deploy, err := spokeKubeClient.AppsV1().Deployments(o.SpokeClusterName).Get(context.Background(), "deploy1", metav1.GetOptions{})
				if err != nil {
					return err
				}

				deploy.Status.AvailableReplicas = 3
				deploy.Status.Replicas = 3
				deploy.Status.ReadyReplicas = 3

				_, err = spokeKubeClient.AppsV1().Deployments(o.SpokeClusterName).UpdateStatus(context.Background(), deploy, metav1.UpdateOptions{})
				return err
			}, eventuallyTimeout, eventuallyInterval).ShouldNot(gomega.HaveOccurred())

			// Check if the status of deployment is synced on work api
			gomega.Eventually(func() error {
				work, err = hubWorkClient.WorkV1().ManifestWorks(o.SpokeClusterName).Get(context.Background(), work.Name, metav1.GetOptions{})
				if err != nil {
					return err
				}

				if len(work.Status.ResourceStatus.Manifests) != 1 {
					return fmt.Errorf("The size of resource status is not correct, expect to be 1 but got %d", len(work.Status.ResourceStatus.Manifests))
				}

				values := work.Status.ResourceStatus.Manifests[0].StatusFeedbacks.Values

				expectedValues := []workapiv1.FeedbackValue{
					{
						Name: "ReadyReplicas",
						Value: workapiv1.FieldValue{
							Type:    workapiv1.Integer,
							Integer: util.Int64Ptr(3),
						},
					},
					{
						Name: "Replicas",
						Value: workapiv1.FieldValue{
							Type:    workapiv1.Integer,
							Integer: util.Int64Ptr(3),
						},
					},
					{
						Name: "AvailableReplicas",
						Value: workapiv1.FieldValue{
							Type:    workapiv1.Integer,
							Integer: util.Int64Ptr(3),
						},
					},
				}
				if !apiequality.Semantic.DeepEqual(values, expectedValues) {
					return fmt.Errorf("Status feedback values are not correct, we got %v", values)
				}

				if !util.HaveManifestCondition(work.Status.ResourceStatus.Manifests, "StatusFeedbackSynced", []metav1.ConditionStatus{metav1.ConditionTrue}) {
					return fmt.Errorf("Status sync condition should be True")
				}

				return nil
			}, eventuallyTimeout, eventuallyInterval).ShouldNot(gomega.HaveOccurred())
		})

		ginkgo.It("should return statuses by JSONPaths", func() {
			work.Spec.ManifestConfigs = []workapiv1.ManifestConfigOption{
				{
					ResourceIdentifier: workapiv1.ResourceIdentifier{
						Group:     "apps",
						Resource:  "deployments",
						Namespace: o.SpokeClusterName,
						Name:      "deploy1",
					},
					FeedbackRules: []workapiv1.FeedbackRule{
						{
							Type: workapiv1.JSONPathsType,
							JsonPaths: []workapiv1.JsonPath{
								{
									Name: "Available",
									Path: ".status.conditions[?(@.type==\"Available\")].status",
								},
								{
									Name: "wrong json path",
									Path: ".status.conditions",
								},
							},
						},
					},
				},
			}

			work, err = hubWorkClient.WorkV1().ManifestWorks(o.SpokeClusterName).Create(context.Background(), work, metav1.CreateOptions{})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())

			util.AssertWorkCondition(work.Namespace, work.Name, hubWorkClient, string(workapiv1.WorkApplied), metav1.ConditionTrue,
				[]metav1.ConditionStatus{metav1.ConditionTrue}, eventuallyTimeout, eventuallyInterval)
			util.AssertWorkCondition(work.Namespace, work.Name, hubWorkClient, string(workapiv1.WorkAvailable), metav1.ConditionTrue,
				[]metav1.ConditionStatus{metav1.ConditionTrue}, eventuallyTimeout, eventuallyInterval)

			gomega.Eventually(func() error {
				deploy, err := spokeKubeClient.AppsV1().Deployments(o.SpokeClusterName).Get(context.Background(), "deploy1", metav1.GetOptions{})
				if err != nil {
					return err
				}

				deploy.Status.Conditions = []appsv1.DeploymentCondition{
					{
						Type:   "Available",
						Status: "True",
					},
				}

				_, err = spokeKubeClient.AppsV1().Deployments(o.SpokeClusterName).UpdateStatus(context.Background(), deploy, metav1.UpdateOptions{})
				return err
			}, eventuallyTimeout, eventuallyInterval).ShouldNot(gomega.HaveOccurred())

			// Check if we get status of deployment on work api
			gomega.Eventually(func() error {
				work, err = hubWorkClient.WorkV1().ManifestWorks(o.SpokeClusterName).Get(context.Background(), work.Name, metav1.GetOptions{})
				if err != nil {
					return err
				}

				if len(work.Status.ResourceStatus.Manifests) != 1 {
					return fmt.Errorf("The size of resource status is not correct, expect to be 1 but got %d", len(work.Status.ResourceStatus.Manifests))
				}

				values := work.Status.ResourceStatus.Manifests[0].StatusFeedbacks.Values

				expectedValues := []workapiv1.FeedbackValue{
					{
						Name: "Available",
						Value: workapiv1.FieldValue{
							Type:   workapiv1.String,
							String: util.StringPtr("True"),
						},
					},
				}
				if !apiequality.Semantic.DeepEqual(values, expectedValues) {
					return fmt.Errorf("Status feedback values are not correct, we got %v", values)
				}

				if !util.HaveManifestCondition(work.Status.ResourceStatus.Manifests, "StatusFeedbackSynced", []metav1.ConditionStatus{metav1.ConditionFalse}) {
					return fmt.Errorf("Status sync condition should be True")
				}

				return nil
			}, eventuallyTimeout, eventuallyInterval).ShouldNot(gomega.HaveOccurred())
		})

		ginkgo.It("should return none for resources with no wellKnowne status", func() {
			sa, _ := util.NewServiceAccount(o.SpokeClusterName, "sa")
			work.Spec.Workload.Manifests = append(work.Spec.Workload.Manifests, util.ToManifest(sa))

			work.Spec.ManifestConfigs = []workapiv1.ManifestConfigOption{
				{
					ResourceIdentifier: workapiv1.ResourceIdentifier{
						Group:     "apps",
						Resource:  "deployments",
						Namespace: o.SpokeClusterName,
						Name:      "deploy1",
					},
					FeedbackRules: []workapiv1.FeedbackRule{
						{
							Type: workapiv1.WellKnownStatusType,
						},
					},
				},
				{
					ResourceIdentifier: workapiv1.ResourceIdentifier{
						Group:     "",
						Resource:  "serviceaccounts",
						Namespace: o.SpokeClusterName,
						Name:      "sa",
					},
					FeedbackRules: []workapiv1.FeedbackRule{
						{
							Type: workapiv1.WellKnownStatusType,
						},
					},
				},
			}

			work, err = hubWorkClient.WorkV1().ManifestWorks(o.SpokeClusterName).Create(context.Background(), work, metav1.CreateOptions{})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())

			util.AssertWorkCondition(work.Namespace, work.Name, hubWorkClient, string(workapiv1.WorkApplied), metav1.ConditionTrue,
				[]metav1.ConditionStatus{metav1.ConditionTrue, metav1.ConditionTrue}, eventuallyTimeout, eventuallyInterval)
			util.AssertWorkCondition(work.Namespace, work.Name, hubWorkClient, string(workapiv1.WorkAvailable), metav1.ConditionTrue,
				[]metav1.ConditionStatus{metav1.ConditionTrue, metav1.ConditionTrue}, eventuallyTimeout, eventuallyInterval)

			// Update Deployment status on spoke
			gomega.Eventually(func() error {
				deploy, err := spokeKubeClient.AppsV1().Deployments(o.SpokeClusterName).Get(context.Background(), "deploy1", metav1.GetOptions{})
				if err != nil {
					return err
				}

				deploy.Status.AvailableReplicas = 2
				deploy.Status.Replicas = 3
				deploy.Status.ReadyReplicas = 2

				_, err = spokeKubeClient.AppsV1().Deployments(o.SpokeClusterName).UpdateStatus(context.Background(), deploy, metav1.UpdateOptions{})
				return err
			}, eventuallyTimeout, eventuallyInterval).ShouldNot(gomega.HaveOccurred())

			// Check if we get status of deployment on work api
			gomega.Eventually(func() error {
				work, err = hubWorkClient.WorkV1().ManifestWorks(o.SpokeClusterName).Get(context.Background(), work.Name, metav1.GetOptions{})
				if err != nil {
					return err
				}

				if len(work.Status.ResourceStatus.Manifests) != 2 {
					return fmt.Errorf("The size of resource status is not correct, expect to be 2 but got %d", len(work.Status.ResourceStatus.Manifests))
				}

				values := work.Status.ResourceStatus.Manifests[0].StatusFeedbacks.Values

				expectedValues := []workapiv1.FeedbackValue{
					{
						Name: "ReadyReplicas",
						Value: workapiv1.FieldValue{
							Type:    workapiv1.Integer,
							Integer: util.Int64Ptr(2),
						},
					},
					{
						Name: "Replicas",
						Value: workapiv1.FieldValue{
							Type:    workapiv1.Integer,
							Integer: util.Int64Ptr(3),
						},
					},
					{
						Name: "AvailableReplicas",
						Value: workapiv1.FieldValue{
							Type:    workapiv1.Integer,
							Integer: util.Int64Ptr(2),
						},
					},
				}
				if !apiequality.Semantic.DeepEqual(values, expectedValues) {
					return fmt.Errorf("Status feedback values are not correct, we got %v", values)
				}

				if len(work.Status.ResourceStatus.Manifests[1].StatusFeedbacks.Values) != 0 {
					return fmt.Errorf("Status feedback values are not correct, we got %v", work.Status.ResourceStatus.Manifests[1].StatusFeedbacks.Values)
				}

				if !util.HaveManifestCondition(work.Status.ResourceStatus.Manifests, "StatusFeedbackSynced", []metav1.ConditionStatus{metav1.ConditionTrue, metav1.ConditionFalse}) {
					return fmt.Errorf("Status sync condition should be True")
				}

				return nil
			}, eventuallyTimeout, eventuallyInterval).ShouldNot(gomega.HaveOccurred())
		})
	})
})
