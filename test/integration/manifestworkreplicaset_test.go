package integration

import (
	"context"
	"github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	utilrand "k8s.io/apimachinery/pkg/util/rand"
	workapiv1 "open-cluster-management.io/api/work/v1"
	workapiv1alpha1 "open-cluster-management.io/api/work/v1alpha1"
	"open-cluster-management.io/work/test/integration/util"
)

var _ = ginkgo.Describe("ManifestWorkReplicaSet", func() {
	var namespaceName string

	ginkgo.BeforeEach(func() {
		namespaceName = utilrand.String(5)
		ns := &corev1.Namespace{}
		ns.Name = namespaceName
		_, err := spokeKubeClient.CoreV1().Namespaces().Create(context.Background(), ns, metav1.CreateOptions{})
		gomega.Expect(err).ToNot(gomega.HaveOccurred())
	})

	ginkgo.AfterEach(func() {
		err := spokeKubeClient.CoreV1().Namespaces().Delete(context.Background(), namespaceName, metav1.DeleteOptions{})
		gomega.Expect(err).ToNot(gomega.HaveOccurred())
	})

	// A sanity check ensuring crd is created correctly which should be refactored later
	ginkgo.Context("Create a manifestWorkReplicaSet", func() {
		ginkgo.It("should create successfully", func() {
			manifests := []workapiv1.Manifest{
				util.ToManifest(util.NewConfigmap("defaut", "cm1", map[string]string{"a": "b"}, nil)),
			}
			placementRef := workapiv1alpha1.LocalPlacementReference{Name: "placement-test"}

			manifestWorkReplicaSet := &workapiv1alpha1.ManifestWorkReplicaSet{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-work",
					Namespace: namespaceName,
				},
				Spec: workapiv1alpha1.ManifestWorkReplicaSetSpec{
					ManifestWorkTemplate: workapiv1.ManifestWorkSpec{
						Workload: workapiv1.ManifestsTemplate{
							Manifests: manifests,
						},
					},
					PlacementRefs: []workapiv1alpha1.LocalPlacementReference{placementRef},
				},
			}

			_, err := hubWorkClient.WorkV1alpha1().ManifestWorkReplicaSets(namespaceName).Create(context.TODO(), manifestWorkReplicaSet, metav1.CreateOptions{})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())
		})
	})
})
