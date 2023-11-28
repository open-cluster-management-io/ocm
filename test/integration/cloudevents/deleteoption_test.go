package cloudevents

import (
	"context"
	"fmt"
	"time"

	"github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	utilrand "k8s.io/apimachinery/pkg/util/rand"

	workapiv1 "open-cluster-management.io/api/work/v1"

	commonoptions "open-cluster-management.io/ocm/pkg/common/options"
	"open-cluster-management.io/ocm/pkg/work/spoke"
	"open-cluster-management.io/ocm/test/integration/util"
)

var _ = ginkgo.Describe("ManifestWork Delete Option", func() {
	var o *spoke.WorkloadAgentOptions
	var commOptions *commonoptions.AgentOptions
	var cancel context.CancelFunc

	var work *workapiv1.ManifestWork
	var manifests []workapiv1.Manifest

	var err error

	ginkgo.BeforeEach(func() {
		o = spoke.NewWorkloadAgentOptions()
		o.StatusSyncInterval = 3 * time.Second
		o.WorkloadSourceDriver.Type = workSourceDriver
		o.WorkloadSourceDriver.Config = workSourceConfigFileName

		commOptions = commonoptions.NewAgentOptions()
		commOptions.SpokeClusterName = utilrand.String(5)

		ns := &corev1.Namespace{}
		ns.Name = commOptions.SpokeClusterName
		_, err := spokeKubeClient.CoreV1().Namespaces().Create(context.Background(), ns, metav1.CreateOptions{})
		gomega.Expect(err).ToNot(gomega.HaveOccurred())

		var ctx context.Context
		ctx, cancel = context.WithCancel(context.Background())
		go startWorkAgent(ctx, o, commOptions)

		// reset manifests
		manifests = nil
	})

	ginkgo.JustBeforeEach(func() {
		work = util.NewManifestWork(commOptions.SpokeClusterName, "", manifests)
		gomega.Expect(err).ToNot(gomega.HaveOccurred())
	})

	ginkgo.AfterEach(func() {
		if cancel != nil {
			cancel()
		}
		err := spokeKubeClient.CoreV1().Namespaces().Delete(context.Background(), commOptions.SpokeClusterName, metav1.DeleteOptions{})
		gomega.Expect(err).ToNot(gomega.HaveOccurred())
	})

	// TODO test multiple manifests after the manifestbundles is enabled

	ginkgo.Context("Delete options", func() {
		ginkgo.BeforeEach(func() {
			manifests = []workapiv1.Manifest{
				util.ToManifest(util.NewConfigmap(commOptions.SpokeClusterName, cm1, map[string]string{"a": "b"}, []string{})),
			}
		})

		ginkgo.It("Orphan deletion of the whole manifestwork", func() {
			work.Spec.DeleteOption = &workapiv1.DeleteOption{
				PropagationPolicy: workapiv1.DeletePropagationPolicyTypeOrphan,
			}

			work, err = workSourceWorkClient.WorkV1().ManifestWorks(commOptions.SpokeClusterName).Create(context.Background(), work, metav1.CreateOptions{})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())

			util.AssertWorkCondition(work.Namespace, work.Name, workSourceWorkClient, workapiv1.WorkApplied, metav1.ConditionTrue,
				[]metav1.ConditionStatus{metav1.ConditionTrue}, eventuallyTimeout, eventuallyInterval)
			util.AssertWorkCondition(work.Namespace, work.Name, workSourceWorkClient, workapiv1.WorkAvailable, metav1.ConditionTrue,
				[]metav1.ConditionStatus{metav1.ConditionTrue}, eventuallyTimeout, eventuallyInterval)

			// Ensure configmap exists
			util.AssertExistenceOfConfigMaps(manifests, spokeKubeClient, eventuallyTimeout, eventuallyInterval)

			// Ensure ownership of configmap is updated
			gomega.Eventually(func() error {
				cm, err := spokeKubeClient.CoreV1().ConfigMaps(commOptions.SpokeClusterName).Get(context.Background(), cm1, metav1.GetOptions{})
				if err != nil {
					return err
				}

				if len(cm.OwnerReferences) != 0 {
					return fmt.Errorf("owner reference are not correctly updated, current ownerrefs are %v", cm.OwnerReferences)
				}

				return nil
			}, eventuallyTimeout, eventuallyInterval).ShouldNot(gomega.HaveOccurred())

			// Delete the work
			err = workSourceWorkClient.WorkV1().ManifestWorks(commOptions.SpokeClusterName).Delete(context.Background(), work.Name, metav1.DeleteOptions{})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())

			// Wait for deletion of manifest work
			gomega.Eventually(func() bool {
				_, err := workSourceWorkClient.WorkV1().ManifestWorks(commOptions.SpokeClusterName).Get(context.Background(), work.Name, metav1.GetOptions{})
				return errors.IsNotFound(err)
			}, eventuallyTimeout, eventuallyInterval).Should(gomega.BeTrue())

			// Ensure configmap exists
			util.AssertExistenceOfConfigMaps(manifests, spokeKubeClient, eventuallyTimeout, eventuallyInterval)
		})

		ginkgo.It("Clean the resource when orphan deletion option is removed", func() {
			work.Spec.DeleteOption = &workapiv1.DeleteOption{
				PropagationPolicy: workapiv1.DeletePropagationPolicyTypeSelectivelyOrphan,
				SelectivelyOrphan: &workapiv1.SelectivelyOrphan{
					OrphaningRules: []workapiv1.OrphaningRule{
						{
							Group:     "",
							Resource:  "configmaps",
							Namespace: commOptions.SpokeClusterName,
							Name:      cm1,
						},
					},
				},
			}

			work, err = workSourceWorkClient.WorkV1().ManifestWorks(commOptions.SpokeClusterName).Create(context.Background(), work, metav1.CreateOptions{})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())

			util.AssertWorkCondition(work.Namespace, work.Name, workSourceWorkClient, workapiv1.WorkApplied, metav1.ConditionTrue,
				[]metav1.ConditionStatus{metav1.ConditionTrue}, eventuallyTimeout, eventuallyInterval)
			util.AssertWorkCondition(work.Namespace, work.Name, workSourceWorkClient, workapiv1.WorkAvailable, metav1.ConditionTrue,
				[]metav1.ConditionStatus{metav1.ConditionTrue}, eventuallyTimeout, eventuallyInterval)

			// Ensure configmap exists
			util.AssertExistenceOfConfigMaps(manifests, spokeKubeClient, eventuallyTimeout, eventuallyInterval)

			// Ensure ownership of configmap is updated
			gomega.Eventually(func() error {
				cm, err := spokeKubeClient.CoreV1().ConfigMaps(commOptions.SpokeClusterName).Get(context.Background(), cm1, metav1.GetOptions{})
				if err != nil {
					return err
				}

				if len(cm.OwnerReferences) != 0 {
					return fmt.Errorf("owner reference are not correctly updated, current ownerrefs are %v", cm.OwnerReferences)
				}

				return nil
			}, eventuallyTimeout, eventuallyInterval).ShouldNot(gomega.HaveOccurred())

			// Remove the delete option
			gomega.Eventually(func() error {
				work, err = workSourceWorkClient.WorkV1().ManifestWorks(commOptions.SpokeClusterName).Get(context.Background(), work.Name, metav1.GetOptions{})
				if err != nil {
					return err
				}

				work.Spec.DeleteOption = nil
				_, err = workSourceWorkClient.WorkV1().ManifestWorks(commOptions.SpokeClusterName).Update(context.Background(), work, metav1.UpdateOptions{})
				return err
			}, eventuallyTimeout, eventuallyInterval).ShouldNot(gomega.HaveOccurred())

			// Ensure ownership of configmap is updated
			gomega.Eventually(func() error {
				cm, err := spokeKubeClient.CoreV1().ConfigMaps(commOptions.SpokeClusterName).Get(context.Background(), cm1, metav1.GetOptions{})
				if err != nil {
					return err
				}

				if len(cm.OwnerReferences) != 1 {
					return fmt.Errorf("owner reference are not correctly updated, current ownerrefs are %v", cm.OwnerReferences)
				}

				return nil
			}, eventuallyTimeout, eventuallyInterval).ShouldNot(gomega.HaveOccurred())

			// Delete the work
			err = workSourceWorkClient.WorkV1().ManifestWorks(commOptions.SpokeClusterName).Delete(context.Background(), work.Name, metav1.DeleteOptions{})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())

			// Wait for deletion of manifest work
			gomega.Eventually(func() bool {
				_, err := workSourceWorkClient.WorkV1().ManifestWorks(commOptions.SpokeClusterName).Get(context.Background(), work.Name, metav1.GetOptions{})
				return errors.IsNotFound(err)
			}, eventuallyTimeout, eventuallyInterval).Should(gomega.BeTrue())

			// All of the resource should be deleted.
			_, err = spokeKubeClient.CoreV1().ConfigMaps(commOptions.SpokeClusterName).Get(context.Background(), cm1, metav1.GetOptions{})
			gomega.Expect(errors.IsNotFound(err)).To(gomega.BeTrue())
		})
	})
})
