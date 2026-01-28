package work

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
	"k8s.io/apimachinery/pkg/util/rand"
	"k8s.io/utils/ptr"

	ocmfeature "open-cluster-management.io/api/feature"
	workapiv1 "open-cluster-management.io/api/work/v1"

	commonoptions "open-cluster-management.io/ocm/pkg/common/options"
	"open-cluster-management.io/ocm/pkg/features"
	"open-cluster-management.io/ocm/pkg/work/spoke"
	"open-cluster-management.io/ocm/test/integration/util"
)

var _ = ginkgo.Describe("ManifestWork Status Feedback", func() {
	var cancel context.CancelFunc

	var workName string
	var clusterName string
	var work *workapiv1.ManifestWork
	var manifests []workapiv1.Manifest

	var err error

	ginkgo.BeforeEach(func() {
		workName = fmt.Sprintf("status-feedback-work-%s", rand.String(5))
		clusterName = rand.String(5)

		ns := &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{Name: clusterName},
		}
		_, err = spokeKubeClient.CoreV1().Namespaces().Create(context.Background(), ns, metav1.CreateOptions{})
		gomega.Expect(err).ToNot(gomega.HaveOccurred())

		// reset manifests
		manifests = nil
	})

	ginkgo.JustBeforeEach(func() {
		work = util.NewManifestWork(clusterName, workName, manifests)
		gomega.Expect(err).ToNot(gomega.HaveOccurred())
	})

	ginkgo.AfterEach(func() {
		err := spokeKubeClient.CoreV1().Namespaces().Delete(context.Background(), clusterName, metav1.DeleteOptions{})
		gomega.Expect(err).ToNot(gomega.HaveOccurred())
	})

	ginkgo.Context("Deployment Status feedback", func() {
		ginkgo.BeforeEach(func() {
			u, _, err := util.NewDeployment(clusterName, "deploy1", "sa")
			gomega.Expect(err).ToNot(gomega.HaveOccurred())
			manifests = append(manifests, util.ToManifest(u))

			var ctx context.Context
			ctx, cancel = context.WithCancel(context.Background())
			go startWorkAgent(ctx, clusterName)
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

			work, err = hubWorkClient.WorkV1().ManifestWorks(clusterName).Create(context.Background(), work, metav1.CreateOptions{})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())

			util.AssertWorkCondition(work.Namespace, work.Name, hubWorkClient, workapiv1.WorkApplied, metav1.ConditionTrue,
				[]metav1.ConditionStatus{metav1.ConditionTrue}, eventuallyTimeout, eventuallyInterval)
			util.AssertWorkCondition(work.Namespace, work.Name, hubWorkClient, workapiv1.WorkAvailable, metav1.ConditionTrue,
				[]metav1.ConditionStatus{metav1.ConditionTrue}, eventuallyTimeout, eventuallyInterval)

			// Update Deployment status on spoke
			gomega.Eventually(func() error {
				deploy, err := spokeKubeClient.AppsV1().Deployments(clusterName).Get(context.Background(), "deploy1", metav1.GetOptions{})
				if err != nil {
					return err
				}

				deploy.Status.AvailableReplicas = 2
				deploy.Status.Replicas = 3
				deploy.Status.ReadyReplicas = 2

				_, err = spokeKubeClient.AppsV1().Deployments(clusterName).UpdateStatus(context.Background(), deploy, metav1.UpdateOptions{})
				return err
			}, eventuallyTimeout, eventuallyInterval).ShouldNot(gomega.HaveOccurred())

			// Check if we get status of deployment on work api
			gomega.Eventually(func() error {
				work, err = hubWorkClient.WorkV1().ManifestWorks(clusterName).Get(context.Background(), work.Name, metav1.GetOptions{})
				if err != nil {
					return err
				}

				if len(work.Status.ResourceStatus.Manifests) != 1 {
					return fmt.Errorf("the size of resource status is not correct, expect to be 1 but got %d", len(work.Status.ResourceStatus.Manifests))
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

				if !util.HaveManifestCondition(work.Status.ResourceStatus.Manifests, "StatusFeedbackSynced", []metav1.ConditionStatus{metav1.ConditionTrue}) {
					return fmt.Errorf("status sync condition should be True")
				}

				return err
			}, eventuallyTimeout, eventuallyInterval).ShouldNot(gomega.HaveOccurred())

			// Update replica of deployment
			gomega.Eventually(func() error {
				deploy, err := spokeKubeClient.AppsV1().Deployments(clusterName).Get(context.Background(), "deploy1", metav1.GetOptions{})
				if err != nil {
					return err
				}

				deploy.Status.AvailableReplicas = 3
				deploy.Status.Replicas = 3
				deploy.Status.ReadyReplicas = 3

				_, err = spokeKubeClient.AppsV1().Deployments(clusterName).UpdateStatus(context.Background(), deploy, metav1.UpdateOptions{})
				return err
			}, eventuallyTimeout, eventuallyInterval).ShouldNot(gomega.HaveOccurred())

			// Check if the status of deployment is synced on work api
			gomega.Eventually(func() error {
				work, err = hubWorkClient.WorkV1().ManifestWorks(clusterName).Get(context.Background(), work.Name, metav1.GetOptions{})
				if err != nil {
					return err
				}

				if len(work.Status.ResourceStatus.Manifests) != 1 {
					return fmt.Errorf("the size of resource status is not correct, expect to be 1 but got %d", len(work.Status.ResourceStatus.Manifests))
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

				if !util.HaveManifestCondition(work.Status.ResourceStatus.Manifests, "StatusFeedbackSynced", []metav1.ConditionStatus{metav1.ConditionTrue}) {
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
						Namespace: "*",
						Name:      "*1",
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

			work, err = hubWorkClient.WorkV1().ManifestWorks(clusterName).Create(context.Background(), work, metav1.CreateOptions{})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())

			util.AssertWorkCondition(work.Namespace, work.Name, hubWorkClient, workapiv1.WorkApplied, metav1.ConditionTrue,
				[]metav1.ConditionStatus{metav1.ConditionTrue}, eventuallyTimeout, eventuallyInterval)
			util.AssertWorkCondition(work.Namespace, work.Name, hubWorkClient, workapiv1.WorkAvailable, metav1.ConditionTrue,
				[]metav1.ConditionStatus{metav1.ConditionTrue}, eventuallyTimeout, eventuallyInterval)

			gomega.Eventually(func() error {
				deploy, err := spokeKubeClient.AppsV1().Deployments(clusterName).Get(context.Background(), "deploy1", metav1.GetOptions{})
				if err != nil {
					return err
				}

				deploy.Status.Conditions = []appsv1.DeploymentCondition{
					{
						Type:   "Available",
						Status: "True",
					},
				}

				_, err = spokeKubeClient.AppsV1().Deployments(clusterName).UpdateStatus(context.Background(), deploy, metav1.UpdateOptions{})
				return err
			}, eventuallyTimeout, eventuallyInterval).ShouldNot(gomega.HaveOccurred())

			// Check if we get status of deployment on work api
			gomega.Eventually(func() error {
				work, err = hubWorkClient.WorkV1().ManifestWorks(clusterName).Get(context.Background(), work.Name, metav1.GetOptions{})
				if err != nil {
					return err
				}

				if len(work.Status.ResourceStatus.Manifests) != 1 {
					return fmt.Errorf("the size of resource status is not correct, expect to be 1 but got %d", len(work.Status.ResourceStatus.Manifests))
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

				if !util.HaveManifestCondition(work.Status.ResourceStatus.Manifests, "StatusFeedbackSynced", []metav1.ConditionStatus{metav1.ConditionFalse}) {
					return fmt.Errorf("status sync condition should be False")
				}

				return nil
			}, eventuallyTimeout, eventuallyInterval).ShouldNot(gomega.HaveOccurred())
		})

		ginkgo.It("should return none for resources with no wellknown status", func() {
			sa, _ := util.NewServiceAccount(clusterName, "sa")
			work.Spec.Workload.Manifests = append(work.Spec.Workload.Manifests, util.ToManifest(sa))

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

			work, err = hubWorkClient.WorkV1().ManifestWorks(clusterName).Create(context.Background(), work, metav1.CreateOptions{})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())

			util.AssertWorkCondition(work.Namespace, work.Name, hubWorkClient, workapiv1.WorkApplied, metav1.ConditionTrue,
				[]metav1.ConditionStatus{metav1.ConditionTrue, metav1.ConditionTrue}, eventuallyTimeout, eventuallyInterval)
			util.AssertWorkCondition(work.Namespace, work.Name, hubWorkClient, workapiv1.WorkAvailable, metav1.ConditionTrue,
				[]metav1.ConditionStatus{metav1.ConditionTrue, metav1.ConditionTrue}, eventuallyTimeout, eventuallyInterval)

			// Update Deployment status on spoke
			gomega.Eventually(func() error {
				deploy, err := spokeKubeClient.AppsV1().Deployments(clusterName).Get(context.Background(), "deploy1", metav1.GetOptions{})
				if err != nil {
					return err
				}

				deploy.Status.AvailableReplicas = 2
				deploy.Status.Replicas = 3
				deploy.Status.ReadyReplicas = 2

				_, err = spokeKubeClient.AppsV1().Deployments(clusterName).UpdateStatus(context.Background(), deploy, metav1.UpdateOptions{})
				return err
			}, eventuallyTimeout, eventuallyInterval).ShouldNot(gomega.HaveOccurred())

			// Check if we get status of deployment on work api
			gomega.Eventually(func() error {
				work, err = hubWorkClient.WorkV1().ManifestWorks(clusterName).Get(context.Background(), work.Name, metav1.GetOptions{})
				if err != nil {
					return err
				}

				if len(work.Status.ResourceStatus.Manifests) != 2 {
					return fmt.Errorf("the size of resource status is not correct, expect to be 2 but got %d", len(work.Status.ResourceStatus.Manifests))
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

				if len(work.Status.ResourceStatus.Manifests[1].StatusFeedbacks.Values) != 0 {
					return fmt.Errorf("status feedback values are not correct, we got %v", work.Status.ResourceStatus.Manifests[1].StatusFeedbacks.Values)
				}

				if !util.HaveManifestCondition(
					work.Status.ResourceStatus.Manifests, "StatusFeedbackSynced",
					[]metav1.ConditionStatus{metav1.ConditionTrue, metav1.ConditionFalse}) {
					return fmt.Errorf("status sync condition should be True")
				}

				return nil
			}, eventuallyTimeout, eventuallyInterval).ShouldNot(gomega.HaveOccurred())
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
									Name: "wrong json path",
									Path: ".status.conditions",
								},
							},
						},
					},
				},
			}

			work, err = hubWorkClient.WorkV1().ManifestWorks(clusterName).Create(context.Background(), work, metav1.CreateOptions{})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())

			util.AssertWorkCondition(work.Namespace, work.Name, hubWorkClient, workapiv1.WorkApplied, metav1.ConditionTrue,
				[]metav1.ConditionStatus{metav1.ConditionTrue}, eventuallyTimeout, eventuallyInterval)
			util.AssertWorkCondition(work.Namespace, work.Name, hubWorkClient, workapiv1.WorkAvailable, metav1.ConditionTrue,
				[]metav1.ConditionStatus{metav1.ConditionTrue}, eventuallyTimeout, eventuallyInterval)
		})
	})

	ginkgo.Context("Deployment Status feedback with RawJsonString enabled", func() {
		ginkgo.BeforeEach(func() {
			u, _, err := util.NewDeployment(clusterName, "deploy1", "sa")
			gomega.Expect(err).ToNot(gomega.HaveOccurred())
			manifests = append(manifests, util.ToManifest(u))

			err = features.SpokeMutableFeatureGate.Set(fmt.Sprintf("%s=true", ocmfeature.RawFeedbackJsonString))
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
			ginkgo.DeferCleanup(func() {
				_ = features.SpokeMutableFeatureGate.Set(fmt.Sprintf("%s=false", ocmfeature.RawFeedbackJsonString))
			})
			var ctx context.Context
			ctx, cancel = context.WithCancel(context.Background())
			go startWorkAgent(ctx, clusterName)
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

			work, err = hubWorkClient.WorkV1().ManifestWorks(clusterName).Create(context.Background(), work, metav1.CreateOptions{})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())

			util.AssertWorkCondition(work.Namespace, work.Name, hubWorkClient, workapiv1.WorkApplied, metav1.ConditionTrue,
				[]metav1.ConditionStatus{metav1.ConditionTrue}, eventuallyTimeout, eventuallyInterval)
			util.AssertWorkCondition(work.Namespace, work.Name, hubWorkClient, workapiv1.WorkAvailable, metav1.ConditionTrue,
				[]metav1.ConditionStatus{metav1.ConditionTrue}, eventuallyTimeout, eventuallyInterval)

			gomega.Eventually(func() error {
				deploy, err := spokeKubeClient.AppsV1().Deployments(clusterName).Get(context.Background(), "deploy1", metav1.GetOptions{})
				if err != nil {
					return err
				}

				deploy.Status.Conditions = []appsv1.DeploymentCondition{
					{
						Type:   "Available",
						Status: "True",
					},
				}

				_, err = spokeKubeClient.AppsV1().Deployments(clusterName).UpdateStatus(context.Background(), deploy, metav1.UpdateOptions{})
				return err
			}, eventuallyTimeout, eventuallyInterval).ShouldNot(gomega.HaveOccurred())

			// Check if we get status of deployment on work api
			gomega.Eventually(func() error {
				work, err = hubWorkClient.WorkV1().ManifestWorks(clusterName).Get(context.Background(), work.Name, metav1.GetOptions{})
				if err != nil {
					return err
				}

				if len(work.Status.ResourceStatus.Manifests) != 1 {
					return fmt.Errorf("the size of resource status is not correct, expect to be 1 but got %d", len(work.Status.ResourceStatus.Manifests))
				}

				values := work.Status.ResourceStatus.Manifests[0].StatusFeedbacks.Values

				expectedValues := []workapiv1.FeedbackValue{
					{
						Name: "conditions",
						Value: workapiv1.FieldValue{
							Type:    workapiv1.JsonRaw,
							JsonRaw: ptr.To[string](`[{"lastTransitionTime":null,"lastUpdateTime":null,"status":"True","type":"Available"}]`),
						},
					},
				}
				if !apiequality.Semantic.DeepEqual(values, expectedValues) {
					if len(values) > 0 {
						return fmt.Errorf("status feedback values are not correct, we got %v", *values[0].Value.JsonRaw)
					}
					return fmt.Errorf("status feedback values are not correct, we got %v", values)
				}

				if !util.HaveManifestCondition(work.Status.ResourceStatus.Manifests, "StatusFeedbackSynced", []metav1.ConditionStatus{metav1.ConditionTrue}) {
					return fmt.Errorf("status sync condition should be True")
				}

				return nil
			}, eventuallyTimeout, eventuallyInterval).ShouldNot(gomega.HaveOccurred())
		})
	})

	ginkgo.Context("DaemonSet Status feedback", func() {
		ginkgo.BeforeEach(func() {
			u, _, err := util.NewDaemonSet(clusterName, "ds1")
			gomega.Expect(err).ToNot(gomega.HaveOccurred())
			manifests = append(manifests, util.ToManifest(u))

			var ctx context.Context
			ctx, cancel = context.WithCancel(context.Background())
			go startWorkAgent(ctx, clusterName)
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
						Resource:  "daemonsets",
						Namespace: clusterName,
						Name:      "ds1",
					},
					FeedbackRules: []workapiv1.FeedbackRule{
						{
							Type: workapiv1.WellKnownStatusType,
						},
					},
				},
			}

			work, err = hubWorkClient.WorkV1().ManifestWorks(clusterName).
				Create(context.Background(), work, metav1.CreateOptions{})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())

			util.AssertWorkCondition(work.Namespace, work.Name, hubWorkClient,
				workapiv1.WorkApplied, metav1.ConditionTrue, []metav1.ConditionStatus{metav1.ConditionTrue},
				eventuallyTimeout, eventuallyInterval)
			util.AssertWorkCondition(work.Namespace, work.Name, hubWorkClient,
				workapiv1.WorkAvailable, metav1.ConditionTrue, []metav1.ConditionStatus{metav1.ConditionTrue},
				eventuallyTimeout, eventuallyInterval)

			// Update DaemonSet status on spoke
			gomega.Eventually(func() error {
				ds, err := spokeKubeClient.AppsV1().DaemonSets(clusterName).
					Get(context.Background(), "ds1", metav1.GetOptions{})
				if err != nil {
					return err
				}

				ds.Status.NumberAvailable = 2
				ds.Status.DesiredNumberScheduled = 3
				ds.Status.NumberReady = 2

				_, err = spokeKubeClient.AppsV1().DaemonSets(clusterName).
					UpdateStatus(context.Background(), ds, metav1.UpdateOptions{})
				return err
			}, eventuallyTimeout, eventuallyInterval).ShouldNot(gomega.HaveOccurred())

			// Check if we get status of daemonset on work api
			gomega.Eventually(func() error {
				work, err = hubWorkClient.WorkV1().ManifestWorks(clusterName).
					Get(context.Background(), work.Name, metav1.GetOptions{})
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
						Name: "NumberReady",
						Value: workapiv1.FieldValue{
							Type:    workapiv1.Integer,
							Integer: ptr.To[int64](2),
						},
					},
					{
						Name: "DesiredNumberScheduled",
						Value: workapiv1.FieldValue{
							Type:    workapiv1.Integer,
							Integer: ptr.To[int64](3),
						},
					},
					{
						Name: "NumberAvailable",
						Value: workapiv1.FieldValue{
							Type:    workapiv1.Integer,
							Integer: ptr.To[int64](2),
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

				return err
			}, eventuallyTimeout, eventuallyInterval).ShouldNot(gomega.HaveOccurred())

			// Update replica of deployment
			gomega.Eventually(func() error {
				ds, err := spokeKubeClient.AppsV1().DaemonSets(clusterName).
					Get(context.Background(), "ds1", metav1.GetOptions{})
				if err != nil {
					return err
				}

				ds.Status.NumberAvailable = 3
				ds.Status.DesiredNumberScheduled = 3
				ds.Status.NumberReady = 3

				_, err = spokeKubeClient.AppsV1().DaemonSets(clusterName).
					UpdateStatus(context.Background(), ds, metav1.UpdateOptions{})
				return err
			}, eventuallyTimeout, eventuallyInterval).ShouldNot(gomega.HaveOccurred())

			// Check if the status of the daemonset is synced on work api
			gomega.Eventually(func() error {
				work, err = hubWorkClient.WorkV1().ManifestWorks(clusterName).
					Get(context.Background(), work.Name, metav1.GetOptions{})
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
						Name: "NumberReady",
						Value: workapiv1.FieldValue{
							Type:    workapiv1.Integer,
							Integer: ptr.To[int64](3),
						},
					},
					{
						Name: "DesiredNumberScheduled",
						Value: workapiv1.FieldValue{
							Type:    workapiv1.Integer,
							Integer: ptr.To[int64](3),
						},
					},
					{
						Name: "NumberAvailable",
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
	})

	ginkgo.Context("Deployments Status feedback with wildcard", func() {
		ginkgo.BeforeEach(func() {
			deployment1, _, err := util.NewDeployment(clusterName, "deploy1", "sa")
			gomega.Expect(err).ToNot(gomega.HaveOccurred())
			manifests = append(manifests, util.ToManifest(deployment1))
			deployment2, _, err := util.NewDeployment(clusterName, "deploy2", "sa")
			gomega.Expect(err).ToNot(gomega.HaveOccurred())
			manifests = append(manifests, util.ToManifest(deployment2))

			err = features.SpokeMutableFeatureGate.Set(fmt.Sprintf("%s=true", ocmfeature.RawFeedbackJsonString))
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
			var ctx context.Context
			ctx, cancel = context.WithCancel(context.Background())
			go startWorkAgent(ctx, clusterName)
		})

		ginkgo.AfterEach(func() {
			if cancel != nil {
				cancel()
			}
		})

		ginkgo.It("wildcard option should match all deployments", func() {
			work.Spec.ManifestConfigs = []workapiv1.ManifestConfigOption{
				{
					ResourceIdentifier: workapiv1.ResourceIdentifier{
						Group:     "apps",
						Resource:  "deployments",
						Namespace: "*",
						Name:      "deploy*",
					},
					FeedbackRules: []workapiv1.FeedbackRule{
						{
							Type: workapiv1.WellKnownStatusType,
						},
						{
							Type: workapiv1.JSONPathsType,
							JsonPaths: []workapiv1.JsonPath{
								{Name: "name", Path: ".metadata.name"},
							},
						},
					},
				},
			}

			work, err = hubWorkClient.WorkV1().ManifestWorks(clusterName).Create(context.Background(), work, metav1.CreateOptions{})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())

			util.AssertWorkCondition(work.Namespace, work.Name, hubWorkClient, workapiv1.WorkApplied, metav1.ConditionTrue,
				[]metav1.ConditionStatus{metav1.ConditionTrue, metav1.ConditionTrue}, eventuallyTimeout, eventuallyInterval)
			util.AssertWorkCondition(work.Namespace, work.Name, hubWorkClient, workapiv1.WorkAvailable, metav1.ConditionTrue,
				[]metav1.ConditionStatus{metav1.ConditionTrue, metav1.ConditionTrue}, eventuallyTimeout, eventuallyInterval)

			// Update Deployment status on spoke
			gomega.Eventually(func() error {
				deploy, err := spokeKubeClient.AppsV1().Deployments(clusterName).Get(context.Background(), "deploy1", metav1.GetOptions{})
				if err != nil {
					return err
				}

				deploy.Status.AvailableReplicas = 3
				deploy.Status.Replicas = 3
				deploy.Status.ReadyReplicas = 3

				_, err = spokeKubeClient.AppsV1().Deployments(clusterName).UpdateStatus(context.Background(), deploy, metav1.UpdateOptions{})
				if err != nil {
					return err
				}

				deploy, err = spokeKubeClient.AppsV1().Deployments(clusterName).Get(context.Background(), "deploy2", metav1.GetOptions{})
				if err != nil {
					return err
				}

				deploy.Status.AvailableReplicas = 4
				deploy.Status.Replicas = 4
				deploy.Status.ReadyReplicas = 4

				_, err = spokeKubeClient.AppsV1().Deployments(clusterName).UpdateStatus(context.Background(), deploy, metav1.UpdateOptions{})
				return err
			}, eventuallyTimeout, eventuallyInterval).ShouldNot(gomega.HaveOccurred())

			// Check if we get status of deployment on work api
			gomega.Eventually(func() error {
				work, err = hubWorkClient.WorkV1().ManifestWorks(clusterName).Get(context.Background(), work.Name, metav1.GetOptions{})
				if err != nil {
					return err
				}

				if len(work.Status.ResourceStatus.Manifests) != 2 {
					return fmt.Errorf("the size of resource status is not correct, expect to be 2 but got %d", len(work.Status.ResourceStatus.Manifests))
				}

				values := work.Status.ResourceStatus.Manifests[0].StatusFeedbacks.Values
				for _, v := range values {
					switch v.Name {
					case "ReadyReplicas", "Replicas", "AvailableReplicas":
						if *v.Value.Integer != 3 {
							return fmt.Errorf("ReadyReplicas expected 3,but got %v", *v.Value.Integer)
						}
					case "name":
						if *v.Value.String != "deploy1" {
							return fmt.Errorf("named value is not correct, we got %v", *v.Value.String)
						}
					default:
						return fmt.Errorf("not expected value type: %v", v.Name)
					}
				}
				values = work.Status.ResourceStatus.Manifests[1].StatusFeedbacks.Values
				for _, v := range values {
					switch v.Name {
					case "ReadyReplicas", "Replicas", "AvailableReplicas":
						if *v.Value.Integer != 4 {
							return fmt.Errorf("ReadyReplicas expected 4,but got %v", *v.Value.Integer)
						}
					case "name":
						if *v.Value.String != "deploy2" {
							return fmt.Errorf("named value is not correct, we got %v", *v.Value.String)
						}
					default:
						return fmt.Errorf("not expected value type: %v", v.Name)
					}

				}

				if !util.HaveManifestCondition(work.Status.ResourceStatus.Manifests, "StatusFeedbackSynced",
					[]metav1.ConditionStatus{metav1.ConditionTrue, metav1.ConditionTrue}) {
					return fmt.Errorf("status sync condition should be True")
				}

				return err
			}, eventuallyTimeout, eventuallyInterval).ShouldNot(gomega.HaveOccurred())

		})

		ginkgo.It("matched multi options only the first works ", func() {
			work.Spec.ManifestConfigs = []workapiv1.ManifestConfigOption{
				{
					ResourceIdentifier: workapiv1.ResourceIdentifier{
						Group:     "apps",
						Resource:  "deployments",
						Namespace: "*",
						Name:      "deploy1",
					},
					FeedbackRules: []workapiv1.FeedbackRule{
						{
							Type: workapiv1.JSONPathsType,
							JsonPaths: []workapiv1.JsonPath{
								{Name: "conditions", Path: ".status.conditions"},
							},
						},
						{
							Type: workapiv1.JSONPathsType,
							JsonPaths: []workapiv1.JsonPath{
								{Name: "name", Path: ".metadata.name"},
							},
						},
					},
				},
				{
					ResourceIdentifier: workapiv1.ResourceIdentifier{
						Group:     "apps",
						Resource:  "deployments",
						Namespace: "*",
						Name:      "deploy*",
					},
					FeedbackRules: []workapiv1.FeedbackRule{
						{Type: workapiv1.WellKnownStatusType},
						{
							Type: workapiv1.JSONPathsType,
							JsonPaths: []workapiv1.JsonPath{
								{Name: "name", Path: ".metadata.name"},
							},
						},
					},
				},
			}

			work, err = hubWorkClient.WorkV1().ManifestWorks(clusterName).Create(context.Background(),
				work, metav1.CreateOptions{})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())

			util.AssertWorkCondition(work.Namespace, work.Name, hubWorkClient, workapiv1.WorkApplied, metav1.ConditionTrue,
				[]metav1.ConditionStatus{metav1.ConditionTrue, metav1.ConditionTrue}, eventuallyTimeout, eventuallyInterval)
			util.AssertWorkCondition(work.Namespace, work.Name, hubWorkClient, workapiv1.WorkAvailable, metav1.ConditionTrue,
				[]metav1.ConditionStatus{metav1.ConditionTrue, metav1.ConditionTrue}, eventuallyTimeout, eventuallyInterval)

			// Update Deployment status on spoke
			gomega.Eventually(func() error {
				deploy, err := spokeKubeClient.AppsV1().Deployments(clusterName).
					Get(context.Background(), "deploy1", metav1.GetOptions{})
				if err != nil {
					return err
				}

				deploy.Status.Conditions = []appsv1.DeploymentCondition{
					{
						Type:   "Available",
						Status: "True",
					},
				}

				_, err = spokeKubeClient.AppsV1().Deployments(clusterName).
					UpdateStatus(context.Background(), deploy, metav1.UpdateOptions{})
				if err != nil {
					return err
				}

				deploy, err = spokeKubeClient.AppsV1().Deployments(clusterName).
					Get(context.Background(), "deploy2", metav1.GetOptions{})
				if err != nil {
					return err
				}

				deploy.Status.AvailableReplicas = 4
				deploy.Status.Replicas = 4
				deploy.Status.ReadyReplicas = 4

				_, err = spokeKubeClient.AppsV1().Deployments(clusterName).
					UpdateStatus(context.Background(), deploy, metav1.UpdateOptions{})
				return err
			}, eventuallyTimeout, eventuallyInterval).ShouldNot(gomega.HaveOccurred())

			// Check if we get status of deployment on work api
			gomega.Eventually(func() error {
				work, err = hubWorkClient.WorkV1().ManifestWorks(clusterName).
					Get(context.Background(), work.Name, metav1.GetOptions{})
				if err != nil {
					return err
				}

				if len(work.Status.ResourceStatus.Manifests) != 2 {
					return fmt.Errorf("the size of resource status is not correct, expect to be 2 but got %d",
						len(work.Status.ResourceStatus.Manifests))
				}

				// deploy1 feedback result is condition
				values := work.Status.ResourceStatus.Manifests[0].StatusFeedbacks.Values
				for _, v := range values {
					switch v.Name {
					case "name":
						if *v.Value.String != "deploy1" {
							return fmt.Errorf("named value is not correct, we got %v", *v.Value.String)
						}
					case "conditions":
						if v.Value.Type != workapiv1.JsonRaw {
							return fmt.Errorf("expected JsonRaw,but got %v", v.Value.Type)
						}
					default:
						return fmt.Errorf("not expected value type: %v", v.Value.Type)

					}
				}
				// deploy2 feedback result is wellKnown
				values = work.Status.ResourceStatus.Manifests[1].StatusFeedbacks.Values
				for _, v := range values {
					switch v.Name {
					case "ReadyReplicas", "Replicas", "AvailableReplicas":
						if *v.Value.Integer != 4 {
							return fmt.Errorf("ReadyReplicas expected 3,but got %v", *v.Value.Integer)
						}
					case "name":
						if *v.Value.String != "deploy2" {
							return fmt.Errorf("named value is not correct, we got %v", *v.Value.String)
						}
					default:
						return fmt.Errorf("not expected value type: %v", v.Name)
					}
				}

				if !util.HaveManifestCondition(work.Status.ResourceStatus.Manifests, "StatusFeedbackSynced",
					[]metav1.ConditionStatus{metav1.ConditionTrue, metav1.ConditionTrue}) {
					return fmt.Errorf("status sync condition should be True")
				}
				return err
			}, eventuallyTimeout, eventuallyInterval).ShouldNot(gomega.HaveOccurred())
		})
	})

	ginkgo.Context("Watch-based Status Feedback", func() {
		ginkgo.BeforeEach(func() {
			u, _, err := util.NewDeployment(clusterName, "deploy-watch", "sa")
			gomega.Expect(err).ToNot(gomega.HaveOccurred())
			manifests = append(manifests, util.ToManifest(u))

			var ctx context.Context
			ctx, cancel = context.WithCancel(context.Background())
			// increase normal sync interval so we can validate watch based status feedback
			syncIntervalOptionDecorator := func(
				opt *spoke.WorkloadAgentOptions,
				commonOpt *commonoptions.AgentOptions) (*spoke.WorkloadAgentOptions, *commonoptions.AgentOptions) {
				opt.StatusSyncInterval = 30 * time.Second
				return opt, commonOpt
			}
			go startWorkAgent(ctx, clusterName, syncIntervalOptionDecorator)
		})

		ginkgo.AfterEach(func() {
			if cancel != nil {
				cancel()
			}
		})

		ginkgo.It("should register informer and watch resource status changes", func() {
			// Create ManifestWork with watch-based feedback
			work.Spec.ManifestConfigs = []workapiv1.ManifestConfigOption{
				{
					ResourceIdentifier: workapiv1.ResourceIdentifier{
						Group:     "apps",
						Resource:  "deployments",
						Namespace: clusterName,
						Name:      "deploy-watch",
					},
					FeedbackRules: []workapiv1.FeedbackRule{
						{
							Type: workapiv1.JSONPathsType,
							JsonPaths: []workapiv1.JsonPath{
								{
									Name: "replicas",
									Path: ".spec.replicas",
								},
								{
									Name: "availableReplicas",
									Path: ".status.availableReplicas",
								},
							},
						},
					},
					FeedbackScrapeType: workapiv1.FeedbackWatchType,
				},
			}

			work, err = hubWorkClient.WorkV1().ManifestWorks(clusterName).Create(context.Background(), work, metav1.CreateOptions{})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())

			// Wait for work to be applied
			util.AssertWorkCondition(work.Namespace, work.Name, hubWorkClient,
				workapiv1.WorkApplied, metav1.ConditionTrue, []metav1.ConditionStatus{metav1.ConditionTrue},
				eventuallyTimeout, eventuallyInterval)
			util.AssertWorkCondition(work.Namespace, work.Name, hubWorkClient,
				workapiv1.WorkAvailable, metav1.ConditionTrue, []metav1.ConditionStatus{metav1.ConditionTrue},
				eventuallyTimeout, eventuallyInterval)

			// Update Deployment status on spoke
			gomega.Eventually(func() error {
				deploy, err := spokeKubeClient.AppsV1().Deployments(clusterName).
					Get(context.Background(), "deploy-watch", metav1.GetOptions{})
				if err != nil {
					return err
				}

				deploy.Status.Replicas = 3
				deploy.Status.ReadyReplicas = 3
				deploy.Status.AvailableReplicas = 3
				_, err = spokeKubeClient.AppsV1().Deployments(clusterName).
					UpdateStatus(context.Background(), deploy, metav1.UpdateOptions{})
				return err
			}, eventuallyTimeout, eventuallyInterval).ShouldNot(gomega.HaveOccurred())

			// Verify feedback values are updated via watch
			gomega.Eventually(func() error {
				work, err = hubWorkClient.WorkV1().ManifestWorks(clusterName).
					Get(context.Background(), work.Name, metav1.GetOptions{})
				if err != nil {
					return err
				}

				if len(work.Status.ResourceStatus.Manifests) != 1 {
					return fmt.Errorf("expected 1 manifest status, got %d",
						len(work.Status.ResourceStatus.Manifests))
				}

				values := work.Status.ResourceStatus.Manifests[0].StatusFeedbacks.Values
				if len(values) != 2 {
					return fmt.Errorf("expected 2 feedback values, got %d", len(values))
				}

				for _, v := range values {
					if v.Name == "availableReplicas" {
						if v.Value.Integer == nil || *v.Value.Integer != 3 {
							return fmt.Errorf("expected availableReplicas to be 3, got %v", v.Value.Integer)
						}
					}
				}

				return nil
			}, eventuallyTimeout, eventuallyInterval).ShouldNot(gomega.HaveOccurred())
		})

		ginkgo.It("should watch cluster-scope resources like namespaces", func() {
			// Create a namespace manifest
			testNsName := fmt.Sprintf("test-ns-%s", rand.String(5))
			namespaceManifest := util.ToManifest(&corev1.Namespace{
				TypeMeta: metav1.TypeMeta{
					APIVersion: "v1",
					Kind:       "Namespace",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name: testNsName,
					Labels: map[string]string{
						"test": "watch-feedback",
					},
				},
			})

			// Create ManifestWork with watch-based feedback for cluster-scope resource
			work.Spec.Workload.Manifests = []workapiv1.Manifest{namespaceManifest}
			work.Spec.ManifestConfigs = []workapiv1.ManifestConfigOption{
				{
					ResourceIdentifier: workapiv1.ResourceIdentifier{
						Group:    "",
						Resource: "namespaces",
						Name:     testNsName,
						// Note: No Namespace field for cluster-scoped resources
					},
					FeedbackRules: []workapiv1.FeedbackRule{
						{
							Type: workapiv1.JSONPathsType,
							JsonPaths: []workapiv1.JsonPath{
								{
									Name: "phase",
									Path: ".status.phase",
								},
								{
									Name: "name",
									Path: ".metadata.name",
								},
							},
						},
					},
					FeedbackScrapeType: workapiv1.FeedbackWatchType,
				},
			}

			work, err = hubWorkClient.WorkV1().ManifestWorks(clusterName).Create(context.Background(), work, metav1.CreateOptions{})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())

			// Wait for work to be applied
			util.AssertWorkCondition(work.Namespace, work.Name, hubWorkClient,
				workapiv1.WorkApplied, metav1.ConditionTrue, []metav1.ConditionStatus{metav1.ConditionTrue},
				eventuallyTimeout, eventuallyInterval)
			util.AssertWorkCondition(work.Namespace, work.Name, hubWorkClient,
				workapiv1.WorkAvailable, metav1.ConditionTrue, []metav1.ConditionStatus{metav1.ConditionTrue},
				eventuallyTimeout, eventuallyInterval)

			// Update namespace status - Kubernetes sets phase to Active automatically,
			// but we'll verify it's being watched
			gomega.Eventually(func() error {
				ns, err := spokeKubeClient.CoreV1().Namespaces().Get(context.Background(), testNsName, metav1.GetOptions{})
				if err != nil {
					return err
				}

				// Kubernetes automatically sets the phase to Active for new namespaces
				// We just need to ensure it has a phase set
				if ns.Status.Phase == "" {
					ns.Status.Phase = corev1.NamespaceActive
					_, err = spokeKubeClient.CoreV1().Namespaces().UpdateStatus(context.Background(), ns, metav1.UpdateOptions{})
					return err
				}
				return nil
			}, eventuallyTimeout, eventuallyInterval).ShouldNot(gomega.HaveOccurred())

			// Verify feedback values are updated via watch
			gomega.Eventually(func() error {
				work, err = hubWorkClient.WorkV1().ManifestWorks(clusterName).
					Get(context.Background(), work.Name, metav1.GetOptions{})
				if err != nil {
					return err
				}

				if len(work.Status.ResourceStatus.Manifests) != 1 {
					return fmt.Errorf("expected 1 manifest status, got %d",
						len(work.Status.ResourceStatus.Manifests))
				}

				values := work.Status.ResourceStatus.Manifests[0].StatusFeedbacks.Values
				if len(values) != 2 {
					return fmt.Errorf("expected 2 feedback values, got %d", len(values))
				}

				var foundPhase, foundName bool
				for _, v := range values {
					if v.Name == "phase" {
						if v.Value.String == nil || *v.Value.String != string(corev1.NamespaceActive) {
							return fmt.Errorf("expected phase to be Active, got %v", v.Value.String)
						}
						foundPhase = true
					}
					if v.Name == "name" {
						if v.Value.String == nil || *v.Value.String != testNsName {
							return fmt.Errorf("expected name to be %s, got %v", testNsName, v.Value.String)
						}
						foundName = true
					}
				}

				if !foundPhase || !foundName {
					return fmt.Errorf("missing expected feedback values: phase=%v, name=%v", foundPhase, foundName)
				}

				if !util.HaveManifestCondition(work.Status.ResourceStatus.Manifests,
					"StatusFeedbackSynced", []metav1.ConditionStatus{metav1.ConditionTrue}) {
					return fmt.Errorf("status sync condition should be True")
				}

				return nil
			}, eventuallyTimeout, eventuallyInterval).ShouldNot(gomega.HaveOccurred())
		})
	})
})
