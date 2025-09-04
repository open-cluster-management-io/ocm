package work

import (
	"context"
	"fmt"

	"github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/rand"
	"k8s.io/utils/ptr"

	workapiv1 "open-cluster-management.io/api/work/v1"

	"open-cluster-management.io/ocm/test/integration/util"
)

var _ = ginkgo.Describe("ManifestWork Condition Rules", func() {
	var cancel context.CancelFunc
	var clusterName string

	var workName string
	var work *workapiv1.ManifestWork
	var manifests []workapiv1.Manifest

	var err error

	ginkgo.BeforeEach(func() {
		workName = fmt.Sprintf("condition-rules-work-%s", rand.String(5))
		clusterName = rand.String(5)

		ns := &corev1.Namespace{}
		ns.Name = clusterName
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

	ginkgo.Context("Job Condition Rules", func() {
		ginkgo.BeforeEach(func() {
			u, _, err := util.NewJob(clusterName, "job1", "sa")
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

		ginkgo.It("should return well known completed condition rule", func() {
			work.Spec.ManifestConfigs = []workapiv1.ManifestConfigOption{
				{
					ResourceIdentifier: workapiv1.ResourceIdentifier{
						Group:     "batch",
						Resource:  "jobs",
						Namespace: clusterName,
						Name:      "job1",
					},
					UpdateStrategy: &workapiv1.UpdateStrategy{
						Type: workapiv1.UpdateStrategyTypeServerSideApply,
					},
					ConditionRules: []workapiv1.ConditionRule{
						{
							Type:      workapiv1.WellKnownConditionsType,
							Condition: workapiv1.ManifestComplete,
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

			// Update Job status on spoke
			gomega.Eventually(func() error {
				job, err := spokeKubeClient.BatchV1().Jobs(clusterName).Get(context.Background(), "job1", metav1.GetOptions{})
				if err != nil {
					return err
				}

				job.Status.Active = 1
				job.Status.Ready = ptr.To(int32(1))

				_, err = spokeKubeClient.BatchV1().Jobs(clusterName).UpdateStatus(context.Background(), job, metav1.UpdateOptions{})
				return err
			}, eventuallyTimeout, eventuallyInterval).ShouldNot(gomega.HaveOccurred())

			// Check completed condition
			gomega.Eventually(func() error {
				work, err = hubWorkClient.WorkV1().ManifestWorks(clusterName).Get(context.Background(), work.Name, metav1.GetOptions{})
				if err != nil {
					return err
				}

				if len(work.Status.ResourceStatus.Manifests) != 1 {
					return fmt.Errorf("the size of resource status is not correct, expect to be 1 but got %d", len(work.Status.ResourceStatus.Manifests))
				}

				return util.CheckExpectedConditions(work.Status.ResourceStatus.Manifests[0].Conditions, metav1.Condition{
					Type:   workapiv1.ManifestComplete,
					Status: metav1.ConditionFalse,
					Reason: workapiv1.ConditionRuleEvaluated,
				})
			}, eventuallyTimeout, eventuallyInterval).ShouldNot(gomega.HaveOccurred())

			// Update complete condition on job
			gomega.Eventually(func() error {
				job, err := spokeKubeClient.BatchV1().Jobs(clusterName).Get(context.Background(), "job1", metav1.GetOptions{})
				if err != nil {
					return err
				}

				job.Status.Active = 0
				job.Status.Ready = ptr.To(int32(0))
				job.Status.Succeeded = 1
				job.Status.Conditions = []batchv1.JobCondition{
					{
						Type:    batchv1.JobComplete,
						Status:  corev1.ConditionTrue,
						Reason:  batchv1.JobReasonCompletionsReached,
						Message: "Reached expected number of succeeded pods",
					},
				}

				_, err = spokeKubeClient.BatchV1().Jobs(clusterName).UpdateStatus(context.Background(), job, metav1.UpdateOptions{})
				return err
			}, eventuallyTimeout, eventuallyInterval).ShouldNot(gomega.HaveOccurred())

			// Check if the condition is updated on work api
			gomega.Eventually(func() error {
				work, err = hubWorkClient.WorkV1().ManifestWorks(clusterName).Get(context.Background(), work.Name, metav1.GetOptions{})
				if err != nil {
					return err
				}

				if len(work.Status.ResourceStatus.Manifests) != 1 {
					return fmt.Errorf("the size of resource status is not correct, expect to be 1 but got %d", len(work.Status.ResourceStatus.Manifests))
				}

				return util.CheckExpectedConditions(work.Status.ResourceStatus.Manifests[0].Conditions, metav1.Condition{
					Type:   workapiv1.ManifestComplete,
					Status: metav1.ConditionTrue,
					Reason: workapiv1.ConditionRuleEvaluated,
				})
			}, eventuallyTimeout, eventuallyInterval).ShouldNot(gomega.HaveOccurred())
		})

		ginkgo.It("should return custom conditions by CEL rules", func() {
			work.Spec.ManifestConfigs = []workapiv1.ManifestConfigOption{
				{
					ResourceIdentifier: workapiv1.ResourceIdentifier{
						Group:     "batch",
						Resource:  "jobs",
						Namespace: "*",
						Name:      "*1",
					},
					UpdateStrategy: &workapiv1.UpdateStrategy{
						Type: workapiv1.UpdateStrategyTypeServerSideApply,
					},
					ConditionRules: []workapiv1.ConditionRule{
						{
							Type:              workapiv1.CelConditionExpressionsType,
							Condition:         "OneActive",
							MessageExpression: "result ? 'One pod is active' : 'There is not only one pod active'",
							CelExpressions: []string{
								"object.status.active == 1",
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
				job, err := spokeKubeClient.BatchV1().Jobs(clusterName).Get(context.Background(), "job1", metav1.GetOptions{})
				if err != nil {
					return err
				}

				job.Status.Active = 3
				job.Status.Conditions = []batchv1.JobCondition{}

				_, err = spokeKubeClient.BatchV1().Jobs(clusterName).UpdateStatus(context.Background(), job, metav1.UpdateOptions{})
				return err
			}, eventuallyTimeout, eventuallyInterval).ShouldNot(gomega.HaveOccurred())

			// Check if we get condition on work api
			gomega.Eventually(func() error {
				work, err = hubWorkClient.WorkV1().ManifestWorks(clusterName).Get(context.Background(), work.Name, metav1.GetOptions{})
				if err != nil {
					return err
				}

				if len(work.Status.ResourceStatus.Manifests) != 1 {
					return fmt.Errorf("the size of resource status is not correct, expect to be 1 but got %d", len(work.Status.ResourceStatus.Manifests))
				}

				return util.CheckExpectedConditions(work.Status.ResourceStatus.Manifests[0].Conditions, metav1.Condition{
					Type:    "OneActive",
					Reason:  workapiv1.ConditionRuleEvaluated,
					Status:  metav1.ConditionFalse,
					Message: "There is not only one pod active",
				})
			}, eventuallyTimeout, eventuallyInterval).ShouldNot(gomega.HaveOccurred())

			// Set active to 1
			gomega.Eventually(func() error {
				job, err := spokeKubeClient.BatchV1().Jobs(clusterName).Get(context.Background(), "job1", metav1.GetOptions{})
				if err != nil {
					return err
				}

				job.Status.Active = 1

				_, err = spokeKubeClient.BatchV1().Jobs(clusterName).UpdateStatus(context.Background(), job, metav1.UpdateOptions{})
				return err
			}, eventuallyTimeout, eventuallyInterval).ShouldNot(gomega.HaveOccurred())

			// Check if condition is updated on work api
			gomega.Eventually(func() error {
				work, err = hubWorkClient.WorkV1().ManifestWorks(clusterName).Get(context.Background(), work.Name, metav1.GetOptions{})
				if err != nil {
					return err
				}

				if len(work.Status.ResourceStatus.Manifests) != 1 {
					return fmt.Errorf("the size of resource status is not correct, expect to be 1 but got %d", len(work.Status.ResourceStatus.Manifests))
				}

				return util.CheckExpectedConditions(work.Status.ResourceStatus.Manifests[0].Conditions, metav1.Condition{
					Type:    "OneActive",
					Reason:  workapiv1.ConditionRuleEvaluated,
					Status:  metav1.ConditionTrue,
					Message: "One pod is active",
				})
			}, eventuallyTimeout, eventuallyInterval).ShouldNot(gomega.HaveOccurred())
		})

		ginkgo.It("should return none for missing wellknown condition", func() {
			work.Spec.ManifestConfigs = []workapiv1.ManifestConfigOption{
				{
					ResourceIdentifier: workapiv1.ResourceIdentifier{
						Group:     "batch",
						Resource:  "jobs",
						Namespace: clusterName,
						Name:      "job1",
					},
					UpdateStrategy: &workapiv1.UpdateStrategy{
						Type: workapiv1.UpdateStrategyTypeServerSideApply,
					},
					ConditionRules: []workapiv1.ConditionRule{
						{
							Type:      workapiv1.WellKnownConditionsType,
							Condition: "NotWellKnown",
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

			// Update Job status on spoke
			gomega.Eventually(func() error {
				job, err := spokeKubeClient.BatchV1().Jobs(clusterName).Get(context.Background(), "job1", metav1.GetOptions{})
				if err != nil {
					return err
				}

				job.Status.Active = 1
				job.Status.Ready = ptr.To(int32(1))
				job.Status.Conditions = []batchv1.JobCondition{}

				_, err = spokeKubeClient.BatchV1().Jobs(clusterName).UpdateStatus(context.Background(), job, metav1.UpdateOptions{})
				return err
			}, eventuallyTimeout, eventuallyInterval).ShouldNot(gomega.HaveOccurred())

			// Check if we get condition on work api
			gomega.Eventually(func() error {
				work, err = hubWorkClient.WorkV1().ManifestWorks(clusterName).Get(context.Background(), work.Name, metav1.GetOptions{})
				if err != nil {
					return err
				}

				if len(work.Status.ResourceStatus.Manifests) != 1 {
					return fmt.Errorf("the size of resource status is not correct, expect to be 1 but got %d", len(work.Status.ResourceStatus.Manifests))
				}

				return util.CheckExpectedConditions(work.Status.ResourceStatus.Manifests[0].Conditions, metav1.Condition{
					Type:   "NotWellKnown",
					Status: util.ConditionNotFound,
				})
			}, eventuallyTimeout, eventuallyInterval).ShouldNot(gomega.HaveOccurred())
		})
	})

	ginkgo.Context("Jobs condition rules with wildcard", func() {
		ginkgo.BeforeEach(func() {
			job1, _, err := util.NewJob(clusterName, "job1", "sa")
			gomega.Expect(err).ToNot(gomega.HaveOccurred())
			manifests = append(manifests, util.ToManifest(job1))
			job2, _, err := util.NewJob(clusterName, "job2", "sa")
			gomega.Expect(err).ToNot(gomega.HaveOccurred())
			manifests = append(manifests, util.ToManifest(job2))

			var ctx context.Context
			ctx, cancel = context.WithCancel(context.Background())
			go startWorkAgent(ctx, clusterName)
		})

		ginkgo.AfterEach(func() {
			if cancel != nil {
				cancel()
			}
		})

		ginkgo.It("wildcard option should match all jobs", func() {
			work.Spec.ManifestConfigs = []workapiv1.ManifestConfigOption{
				{
					ResourceIdentifier: workapiv1.ResourceIdentifier{
						Group:     "batch",
						Resource:  "jobs",
						Namespace: "*",
						Name:      "job*",
					},
					UpdateStrategy: &workapiv1.UpdateStrategy{
						Type: workapiv1.UpdateStrategyTypeServerSideApply,
					},
					ConditionRules: []workapiv1.ConditionRule{
						{
							Type:      workapiv1.WellKnownConditionsType,
							Condition: workapiv1.ManifestComplete,
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

			// Update Job status on spoke
			gomega.Eventually(func() error {
				// Set first job as active
				job, err := spokeKubeClient.BatchV1().Jobs(clusterName).Get(context.Background(), "job1", metav1.GetOptions{})
				if err != nil {
					return err
				}

				job.Status.Active = 1
				job.Status.Ready = ptr.To(int32(1))

				_, err = spokeKubeClient.BatchV1().Jobs(clusterName).UpdateStatus(context.Background(), job, metav1.UpdateOptions{})
				if err != nil {
					return err
				}

				// Set second job as complete
				job, err = spokeKubeClient.BatchV1().Jobs(clusterName).Get(context.Background(), "job2", metav1.GetOptions{})
				if err != nil {
					return err
				}

				job.Status.Active = 0
				job.Status.Ready = ptr.To(int32(0))
				job.Status.Succeeded = 1
				job.Status.Conditions = []batchv1.JobCondition{
					{
						Type:    batchv1.JobComplete,
						Status:  corev1.ConditionTrue,
						Reason:  batchv1.JobReasonCompletionsReached,
						Message: "Reached expected number of succeeded pods",
					},
				}

				_, err = spokeKubeClient.BatchV1().Jobs(clusterName).UpdateStatus(context.Background(), job, metav1.UpdateOptions{})
				return err
			}, eventuallyTimeout, eventuallyInterval).ShouldNot(gomega.HaveOccurred())

			// Check if we get conditions of jobs on work api
			gomega.Eventually(func() error {
				work, err = hubWorkClient.WorkV1().ManifestWorks(clusterName).Get(context.Background(), work.Name, metav1.GetOptions{})
				if err != nil {
					return err
				}

				if len(work.Status.ResourceStatus.Manifests) != 2 {
					return fmt.Errorf("the size of resource status is not correct, expect to be 2 but got %d", len(work.Status.ResourceStatus.Manifests))
				}

				err := util.CheckExpectedConditions(work.Status.ResourceStatus.Manifests[0].Conditions, metav1.Condition{
					Type:   workapiv1.ManifestComplete,
					Status: metav1.ConditionFalse,
					Reason: workapiv1.ConditionRuleEvaluated,
				})
				if err != nil {
					return fmt.Errorf("Job1: %v", err)
				}

				err = util.CheckExpectedConditions(work.Status.ResourceStatus.Manifests[1].Conditions, metav1.Condition{
					Type:   workapiv1.ManifestComplete,
					Status: metav1.ConditionTrue,
					Reason: workapiv1.ConditionRuleEvaluated,
				})
				if err != nil {
					return fmt.Errorf("Job2: %v", err)
				}
				return nil
			}, eventuallyTimeout, eventuallyInterval).ShouldNot(gomega.HaveOccurred())
		})

		ginkgo.It("matched multi options only the first works ", func() {
			work.Spec.ManifestConfigs = []workapiv1.ManifestConfigOption{
				{
					ResourceIdentifier: workapiv1.ResourceIdentifier{
						Group:     "batch",
						Resource:  "jobs",
						Namespace: "*",
						Name:      "job1",
					},
					UpdateStrategy: &workapiv1.UpdateStrategy{
						Type: workapiv1.UpdateStrategyTypeServerSideApply,
					},
					ConditionRules: []workapiv1.ConditionRule{
						{Type: workapiv1.WellKnownConditionsType, Condition: workapiv1.ManifestComplete},
					},
				},
				{
					ResourceIdentifier: workapiv1.ResourceIdentifier{
						Group:     "batch",
						Resource:  "jobs",
						Namespace: "*",
						Name:      "job*",
					},
					UpdateStrategy: &workapiv1.UpdateStrategy{
						Type: workapiv1.UpdateStrategyTypeServerSideApply,
					},
					ConditionRules: []workapiv1.ConditionRule{
						{
							Type:      workapiv1.CelConditionExpressionsType,
							Condition: workapiv1.ManifestComplete,
							CelExpressions: []string{
								"badexpression",
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

			// Update Job status on spoke
			gomega.Eventually(func() error {
				// Set first job as active
				job, err := spokeKubeClient.BatchV1().Jobs(clusterName).Get(context.Background(), "job1", metav1.GetOptions{})
				if err != nil {
					return err
				}

				job.Status.Active = 1
				job.Status.Ready = ptr.To(int32(1))

				_, err = spokeKubeClient.BatchV1().Jobs(clusterName).UpdateStatus(context.Background(), job, metav1.UpdateOptions{})
				if err != nil {
					return err
				}

				// Set second job as complete
				job, err = spokeKubeClient.BatchV1().Jobs(clusterName).Get(context.Background(), "job2", metav1.GetOptions{})
				if err != nil {
					return err
				}

				job.Status.Active = 0
				job.Status.Ready = ptr.To(int32(0))
				job.Status.Succeeded = 1
				job.Status.Conditions = []batchv1.JobCondition{
					{
						Type:    batchv1.JobComplete,
						Status:  corev1.ConditionTrue,
						Reason:  batchv1.JobReasonCompletionsReached,
						Message: "Reached expected number of succeeded pods",
					},
				}

				_, err = spokeKubeClient.BatchV1().Jobs(clusterName).UpdateStatus(context.Background(), job, metav1.UpdateOptions{})
				return err
			}, eventuallyTimeout, eventuallyInterval).ShouldNot(gomega.HaveOccurred())

			// Check if we get conditions of jobs on work api
			gomega.Eventually(func() error {
				work, err = hubWorkClient.WorkV1().ManifestWorks(clusterName).Get(context.Background(), work.Name, metav1.GetOptions{})
				if err != nil {
					return err
				}

				if len(work.Status.ResourceStatus.Manifests) != 2 {
					return fmt.Errorf("the size of resource status is not correct, expect to be 2 but got %d", len(work.Status.ResourceStatus.Manifests))
				}

				err = util.CheckExpectedConditions(work.Status.ResourceStatus.Manifests[0].Conditions, metav1.Condition{
					Type:   workapiv1.ManifestComplete,
					Status: metav1.ConditionFalse,
					Reason: workapiv1.ConditionRuleEvaluated,
				})
				if err != nil {
					return fmt.Errorf("Job1: %v", err)
				}

				err = util.CheckExpectedConditions(work.Status.ResourceStatus.Manifests[1].Conditions, metav1.Condition{
					Type:   workapiv1.ManifestComplete,
					Status: metav1.ConditionFalse,
					Reason: workapiv1.ConditionRuleExpressionError,
				})
				if err != nil {
					return fmt.Errorf("Job2: %v", err)
				}
				return nil
			}, eventuallyTimeout, eventuallyInterval).ShouldNot(gomega.HaveOccurred())
		})
	})
})
