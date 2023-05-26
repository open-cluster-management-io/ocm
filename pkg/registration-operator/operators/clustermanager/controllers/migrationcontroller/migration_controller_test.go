package migrationcontroller

import (
	"context"
	"reflect"
	"testing"
	"time"

	testinghelper "open-cluster-management.io/ocm/pkg/registration-operator/helpers/testing"

	fakeoperatorlient "open-cluster-management.io/api/client/operator/clientset/versioned/fake"
	operatorinformers "open-cluster-management.io/api/client/operator/informers/externalversions"
	operatorapiv1 "open-cluster-management.io/api/operator/v1"

	"github.com/openshift/library-go/pkg/operator/events/eventstesting"
	v1 "k8s.io/api/core/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	apiextensionsclient "k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset"
	fakeapiextensions "k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset/fake"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/rest"
	clienttesting "k8s.io/client-go/testing"
	migrationv1alpha1 "sigs.k8s.io/kube-storage-version-migrator/pkg/apis/migration/v1alpha1"
	fakemigrationclient "sigs.k8s.io/kube-storage-version-migrator/pkg/clients/clientset/fake"
	migrationv1alpha1client "sigs.k8s.io/kube-storage-version-migrator/pkg/clients/clientset/typed/migration/v1alpha1"
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
				newCrd(migrationRequestCRDName),
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

func newCrd(name string) runtime.Object {
	return &apiextensionsv1.CustomResourceDefinition{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
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
			err := removeStorageVersionMigrations(context.TODO(), fakeMigrationClient.MigrationV1alpha1())
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

func Test_syncStorageVersionMigrationsCondition(t *testing.T) {

	tests := []struct {
		name            string
		existingObjects []runtime.Object
		want            metav1.Condition
		wantErr         bool
	}{
		{
			name: "empty condition",
			existingObjects: []runtime.Object{
				&migrationv1alpha1.StorageVersionMigration{
					ObjectMeta: metav1.ObjectMeta{
						Name: "managedclustersetbindings.cluster.open-cluster-management.io",
					},
				},
				&migrationv1alpha1.StorageVersionMigration{
					ObjectMeta: metav1.ObjectMeta{
						Name: "managedclustersets.cluster.open-cluster-management.io",
					},
				},
			},
			wantErr: false,
			want: metav1.Condition{
				Type:   MigrationSucceeded,
				Status: metav1.ConditionFalse,
			},
		},
		{
			name: "all migration running condition",
			existingObjects: []runtime.Object{
				&migrationv1alpha1.StorageVersionMigration{
					ObjectMeta: metav1.ObjectMeta{
						Name: "managedclustersetbindings.cluster.open-cluster-management.io",
					},
					Status: migrationv1alpha1.StorageVersionMigrationStatus{
						Conditions: []migrationv1alpha1.MigrationCondition{
							{
								Type:   migrationv1alpha1.MigrationRunning,
								Status: v1.ConditionTrue,
							},
						},
					},
				},
				&migrationv1alpha1.StorageVersionMigration{
					ObjectMeta: metav1.ObjectMeta{
						Name: "managedclustersets.cluster.open-cluster-management.io",
					},
					Status: migrationv1alpha1.StorageVersionMigrationStatus{
						Conditions: []migrationv1alpha1.MigrationCondition{
							{
								Type:   migrationv1alpha1.MigrationRunning,
								Status: v1.ConditionTrue,
							},
						},
					},
				},
			},
			wantErr: false,
			want: metav1.Condition{
				Type:   MigrationSucceeded,
				Status: metav1.ConditionFalse,
			},
		},
		{
			name: "one migration running, one succeed",
			existingObjects: []runtime.Object{
				&migrationv1alpha1.StorageVersionMigration{
					ObjectMeta: metav1.ObjectMeta{
						Name: "managedclustersetbindings.cluster.open-cluster-management.io",
					},
					Status: migrationv1alpha1.StorageVersionMigrationStatus{
						Conditions: []migrationv1alpha1.MigrationCondition{
							{
								Type:   migrationv1alpha1.MigrationSucceeded,
								Status: v1.ConditionTrue,
							},
						},
					},
				},
				&migrationv1alpha1.StorageVersionMigration{
					ObjectMeta: metav1.ObjectMeta{
						Name: "managedclustersets.cluster.open-cluster-management.io",
					},
					Status: migrationv1alpha1.StorageVersionMigrationStatus{
						Conditions: []migrationv1alpha1.MigrationCondition{
							{
								Type:   migrationv1alpha1.MigrationRunning,
								Status: v1.ConditionTrue,
							},
						},
					},
				},
			},
			wantErr: false,
			want: metav1.Condition{
				Type:   MigrationSucceeded,
				Status: metav1.ConditionFalse,
			},
		},
		{
			name: "one migration failed, one succeed",
			existingObjects: []runtime.Object{
				&migrationv1alpha1.StorageVersionMigration{
					ObjectMeta: metav1.ObjectMeta{
						Name: "managedclustersetbindings.cluster.open-cluster-management.io",
					},
					Status: migrationv1alpha1.StorageVersionMigrationStatus{
						Conditions: []migrationv1alpha1.MigrationCondition{
							{
								Type:   migrationv1alpha1.MigrationFailed,
								Status: v1.ConditionTrue,
							},
						},
					},
				},
				&migrationv1alpha1.StorageVersionMigration{
					ObjectMeta: metav1.ObjectMeta{
						Name: "managedclustersets.cluster.open-cluster-management.io",
					},
					Status: migrationv1alpha1.StorageVersionMigrationStatus{
						Conditions: []migrationv1alpha1.MigrationCondition{
							{
								Type:   migrationv1alpha1.MigrationSucceeded,
								Status: v1.ConditionTrue,
							},
						},
					},
				},
			},
			wantErr: false,
			want: metav1.Condition{
				Type:   MigrationSucceeded,
				Status: metav1.ConditionFalse,
			},
		},
		{
			name: "all migration succeed",
			existingObjects: []runtime.Object{
				&migrationv1alpha1.StorageVersionMigration{
					ObjectMeta: metav1.ObjectMeta{
						Name: "managedclustersetbindings.cluster.open-cluster-management.io",
					},
					Status: migrationv1alpha1.StorageVersionMigrationStatus{
						Conditions: []migrationv1alpha1.MigrationCondition{
							{
								Type:   migrationv1alpha1.MigrationSucceeded,
								Status: v1.ConditionTrue,
							},
						},
					},
				},
				&migrationv1alpha1.StorageVersionMigration{
					ObjectMeta: metav1.ObjectMeta{
						Name: "managedclustersets.cluster.open-cluster-management.io",
					},
					Status: migrationv1alpha1.StorageVersionMigrationStatus{
						Conditions: []migrationv1alpha1.MigrationCondition{
							{
								Type:   migrationv1alpha1.MigrationSucceeded,
								Status: v1.ConditionTrue,
							},
						},
					},
				},
			},
			wantErr: false,
			want: metav1.Condition{
				Type:   MigrationSucceeded,
				Status: metav1.ConditionTrue,
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fakeMigrationClient := fakemigrationclient.NewSimpleClientset(tt.existingObjects...)

			got, err := syncStorageVersionMigrationsCondition(context.Background(), fakeMigrationClient.MigrationV1alpha1())
			if (err != nil) != tt.wantErr {
				t.Errorf("syncStorageVersionMigrationsCondition() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !reflect.DeepEqual(got.Type, tt.want.Type) || !reflect.DeepEqual(got.Status, tt.want.Status) {
				t.Errorf("syncStorageVersionMigrationsCondition() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestSync(t *testing.T) {
	clusterManager := newClusterManager("testhub")
	tc := newTestController(t, clusterManager)

	syncContext := testinghelper.NewFakeSyncContext(t, "testhub")
	//Do not support migration
	err := tc.sync(context.Background(), syncContext)
	if err != nil {
		t.Fatalf("Expected no error when sync, %v", err)
	}

	clusterManager, err = tc.clusterManagerClient.Get(context.Background(), "testhub", metav1.GetOptions{})
	if err != nil {
		t.Fatalf("Expected no error when sync, %v", err)
	}

	if notsucceeded := meta.IsStatusConditionFalse(clusterManager.Status.Conditions, MigrationSucceeded); !notsucceeded {
		t.Errorf("Error to sync clusterManager.Status.Conditions %v", clusterManager.Status.Conditions)
	}
	// all resources applied
	clusterManager.Status.Conditions = []metav1.Condition{
		{
			Type:   clusterManagerApplied,
			Status: metav1.ConditionTrue,
		},
	}
	migrateCrd := newCrd(migrationRequestCRDName)
	tc = newTestController(t, clusterManager, migrateCrd)
	err = tc.sync(context.Background(), syncContext)
	if err != nil {
		t.Fatalf("Expected no error when sync, %v", err)
	}
	clusterManager, err = tc.clusterManagerClient.Get(context.Background(), "testhub", metav1.GetOptions{})
	if err != nil {
		t.Fatalf("Expected no error when sync, %v", err)
	}
	if notsucceeded := meta.IsStatusConditionFalse(clusterManager.Status.Conditions, MigrationSucceeded); !notsucceeded {
		t.Errorf("Error to sync clusterManager.Status.Conditions %v", clusterManager.Status.Conditions)
	}
}

func newTestController(t *testing.T, clustermanager *operatorapiv1.ClusterManager, crds ...runtime.Object) *crdMigrationController {
	fakeOperatorClient := fakeoperatorlient.NewSimpleClientset(clustermanager)
	operatorInformers := operatorinformers.NewSharedInformerFactory(fakeOperatorClient, 5*time.Minute)
	fakeAPIExtensionClient := fakeapiextensions.NewSimpleClientset(crds...)
	fakeMigrationClient := fakemigrationclient.NewSimpleClientset()

	crdMigrationController := &crdMigrationController{
		clusterManagerClient: fakeOperatorClient.OperatorV1().ClusterManagers(),
		clusterManagerLister: operatorInformers.Operator().V1().ClusterManagers().Lister(),
		recorder:             eventstesting.NewTestingEventRecorder(t),
	}
	crdMigrationController.generateHubClusterClients = func(hubKubeConfig *rest.Config) (apiextensionsclient.Interface, migrationv1alpha1client.StorageVersionMigrationsGetter, error) {
		return fakeAPIExtensionClient, fakeMigrationClient.MigrationV1alpha1(), nil
	}
	store := operatorInformers.Operator().V1().ClusterManagers().Informer().GetStore()
	if err := store.Add(clustermanager); err != nil {
		t.Fatal(err)
	}

	return crdMigrationController
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
