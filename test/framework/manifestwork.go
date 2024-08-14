package framework

import (
	"context"

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
