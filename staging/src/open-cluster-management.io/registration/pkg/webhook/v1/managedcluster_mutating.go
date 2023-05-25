package v1

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	clusterv1 "open-cluster-management.io/api/cluster/v1"
	clusterv1beta2 "open-cluster-management.io/api/cluster/v1beta2"
	ocmfeature "open-cluster-management.io/api/feature"
	"open-cluster-management.io/registration/pkg/features"
	"open-cluster-management.io/registration/pkg/helpers"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
)

var (
	nowFunc                                       = time.Now
	defaultClusterSetName                         = "default"
	_                     webhook.CustomDefaulter = &ManagedClusterWebhook{}
)

func (r *ManagedClusterWebhook) Default(ctx context.Context, obj runtime.Object) error {
	req, err := admission.RequestFromContext(ctx)
	if err != nil {
		return apierrors.NewBadRequest(err.Error())
	}

	var oldManagedCluster *clusterv1.ManagedCluster
	if len(req.OldObject.Raw) > 0 {
		cluster := &clusterv1.ManagedCluster{}
		if err := json.Unmarshal(req.OldObject.Raw, cluster); err != nil {
			return apierrors.NewBadRequest(err.Error())
		}
		oldManagedCluster = cluster
	}

	managedCluster, ok := obj.(*clusterv1.ManagedCluster)
	if !ok {
		return apierrors.NewBadRequest("Request cluster obj format is not right")
	}

	//Generate taints
	err = r.processTaints(managedCluster, oldManagedCluster)
	if err != nil {
		return err
	}

	//Set default clusterset label
	if features.DefaultHubMutableFeatureGate.Enabled(ocmfeature.DefaultClusterSet) {
		r.addDefaultClusterSetLabel(managedCluster)
	}

	return nil
}

// processTaints set cluster taints
func (r *ManagedClusterWebhook) processTaints(managedCluster, oldManagedCluster *clusterv1.ManagedCluster) error {
	if len(managedCluster.Spec.Taints) == 0 {
		return nil
	}
	now := metav1.NewTime(nowFunc())
	var invalidTaints []string
	for index, taint := range managedCluster.Spec.Taints {
		originalTaint := helpers.FindTaintByKey(oldManagedCluster, taint.Key)
		switch {
		case oldManagedCluster == nil:
			// handle CREATE operation.
			// The request will not be denied if it has taints with timeAdded specified,
			// while the specified values will be ignored.
			managedCluster.Spec.Taints[index].TimeAdded = now
		case originalTaint == nil:
			// handle UPDATE operation.
			// new taint
			// The request will be denied if it has any taint with timeAdded specified.
			if !taint.TimeAdded.IsZero() {
				invalidTaints = append(invalidTaints, taint.Key)
				continue
			}
			managedCluster.Spec.Taints[index].TimeAdded = now
		case originalTaint.Value == taint.Value && originalTaint.Effect == taint.Effect:
			// handle UPDATE operation.
			// no change
			// The request will be denied if it has any taint with different timeAdded specified.
			if !originalTaint.TimeAdded.Equal(&taint.TimeAdded) {
				invalidTaints = append(invalidTaints, taint.Key)
			}
		default:
			// handle UPDATE operation.
			// taint's value/effect has changed
			// The request will be denied if it has any taint with timeAdded specified.
			if !taint.TimeAdded.IsZero() {
				invalidTaints = append(invalidTaints, taint.Key)
				continue
			}
			managedCluster.Spec.Taints[index].TimeAdded = now
		}
	}

	if len(invalidTaints) == 0 {
		return nil
	}
	return apierrors.NewBadRequest(fmt.Sprintf("It is not allowed to set TimeAdded of Taint %q.", strings.Join(invalidTaints, ",")))
}

// addDefaultClusterSetLabel add label "cluster.open-cluster-management.io/clusterset:default" for ManagedCluster if the managedCluster has no ManagedClusterSet label
func (a *ManagedClusterWebhook) addDefaultClusterSetLabel(managedCluster *clusterv1.ManagedCluster) {
	if len(managedCluster.Labels) == 0 {
		managedCluster.Labels = map[string]string{
			clusterv1beta2.ClusterSetLabel: defaultClusterSetName,
		}
		return
	}

	clusterSetName, ok := managedCluster.Labels[clusterv1beta2.ClusterSetLabel]
	// Clusterset label do not exist or "", set default clusterset label
	if !ok || len(clusterSetName) == 0 {
		managedCluster.Labels[clusterv1beta2.ClusterSetLabel] = defaultClusterSetName
	}
}
