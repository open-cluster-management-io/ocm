package helpers

import (
	"context"
	"fmt"
	"testing"

	fakekube "k8s.io/client-go/kubernetes/fake"
)

// TODO: enhance the u-t in the future @xuezhaojun
func TestCreateSelfSubjectAccessReviews(t *testing.T) {
	kubeClient := fakekube.NewSimpleClientset()
	ctx := context.TODO()

	// Create sample selfSubjectAccessReviews
	bootstrapSSARs := GetBootstrapSSARs()
	hubConfigSSARs := GetHubConfigSSARs("test-cluster")

	// Call the function under test
	_, _, err := CreateSelfSubjectAccessReviews(ctx, kubeClient, bootstrapSSARs)
	if err != nil {
		fmt.Println(err)
	}

	_, _, err = CreateSelfSubjectAccessReviews(ctx, kubeClient, hubConfigSSARs)
	if err != nil {
		fmt.Println(err)
	}
}
