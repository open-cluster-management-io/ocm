package e2e

import (
	"context"
	"fmt"
	"reflect"
	"strings"
	"time"

	"github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apiequality "k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/rand"
	applyappsv1 "k8s.io/client-go/applyconfigurations/apps/v1"
	"k8s.io/utils/pointer"

	workapiv1 "open-cluster-management.io/api/work/v1"

	"open-cluster-management.io/ocm/test/integration/util"
)

const (
	guestBookCRDJson = `{
		"apiVersion": "apiextensions.k8s.io/v1",
		"kind": "CustomResourceDefinition",
		"metadata": {
			"name": "guestbooks.my.domain"
		},
		"spec": {
			"conversion": {
				"strategy": "None"
			},
			"group": "my.domain",
			"names": {
				"kind": "Guestbook",
				"listKind": "GuestbookList",
				"plural": "guestbooks",
				"singular": "guestbook"
			},
			"preserveUnknownFields": false,
			"scope": "Namespaced",
			"versions": [
				{
					"name": "v1",
					"schema": {
						"openAPIV3Schema": {
							"properties": {
								"apiVersion": {
									"type": "string"
								},
								"kind": {
									"type": "string"
								},
								"metadata": {
									"type": "object"
								},
								"spec": {
									"properties": {
										"foo": {
											"type": "string"
										}
									},
									"type": "object"
								},
								"status": {
									"type": "object"
								}
							},
							"type": "object"
						}
					},
					"served": true,
					"storage": false
				},
				{
					"name": "v2",
					"schema": {
						"openAPIV3Schema": {
							"properties": {
								"apiVersion": {
									"type": "string"
								},
								"kind": {
									"type": "string"
								},
								"metadata": {
									"type": "object"
								},
								"spec": {
									"properties": {
										"foo": {
											"type": "string"
										}
									},
									"type": "object"
								},
								"status": {
									"type": "object"
								}
							},
							"type": "object"
						}
					},
					"served": true,
					"storage": true
				}
			]
		}
	}`

	guestbookCRJson = `{
		"apiVersion": "my.domain/v1",
		"kind": "Guestbook",
		"metadata": {
			"name": "guestbook1",
			"namespace": "default"
		},
		"spec": {
			"foo": "bar"
		}
	}`

	upgradedGuestBookCRJson = `{
		"apiVersion": "my.domain/v2",
		"kind": "Guestbook",
		"metadata": {
			"name": "guestbook1",
			"namespace": "default"
		},
		"spec": {
			"foo": "foo"
		}
	}`
)

// Test cases with lable "sanity-check" could be ran on an existing environment with work agent installed
// and well configured as sanity check. Resource leftovers should be cleaned up on both hub and managed cluster.
var _ = ginkgo.Describe("Work agent", ginkgo.Label("work-agent", "sanity-check"), func() {
	var workName string
	var err error
	var nameSuffix string

	ginkgo.BeforeEach(func() {
		nameSuffix = rand.String(5)
		workName = fmt.Sprintf("work-%s", nameSuffix)
	})

	ginkgo.AfterEach(func() {
		ginkgo.By(fmt.Sprintf("delete manifestwork %v/%v", universalClusterName, workName))
		gomega.Expect(hub.CleanManifestWorks(universalClusterName, workName)).To(gomega.BeNil())
	})

	ginkgo.Context("Work CRUD", func() {
		var ns1 string
		var ns2 string

		ginkgo.BeforeEach(func() {
			ns1 = fmt.Sprintf("ns1-%s", nameSuffix)
			ns2 = fmt.Sprintf("ns2-%s", nameSuffix)

			// create ns2
			ns := &corev1.Namespace{}
			ns.Name = ns2
			_, err = spoke.KubeClient.CoreV1().Namespaces().Create(context.Background(), ns, metav1.CreateOptions{})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())

		})

		ginkgo.AfterEach(func() {
			// remove finalizer from cm3 if necessary
			cm3, err := spoke.KubeClient.CoreV1().ConfigMaps(ns2).Get(context.Background(), "cm3", metav1.GetOptions{})
			if err == nil {
				cm3.Finalizers = nil
				err = spoke.KubeClient.CoreV1().ConfigMaps(ns2).Delete(context.Background(), "cm3", metav1.DeleteOptions{})
			}
			if err != nil {
				gomega.Expect(errors.IsNotFound(err)).To(gomega.BeTrue())
			}

			// delete ns2
			err = spoke.KubeClient.CoreV1().Namespaces().Delete(context.Background(), ns2, metav1.DeleteOptions{})
			if err != nil {
				gomega.Expect(errors.IsNotFound(err)).To(gomega.BeTrue())
			}
		})

		ginkgo.It("Should create, update and delete manifestwork successfully", func() {
			ginkgo.By("create manifestwork")

			cmFinalizers := []string{"cluster.open-cluster-management.io/testing"}
			objects := []runtime.Object{
				util.NewConfigmap(ns1, "cm1", nil, nil),
				newNamespace(ns1),
				util.NewConfigmap(ns1, "cm2", nil, nil),
				util.NewConfigmap(ns2, "cm3", nil, cmFinalizers),
			}
			work := newManifestWork(universalClusterName, workName, objects...)
			work, err = hub.WorkClient.WorkV1().ManifestWorks(universalClusterName).Create(context.Background(), work, metav1.CreateOptions{})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())

			// check if resources are applied for manifests
			gomega.Eventually(func() error {
				_, err := spoke.KubeClient.CoreV1().ConfigMaps(ns1).Get(context.Background(), "cm1", metav1.GetOptions{})
				if err != nil {
					return err
				}

				_, err = spoke.KubeClient.CoreV1().Namespaces().Get(context.Background(), ns1, metav1.GetOptions{})
				if err != nil {
					return err
				}

				_, err = spoke.KubeClient.CoreV1().ConfigMaps(ns1).Get(context.Background(), "cm2", metav1.GetOptions{})
				if err != nil {
					return err
				}

				_, err = spoke.KubeClient.CoreV1().ConfigMaps(ns2).Get(context.Background(), "cm3", metav1.GetOptions{})
				return err
			}).ShouldNot(gomega.HaveOccurred())

			// check status conditions in manifestwork status
			gomega.Eventually(func() error {
				// check manifest status conditions
				expectedManifestStatuses := []metav1.ConditionStatus{
					metav1.ConditionTrue, metav1.ConditionTrue, metav1.ConditionTrue, metav1.ConditionTrue}
				return assertManifestWorkAppliedSuccessfully(universalClusterName, workName, expectedManifestStatuses)
			}).ShouldNot(gomega.HaveOccurred())

			// get the corresponding AppliedManifestWork
			var appliedManifestWork *workapiv1.AppliedManifestWork
			gomega.Eventually(func() error {
				appliedManifestWorkList, err := spoke.WorkClient.WorkV1().AppliedManifestWorks().List(context.Background(), metav1.ListOptions{})
				if err != nil {
					return err
				}

				for _, item := range appliedManifestWorkList.Items {
					if strings.HasSuffix(item.Name, workName) {
						appliedManifestWork = item.DeepCopy()
						return nil
					}
				}

				return fmt.Errorf("not found the applied manifest work with suffix %s", workName)
			}).ShouldNot(gomega.HaveOccurred())

			// check applied resources in manifestwork status
			expectedAppliedResources := []workapiv1.AppliedManifestResourceMeta{
				{Version: "v1", ResourceIdentifier: workapiv1.ResourceIdentifier{Resource: "configmaps", Namespace: ns1, Name: "cm1"}},
				{Version: "v1", ResourceIdentifier: workapiv1.ResourceIdentifier{Resource: "configmaps", Namespace: ns1, Name: "cm2"}},
				{Version: "v1", ResourceIdentifier: workapiv1.ResourceIdentifier{Resource: "configmaps", Namespace: ns2, Name: "cm3"}},
				{Version: "v1", ResourceIdentifier: workapiv1.ResourceIdentifier{Resource: "namespaces", Name: ns1}},
			}
			var actualAppliedResources []workapiv1.AppliedManifestResourceMeta
			for _, appliedResource := range appliedManifestWork.Status.AppliedResources {
				actualAppliedResources = append(actualAppliedResources, workapiv1.AppliedManifestResourceMeta{
					ResourceIdentifier: workapiv1.ResourceIdentifier{
						Group:     appliedResource.Group,
						Resource:  appliedResource.Resource,
						Namespace: appliedResource.Namespace,
						Name:      appliedResource.Name,
					},
					Version: appliedResource.Version,
				})
			}
			gomega.Expect(reflect.DeepEqual(actualAppliedResources, expectedAppliedResources)).To(gomega.BeTrue())

			ginkgo.By("update manifestwork")
			cmData := map[string]string{"x": "y"}
			newObjects := []runtime.Object{
				objects[1],
				objects[2],
				util.NewConfigmap(ns2, "cm3", cmData, cmFinalizers),
			}
			newWork := newManifestWork(universalClusterName, workName, newObjects...)
			gomega.Eventually(func() error {
				work, err = hub.WorkClient.WorkV1().ManifestWorks(universalClusterName).Get(context.Background(), workName, metav1.GetOptions{})
				if err != nil {
					return err
				}

				work.Spec.Workload.Manifests = newWork.Spec.Workload.Manifests
				work, err = hub.WorkClient.WorkV1().ManifestWorks(universalClusterName).Update(context.Background(), work, metav1.UpdateOptions{})
				return err
			}).Should(gomega.Succeed())

			// check if cm1 is removed from applied resources list in status
			gomega.Eventually(func() error {
				appliedManifestWork, err = spoke.WorkClient.WorkV1().AppliedManifestWorks().Get(
					context.Background(), appliedManifestWork.Name, metav1.GetOptions{})
				if err != nil {
					return err
				}

				for _, resource := range appliedManifestWork.Status.AppliedResources {
					if resource.Name == "cm1" {
						return fmt.Errorf("found resource cm1")
					}
				}

				// check manifest status conditions
				expectedManifestStatuses := []metav1.ConditionStatus{
					metav1.ConditionTrue, metav1.ConditionTrue, metav1.ConditionTrue}
				return assertManifestWorkAppliedSuccessfully(universalClusterName, workName, expectedManifestStatuses)
			}).ShouldNot(gomega.HaveOccurred())

			// check if cm1 is deleted
			_, err = spoke.KubeClient.CoreV1().ConfigMaps(ns1).Get(context.Background(), "cm1", metav1.GetOptions{})
			gomega.Expect(errors.IsNotFound(err)).To(gomega.BeTrue())

			// check if cm3 is updated
			gomega.Eventually(func() error {
				cm, err := spoke.KubeClient.CoreV1().ConfigMaps(ns2).Get(context.Background(), "cm3", metav1.GetOptions{})
				if err != nil {
					return err
				}

				if !reflect.DeepEqual(cm.Data, cmData) {
					return fmt.Errorf("resource not equal")
				}

				return nil
			}).ShouldNot(gomega.HaveOccurred())

			ginkgo.By("delete manifestwork")
			err = hub.WorkClient.WorkV1().ManifestWorks(universalClusterName).Delete(context.Background(), workName, metav1.DeleteOptions{})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())

			// remove finalizer from cm3 in 2 seconds
			timer := time.NewTimer(2 * time.Second)
			go func() {
				<-timer.C
				cm, err := spoke.KubeClient.CoreV1().ConfigMaps(ns2).Get(context.Background(), "cm3", metav1.GetOptions{})
				if err == nil {
					cm.Finalizers = nil
					_, _ = spoke.KubeClient.CoreV1().ConfigMaps(ns2).Update(context.Background(), cm, metav1.UpdateOptions{})
				}
			}()

			// wait for deletion of manifest work
			gomega.Eventually(func() bool {
				_, err := hub.WorkClient.WorkV1().ManifestWorks(universalClusterName).Get(context.Background(), workName, metav1.GetOptions{})
				return errors.IsNotFound(err)
			}).Should(gomega.BeTrue())

			// Once manifest work is deleted, its corresponding appliedManifestWorks should be deleted as well
			_, err = spoke.WorkClient.WorkV1().AppliedManifestWorks().Get(context.Background(), appliedManifestWork.Name, metav1.GetOptions{})
			gomega.Expect(errors.IsNotFound(err)).To(gomega.BeTrue())

			// Once manifest work is deleted, all applied resources should have already been deleted too
			_, err = spoke.KubeClient.CoreV1().Namespaces().Get(context.Background(), ns1, metav1.GetOptions{})
			gomega.Expect(errors.IsNotFound(err)).To(gomega.BeTrue())

			_, err = spoke.KubeClient.CoreV1().ConfigMaps(ns1).Get(context.Background(), "cm2", metav1.GetOptions{})
			gomega.Expect(errors.IsNotFound(err)).To(gomega.BeTrue())

			_, err = spoke.KubeClient.CoreV1().ConfigMaps(ns2).Get(context.Background(), "cm3", metav1.GetOptions{})
			gomega.Expect(errors.IsNotFound(err)).To(gomega.BeTrue())

			err = spoke.KubeClient.CoreV1().Namespaces().Delete(context.Background(), ns2, metav1.DeleteOptions{})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())
		})
	})

	ginkgo.Context("With Job", func() {
		var jobName string

		ginkgo.BeforeEach(func() {
			jobName = fmt.Sprintf("job1-%s", nameSuffix)
		})

		ginkgo.It("Should create, delete job in manifestwork successfully", func() {
			ginkgo.By("create manifestwork")
			objects := []runtime.Object{
				newJob(jobName),
			}
			work := newManifestWork(universalClusterName, workName, objects...)
			work, err := hub.WorkClient.WorkV1().ManifestWorks(universalClusterName).Create(context.Background(), work, metav1.CreateOptions{})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())

			// check status conditions in manifestwork status
			gomega.Eventually(func() error {
				work, err = hub.WorkClient.WorkV1().ManifestWorks(universalClusterName).Get(context.Background(), workName, metav1.GetOptions{})
				if err != nil {
					return err
				}

				// check work status condition
				if !meta.IsStatusConditionTrue(work.Status.Conditions, workapiv1.WorkApplied) {
					return fmt.Errorf("condition %s is not true", workapiv1.WorkApplied)
				}
				if !meta.IsStatusConditionTrue(work.Status.Conditions, workapiv1.WorkAvailable) {
					return fmt.Errorf("condition %s is not true", workapiv1.WorkAvailable)
				}
				return nil
			}).ShouldNot(gomega.HaveOccurred())

			// Ensure pod is created
			gomega.Eventually(func() error {
				pods, err := spoke.KubeClient.CoreV1().Pods("default").List(context.Background(), metav1.ListOptions{})
				if err != nil {
					return err
				}

				if len(pods.Items) == 0 {
					return fmt.Errorf("not found")
				}

				return nil
			}).ShouldNot(gomega.HaveOccurred())

			ginkgo.By("delete manifestwork")
			err = hub.WorkClient.WorkV1().ManifestWorks(universalClusterName).Delete(context.Background(), workName, metav1.DeleteOptions{})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())

			// pods should be all cleaned.
			gomega.Eventually(func() error {
				pods, err := spoke.KubeClient.CoreV1().Pods("default").List(context.Background(), metav1.ListOptions{})
				if err != nil {
					return err
				}

				if len(pods.Items) > 0 {
					return fmt.Errorf("there are %d pods left", len(pods.Items))
				}

				return nil
			}).ShouldNot(gomega.HaveOccurred())
		})
	})

	ginkgo.Context("With CRD/CR", func() {
		var crNamespace string

		ginkgo.BeforeEach(func() {
			crNamespace = fmt.Sprintf("ns3-%s", nameSuffix)

			// create namespace for cr
			ns := &corev1.Namespace{}
			ns.Name = crNamespace
			_, err = spoke.KubeClient.CoreV1().Namespaces().Create(context.Background(), ns, metav1.CreateOptions{})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())
		})

		ginkgo.AfterEach(func() {
			// delete namespace for cr
			err = spoke.KubeClient.CoreV1().Namespaces().Delete(context.Background(), crNamespace, metav1.DeleteOptions{})
			if err != nil {
				gomega.Expect(errors.IsNotFound(err)).To(gomega.BeTrue())
			}
		})

		ginkgo.It("should create crd/cr with aggregated cluster role successfully", func() {
			crd, err := newCrd(guestBookCRDJson)
			gomega.Expect(err).ToNot(gomega.HaveOccurred())

			clusterRoleName := fmt.Sprintf("cr-%s", nameSuffix)
			clusterRole := newAggregatedClusterRole(clusterRoleName, "my.domain", "guestbooks")

			cr, err := newCr(guestbookCRJson, crNamespace, "cr1")
			gomega.Expect(err).ToNot(gomega.HaveOccurred())

			objects := []runtime.Object{crd, clusterRole, cr}
			work := newManifestWork(universalClusterName, workName, objects...)
			_, err = hub.WorkClient.WorkV1().ManifestWorks(universalClusterName).Create(context.Background(), work, metav1.CreateOptions{})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())

			// check status conditions in manifestwork status
			gomega.Eventually(func() error {
				expectedManifestStatuses := []metav1.ConditionStatus{
					metav1.ConditionTrue, metav1.ConditionTrue, metav1.ConditionTrue}
				return assertManifestWorkAppliedSuccessfully(universalClusterName, workName, expectedManifestStatuses)
			}).ShouldNot(gomega.HaveOccurred())

			// Upgrade crd/cr and check if cr resource is recreated.
			// Get UID of cr resource at first.
			guestbook, err := spoke.DynamicClient.Resource(schema.GroupVersionResource{
				Resource: "guestbooks",
				Version:  "v1",
				Group:    "my.domain",
			}).Namespace(crNamespace).Get(context.Background(), "cr1", metav1.GetOptions{})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())
			currentUID := guestbook.GetUID()

			// Upgrade crd/cr in the work
			cr, err = newCr(upgradedGuestBookCRJson, crNamespace, "cr1")
			gomega.Expect(err).ToNot(gomega.HaveOccurred())
			objects = []runtime.Object{crd, clusterRole, cr}
			work = newManifestWork(universalClusterName, workName, objects...)

			// Update work
			existingWork, err := hub.WorkClient.WorkV1().ManifestWorks(universalClusterName).Get(context.Background(), workName, metav1.GetOptions{})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())
			work.ResourceVersion = existingWork.ResourceVersion
			_, err = hub.WorkClient.WorkV1().ManifestWorks(universalClusterName).Update(context.Background(), work, metav1.UpdateOptions{})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())

			// check if v2 cr is applied
			gomega.Eventually(func() error {
				guestbook, err := spoke.DynamicClient.Resource(schema.GroupVersionResource{
					Resource: "guestbooks",
					Version:  "v2",
					Group:    "my.domain",
				}).Namespace(crNamespace).Get(context.Background(), "cr1", metav1.GetOptions{})
				if err != nil {
					return err
				}
				// Check if the spec in the cr is updated
				if !reflect.DeepEqual(guestbook.Object["spec"], cr.Object["spec"]) {
					return fmt.Errorf("expect CR spec is updated, expected: %v, actual %v", cr.Object["spec"], guestbook.Object["spec"])
				}
				// Ensure that the CR is updated rather than recreated
				if currentUID != guestbook.GetUID() {
					return fmt.Errorf("expect UID to be the same, expected: %q, actual %q", currentUID, guestbook.GetUID())
				}
				return nil
			}).ShouldNot(gomega.HaveOccurred())
		})
	})

	ginkgo.Context("ManifestWork status feedback", func() {
		ginkgo.It("should return wellkown status of deployment", func() {
			deployment := newDeployment("busybox")
			objects := []runtime.Object{deployment}
			work := newManifestWork(universalClusterName, workName, objects...)
			work.Spec.ManifestConfigs = []workapiv1.ManifestConfigOption{
				{
					ResourceIdentifier: workapiv1.ResourceIdentifier{
						Group:     "apps",
						Resource:  "deployments",
						Name:      "busybox",
						Namespace: "default",
					},
					FeedbackRules: []workapiv1.FeedbackRule{
						{
							Type: workapiv1.WellKnownStatusType,
						},
						{
							Type: workapiv1.JSONPathsType,
							JsonPaths: []workapiv1.JsonPath{
								{
									Name: "AvailableCondition",
									Path: ".status.conditions[?(@.type==\"Available\")].status",
								},
							},
						},
					},
				},
			}
			_, err = hub.WorkClient.WorkV1().ManifestWorks(universalClusterName).Create(context.Background(), work, metav1.CreateOptions{})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())

			// Check deployment status
			gomega.Eventually(func() error {
				work, err := hub.WorkClient.WorkV1().ManifestWorks(universalClusterName).Get(context.Background(), workName, metav1.GetOptions{})
				if err != nil {
					return err
				}

				if len(work.Status.ResourceStatus.Manifests) != 1 {
					return fmt.Errorf("manifest count is not correct")
				}

				values := work.Status.ResourceStatus.Manifests[0].StatusFeedbacks.Values

				expectedValues := []workapiv1.FeedbackValue{
					{
						Name: "ReadyReplicas",
						Value: workapiv1.FieldValue{
							Type:    workapiv1.Integer,
							Integer: pointer.Int64(1),
						},
					},
					{
						Name: "Replicas",
						Value: workapiv1.FieldValue{
							Type:    workapiv1.Integer,
							Integer: pointer.Int64(1),
						},
					},
					{
						Name: "AvailableReplicas",
						Value: workapiv1.FieldValue{
							Type:    workapiv1.Integer,
							Integer: pointer.Int64(1),
						},
					},
					{
						Name: "AvailableCondition",
						Value: workapiv1.FieldValue{
							Type:   workapiv1.String,
							String: pointer.String("True"),
						},
					},
				}
				if !apiequality.Semantic.DeepEqual(values, expectedValues) {
					return fmt.Errorf("status feedback values are not correct, we got %v", values)
				}

				if ok := haveManifestCondition(work.Status.ResourceStatus.Manifests, "StatusFeedbackSynced", []metav1.ConditionStatus{metav1.ConditionTrue}); !ok {
					return fmt.Errorf("statusFeedbackSynced condition should be True")
				}
				return nil
			}).WithTimeout(2 * time.Minute).ShouldNot(gomega.HaveOccurred())
		})
	})

	ginkgo.Context("Multiple owner references deletion", func() {
		var nsName, cmName string
		ctx := context.Background()

		ginkgo.BeforeEach(func() {
			nsName = fmt.Sprintf("ns1-%s", rand.String(5))
			cmName = "cm1"
			ns := &corev1.Namespace{}
			ns.Name = nsName
			_, err := spoke.KubeClient.CoreV1().Namespaces().Create(ctx, ns, metav1.CreateOptions{})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())

			objects := []runtime.Object{
				util.NewConfigmap(nsName, cmName, nil, nil),
			}
			work := newManifestWork(universalClusterName, workName, objects...)
			_, err = hub.WorkClient.WorkV1().ManifestWorks(universalClusterName).Create(
				context.Background(), work, metav1.CreateOptions{})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())
		})

		ginkgo.AfterEach(func() {
			err := spoke.KubeClient.CoreV1().Namespaces().Delete(ctx, nsName, metav1.DeleteOptions{})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())

		})

		ginkgo.It("Should keep the resource when there are other appliedManifestWork owners", func() {
			work2Name := fmt.Sprintf("w2-%s", nameSuffix)
			objects := []runtime.Object{
				util.NewConfigmap(nsName, cmName, nil, nil),
			}
			work2 := newManifestWork(universalClusterName, work2Name, objects...)
			_, err := hub.WorkClient.WorkV1().ManifestWorks(universalClusterName).Create(ctx, work2, metav1.CreateOptions{})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())

			for _, name := range []string{workName, work2Name} {
				// check status conditions in manifestwork status
				gomega.Eventually(func() error {
					work, err := hub.WorkClient.WorkV1().ManifestWorks(universalClusterName).Get(
						ctx, name, metav1.GetOptions{})
					if err != nil {
						return err
					}

					// check manifest status conditions
					expectedManifestStatuses := []metav1.ConditionStatus{metav1.ConditionTrue}
					return assertManifestWorkAppliedSuccessfully(universalClusterName, work.Name, expectedManifestStatuses)
				}).ShouldNot(gomega.HaveOccurred())
			}

			cmUID := types.UID("test")
			// check if resources are applied for manifests
			gomega.Eventually(func() error {
				cm, err := spoke.KubeClient.CoreV1().ConfigMaps(nsName).Get(ctx, cmName, metav1.GetOptions{})
				if err != nil {
					return err
				}

				cmUID = cm.UID
				if len(cm.OwnerReferences) != 2 {
					return fmt.Errorf("expected 2 owners, but got %d", len(cm.OwnerReferences))
				}
				return nil
			}).ShouldNot(gomega.HaveOccurred())

			ginkgo.By("delete manifestwork mw1")
			err = hub.WorkClient.WorkV1().ManifestWorks(universalClusterName).Delete(ctx, workName, metav1.DeleteOptions{})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())

			// wait for deletion of manifest work
			gomega.Eventually(func() bool {
				_, err := hub.WorkClient.WorkV1().ManifestWorks(universalClusterName).Get(ctx, workName, metav1.GetOptions{})
				return errors.IsNotFound(err)
			}).Should(gomega.BeTrue())

			cm, err := spoke.KubeClient.CoreV1().ConfigMaps(nsName).Get(ctx, cmName, metav1.GetOptions{})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())
			gomega.Expect(cm.UID).To(gomega.Equal(cmUID))

			ginkgo.By("delete manifestwork mw2")
			err = hub.WorkClient.WorkV1().ManifestWorks(universalClusterName).Delete(ctx, work2Name, metav1.DeleteOptions{})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())

			// wait for deletion of manifest work
			gomega.Eventually(func() bool {
				_, err := hub.WorkClient.WorkV1().ManifestWorks(universalClusterName).Get(ctx, work2Name, metav1.GetOptions{})
				return errors.IsNotFound(err)
			}).Should(gomega.BeTrue())

			_, err = spoke.KubeClient.CoreV1().ConfigMaps(nsName).Get(ctx, cmName, metav1.GetOptions{})
			gomega.Expect(errors.IsNotFound(err)).To(gomega.BeTrue())
		})

		ginkgo.It("Should delete the resource even if there are other non-appliedManifestWork owners", func() {
			// check status conditions in manifestwork status
			gomega.Eventually(func() error { // check manifest status conditions
				expectedManifestStatuses := []metav1.ConditionStatus{metav1.ConditionTrue}
				return assertManifestWorkAppliedSuccessfully(universalClusterName, workName, expectedManifestStatuses)
			}).ShouldNot(gomega.HaveOccurred())

			ginkgo.By("check if resources are applied for manifests")
			_, err := spoke.KubeClient.CoreV1().ConfigMaps(nsName).Get(ctx, cmName, metav1.GetOptions{})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())

			ginkgo.By("Add a non-appliedManifestWork owner to the applied resource")
			cmOwner, err := spoke.KubeClient.CoreV1().ConfigMaps(nsName).Create(ctx,
				util.NewConfigmap(nsName, "owner", nil, nil), metav1.CreateOptions{})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())

			gomega.Eventually(func() error {
				cm, err := spoke.KubeClient.CoreV1().ConfigMaps(nsName).Get(ctx, cmName, metav1.GetOptions{})
				if err != nil {
					return err
				}
				cm.OwnerReferences = append(cm.OwnerReferences, metav1.OwnerReference{
					Name:       cmOwner.Name,
					UID:        cmOwner.UID,
					Kind:       "ConfigMap",
					APIVersion: "v1",
				})

				_, err = spoke.KubeClient.CoreV1().ConfigMaps(nsName).Update(ctx, cm, metav1.UpdateOptions{})
				return err
			}).ShouldNot(gomega.HaveOccurred())

			cm, err := spoke.KubeClient.CoreV1().ConfigMaps(nsName).Get(ctx, cmName, metav1.GetOptions{})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())
			gomega.Expect(len(cm.OwnerReferences) == 2).To(gomega.BeTrue())

			ginkgo.By("delete manifestwork mw1")
			err = hub.WorkClient.WorkV1().ManifestWorks(universalClusterName).Delete(ctx, workName, metav1.DeleteOptions{})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())

			ginkgo.By("wait for deletion of manifest work")
			gomega.Eventually(func() bool {
				_, err := hub.WorkClient.WorkV1().ManifestWorks(universalClusterName).Get(ctx, workName, metav1.GetOptions{})
				return errors.IsNotFound(err)
			}).Should(gomega.BeTrue())

			ginkgo.By("check the resource cm was deleted successfully")
			gomega.Eventually(func() bool {
				_, err := spoke.KubeClient.CoreV1().ConfigMaps(nsName).Get(ctx, cmName, metav1.GetOptions{})
				return errors.IsNotFound(err)
			}).Should(gomega.BeTrue())

			err = spoke.KubeClient.CoreV1().ConfigMaps(nsName).Delete(ctx, cmOwner.Name, metav1.DeleteOptions{})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())
		})
	})

	ginkgo.Context("CleanUpCompletedManifestWork feature", func() {
		ginkgo.It("Should cleanup manifestwork with TTL after job completion", func() {
			ginkgo.By("Create manifestwork with job that sleeps 5 seconds")
			jobName := fmt.Sprintf("sleep-job-%s", nameSuffix)
			sleepJob := newSleepJob(jobName, 10)
			objects := []runtime.Object{
				sleepJob,
			}
			work := newManifestWork(universalClusterName, workName, objects...)

			// Set TTL for cleanup after completion
			ttlSeconds := int64(3)
			work.Spec.DeleteOption = &workapiv1.DeleteOption{
				PropagationPolicy:       workapiv1.DeletePropagationPolicyTypeForeground,
				TTLSecondsAfterFinished: &ttlSeconds,
			}

			// Configure CEL condition for completion when job succeeds
			work.Spec.ManifestConfigs = []workapiv1.ManifestConfigOption{
				{
					ResourceIdentifier: workapiv1.ResourceIdentifier{
						Group:     "batch",
						Resource:  "jobs",
						Name:      jobName,
						Namespace: "default",
					},
					ConditionRules: []workapiv1.ConditionRule{
						{
							Condition: workapiv1.WorkComplete,
							Type:      workapiv1.CelConditionExpressionsType,
							CelExpressions: []string{
								"object.status.conditions.exists(c, c.type == 'Complete' && c.status == 'True')",
							},
							Message: "Job completed successfully",
						},
					},
				},
			}

			work, err = hub.WorkClient.WorkV1().ManifestWorks(universalClusterName).Create(context.Background(), work, metav1.CreateOptions{})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())

			ginkgo.By("Wait for job to be applied and running")
			gomega.Eventually(func() error {
				actualWork, err := hub.WorkClient.WorkV1().ManifestWorks(universalClusterName).Get(context.Background(), workName, metav1.GetOptions{})
				if err != nil {
					return err
				}
				if !meta.IsStatusConditionTrue(actualWork.Status.Conditions, workapiv1.WorkApplied) {
					return fmt.Errorf("work not applied yet")
				}
				if !meta.IsStatusConditionTrue(actualWork.Status.Conditions, workapiv1.WorkAvailable) {
					return fmt.Errorf("work not available yet")
				}
				return nil
			}).Should(gomega.Succeed())

			// Verify job pod was created
			gomega.Eventually(func() error {
				pods, err := spoke.KubeClient.CoreV1().Pods("default").List(context.Background(), metav1.ListOptions{
					LabelSelector: fmt.Sprintf("job=%s", jobName),
				})
				if err != nil {
					return err
				}
				if len(pods.Items) == 0 {
					return fmt.Errorf("job pod not found")
				}
				return nil
			}).Should(gomega.Succeed())

			ginkgo.By("Wait for job to complete and manifestwork to be marked complete")
			gomega.Eventually(func() error {
				actualWork, err := hub.WorkClient.WorkV1().ManifestWorks(universalClusterName).Get(context.Background(), workName, metav1.GetOptions{})
				if err != nil {
					return err
				}
				if !meta.IsStatusConditionTrue(actualWork.Status.Conditions, workapiv1.WorkComplete) {
					return fmt.Errorf("work not complete yet")
				}
				return nil
			}, 90*time.Second, 1*time.Second).Should(gomega.Succeed())

			ginkgo.By("Wait for manifestwork to be deleted after TTL")
			gomega.Eventually(func() error {
				currentWork, err := hub.WorkClient.WorkV1().ManifestWorks(universalClusterName).Get(context.Background(), workName, metav1.GetOptions{})
				if errors.IsNotFound(err) {
					return nil
				}
				if err != nil {
					return err
				}
				return fmt.Errorf("manifestwork still exists %v, %v", currentWork.DeletionTimestamp, currentWork.Status.Conditions)
			}, 30*time.Second, 2*time.Second).Should(gomega.Succeed())

			ginkgo.By("Verify job resources are cleaned up")
			gomega.Eventually(func() bool {
				_, err := spoke.KubeClient.BatchV1().Jobs("default").Get(context.Background(), jobName, metav1.GetOptions{})
				return errors.IsNotFound(err)
			}).Should(gomega.BeTrue())
		})
	})

	ginkgo.Context("ManifestWork server side apply", func() {
		ginkgo.It("should ignore fields with certain field", func() {
			deployment := newDeployment("busybox-ssa")
			deployment.Spec.Replicas = pointer.Int32(2)
			objects := []runtime.Object{deployment}
			work := newManifestWork(universalClusterName, workName, objects...)
			work.Spec.ManifestConfigs = []workapiv1.ManifestConfigOption{
				{
					ResourceIdentifier: workapiv1.ResourceIdentifier{
						Group:     "apps",
						Resource:  "deployments",
						Name:      "busybox-ssa",
						Namespace: "default",
					},
					UpdateStrategy: &workapiv1.UpdateStrategy{
						Type: workapiv1.UpdateStrategyTypeServerSideApply,
						ServerSideApply: &workapiv1.ServerSideApplyConfig{
							IgnoreFields: []workapiv1.IgnoreField{
								{
									Condition: workapiv1.IgnoreFieldsConditionOnSpokeChange,
									JSONPaths: []string{".spec.replicas"},
								},
							},
						},
					},
				},
			}
			_, err = hub.WorkClient.WorkV1().ManifestWorks(universalClusterName).Create(context.Background(), work, metav1.CreateOptions{})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())

			// Check deployment status
			gomega.Eventually(func() error {
				deploy, err := spoke.KubeClient.AppsV1().Deployments(deployment.Namespace).Get(context.Background(), deployment.Name, metav1.GetOptions{})
				if err != nil {
					return err
				}
				if *deploy.Spec.Replicas != int32(2) {
					return fmt.Errorf("expected 2 replicas, got %d", *deploy.Spec.Replicas)
				}
				if len(deploy.OwnerReferences) == 0 {
					return fmt.Errorf("expected owner references, got none")
				}
				return nil
			}).ShouldNot(gomega.HaveOccurred())

			ginkgo.By("update replicas in the manifestwork")
			gomega.Eventually(func() error {
				actualWork, err := hub.WorkClient.WorkV1().ManifestWorks(universalClusterName).Get(context.Background(), workName, metav1.GetOptions{})
				if err != nil {
					return err
				}
				deployment.Spec.Replicas = pointer.Int32(3)
				objects := []runtime.Object{deployment}
				newWork := newManifestWork(universalClusterName, workName, objects...)
				actualWork.Spec.Workload = newWork.Spec.Workload
				_, err = hub.WorkClient.WorkV1().ManifestWorks(universalClusterName).Update(context.Background(), actualWork, metav1.UpdateOptions{})
				return err
			}).ShouldNot(gomega.HaveOccurred())

			// Check deployment status
			gomega.Eventually(func() error {
				deploy, err := spoke.KubeClient.AppsV1().Deployments(deployment.Namespace).Get(context.Background(), deployment.Name, metav1.GetOptions{})
				if err != nil {
					return err
				}
				if *deploy.Spec.Replicas != int32(3) {
					return fmt.Errorf("expected 3 replicas, got %d", *deploy.Spec.Replicas)
				}
				return nil
			}).ShouldNot(gomega.HaveOccurred())

			ginkgo.By("update deployment with new replicas")
			specConfig := applyappsv1.DeploymentSpec().WithReplicas(5)
			applyConfig := applyappsv1.Deployment(deployment.Name, deployment.Namespace).WithSpec(specConfig)
			_, err = spoke.KubeClient.AppsV1().Deployments(deployment.Namespace).Apply(context.Background(), applyConfig, metav1.ApplyOptions{
				FieldManager: "another-actor",
				Force:        true,
			})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())

			ginkgo.By("update work with new replicas and type to onSpokePresent")
			gomega.Eventually(func() error {
				actualWork, err := hub.WorkClient.WorkV1().ManifestWorks(universalClusterName).Get(context.Background(), workName, metav1.GetOptions{})
				if err != nil {
					return err
				}
				deployment.Spec.Replicas = pointer.Int32(4)
				objects := []runtime.Object{deployment}
				newWork := newManifestWork(universalClusterName, workName, objects...)
				actualWork.Spec.Workload = newWork.Spec.Workload
				actualWork.Spec.ManifestConfigs[0].UpdateStrategy.ServerSideApply.IgnoreFields[0].Condition = workapiv1.IgnoreFieldsConditionOnSpokePresent
				_, err = hub.WorkClient.WorkV1().ManifestWorks(universalClusterName).Update(context.Background(), actualWork, metav1.UpdateOptions{})
				return err
			}).ShouldNot(gomega.HaveOccurred())

			// wait until the work has been applied
			gomega.Eventually(func() error {
				actualWork, err := hub.WorkClient.WorkV1().ManifestWorks(universalClusterName).Get(context.Background(), workName, metav1.GetOptions{})
				if err != nil {
					return err
				}
				appliedCond := meta.FindStatusCondition(actualWork.Status.Conditions, workapiv1.WorkApplied)
				if appliedCond == nil {
					return fmt.Errorf("expected a work applied condition")
				}
				if appliedCond.ObservedGeneration != actualWork.Generation {
					return fmt.Errorf("expect work is applied.")
				}
				return nil
			}).ShouldNot(gomega.HaveOccurred())

			// Check deployment status, it should not be changed
			gomega.Eventually(func() error {
				deploy, err := spoke.KubeClient.AppsV1().Deployments(deployment.Namespace).Get(context.Background(), deployment.Name, metav1.GetOptions{})
				if err != nil {
					return err
				}
				if *deploy.Spec.Replicas != int32(5) {
					return fmt.Errorf("expected 5 replicas, got %d", *deploy.Spec.Replicas)
				}
				return nil
			}).ShouldNot(gomega.HaveOccurred())

			err = hub.WorkClient.WorkV1().ManifestWorks(universalClusterName).Delete(
				context.Background(), work.Name, metav1.DeleteOptions{})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())

			// Check deployment is removed
			gomega.Eventually(func() error {
				_, err := spoke.KubeClient.AppsV1().Deployments(deployment.Namespace).Get(
					context.Background(), deployment.Name, metav1.GetOptions{})
				if errors.IsNotFound(err) {
					return nil
				}
				if err != nil {
					return err
				}
				return fmt.Errorf("resource still exists")
			}).ShouldNot(gomega.HaveOccurred())
		})
	})
})

func assertManifestWorkAppliedSuccessfully(workNamespace, workName string,
	expectedManifestStatuses []metav1.ConditionStatus) error {
	work, err := hub.WorkClient.WorkV1().ManifestWorks(workNamespace).Get(
		context.Background(), workName, metav1.GetOptions{})
	if err != nil {
		return err
	}

	// check manifest status conditions
	if ok := haveManifestCondition(work.Status.ResourceStatus.Manifests,
		workapiv1.ManifestApplied, expectedManifestStatuses); !ok {
		return fmt.Errorf("%s not equal", workapiv1.ManifestApplied)
	}
	if ok := haveManifestCondition(work.Status.ResourceStatus.Manifests,
		workapiv1.ManifestAvailable, expectedManifestStatuses); !ok {
		return fmt.Errorf("%s not equal", workapiv1.ManifestAvailable)
	}

	// check work status condition
	if !meta.IsStatusConditionTrue(work.Status.Conditions, workapiv1.WorkApplied) {
		return fmt.Errorf("work %s condition not true", workapiv1.WorkApplied)
	}
	if !meta.IsStatusConditionTrue(work.Status.Conditions, workapiv1.WorkAvailable) {
		return fmt.Errorf("work %s condition not true", workapiv1.WorkAvailable)
	}
	return nil
}

func newManifestWork(namespace, name string, objects ...runtime.Object) *workapiv1.ManifestWork {
	work := &workapiv1.ManifestWork{}

	work.Namespace = namespace
	if name != "" {
		work.Name = name
	} else {
		work.GenerateName = "work-"
	}

	work.Labels = map[string]string{
		"created-for-testing": "true",
	}

	var manifests []workapiv1.Manifest
	for _, object := range objects {
		manifest := workapiv1.Manifest{}
		manifest.Object = object
		manifests = append(manifests, manifest)
	}
	work.Spec.Workload.Manifests = manifests

	return work
}

func newNamespace(name string) *corev1.Namespace {
	return &corev1.Namespace{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Namespace",
			APIVersion: "v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
	}
}

func newJob(name string) *batchv1.Job {
	manualSelector := true
	job := &batchv1.Job{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Job",
			APIVersion: "batch/v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "default",
			Name:      name,
		},
		Spec: batchv1.JobSpec{
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{"job": name},
			},
			ManualSelector: &manualSelector,
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{"job": name},
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:    "pi",
							Image:   "perl",
							Command: []string{"perl", "-Mbignum=bpi", "-wle", "print bpi(2000)"},
						},
					},
					RestartPolicy: corev1.RestartPolicyNever,
				},
			},
		},
	}

	return job
}

func newSleepJob(name string, sleepSeconds int) *batchv1.Job {
	maunualSelector := true
	job := &batchv1.Job{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Job",
			APIVersion: "batch/v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "default",
			Name:      name,
		},
		Spec: batchv1.JobSpec{
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{"job": name},
			},
			ManualSelector: &maunualSelector,
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{"job": name},
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:    "sleeper",
							Image:   "quay.io/asmacdo/busybox",
							Command: []string{"/bin/sh"},
							Args:    []string{"-c", fmt.Sprintf("echo 'Starting sleep job'; sleep %d; echo 'Sleep job completed'", sleepSeconds)},
						},
					},
					RestartPolicy: corev1.RestartPolicyNever,
				},
			},
		},
	}

	return job
}

func newDeployment(name string) *appsv1.Deployment {
	replica := int32(1)
	deployment := &appsv1.Deployment{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Deployment",
			APIVersion: "apps/v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "default",
			Name:      name,
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: &replica,
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{
					"app": "busybox",
				},
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						"app": "busybox",
					},
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:  "busybox",
							Image: "quay.io/asmacdo/busybox",
							Ports: []corev1.ContainerPort{
								{
									ContainerPort: 80,
								},
							},
							Command: []string{"sh", "-c", "sleep 3600"},
						},
					},
				},
			},
		},
	}

	return deployment
}

func newSecretBySize(namespace, name string, size int32) *corev1.Secret {
	data := ""
	for i := int32(0); i < size; i++ {
		data += "a"
	}

	return &corev1.Secret{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Secret",
			APIVersion: "v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Data: map[string][]byte{
			"test": []byte(data),
		},
	}
}

func haveManifestCondition(conditions []workapiv1.ManifestCondition, expectedType string, expectedStatuses []metav1.ConditionStatus) bool {
	if len(conditions) != len(expectedStatuses) {
		return false
	}

	for index, condition := range conditions {
		expectedStatus := expectedStatuses[index]
		if expectedStatus == "" {
			continue
		}

		if ok := meta.IsStatusConditionPresentAndEqual(condition.Conditions, expectedType, expectedStatus); !ok {
			return false
		}
	}

	return true
}

func newCrd(content string) (crd *unstructured.Unstructured, err error) {
	crd, err = loadResourceFromJSON(content)
	return crd, err
}

func newCr(content, namespace, name string) (cr *unstructured.Unstructured, err error) {
	cr, err = loadResourceFromJSON(content)
	if err != nil {
		return nil, err
	}

	cr.SetNamespace(namespace)
	cr.SetName(name)
	return cr, nil
}

func loadResourceFromJSON(json string) (*unstructured.Unstructured, error) {
	obj := unstructured.Unstructured{}
	err := obj.UnmarshalJSON([]byte(json))
	return &obj, err
}

func newAggregatedClusterRole(name, apiGroup, resource string) *rbacv1.ClusterRole {
	return &rbacv1.ClusterRole{
		TypeMeta: metav1.TypeMeta{
			Kind:       "ClusterRole",
			APIVersion: "rbac.authorization.k8s.io/v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
			Labels: map[string]string{
				"open-cluster-management.io/aggregate-to-work": "true",
			},
		},
		Rules: []rbacv1.PolicyRule{
			{
				APIGroups: []string{apiGroup},
				Resources: []string{resource},
				Verbs:     []string{"get", "list", "watch", "create", "update", "patch", "delete"},
			},
		},
	}
}
