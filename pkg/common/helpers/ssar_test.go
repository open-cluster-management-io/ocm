package helpers

import (
	"context"
	"testing"

	fakekube "k8s.io/client-go/kubernetes/fake"
)

// TODO: enhance the u-t in the future @xuezhaojun
func TestCreateBootStrapSelfSubjectAccessReviews(t *testing.T) {
	kubeClient := fakekube.NewSimpleClientset()
	ctx := context.TODO()

	// Create sample selfSubjectAccessReviews
	bootstrapSSARs := GetBootstrapSSARs()

	// Call the function under test
	_, _, err := CreateSelfSubjectAccessReviews(ctx, kubeClient, bootstrapSSARs)
	if err != nil {
		t.Error(err)
	}
}

func TestCreateHubConfigpSelfSubjectAccessReviews(t *testing.T) {
	kubeClient := fakekube.NewSimpleClientset()
	ctx := context.TODO()

	// Create sample selfSubjectAccessReviews
	hubConfigSSARs := GetHubConfigSSARs("test-cluster")

	_, _, err := CreateSelfSubjectAccessReviews(ctx, kubeClient, hubConfigSSARs)
	if err != nil {
		t.Error(err)
	}
}
