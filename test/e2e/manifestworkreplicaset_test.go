package e2e

import (
	"context"
	"github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	workapiv1alpha1 "open-cluster-management.io/api/work/v1alpha1"
)

var _ = ginkgo.Describe("ManifestWorkReplicaSet", func() {
	var err error
	ginkgo.Context("Creating a ManifestWorkReplicaSet", func() {
		ginkgo.It("Should create ManifestWorkReplicaSet successfullt", func() {
			work := newManifestWork("", "", []runtime.Object{newConfigmap("default", "cm1", nil, nil)}...)
			placementRef := workapiv1alpha1.LocalPlacementReference{Name: "placement-test"}
			manifestWorkReplicaSet := &workapiv1alpha1.ManifestWorkReplicaSet{
				ObjectMeta: metav1.ObjectMeta{
					GenerateName: "mwrset-",
					Namespace:    "default",
				},
				Spec: workapiv1alpha1.ManifestWorkReplicaSetSpec{
					ManifestWorkTemplate: work.Spec,
					PlacementRefs:        []workapiv1alpha1.LocalPlacementReference{placementRef},
				},
			}
			manifestWorkReplicaSet, err = hubWorkClient.WorkV1alpha1().ManifestWorkReplicaSets("default").Create(context.TODO(), manifestWorkReplicaSet, metav1.CreateOptions{})
			gomega.Expect(err).NotTo(gomega.HaveOccurred())

			err = hubWorkClient.WorkV1alpha1().ManifestWorkReplicaSets("default").Delete(context.TODO(), manifestWorkReplicaSet.Name, metav1.DeleteOptions{})
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
		})
	})
})
