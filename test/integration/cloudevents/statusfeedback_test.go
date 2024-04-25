package cloudevents

import (
	"context"
	"fmt"
	"time"

	"github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apiequality "k8s.io/apimachinery/pkg/api/equality"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"

	workclientset "open-cluster-management.io/api/client/work/clientset/versioned"
	ocmfeature "open-cluster-management.io/api/feature"
	workapiv1 "open-cluster-management.io/api/work/v1"

	commonoptions "open-cluster-management.io/ocm/pkg/common/options"
	"open-cluster-management.io/ocm/pkg/features"
	"open-cluster-management.io/ocm/pkg/work/spoke"
	"open-cluster-management.io/ocm/test/integration/util"
)

func runStatusFeedbackTest(sourceInfoGetter sourceInfoGetter, clusterNameGetter clusterNameGetter) func() {
	return func() {
		var err error

		var cancel context.CancelFunc

		var clusterName string

		var sourceDriver string
		var sourceConfigPath string
		var sourceClient workclientset.Interface

		var work *workapiv1.ManifestWork
		var manifests []workapiv1.Manifest

		ginkgo.BeforeEach(func() {
			sourceClient, sourceDriver, sourceConfigPath, _ = sourceInfoGetter()
			gomega.Expect(sourceClient).ToNot(gomega.BeNil())

			clusterName = clusterNameGetter()

			ns := &corev1.Namespace{}
			ns.Name = clusterName
			_, err = spokeKubeClient.CoreV1().Namespaces().Create(context.Background(), ns, metav1.CreateOptions{})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())

			// reset manifests
			manifests = nil
		})

		ginkgo.JustBeforeEach(func() {
			work = util.NewManifestWork(clusterName, "", manifests)
			gomega.Expect(err).ToNot(gomega.HaveOccurred())
		})

		ginkgo.AfterEach(func() {
			err := spokeKubeClient.CoreV1().Namespaces().Delete(
				context.Background(), clusterName, metav1.DeleteOptions{})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())
		})

		ginkgo.Context("Deployment Status feedback", func() {
			ginkgo.BeforeEach(func() {
				u, _, err := util.NewDeployment(clusterName, "deploy1", "sa")
				gomega.Expect(err).ToNot(gomega.HaveOccurred())
				manifests = append(manifests, util.ToManifest(u))

				var ctx context.Context
				ctx, cancel = context.WithCancel(context.Background())

				o := spoke.NewWorkloadAgentOptions()
				o.StatusSyncInterval = 3 * time.Second
				o.WorkloadSourceDriver = sourceDriver
				o.WorkloadSourceConfig = sourceConfigPath
				o.CloudEventsClientID = fmt.Sprintf("%s-work-agent", clusterName)
				o.CloudEventsClientCodecs = []string{"manifest", "manifestbundle"}

				commOptions := commonoptions.NewAgentOptions()
				commOptions.SpokeClusterName = clusterName
				go runWorkAgent(ctx, o, commOptions)
			})

			ginkgo.AfterEach(func() {
				if cancel != nil {
					cancel()
				}
			})

			ginkgo.It("should return well known statuses", func() {
				work.Spec.ManifestConfigs = []workapiv1.ManifestConfigOption{
					{
						ResourceIdentifier: workapiv1.ResourceIdentifier{
							Group:     "apps",
							Resource:  "deployments",
							Namespace: clusterName,
							Name:      "deploy1",
						},
						FeedbackRules: []workapiv1.FeedbackRule{
							{
								Type: workapiv1.WellKnownStatusType,
							},
						},
					},
				}

				work, err = sourceClient.WorkV1().ManifestWorks(clusterName).Create(
					context.Background(), work, metav1.CreateOptions{})
				gomega.Expect(err).ToNot(gomega.HaveOccurred())

				util.AssertWorkCondition(work.Namespace, work.Name, sourceClient,
					workapiv1.WorkApplied, metav1.ConditionTrue, []metav1.ConditionStatus{metav1.ConditionTrue},
					eventuallyTimeout, eventuallyInterval)
				util.AssertWorkCondition(work.Namespace, work.Name, sourceClient,
					workapiv1.WorkAvailable, metav1.ConditionTrue, []metav1.ConditionStatus{metav1.ConditionTrue},
					eventuallyTimeout, eventuallyInterval)

				// Update Deployment status on spoke
				gomega.Eventually(func() error {
					deploy, err := spokeKubeClient.AppsV1().Deployments(clusterName).Get(
						context.Background(), "deploy1", metav1.GetOptions{})
					if err != nil {
						return err
					}

					deploy.Status.AvailableReplicas = 2
					deploy.Status.Replicas = 3
					deploy.Status.ReadyReplicas = 2

					_, err = spokeKubeClient.AppsV1().Deployments(clusterName).UpdateStatus(
						context.Background(), deploy, metav1.UpdateOptions{})
					return err
				}, eventuallyTimeout, eventuallyInterval).ShouldNot(gomega.HaveOccurred())

				// Check if we get status of deployment on work api
				gomega.Eventually(func() error {
					work, err = sourceClient.WorkV1().ManifestWorks(clusterName).Get(
						context.Background(), work.Name, metav1.GetOptions{})
					if err != nil {
						return err
					}

					if len(work.Status.ResourceStatus.Manifests) != 1 {
						return fmt.Errorf("the size of resource status is not correct, expect to be 1 but got %d",
							len(work.Status.ResourceStatus.Manifests))
					}

					values := work.Status.ResourceStatus.Manifests[0].StatusFeedbacks.Values

					expectedValues := []workapiv1.FeedbackValue{
						{
							Name: "ReadyReplicas",
							Value: workapiv1.FieldValue{
								Type:    workapiv1.Integer,
								Integer: ptr.To[int64](2),
							},
						},
						{
							Name: "Replicas",
							Value: workapiv1.FieldValue{
								Type:    workapiv1.Integer,
								Integer: ptr.To[int64](3),
							},
						},
						{
							Name: "AvailableReplicas",
							Value: workapiv1.FieldValue{
								Type:    workapiv1.Integer,
								Integer: ptr.To[int64](2),
							},
						},
					}
					if !apiequality.Semantic.DeepEqual(values, expectedValues) {
						return fmt.Errorf("status feedback values are not correct, we got %v", values)
					}

					if !util.HaveManifestCondition(work.Status.ResourceStatus.Manifests, "StatusFeedbackSynced",
						[]metav1.ConditionStatus{metav1.ConditionTrue}) {
						return fmt.Errorf("status sync condition should be True")
					}

					return err
				}, eventuallyTimeout, eventuallyInterval).ShouldNot(gomega.HaveOccurred())

				// Update replica of deployment
				gomega.Eventually(func() error {
					deploy, err := spokeKubeClient.AppsV1().Deployments(clusterName).Get(
						context.Background(), "deploy1", metav1.GetOptions{})
					if err != nil {
						return err
					}

					deploy.Status.AvailableReplicas = 3
					deploy.Status.Replicas = 3
					deploy.Status.ReadyReplicas = 3

					_, err = spokeKubeClient.AppsV1().Deployments(clusterName).UpdateStatus(
						context.Background(), deploy, metav1.UpdateOptions{})
					return err
				}, eventuallyTimeout, eventuallyInterval).ShouldNot(gomega.HaveOccurred())

				// Check if the status of deployment is synced on work api
				gomega.Eventually(func() error {
					work, err = sourceClient.WorkV1().ManifestWorks(clusterName).Get(
						context.Background(), work.Name, metav1.GetOptions{})
					if err != nil {
						return err
					}

					if len(work.Status.ResourceStatus.Manifests) != 1 {
						return fmt.Errorf("the size of resource status is not correct, expect to be 1 but got %d",
							len(work.Status.ResourceStatus.Manifests))
					}

					values := work.Status.ResourceStatus.Manifests[0].StatusFeedbacks.Values

					expectedValues := []workapiv1.FeedbackValue{
						{
							Name: "ReadyReplicas",
							Value: workapiv1.FieldValue{
								Type:    workapiv1.Integer,
								Integer: ptr.To[int64](3),
							},
						},
						{
							Name: "Replicas",
							Value: workapiv1.FieldValue{
								Type:    workapiv1.Integer,
								Integer: ptr.To[int64](3),
							},
						},
						{
							Name: "AvailableReplicas",
							Value: workapiv1.FieldValue{
								Type:    workapiv1.Integer,
								Integer: ptr.To[int64](3),
							},
						},
					}
					if !apiequality.Semantic.DeepEqual(values, expectedValues) {
						return fmt.Errorf("status feedback values are not correct, we got %v", values)
					}

					if !util.HaveManifestCondition(work.Status.ResourceStatus.Manifests,
						"StatusFeedbackSynced", []metav1.ConditionStatus{metav1.ConditionTrue}) {
						return fmt.Errorf("status sync condition should be True")
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
							Namespace: clusterName,
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

				work, err = sourceClient.WorkV1().ManifestWorks(clusterName).Create(
					context.Background(), work, metav1.CreateOptions{})
				gomega.Expect(err).ToNot(gomega.HaveOccurred())

				util.AssertWorkCondition(work.Namespace, work.Name, sourceClient,
					workapiv1.WorkApplied, metav1.ConditionTrue, []metav1.ConditionStatus{metav1.ConditionTrue},
					eventuallyTimeout, eventuallyInterval)
				util.AssertWorkCondition(work.Namespace, work.Name, sourceClient,
					workapiv1.WorkAvailable, metav1.ConditionTrue, []metav1.ConditionStatus{metav1.ConditionTrue},
					eventuallyTimeout, eventuallyInterval)

				gomega.Eventually(func() error {
					deploy, err := spokeKubeClient.AppsV1().Deployments(clusterName).Get(
						context.Background(), "deploy1", metav1.GetOptions{})
					if err != nil {
						return err
					}

					deploy.Status.Conditions = []appsv1.DeploymentCondition{
						{
							Type:   "Available",
							Status: "True",
						},
					}

					_, err = spokeKubeClient.AppsV1().Deployments(clusterName).UpdateStatus(
						context.Background(), deploy, metav1.UpdateOptions{})
					return err
				}, eventuallyTimeout, eventuallyInterval).ShouldNot(gomega.HaveOccurred())

				// Check if we get status of deployment on work api
				gomega.Eventually(func() error {
					work, err = sourceClient.WorkV1().ManifestWorks(clusterName).Get(
						context.Background(), work.Name, metav1.GetOptions{})
					if err != nil {
						return err
					}

					if len(work.Status.ResourceStatus.Manifests) != 1 {
						return fmt.Errorf("the size of resource status is not correct, expect to be 1 but got %d",
							len(work.Status.ResourceStatus.Manifests))
					}

					values := work.Status.ResourceStatus.Manifests[0].StatusFeedbacks.Values

					expectedValues := []workapiv1.FeedbackValue{
						{
							Name: "Available",
							Value: workapiv1.FieldValue{
								Type:   workapiv1.String,
								String: ptr.To[string]("True"),
							},
						},
					}
					if !apiequality.Semantic.DeepEqual(values, expectedValues) {
						return fmt.Errorf("status feedback values are not correct, we got %v", values)
					}

					if !util.HaveManifestCondition(work.Status.ResourceStatus.Manifests, "StatusFeedbackSynced",
						[]metav1.ConditionStatus{metav1.ConditionFalse}) {
						return fmt.Errorf("status sync condition should be False")
					}

					return nil
				}, eventuallyTimeout, eventuallyInterval).ShouldNot(gomega.HaveOccurred())
			})

			ginkgo.It("should return none for resources with no wellknown status", func() {
				u, _, err := util.NewDeployment(clusterName, "deploy1", "sa")
				gomega.Expect(err).ToNot(gomega.HaveOccurred())

				sa, _ := util.NewServiceAccount(clusterName, "sa")

				work = util.NewManifestWork(clusterName, "", []workapiv1.Manifest{})
				work.Spec.Workload.Manifests = []workapiv1.Manifest{
					util.ToManifest(u),
					util.ToManifest(sa),
				}

				work.Spec.ManifestConfigs = []workapiv1.ManifestConfigOption{
					{
						ResourceIdentifier: workapiv1.ResourceIdentifier{
							Group:     "apps",
							Resource:  "deployments",
							Namespace: clusterName,
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
							Namespace: clusterName,
							Name:      "sa",
						},
						FeedbackRules: []workapiv1.FeedbackRule{
							{
								Type: workapiv1.WellKnownStatusType,
							},
						},
					},
				}

				work, err = sourceClient.WorkV1().ManifestWorks(clusterName).Create(
					context.Background(), work, metav1.CreateOptions{})
				gomega.Expect(err).ToNot(gomega.HaveOccurred())

				util.AssertWorkCondition(work.Namespace, work.Name, sourceClient, workapiv1.WorkApplied,
					metav1.ConditionTrue, []metav1.ConditionStatus{metav1.ConditionTrue, metav1.ConditionTrue},
					eventuallyTimeout, eventuallyInterval)
				util.AssertWorkCondition(work.Namespace, work.Name, sourceClient, workapiv1.WorkAvailable,
					metav1.ConditionTrue, []metav1.ConditionStatus{metav1.ConditionTrue, metav1.ConditionTrue},
					eventuallyTimeout, eventuallyInterval)

				// Update Deployment status on spoke
				gomega.Eventually(func() error {
					deploy, err := spokeKubeClient.AppsV1().Deployments(clusterName).Get(
						context.Background(), "deploy1", metav1.GetOptions{})
					if err != nil {
						return err
					}

					deploy.Status.AvailableReplicas = 2
					deploy.Status.Replicas = 3
					deploy.Status.ReadyReplicas = 2

					_, err = spokeKubeClient.AppsV1().Deployments(clusterName).UpdateStatus(
						context.Background(), deploy, metav1.UpdateOptions{})
					return err
				}, eventuallyTimeout, eventuallyInterval).ShouldNot(gomega.HaveOccurred())

				// Check if we get status of deployment on work api
				gomega.Eventually(func() error {
					work, err = sourceClient.WorkV1().ManifestWorks(clusterName).Get(
						context.Background(), work.Name, metav1.GetOptions{})
					if err != nil {
						return err
					}

					if len(work.Status.ResourceStatus.Manifests) != 2 {
						return fmt.Errorf("the size of resource status is not correct, expect to be 2 but got %d",
							len(work.Status.ResourceStatus.Manifests))
					}

					values := work.Status.ResourceStatus.Manifests[0].StatusFeedbacks.Values

					expectedValues := []workapiv1.FeedbackValue{
						{
							Name: "ReadyReplicas",
							Value: workapiv1.FieldValue{
								Type:    workapiv1.Integer,
								Integer: ptr.To[int64](2),
							},
						},
						{
							Name: "Replicas",
							Value: workapiv1.FieldValue{
								Type:    workapiv1.Integer,
								Integer: ptr.To[int64](3),
							},
						},
						{
							Name: "AvailableReplicas",
							Value: workapiv1.FieldValue{
								Type:    workapiv1.Integer,
								Integer: ptr.To[int64](2),
							},
						},
					}
					if !apiequality.Semantic.DeepEqual(values, expectedValues) {
						return fmt.Errorf("status feedback values are not correct, we got %v",
							work.Status.ResourceStatus.Manifests)
					}

					if len(work.Status.ResourceStatus.Manifests[1].StatusFeedbacks.Values) != 0 {
						return fmt.Errorf("status feedback values are not correct, we got %v",
							work.Status.ResourceStatus.Manifests[1].StatusFeedbacks.Values)
					}

					if !util.HaveManifestCondition(
						work.Status.ResourceStatus.Manifests, "StatusFeedbackSynced",
						[]metav1.ConditionStatus{metav1.ConditionTrue, metav1.ConditionFalse}) {
						return fmt.Errorf("status sync condition should be True")
					}

					return nil
				}, eventuallyTimeout*2, eventuallyInterval).ShouldNot(gomega.HaveOccurred())
			})
		})

		ginkgo.Context("Deployment Status feedback with RawJsonString enabled", func() {
			ginkgo.BeforeEach(func() {
				u, _, err := util.NewDeployment(clusterName, "deploy1", "sa")
				gomega.Expect(err).ToNot(gomega.HaveOccurred())

				manifests = append(manifests, util.ToManifest(u))

				err = features.SpokeMutableFeatureGate.Set(fmt.Sprintf("%s=true", ocmfeature.RawFeedbackJsonString))
				gomega.Expect(err).NotTo(gomega.HaveOccurred())

				o := spoke.NewWorkloadAgentOptions()
				o.StatusSyncInterval = 3 * time.Second
				o.WorkloadSourceDriver = sourceDriver
				o.WorkloadSourceConfig = sourceConfigPath
				o.CloudEventsClientID = fmt.Sprintf("%s-work-agent", clusterName)
				o.CloudEventsClientCodecs = []string{"manifest", "manifestbundle"}

				var ctx context.Context
				ctx, cancel = context.WithCancel(context.Background())

				commOptions := commonoptions.NewAgentOptions()
				commOptions.SpokeClusterName = clusterName
				go runWorkAgent(ctx, o, commOptions)
			})

			ginkgo.AfterEach(func() {
				if cancel != nil {
					cancel()
				}
			})

			ginkgo.It("Should return raw json string if the result is a structure", func() {
				work.Spec.ManifestConfigs = []workapiv1.ManifestConfigOption{
					{
						ResourceIdentifier: workapiv1.ResourceIdentifier{
							Group:     "apps",
							Resource:  "deployments",
							Namespace: clusterName,
							Name:      "deploy1",
						},
						FeedbackRules: []workapiv1.FeedbackRule{
							{
								Type: workapiv1.JSONPathsType,
								JsonPaths: []workapiv1.JsonPath{
									{
										Name: "conditions",
										Path: ".status.conditions",
									},
								},
							},
						},
					},
				}

				work, err = sourceClient.WorkV1().ManifestWorks(clusterName).Create(
					context.Background(), work, metav1.CreateOptions{})
				gomega.Expect(err).ToNot(gomega.HaveOccurred())

				util.AssertWorkCondition(work.Namespace, work.Name, sourceClient, workapiv1.WorkApplied,
					metav1.ConditionTrue, []metav1.ConditionStatus{metav1.ConditionTrue},
					eventuallyTimeout, eventuallyInterval)
				util.AssertWorkCondition(work.Namespace, work.Name, sourceClient, workapiv1.WorkAvailable,
					metav1.ConditionTrue, []metav1.ConditionStatus{metav1.ConditionTrue},
					eventuallyTimeout, eventuallyInterval)

				gomega.Eventually(func() error {
					deploy, err := spokeKubeClient.AppsV1().Deployments(clusterName).Get(
						context.Background(), "deploy1", metav1.GetOptions{})
					if err != nil {
						return err
					}

					deploy.Status.Conditions = []appsv1.DeploymentCondition{
						{
							Type:   "Available",
							Status: "True",
						},
					}

					_, err = spokeKubeClient.AppsV1().Deployments(clusterName).UpdateStatus(
						context.Background(), deploy, metav1.UpdateOptions{})
					return err
				}, eventuallyTimeout, eventuallyInterval).ShouldNot(gomega.HaveOccurred())

				// Check if we get status of deployment on work api
				gomega.Eventually(func() error {
					work, err = sourceClient.WorkV1().ManifestWorks(clusterName).Get(
						context.Background(), work.Name, metav1.GetOptions{})
					if err != nil {
						return err
					}

					if len(work.Status.ResourceStatus.Manifests) != 1 {
						return fmt.Errorf("the size of resource status is not correct, expect to be 1 but got %d",
							len(work.Status.ResourceStatus.Manifests))
					}

					values := work.Status.ResourceStatus.Manifests[0].StatusFeedbacks.Values

					expected := `[{"lastTransitionTime":null,"lastUpdateTime":null,"status":"True","type":"Available"}]`
					expectedValues := []workapiv1.FeedbackValue{
						{
							Name: "conditions",
							Value: workapiv1.FieldValue{
								Type:    workapiv1.JsonRaw,
								JsonRaw: ptr.To[string](expected),
							},
						},
					}
					if !apiequality.Semantic.DeepEqual(values, expectedValues) {
						if len(values) > 0 {
							return fmt.Errorf("status feedback values are not correct, we got %v",
								*values[0].Value.JsonRaw)
						}
						return fmt.Errorf("status feedback values are not correct, we got %v", values)
					}

					if !util.HaveManifestCondition(work.Status.ResourceStatus.Manifests, "StatusFeedbackSynced",
						[]metav1.ConditionStatus{metav1.ConditionTrue}) {
						return fmt.Errorf("status sync condition should be True")
					}

					return nil
				}, eventuallyTimeout, eventuallyInterval).ShouldNot(gomega.HaveOccurred())
			})
		})
	}
}
