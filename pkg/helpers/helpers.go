package helpers

import (
	"context"
	"fmt"
	"net/url"
	"path/filepath"
	"time"

	clusterclientset "github.com/open-cluster-management/api/client/cluster/clientset/versioned"
	clusterv1 "github.com/open-cluster-management/api/cluster/v1"
	"github.com/open-cluster-management/registration/pkg/hub/managedcluster/bindata"

	"github.com/openshift/api"
	"github.com/openshift/library-go/pkg/assets"
	"github.com/openshift/library-go/pkg/operator/events"
	"github.com/openshift/library-go/pkg/operator/resource/resourceapply"
	"github.com/openshift/library-go/pkg/operator/resource/resourcehelper"
	errorhelpers "github.com/openshift/library-go/pkg/operator/v1helpers"

	certificatesv1beta1 "k8s.io/api/certificates/v1beta1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/util/retry"
)

var (
	genericScheme = runtime.NewScheme()
	genericCodecs = serializer.NewCodecFactory(genericScheme)
	genericCodec  = genericCodecs.UniversalDeserializer()
)

func init() {
	utilruntime.Must(api.InstallKube(genericScheme))
}

func IsConditionTrue(condition *clusterv1.StatusCondition) bool {
	if condition == nil {
		return false
	}
	return condition.Status == metav1.ConditionTrue
}

func FindManagedClusterCondition(conditions []clusterv1.StatusCondition, conditionType string) *clusterv1.StatusCondition {
	for i := range conditions {
		if conditions[i].Type == conditionType {
			return &conditions[i]
		}
	}
	return nil
}

func SetManagedClusterCondition(conditions *[]clusterv1.StatusCondition, newCondition clusterv1.StatusCondition) {
	if conditions == nil {
		conditions = &[]clusterv1.StatusCondition{}
	}
	existingCondition := FindManagedClusterCondition(*conditions, newCondition.Type)
	if existingCondition == nil {
		newCondition.LastTransitionTime = metav1.NewTime(time.Now())
		*conditions = append(*conditions, newCondition)
		return
	}

	if existingCondition.Status != newCondition.Status {
		existingCondition.Status = newCondition.Status
		existingCondition.LastTransitionTime = metav1.NewTime(time.Now())
	}

	existingCondition.Reason = newCondition.Reason
	existingCondition.Message = newCondition.Message
}

type UpdateManagedClusterStatusFunc func(status *clusterv1.ManagedClusterStatus) error

func UpdateManagedClusterStatus(
	ctx context.Context,
	client clusterclientset.Interface,
	spokeClusterName string,
	updateFuncs ...UpdateManagedClusterStatusFunc) (*clusterv1.ManagedClusterStatus, bool, error) {
	updated := false
	var updatedManagedClusterStatus *clusterv1.ManagedClusterStatus

	err := retry.RetryOnConflict(retry.DefaultBackoff, func() error {
		managedCluster, err := client.ClusterV1().ManagedClusters().Get(ctx, spokeClusterName, metav1.GetOptions{})
		if err != nil {
			return err
		}
		oldStatus := &managedCluster.Status

		newStatus := oldStatus.DeepCopy()
		for _, update := range updateFuncs {
			if err := update(newStatus); err != nil {
				return err
			}
		}
		if equality.Semantic.DeepEqual(oldStatus, newStatus) {
			// We return the newStatus which is a deep copy of oldStatus but with all update funcs applied.
			updatedManagedClusterStatus = newStatus
			return nil
		}

		managedCluster.Status = *newStatus
		updatedManagedCluster, err := client.ClusterV1().ManagedClusters().UpdateStatus(ctx, managedCluster, metav1.UpdateOptions{})
		if err != nil {
			return err
		}
		updatedManagedClusterStatus = &updatedManagedCluster.Status
		updated = err == nil
		return err
	})

	return updatedManagedClusterStatus, updated, err
}

func UpdateManagedClusterConditionFn(cond clusterv1.StatusCondition) UpdateManagedClusterStatusFunc {
	return func(oldStatus *clusterv1.ManagedClusterStatus) error {
		SetManagedClusterCondition(&oldStatus.Conditions, cond)
		return nil
	}
}

// Check whether a CSR is in terminal state
func IsCSRInTerminalState(status *certificatesv1beta1.CertificateSigningRequestStatus) bool {
	for _, c := range status.Conditions {
		if c.Type == certificatesv1beta1.CertificateApproved {
			return true
		}
		if c.Type == certificatesv1beta1.CertificateDenied {
			return true
		}
	}
	return false
}

// IsValidHTTPSURL validate whether a URL is https URL
func IsValidHTTPSURL(serverURL string) bool {
	if serverURL == "" {
		return false
	}

	parsedServerURL, err := url.Parse(serverURL)
	if err != nil {
		return false
	}

	if parsedServerURL.Scheme != "https" {
		return false
	}

	return true
}

// CleanUpManagedClusterManifests clean up managed cluster resources from its manifest files
func CleanUpManagedClusterManifests(
	ctx context.Context,
	client kubernetes.Interface,
	recorder events.Recorder,
	assetFunc resourceapply.AssetFunc,
	files ...string) error {
	errs := []error{}
	for _, file := range files {
		objectRaw, err := assetFunc(file)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		object, _, err := genericCodec.Decode(objectRaw, nil, nil)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		switch t := object.(type) {
		case *corev1.Namespace:
			err = client.CoreV1().Namespaces().Delete(ctx, t.Name, metav1.DeleteOptions{})
		case *rbacv1.Role:
			err = client.RbacV1().Roles(t.Namespace).Delete(ctx, t.Name, metav1.DeleteOptions{})
		case *rbacv1.RoleBinding:
			err = client.RbacV1().RoleBindings(t.Namespace).Delete(ctx, t.Name, metav1.DeleteOptions{})
		case *rbacv1.ClusterRole:
			err = client.RbacV1().ClusterRoles().Delete(ctx, t.Name, metav1.DeleteOptions{})
		case *rbacv1.ClusterRoleBinding:
			err = client.RbacV1().ClusterRoleBindings().Delete(ctx, t.Name, metav1.DeleteOptions{})
		default:
			err = fmt.Errorf("unhandled type %T", object)
		}
		if errors.IsNotFound(err) {
			continue
		}
		if err != nil {
			errs = append(errs, err)
			continue
		}
		gvk := resourcehelper.GuessObjectGroupVersionKind(object)
		recorder.Eventf(fmt.Sprintf("ManagedCluster%sDeleted", gvk.Kind), "Deleted %s", resourcehelper.FormatResourceForCLIWithNamespace(object))
	}
	return errorhelpers.NewMultiLineAggregate(errs)
}

// CleanUpGroupFromClusterRoleBindings search all clusterrolebindings for managed cluster group and remove the subject entry
// or delete the clusterrolebinding if it's the only subject.
func CleanUpGroupFromClusterRoleBindings(
	ctx context.Context,
	client kubernetes.Interface,
	recorder events.Recorder,
	managedClusterGroup string) error {
	clusterRoleBindings, err := client.RbacV1().ClusterRoleBindings().List(ctx, metav1.ListOptions{})
	if err != nil {
		return err
	}
	for i := range clusterRoleBindings.Items {
		clusterRoleBinding := clusterRoleBindings.Items[i]
		subjects := clusterRoleBinding.Subjects
		newSubjects := []rbacv1.Subject{}
		for _, subject := range subjects {
			if subject.Kind == "Group" && subject.Name == managedClusterGroup {
				continue
			}
			newSubjects = append(newSubjects, subject)
		}
		// no other subjects, remove this clusterrolebinding
		if len(newSubjects) == 0 {
			err := client.RbacV1().ClusterRoleBindings().Delete(ctx, clusterRoleBinding.Name, metav1.DeleteOptions{})
			if err != nil {
				return err
			}
			recorder.Eventf("ClusterRoleBindingDeleted", fmt.Sprintf("Deleted ClusterRoleBinding %q", clusterRoleBinding.Name))
			continue
		}
		// there are other subjects, only remove the cluster managed group
		if len(newSubjects) != len(subjects) {
			clusterRoleBinding.Subjects = newSubjects
			_, err := client.RbacV1().ClusterRoleBindings().Update(ctx, &clusterRoleBinding, metav1.UpdateOptions{})
			if err != nil {
				return err
			}
			recorder.Eventf("ClusterRoleBindingUpdated", fmt.Sprintf("Updated ClusterRoleBinding %q", clusterRoleBinding.Name))
			continue
		}
	}

	return nil
}

// CleanUpGroupFromRoleBindings search all rolebindings for managed cluster group and remove the subject entry
// or delete the rolebinding if it's the only subject.
func CleanUpGroupFromRoleBindings(
	ctx context.Context,
	client kubernetes.Interface,
	recorder events.Recorder,
	managedClusterGroup string) error {
	roleBindings, err := client.RbacV1().RoleBindings(metav1.NamespaceAll).List(ctx, metav1.ListOptions{})
	if err != nil {
		return err
	}
	for i := range roleBindings.Items {
		roleBinding := roleBindings.Items[i]
		subjects := roleBinding.Subjects
		newSubjects := []rbacv1.Subject{}
		for _, subject := range subjects {
			if subject.Kind == "Group" && subject.Name == managedClusterGroup {
				continue
			}
			newSubjects = append(newSubjects, subject)
		}
		// no other subjects, remove this rolebinding
		if len(newSubjects) == 0 {
			err := client.RbacV1().RoleBindings(roleBinding.Namespace).Delete(ctx, roleBinding.Name, metav1.DeleteOptions{})
			if err != nil {
				return err
			}
			recorder.Eventf("RoleBindingDeleted", fmt.Sprintf("Deleted RoleBinding %q/%q", roleBinding.Namespace, roleBinding.Name))
			continue
		}
		// there are other subjects, only remove the cluster managed group
		if len(newSubjects) != len(subjects) {
			roleBinding.Subjects = newSubjects
			_, err := client.RbacV1().RoleBindings(roleBinding.Namespace).Update(ctx, &roleBinding, metav1.UpdateOptions{})
			if err != nil {
				return err
			}
			recorder.Eventf("RoleBindingUpdated", fmt.Sprintf("Updated RoleBinding %q/%q", roleBinding.Namespace, roleBinding.Name))
			continue
		}
	}
	return nil
}

func ManagedClusterAssetFn(manifestDir, managedClusterName string) resourceapply.AssetFunc {
	return func(name string) ([]byte, error) {
		config := struct {
			ManagedClusterName string
		}{
			ManagedClusterName: managedClusterName,
		}
		return assets.MustCreateAssetFromTemplate(name, bindata.MustAsset(filepath.Join(manifestDir, name)), config).Data, nil
	}
}
