package migrationcontroller

import (
	"context"
	"testing"

	"github.com/openshift/library-go/pkg/operator/events/eventstesting"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	fakeapiextensions "k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset/fake"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	clienttesting "k8s.io/client-go/testing"
	migrationv1alpha1 "sigs.k8s.io/kube-storage-version-migrator/pkg/apis/migration/v1alpha1"
	fakemigrationclient "sigs.k8s.io/kube-storage-version-migrator/pkg/clients/clientset/fake"
)

func TestSupportStorageVersionMigration(t *testing.T) {
	cases := []struct {
		name            string
		existingObjects []runtime.Object
		supported       bool
	}{
		{
			name:      "not support",
			supported: false,
		},
		{
			name: "support",
			existingObjects: []runtime.Object{
				&apiextensionsv1.CustomResourceDefinition{
					ObjectMeta: metav1.ObjectMeta{
						Name: migrationRequestCRDName,
					},
				},
			},
			supported: true,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			fakeAPIExtensionClient := fakeapiextensions.NewSimpleClientset(c.existingObjects...)
			actual, err := supportStorageVersionMigration(context.TODO(), fakeAPIExtensionClient)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if actual != c.supported {
				t.Fatalf("expected %v but got %v", c.supported, actual)
			}
		})
	}
}

func TestApplyStorageVersionMigrations(t *testing.T) {
	cases := []struct {
		name            string
		existingObjects []runtime.Object
		validateActions func(t *testing.T, actions []clienttesting.Action)
	}{
		{
			name: "created",
			validateActions: func(t *testing.T, actions []clienttesting.Action) {
				assertActions(t, actions, "get", "create", "get", "create")
				actual := actions[1].(clienttesting.CreateActionImpl).Object
				assertStorageVersionMigration(t, "managedclustersets.cluster.open-cluster-management.io", actual)
				actual = actions[3].(clienttesting.CreateActionImpl).Object
				assertStorageVersionMigration(t, "managedclustersetbindings.cluster.open-cluster-management.io", actual)
			},
		},
		{
			name: "created and updated",
			existingObjects: []runtime.Object{
				&migrationv1alpha1.StorageVersionMigration{
					ObjectMeta: metav1.ObjectMeta{
						Name: "managedclustersetbindings.cluster.open-cluster-management.io",
					},
				},
				&migrationv1alpha1.StorageVersionMigration{
					ObjectMeta: metav1.ObjectMeta{
						Name: "placementdecisions.cluster.open-cluster-management.io",
					},
				},
			},
			validateActions: func(t *testing.T, actions []clienttesting.Action) {
				assertActions(t, actions, "get", "create", "get", "update")
				actual := actions[1].(clienttesting.CreateActionImpl).Object
				assertStorageVersionMigration(t, "managedclustersets.cluster.open-cluster-management.io", actual)
				actual = actions[3].(clienttesting.UpdateActionImpl).Object
				assertStorageVersionMigration(t, "managedclustersetbindings.cluster.open-cluster-management.io", actual)
			},
		},
	}

	if len(migrationRequestFiles) == 0 {
		t.Log("skip testing applyStorageVersionMigrations as no migrationRequestFiles")
		return
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			fakeMigrationClient := fakemigrationclient.NewSimpleClientset(c.existingObjects...)

			err := applyStorageVersionMigrations(context.TODO(), fakeMigrationClient.MigrationV1alpha1(), eventstesting.NewTestingEventRecorder(t))
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			c.validateActions(t, fakeMigrationClient.Actions())
		})
	}
}

func TestRemoveStorageVersionMigrations(t *testing.T) {
	names := []string{
		"managedclustersets.cluster.open-cluster-management.io",
		"managedclustersetbindings.cluster.open-cluster-management.io",
		"placements.cluster.open-cluster-management.io",
		"placementdecisions.cluster.open-cluster-management.io",
	}
	cases := []struct {
		name            string
		existingObjects []runtime.Object
		validateActions func(t *testing.T, actions []clienttesting.Action)
	}{
		{
			name: "not exists",
		},
		{
			name: "removed",
			existingObjects: []runtime.Object{
				&migrationv1alpha1.StorageVersionMigration{
					ObjectMeta: metav1.ObjectMeta{
						Name: "managedclustersetbindings.cluster.open-cluster-management.io",
					},
				},
				&migrationv1alpha1.StorageVersionMigration{
					ObjectMeta: metav1.ObjectMeta{
						Name: "placementdecisions.cluster.open-cluster-management.io",
					},
				},
			},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			fakeMigrationClient := fakemigrationclient.NewSimpleClientset(c.existingObjects...)

			err := applyStorageVersionMigrations(context.TODO(), fakeMigrationClient.MigrationV1alpha1(), eventstesting.NewTestingEventRecorder(t))
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			for _, name := range names {
				_, err := fakeMigrationClient.MigrationV1alpha1().StorageVersionMigrations().Get(context.TODO(), name, metav1.GetOptions{})
				if errors.IsNotFound(err) {
					continue
				}
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
			}
		})
	}
}

func assertActions(t *testing.T, actualActions []clienttesting.Action, expectedVerbs ...string) {
	if len(actualActions) != len(expectedVerbs) {
		t.Fatalf("expected %d call but got: %#v", len(expectedVerbs), actualActions)
	}
	for i, expected := range expectedVerbs {
		if actualActions[i].GetVerb() != expected {
			t.Errorf("expected %s action but got: %#v", expected, actualActions[i])
		}
	}
}

func assertStorageVersionMigration(t *testing.T, name string, object runtime.Object) {
	migration, ok := object.(*migrationv1alpha1.StorageVersionMigration)
	if !ok {
		t.Errorf("expected migration request, but got %v", object)
	}

	if migration.Name != name {
		t.Errorf("expected migration name %q but got %q", name, migration.Name)
	}
}
