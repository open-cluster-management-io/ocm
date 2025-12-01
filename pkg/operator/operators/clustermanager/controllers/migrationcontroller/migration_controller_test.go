package migrationcontroller

import (
	"context"
	"reflect"
	"testing"
	"time"

	v1 "k8s.io/api/core/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	apiextensionsclient "k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset"
	fakeapiextensions "k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset/fake"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/rest"
	clienttesting "k8s.io/client-go/testing"
	migrationv1alpha1 "sigs.k8s.io/kube-storage-version-migrator/pkg/apis/migration/v1alpha1"
	fakemigrationclient "sigs.k8s.io/kube-storage-version-migrator/pkg/clients/clientset/fake"
	migrationv1alpha1client "sigs.k8s.io/kube-storage-version-migrator/pkg/clients/clientset/typed/migration/v1alpha1"

	fakeoperatorlient "open-cluster-management.io/api/client/operator/clientset/versioned/fake"
	operatorinformers "open-cluster-management.io/api/client/operator/informers/externalversions"
	operatorapiv1 "open-cluster-management.io/api/operator/v1"
	"open-cluster-management.io/sdk-go/pkg/basecontroller/events"
	"open-cluster-management.io/sdk-go/pkg/patcher"

	testingcommon "open-cluster-management.io/ocm/pkg/common/testing"
)

func newFakeCRD(name string, storageVersion string, versions ...string) runtime.Object {
	crd := &apiextensionsv1.CustomResourceDefinition{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
	}
	crd.Spec.Versions = make([]apiextensionsv1.CustomResourceDefinitionVersion, len(versions))
	for i, version := range versions {
		storage := false
		if version == storageVersion {
			storage = true
		}
		crd.Spec.Versions[i] = apiextensionsv1.CustomResourceDefinitionVersion{
			Name:    version,
			Storage: storage,
		}
	}
	return crd
}

func newFakeMigration(name, group, resource, version string) *migrationv1alpha1.StorageVersionMigration {
	return &migrationv1alpha1.StorageVersionMigration{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
		Spec: migrationv1alpha1.StorageVersionMigrationSpec{
			Resource: migrationv1alpha1.GroupVersionResource{
				Group:    group,
				Version:  version,
				Resource: resource,
			},
		},
	}
}

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
				newFakeCRD(migrationRequestCRDName, "v1", "v1"),
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

func TestCheckCRDStorageVersion(t *testing.T) {
	cases := []struct {
		name               string
		crds               []runtime.Object
		toCreateMigrations []*migrationv1alpha1.StorageVersionMigration
		expectErr          bool
	}{
		{
			name: "CRDs with 2 versions: v1beta1, v1beta2; the storage version is v1beta2",
			crds: []runtime.Object{
				newFakeCRD("foos.cluster.open-cluster-management.io", "v1beta2", "v1beta1", "v1beta2"),
				newFakeCRD("bars.cluster.open-cluster-management.io", "v1beta2", "v1beta1", "v1beta2"),
			},
			toCreateMigrations: []*migrationv1alpha1.StorageVersionMigration{
				newFakeMigration("foo", "cluster.open-cluster-management.io", "foos", "v1beta1"),
				newFakeMigration("bar", "cluster.open-cluster-management.io", "bars", "v1beta1"),
			},
			expectErr: false,
		},
		{
			name: "CRDs don't exist",
			toCreateMigrations: []*migrationv1alpha1.StorageVersionMigration{
				newFakeMigration("foo", "cluster.open-cluster-management.io", "foos", "v1beta1"),
				newFakeMigration("bar", "cluster.open-cluster-management.io", "bars", "v1beta1"),
			},
			expectErr: true,
		},
		{
			name: "CRDs exist, but the storage version is still the previous one",
			crds: []runtime.Object{
				newFakeCRD("foos.cluster.open-cluster-management.io", "v1beta1", "v1beta1", "v1beta2"),
				newFakeCRD("bars.cluster.open-cluster-management.io", "v1beta1", "v1beta1", "v1beta2"),
			},
			toCreateMigrations: []*migrationv1alpha1.StorageVersionMigration{
				newFakeMigration("foo", "cluster.open-cluster-management.io", "foos", "v1beta1"),
				newFakeMigration("bar", "cluster.open-cluster-management.io", "bars", "v1beta1"),
			},
			expectErr: true,
		},
		{
			name: "CRDs exist, only have 1 version",
			crds: []runtime.Object{
				newFakeCRD("foos.cluster.open-cluster-management.io", "v1beta1", "v1beta1"),
				newFakeCRD("bars.cluster.open-cluster-management.io", "v1beta1", "v1beta1"),
			},
			toCreateMigrations: []*migrationv1alpha1.StorageVersionMigration{
				newFakeMigration("foo", "cluster.open-cluster-management.io", "foos", "v1beta1"),
				newFakeMigration("bar", "cluster.open-cluster-management.io", "bars", "v1beta1"),
			},
			expectErr: true,
		},
		{
			name: "CRDs exist, but the version in the migration CR is not included in the CRD",
			crds: []runtime.Object{
				newFakeCRD("foos.cluster.open-cluster-management.io", "v1beta2", "v1beta1", "v1beta2"),
				newFakeCRD("bars.cluster.open-cluster-management.io", "v1beta2", "v1beta1", "v1beta2"),
			},
			toCreateMigrations: []*migrationv1alpha1.StorageVersionMigration{
				newFakeMigration("foo", "cluster.open-cluster-management.io", "foos", "v1alpha1"),
				newFakeMigration("bar", "cluster.open-cluster-management.io", "bars", "v1alpha1"),
			},
			expectErr: true,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			fakeCRDClient := fakeapiextensions.NewSimpleClientset(c.crds...)

			err := checkCRDStorageVersion(context.TODO(),
				c.toCreateMigrations, fakeCRDClient)
			if c.expectErr && err != nil {
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

func TestCreateStorageVersionMigrations(t *testing.T) {
	cases := []struct {
		name               string
		existingMigrations []runtime.Object
		toCreateMigrations []*migrationv1alpha1.StorageVersionMigration
		expectErr          bool
		validateActions    func(t *testing.T, actions []clienttesting.Action)
	}{
		{
			// No existing migrations been created
			// Expect to create migration requests for the example CRD
			name:               "No existing migrations been created",
			existingMigrations: []runtime.Object{},
			toCreateMigrations: []*migrationv1alpha1.StorageVersionMigration{
				newFakeMigration("foo", "cluster.open-cluster-management.io", "foos", "v1beta1"),
				newFakeMigration("bar", "cluster.open-cluster-management.io", "bars", "v1beta1"),
			},
			expectErr: false,
			validateActions: func(t *testing.T, actions []clienttesting.Action) {
				assertActions(t, actions, "get", "create", "get", "create")
				actual := actions[1].(clienttesting.CreateActionImpl).Object
				assertStorageVersionMigration(t, "foo", actual)
				actual = actions[3].(clienttesting.CreateActionImpl).Object
				assertStorageVersionMigration(t, "bar", actual)
			},
		},
		{
			// The existing migrations been created with different spec
			// Expect to return err
			name: "CRDs storage version not update",
			existingMigrations: []runtime.Object{
				newFakeMigration("foo", "cluster.open-cluster-management.io", "foos", "v1alpha1"),
				newFakeMigration("bar", "cluster.open-cluster-management.io", "bars", "v1alpha1"),
			},
			toCreateMigrations: []*migrationv1alpha1.StorageVersionMigration{
				newFakeMigration("foo", "cluster.open-cluster-management.io", "foos", "v1beta1"),
				newFakeMigration("bar", "cluster.open-cluster-management.io", "bars", "v1beta1"),
			},
			expectErr: true,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			fakeMigrationClient := fakemigrationclient.NewSimpleClientset(c.existingMigrations...)

			err := createStorageVersionMigrations(context.TODO(),
				c.toCreateMigrations, newClusterManagerOwner(&operatorapiv1.ClusterManager{
					ObjectMeta: metav1.ObjectMeta{
						Name: "testhub",
						UID:  "testhub-uid",
					},
				}), fakeMigrationClient.MigrationV1alpha1(),
				events.NewContextualLoggingEventRecorder(t.Name()))
			if c.expectErr && err != nil {
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			c.validateActions(t, fakeMigrationClient.Actions())
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

func TestSyncStorageVersionMigrationsCondition(t *testing.T) {
	toSyncMigrations := []*migrationv1alpha1.StorageVersionMigration{
		newFakeMigration("foos.cluster.open-cluster-management.io", "cluster.open-cluster-management.io", "foos", "v1beta1"),
		newFakeMigration("bars.cluster.open-cluster-management.io", "cluster.open-cluster-management.io", "bars", "v1beta1"),
	}

	cases := []struct {
		name               string
		existingMigrations []runtime.Object
		want               metav1.Condition
		wantErr            bool
	}{
		{
			name: "empty condition",
			existingMigrations: []runtime.Object{
				&migrationv1alpha1.StorageVersionMigration{
					ObjectMeta: metav1.ObjectMeta{
						Name: "foos.cluster.open-cluster-management.io",
					},
				},
				&migrationv1alpha1.StorageVersionMigration{
					ObjectMeta: metav1.ObjectMeta{
						Name: "bars.cluster.open-cluster-management.io",
					},
				},
			},
			wantErr: false,
			want: metav1.Condition{
				Type:   operatorapiv1.ConditionMigrationSucceeded,
				Status: metav1.ConditionFalse,
			},
		},
		{
			name: "all migration running condition",
			existingMigrations: []runtime.Object{
				&migrationv1alpha1.StorageVersionMigration{
					ObjectMeta: metav1.ObjectMeta{
						Name: "foos.cluster.open-cluster-management.io",
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
						Name: "bars.cluster.open-cluster-management.io",
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
				Type:   operatorapiv1.ConditionMigrationSucceeded,
				Status: metav1.ConditionFalse,
			},
		},
		{
			name: "one migration running, one succeed",
			existingMigrations: []runtime.Object{
				&migrationv1alpha1.StorageVersionMigration{
					ObjectMeta: metav1.ObjectMeta{
						Name: "foos.cluster.open-cluster-management.io",
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
						Name: "bars.cluster.open-cluster-management.io",
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
				Type:   operatorapiv1.ConditionMigrationSucceeded,
				Status: metav1.ConditionFalse,
			},
		},
		{
			name: "one migration failed, one succeed",
			existingMigrations: []runtime.Object{
				&migrationv1alpha1.StorageVersionMigration{
					ObjectMeta: metav1.ObjectMeta{
						Name: "foos.cluster.open-cluster-management.io",
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
						Name: "bars.cluster.open-cluster-management.io",
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
				Type:   operatorapiv1.ConditionMigrationSucceeded,
				Status: metav1.ConditionFalse,
			},
		},
		{
			name: "all migration succeed",
			existingMigrations: []runtime.Object{
				&migrationv1alpha1.StorageVersionMigration{
					ObjectMeta: metav1.ObjectMeta{
						Name: "foos.cluster.open-cluster-management.io",
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
						Name: "bars.cluster.open-cluster-management.io",
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
				Type:   operatorapiv1.ConditionMigrationSucceeded,
				Status: metav1.ConditionTrue,
			},
		},
	}

	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			fakeMigrationClient := fakemigrationclient.NewSimpleClientset(tt.existingMigrations...)

			got, err := syncStorageVersionMigrationsCondition(context.Background(), toSyncMigrations, fakeMigrationClient.MigrationV1alpha1())
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
	tc, client := newTestController(t, clusterManager)

	syncContext := testingcommon.NewFakeSyncContext(t, "testhub")
	// Do not support migration
	err := tc.sync(context.Background(), syncContext, "testhub")
	if err != nil {
		t.Fatalf("Expected no error when sync, %v", err)
	}

	clusterManager, err = client.OperatorV1().ClusterManagers().Get(context.Background(), "testhub", metav1.GetOptions{})
	if err != nil {
		t.Fatalf("Expected no error when sync, %v", err)
	}

	if notsucceeded := meta.IsStatusConditionFalse(clusterManager.Status.Conditions, operatorapiv1.ConditionMigrationSucceeded); !notsucceeded {
		t.Errorf("Error to sync clusterManager.Status.Conditions %v", clusterManager.Status.Conditions)
	}
	// all resources applied
	clusterManager.Status.Conditions = []metav1.Condition{
		{
			Type:   operatorapiv1.ConditionClusterManagerApplied,
			Status: metav1.ConditionTrue,
		},
	}

	tc, client = newTestController(t, clusterManager,
		newFakeCRD(migrationRequestCRDName, "v1", "v1"),
		newFakeCRD("foos.cluster.open-cluster-management.io", "v1beta2", "v1beta1", "v1beta2"),
		newFakeCRD("bars.cluster.open-cluster-management.io", "v1beta2", "v1beta1", "v1beta2"))
	err = tc.sync(context.Background(), syncContext, "testhub")
	if err != nil {
		t.Fatalf("Expected no error when sync, %v", err)
	}
	clusterManager, err = client.OperatorV1().ClusterManagers().Get(context.Background(), "testhub", metav1.GetOptions{})
	if err != nil {
		t.Fatalf("Expected no error when sync, %v", err)
	}
	if notsucceeded := meta.IsStatusConditionFalse(clusterManager.Status.Conditions, operatorapiv1.ConditionMigrationSucceeded); !notsucceeded {
		t.Errorf("Error to sync clusterManager.Status.Conditions %v", clusterManager.Status.Conditions)
	}
}

func newTestController(
	t *testing.T,
	clustermanager *operatorapiv1.ClusterManager,
	crds ...runtime.Object) (*crdMigrationController, *fakeoperatorlient.Clientset) {
	fakeOperatorClient := fakeoperatorlient.NewSimpleClientset(clustermanager)
	operatorInformers := operatorinformers.NewSharedInformerFactory(fakeOperatorClient, 5*time.Minute)
	fakeAPIExtensionClient := fakeapiextensions.NewSimpleClientset(crds...)
	fakeMigrationClient := fakemigrationclient.NewSimpleClientset()

	crdMigrationController := &crdMigrationController{
		clusterManagerLister: operatorInformers.Operator().V1().ClusterManagers().Lister(),
		patcher: patcher.NewPatcher[
			*operatorapiv1.ClusterManager, operatorapiv1.ClusterManagerSpec, operatorapiv1.ClusterManagerStatus](
			fakeOperatorClient.OperatorV1().ClusterManagers()),
	}
	crdMigrationController.generateHubClusterClients = func(
		hubKubeConfig *rest.Config) (apiextensionsclient.Interface, migrationv1alpha1client.StorageVersionMigrationsGetter, error) {
		return fakeAPIExtensionClient, fakeMigrationClient.MigrationV1alpha1(), nil
	}
	crdMigrationController.parseMigrations = func() ([]*migrationv1alpha1.StorageVersionMigration, error) {
		return []*migrationv1alpha1.StorageVersionMigration{
			newFakeMigration("foo", "cluster.open-cluster-management.io", "foos", "v1beta1"),
			newFakeMigration("bar", "cluster.open-cluster-management.io", "bars", "v1beta1"),
		}, nil
	}
	store := operatorInformers.Operator().V1().ClusterManagers().Informer().GetStore()
	if err := store.Add(clustermanager); err != nil {
		t.Fatal(err)
	}

	return crdMigrationController, fakeOperatorClient
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
