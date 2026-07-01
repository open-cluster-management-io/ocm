package framework

import (
	"context"
	"fmt"

	. "github.com/onsi/gomega"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func (hub *Hub) CleanManifestWorks(clusterName, workName string) error {
	err := hub.WorkClient.WorkV1().ManifestWorks(clusterName).Delete(context.Background(), workName, metav1.DeleteOptions{})
	if apierrors.IsNotFound(err) {
		return nil
	}
	if err != nil {
		return err
	}

	Eventually(func() bool {
		_, err := hub.WorkClient.WorkV1().ManifestWorks(clusterName).Get(context.Background(), workName, metav1.GetOptions{})
		return apierrors.IsNotFound(err)
	}).Should(BeTrue())

	return nil
}

// WaitUntilNoManifestWorks waits until all ManifestWorks are removed from the managed cluster namespace.
func (hub *Hub) WaitUntilNoManifestWorks(clusterName string) {
	Eventually(func() error {
		works, err := hub.WorkClient.WorkV1().ManifestWorks(clusterName).List(context.Background(), metav1.ListOptions{})
		if err != nil {
			if apierrors.IsNotFound(err) {
				return nil
			}
			return err
		}
		if len(works.Items) != 0 {
			return fmt.Errorf("expected no manifest works, but got %d", len(works.Items))
		}
		return nil
	}).Should(Succeed())
}
