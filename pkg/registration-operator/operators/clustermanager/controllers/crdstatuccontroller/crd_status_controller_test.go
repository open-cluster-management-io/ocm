/*
 * Copyright 2022 Contributors to the Open Cluster Management project
 */

package crdstatuccontroller

import (
	"context"
	"testing"
	"time"

	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	apiextensionsclient "k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset"
	fakeapiextensions "k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset/fake"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/rest"
	fakeoperatorlient "open-cluster-management.io/api/client/operator/clientset/versioned/fake"
	operatorinformers "open-cluster-management.io/api/client/operator/informers/externalversions"
	operatorapiv1 "open-cluster-management.io/api/operator/v1"
	testinghelper "open-cluster-management.io/ocm/pkg/registration-operator/helpers/testing"
	"open-cluster-management.io/ocm/pkg/registration-operator/operators/clustermanager/controllers/migrationcontroller"
)

func TestSync(t *testing.T) {
	clusterManager := newClusterManager("testhub")
	tc := newTestController(t, clusterManager)

	syncContext := testinghelper.NewFakeSyncContext(t, "testhub")
	//Do not support migration
	err := tc.sync(context.Background(), syncContext)
	if err != nil {
		t.Fatalf("Expected no error when sync, %v", err)
	}

	// migration succeed
	clusterManager.Status.Conditions = []metav1.Condition{
		{
			Type:   migrationcontroller.MigrationSucceeded,
			Status: metav1.ConditionTrue,
		},
	}
	crds := newCrds()
	tc = newTestController(t, clusterManager, crds...)
	err = tc.sync(context.Background(), syncContext)
	if err != nil {
		t.Fatalf("Expected no error when sync, %v", err)
	}

}

func newTestController(t *testing.T, clustermanager *operatorapiv1.ClusterManager, crds ...runtime.Object) *crdStatusController {
	fakeOperatorClient := fakeoperatorlient.NewSimpleClientset(clustermanager)
	operatorInformers := operatorinformers.NewSharedInformerFactory(fakeOperatorClient, 5*time.Minute)
	fakeAPIExtensionClient := fakeapiextensions.NewSimpleClientset(crds...)

	crdStatusController := &crdStatusController{
		clusterManagerLister: operatorInformers.Operator().V1().ClusterManagers().Lister(),
	}
	crdStatusController.generateHubClusterClients = func(hubKubeConfig *rest.Config) (apiextensionsclient.Interface, error) {
		return fakeAPIExtensionClient, nil
	}
	store := operatorInformers.Operator().V1().ClusterManagers().Informer().GetStore()
	if err := store.Add(clustermanager); err != nil {
		t.Fatal(err)
	}

	return crdStatusController
}

func newClusterManager(name string) *operatorapiv1.ClusterManager {
	return &operatorapiv1.ClusterManager{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
		Spec: operatorapiv1.ClusterManagerSpec{
			RegistrationImagePullSpec: "testregistration",
			DeployOption: operatorapiv1.ClusterManagerDeployOption{
				Mode: operatorapiv1.InstallModeDefault,
			},
		},
	}
}

func newCrds() []runtime.Object {
	return []runtime.Object{
		&apiextensionsv1.CustomResourceDefinition{
			ObjectMeta: metav1.ObjectMeta{
				Name: "managedclustersets.cluster.open-cluster-management.io",
			},
			Status: apiextensionsv1.CustomResourceDefinitionStatus{
				StoredVersions: []string{
					"v1beta1",
					"v1beta2",
				},
			},
		},
		&apiextensionsv1.CustomResourceDefinition{
			ObjectMeta: metav1.ObjectMeta{
				Name: "managedclustersetbindings.cluster.open-cluster-management.io",
			},
			Status: apiextensionsv1.CustomResourceDefinitionStatus{
				StoredVersions: []string{
					"v1beta1",
				},
			},
		},
	}
}
