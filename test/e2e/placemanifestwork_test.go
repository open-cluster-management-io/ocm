package e2e

import (
	"context"
	"github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	workapiv1alpha1 "open-cluster-management.io/api/work/v1alpha1"
)

var _ = ginkgo.Describe("PlaceManifestWork", func() {
	var err error
	ginkgo.Context("Creating a PlaceManifestWork", func() {
		ginkgo.It("Should create PlaceManifestWork successfullt", func() {
			work := newManifestWork("", "", []runtime.Object{newConfigmap("default", "cm1", nil, nil)}...)
			placeManifestWork := &workapiv1alpha1.PlaceManifestWork{
				ObjectMeta: metav1.ObjectMeta{
					GenerateName: "placework-",
					Namespace:    "default",
				},
				Spec: workapiv1alpha1.PlaceManifestWorkSpec{
					ManifestWorkTemplate: work.Spec,
				},
			}
			placeManifestWork, err = hubWorkClient.WorkV1alpha1().PlaceManifestWorks("default").Create(context.TODO(), placeManifestWork, metav1.CreateOptions{})
			gomega.Expect(err).NotTo(gomega.HaveOccurred())

			err = hubWorkClient.WorkV1alpha1().PlaceManifestWorks("default").Delete(context.TODO(), placeManifestWork.Name, metav1.DeleteOptions{})
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
		})
	})
})
