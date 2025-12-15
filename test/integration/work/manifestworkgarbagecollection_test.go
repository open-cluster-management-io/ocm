package work

import (
	"context"
	"fmt"
	"time"

	"github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/rand"

	workapiv1 "open-cluster-management.io/api/work/v1"

	"open-cluster-management.io/ocm/test/integration/util"
)

var _ = ginkgo.Describe("ManifestWork TTL after completion", func() {
	var cancel context.CancelFunc

	var workName string
	var clusterName string
	var work *workapiv1.ManifestWork
	var manifests []workapiv1.Manifest

	var err error

	ginkgo.BeforeEach(func() {
		clusterName = rand.String(5)
		workName = fmt.Sprintf("work-ttl-%s", rand.String(5))

		ns := &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{Name: clusterName},
		}
		_, err := spokeKubeClient.CoreV1().Namespaces().Create(context.Background(), ns, metav1.CreateOptions{})
		gomega.Expect(err).ToNot(gomega.HaveOccurred())

		var ctx context.Context
		ctx, cancel = context.WithCancel(context.Background())
		go startWorkAgent(ctx, clusterName)

		// Setup manifests - using a simple configmap that can be easily completed
		manifests = []workapiv1.Manifest{
			util.ToManifest(util.NewConfigmap(clusterName, "test-cm", map[string]string{"test": "data"}, []string{})),
		}
	})

	ginkgo.AfterEach(func() {
		if cancel != nil {
			cancel()
		}
		err := spokeKubeClient.CoreV1().Namespaces().Delete(context.Background(), clusterName, metav1.DeleteOptions{})
		gomega.Expect(err).ToNot(gomega.HaveOccurred())
	})

	ginkgo.Context("When ManifestWork has TTLSecondsAfterFinished configured", func() {
		ginkgo.It("should delete the ManifestWork after TTL expires when Complete condition is true", func() {
			// Create ManifestWork with short TTL
			ttlSeconds := int64(5)
			work = util.NewManifestWork(clusterName, workName, manifests)
			work.Spec.DeleteOption = &workapiv1.DeleteOption{
				PropagationPolicy:       workapiv1.DeletePropagationPolicyTypeForeground,
				TTLSecondsAfterFinished: &ttlSeconds,
			}

			// Add condition rule to mark work as complete when configmap is available
			work.Spec.ManifestConfigs = []workapiv1.ManifestConfigOption{
				{
					ResourceIdentifier: workapiv1.ResourceIdentifier{
						Group:     "",
						Resource:  "configmaps",
						Name:      "test-cm",
						Namespace: clusterName,
					},
					ConditionRules: []workapiv1.ConditionRule{
						{
							Condition: workapiv1.WorkComplete,
							Type:      workapiv1.CelConditionExpressionsType,
							CelExpressions: []string{
								"object.metadata.name == 'test-cm'",
							},
						},
					},
				},
			}

			// Create the ManifestWork
			work, err = hubWorkClient.WorkV1().ManifestWorks(clusterName).Create(context.Background(), work, metav1.CreateOptions{})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())

			// Wait for work to be applied and available
			util.AssertWorkCondition(work.Namespace, work.Name, hubWorkClient, workapiv1.WorkApplied, metav1.ConditionTrue,
				[]metav1.ConditionStatus{metav1.ConditionTrue}, eventuallyTimeout, eventuallyInterval)
			util.AssertWorkCondition(work.Namespace, work.Name, hubWorkClient, workapiv1.WorkAvailable, metav1.ConditionTrue,
				[]metav1.ConditionStatus{metav1.ConditionTrue}, eventuallyTimeout, eventuallyInterval)

			// Wait for work to be marked as complete
			gomega.Eventually(func() error {
				work, err = hubWorkClient.WorkV1().ManifestWorks(clusterName).Get(context.Background(), workName, metav1.GetOptions{})
				if err != nil {
					return err
				}
				if meta.IsStatusConditionTrue(work.Status.Conditions, workapiv1.WorkComplete) {
					return nil
				}
				return fmt.Errorf("ManifestWork %s is not complete", work.Name)
			}, eventuallyTimeout, eventuallyInterval).Should(gomega.Succeed())

			ginkgo.By("Verifying the ManifestWork is deleted after TTL expires")
			// Wait for the work to be deleted (TTL + buffer time)
			gomega.Eventually(func() bool {
				_, err = hubWorkClient.WorkV1().ManifestWorks(clusterName).Get(context.Background(), workName, metav1.GetOptions{})
				return errors.IsNotFound(err)
			}, time.Duration(ttlSeconds+10)*time.Second, eventuallyInterval).Should(gomega.BeTrue())
		})

		ginkgo.It("should not delete the ManifestWork when TTL is not configured", func() {
			// Create ManifestWork without TTL
			work = util.NewManifestWork(clusterName, workName, manifests)

			// Add condition rule to mark work as complete
			work.Spec.ManifestConfigs = []workapiv1.ManifestConfigOption{
				{
					ResourceIdentifier: workapiv1.ResourceIdentifier{
						Group:     "",
						Resource:  "configmaps",
						Name:      "test-cm",
						Namespace: clusterName,
					},
					ConditionRules: []workapiv1.ConditionRule{
						{
							Condition: workapiv1.WorkComplete,
							Type:      workapiv1.CelConditionExpressionsType,
							CelExpressions: []string{
								"object.metadata.name == 'test-cm'",
							},
						},
					},
				},
			}

			// Create the ManifestWork
			work, err = hubWorkClient.WorkV1().ManifestWorks(clusterName).Create(context.Background(), work, metav1.CreateOptions{})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())

			// Wait for work to be applied, available, and completed
			util.AssertWorkCondition(work.Namespace, work.Name, hubWorkClient, workapiv1.WorkApplied, metav1.ConditionTrue,
				[]metav1.ConditionStatus{metav1.ConditionTrue}, eventuallyTimeout, eventuallyInterval)
			util.AssertWorkCondition(work.Namespace, work.Name, hubWorkClient, workapiv1.WorkAvailable, metav1.ConditionTrue,
				[]metav1.ConditionStatus{metav1.ConditionTrue}, eventuallyTimeout, eventuallyInterval)

			// Wait for work to be marked as complete
			gomega.Eventually(func() error {
				work, err = hubWorkClient.WorkV1().ManifestWorks(clusterName).Get(context.Background(), workName, metav1.GetOptions{})
				if err != nil {
					return err
				}
				if meta.IsStatusConditionTrue(work.Status.Conditions, workapiv1.WorkComplete) {
					return nil
				}
				return fmt.Errorf("ManifestWork %s is not complete", work.Name)
			}, eventuallyTimeout, eventuallyInterval).Should(gomega.Succeed())

			ginkgo.By("Verifying the ManifestWork is NOT deleted without TTL configuration")
			// Wait some time and verify the work still exists
			gomega.Consistently(func() error {
				_, err = hubWorkClient.WorkV1().ManifestWorks(clusterName).Get(context.Background(), workName, metav1.GetOptions{})
				return err
			}, 10*time.Second, eventuallyInterval).Should(gomega.Succeed())
		})

		ginkgo.It("should not delete the ManifestWork when it is not completed", func() {
			// Create ManifestWork with TTL but without completion condition rule
			ttlSeconds := int64(5)
			work = util.NewManifestWork(clusterName, workName, manifests)
			work.Spec.DeleteOption = &workapiv1.DeleteOption{
				PropagationPolicy:       workapiv1.DeletePropagationPolicyTypeForeground,
				TTLSecondsAfterFinished: &ttlSeconds,
			}

			// Create the ManifestWork without completion rules
			work, err = hubWorkClient.WorkV1().ManifestWorks(clusterName).Create(context.Background(), work, metav1.CreateOptions{})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())

			// Wait for work to be applied and available
			util.AssertWorkCondition(work.Namespace, work.Name, hubWorkClient, workapiv1.WorkApplied, metav1.ConditionTrue,
				[]metav1.ConditionStatus{metav1.ConditionTrue}, eventuallyTimeout, eventuallyInterval)
			util.AssertWorkCondition(work.Namespace, work.Name, hubWorkClient, workapiv1.WorkAvailable, metav1.ConditionTrue,
				[]metav1.ConditionStatus{metav1.ConditionTrue}, eventuallyTimeout, eventuallyInterval)

			ginkgo.By("Verifying the ManifestWork is NOT deleted when not completed")
			// Wait and verify the work still exists (since it's not completed)
			gomega.Consistently(func() error {
				_, err = hubWorkClient.WorkV1().ManifestWorks(clusterName).Get(context.Background(), workName, metav1.GetOptions{})
				return err
			}, time.Duration(ttlSeconds+5)*time.Second, eventuallyInterval).Should(gomega.Succeed())
		})

		ginkgo.It("should delete immediately when TTL is set to zero", func() {
			// Create ManifestWork with zero TTL
			ttlSeconds := int64(0)
			work = util.NewManifestWork(clusterName, workName, manifests)
			work.Spec.DeleteOption = &workapiv1.DeleteOption{
				PropagationPolicy:       workapiv1.DeletePropagationPolicyTypeForeground,
				TTLSecondsAfterFinished: &ttlSeconds,
			}

			// Add condition rule to mark work as complete
			work.Spec.ManifestConfigs = []workapiv1.ManifestConfigOption{
				{
					ResourceIdentifier: workapiv1.ResourceIdentifier{
						Group:     "",
						Resource:  "configmaps",
						Name:      "test-cm",
						Namespace: clusterName,
					},
					ConditionRules: []workapiv1.ConditionRule{
						{
							Condition: workapiv1.WorkComplete,
							Type:      workapiv1.CelConditionExpressionsType,
							CelExpressions: []string{
								"object.metadata.name == 'test-cm'",
							},
						},
					},
				},
			}

			// Create the ManifestWork
			work, err = hubWorkClient.WorkV1().ManifestWorks(clusterName).Create(context.Background(), work, metav1.CreateOptions{})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())

			ginkgo.By("Verifying the ManifestWork is deleted immediately with zero TTL")
			// Should be deleted quickly since TTL is 0
			gomega.Eventually(func() bool {
				_, err = hubWorkClient.WorkV1().ManifestWorks(clusterName).Get(context.Background(), workName, metav1.GetOptions{})
				return errors.IsNotFound(err)
			}, 15*time.Second, eventuallyInterval).Should(gomega.BeTrue())
		})

		ginkgo.It("should not delete the ManifestWork with ManifestWorkReplicaSet label even after TTL expires", func() {
			// Create ManifestWork with TTL and ManifestWorkReplicaSet label
			ttlSeconds := int64(5)
			work = util.NewManifestWork(clusterName, workName, manifests)
			work.Spec.DeleteOption = &workapiv1.DeleteOption{
				PropagationPolicy:       workapiv1.DeletePropagationPolicyTypeForeground,
				TTLSecondsAfterFinished: &ttlSeconds,
			}

			// Add ManifestWorkReplicaSet label
			work.Labels = map[string]string{
				"work.open-cluster-management.io/manifestworkreplicaset": "test-replicaset",
			}

			// Add condition rule to mark work as complete
			work.Spec.ManifestConfigs = []workapiv1.ManifestConfigOption{
				{
					ResourceIdentifier: workapiv1.ResourceIdentifier{
						Group:     "",
						Resource:  "configmaps",
						Name:      "test-cm",
						Namespace: clusterName,
					},
					ConditionRules: []workapiv1.ConditionRule{
						{
							Condition: workapiv1.WorkComplete,
							Type:      workapiv1.CelConditionExpressionsType,
							CelExpressions: []string{
								"object.metadata.name == 'test-cm'",
							},
						},
					},
				},
			}

			// Create the ManifestWork
			work, err = hubWorkClient.WorkV1().ManifestWorks(clusterName).Create(context.Background(), work, metav1.CreateOptions{})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())

			// Wait for work to be applied, available, and completed
			util.AssertWorkCondition(work.Namespace, work.Name, hubWorkClient, workapiv1.WorkApplied, metav1.ConditionTrue,
				[]metav1.ConditionStatus{metav1.ConditionTrue}, eventuallyTimeout, eventuallyInterval)
			util.AssertWorkCondition(work.Namespace, work.Name, hubWorkClient, workapiv1.WorkAvailable, metav1.ConditionTrue,
				[]metav1.ConditionStatus{metav1.ConditionTrue}, eventuallyTimeout, eventuallyInterval)

			// Wait for work to be marked as complete
			gomega.Eventually(func() error {
				work, err = hubWorkClient.WorkV1().ManifestWorks(clusterName).Get(context.Background(), workName, metav1.GetOptions{})
				if err != nil {
					return err
				}
				if meta.IsStatusConditionTrue(work.Status.Conditions, workapiv1.WorkComplete) {
					return nil
				}
				return fmt.Errorf("ManifestWork %s is not complete", work.Name)
			}, eventuallyTimeout, eventuallyInterval).Should(gomega.Succeed())

			ginkgo.By("Verifying the ManifestWork is NOT deleted even after TTL expires due to ManifestWorkReplicaSet label")
			// Wait past TTL expiry and verify the work still exists
			gomega.Consistently(func() error {
				_, err = hubWorkClient.WorkV1().ManifestWorks(clusterName).Get(context.Background(), workName, metav1.GetOptions{})
				return err
			}, time.Duration(ttlSeconds+5)*time.Second, eventuallyInterval).Should(gomega.Succeed())
		})
	})
})
