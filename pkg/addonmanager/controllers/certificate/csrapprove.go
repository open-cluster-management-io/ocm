package certificate

import (
	"context"
	"fmt"
	"strings"

	"github.com/openshift/library-go/pkg/controller/factory"
	"github.com/openshift/library-go/pkg/operator/events"
	certificatesv1 "k8s.io/api/certificates/v1"
	certificatesv1beta1 "k8s.io/api/certificates/v1beta1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	certificatesinformers "k8s.io/client-go/informers/certificates/v1"
	v1beta1certificatesinformers "k8s.io/client-go/informers/certificates/v1beta1"
	"k8s.io/client-go/kubernetes"
	certificateslisters "k8s.io/client-go/listers/certificates/v1"
	v1beta1certificateslisters "k8s.io/client-go/listers/certificates/v1beta1"
	"k8s.io/client-go/tools/cache"
	"k8s.io/klog/v2"
	"open-cluster-management.io/addon-framework/pkg/addonmanager/constants"
	"open-cluster-management.io/addon-framework/pkg/agent"
	addonv1alpha1 "open-cluster-management.io/api/addon/v1alpha1"
	addoninformerv1alpha1 "open-cluster-management.io/api/client/addon/informers/externalversions/addon/v1alpha1"
	addonlisterv1alpha1 "open-cluster-management.io/api/client/addon/listers/addon/v1alpha1"
	clusterinformers "open-cluster-management.io/api/client/cluster/informers/externalversions/cluster/v1"
	clusterlister "open-cluster-management.io/api/client/cluster/listers/cluster/v1"
	clusterv1 "open-cluster-management.io/api/cluster/v1"
)

var (
	// EnableV1Beta1CSRCompatibility is a condition variable that enables/disables
	// the compatibility with V1beta1 CSR api. If enabled, the CSR approver
	// controller wil watch and approve over the V1beta1 CSR api instead of V1.
	// Setting the variable to false will make the CSR signer controller strictly
	// requires V1 CSR api.
	//
	// The distinction between V1 and V1beta1 CSR is that the latter doesn't have
	// a "signerName" field which is used for discriminating external certificate
	// signers. With that being said, under V1beta1 CSR api once a CSR object is
	// approved, it will be immediately signed by the CSR signer controller from
	// kube-controller-manager. So the csr signer controller will be permanently
	// disabled to avoid conflict with Kubernetes' original CSR signer.
	//
	// TODO: Remove this condition gate variable after V1beta1 CSR api fades away
	//       in the Kubernetes community. The code blocks supporting V1beta1 CSR
	//       should also be removed.
	EnableV1Beta1CSRCompatibility = true
)

// csrApprovingController auto approve the renewal CertificateSigningRequests for an accepted spoke cluster on the hub.
type csrApprovingController struct {
	kubeClient                kubernetes.Interface
	agentAddons               map[string]agent.AgentAddon
	eventRecorder             events.Recorder
	managedClusterLister      clusterlister.ManagedClusterLister
	managedClusterAddonLister addonlisterv1alpha1.ManagedClusterAddOnLister
	csrLister                 certificateslisters.CertificateSigningRequestLister
	csrListerBeta             v1beta1certificateslisters.CertificateSigningRequestLister
}

// NewCSRApprovingController creates a new csr approving controller
func NewCSRApprovingController(
	kubeClient kubernetes.Interface,
	clusterInformers clusterinformers.ManagedClusterInformer,
	csrV1Informer certificatesinformers.CertificateSigningRequestInformer,
	csrBetaInformer v1beta1certificatesinformers.CertificateSigningRequestInformer,
	addonInformers addoninformerv1alpha1.ManagedClusterAddOnInformer,
	agentAddons map[string]agent.AgentAddon,
	recorder events.Recorder,
) factory.Controller {
	if (csrV1Informer != nil) == (csrBetaInformer != nil) {
		klog.Fatalf("V1 and V1beta1 CSR informer cannot be present or absent at the same time")
	}
	c := &csrApprovingController{
		kubeClient:                kubeClient,
		agentAddons:               agentAddons,
		managedClusterLister:      clusterInformers.Lister(),
		managedClusterAddonLister: addonInformers.Lister(),
		eventRecorder:             recorder.WithComponentSuffix(fmt.Sprintf("csr-approving-controller")),
	}
	var csrInformer cache.SharedIndexInformer
	if csrV1Informer != nil {
		c.csrLister = csrV1Informer.Lister()
		csrInformer = csrV1Informer.Informer()
	}
	if EnableV1Beta1CSRCompatibility && csrBetaInformer != nil {
		c.csrListerBeta = csrBetaInformer.Lister()
		csrInformer = csrBetaInformer.Informer()
	}

	return factory.New().
		WithFilteredEventsInformersQueueKeyFunc(
			func(obj runtime.Object) string {
				accessor, _ := meta.Accessor(obj)
				return accessor.GetName()
			},
			func(obj interface{}) bool {
				accessor, _ := meta.Accessor(obj)
				if !strings.HasPrefix(accessor.GetName(), "addon") {
					return false
				}
				if len(accessor.GetLabels()) == 0 {
					return false
				}
				addonName := accessor.GetLabels()[constants.AddonLabel]
				if _, ok := agentAddons[addonName]; !ok {
					return false
				}
				return true
			},
			csrInformer).
		WithSync(c.sync).
		ToController("CSRApprovingController", recorder)
}

func (c *csrApprovingController) sync(ctx context.Context, syncCtx factory.SyncContext) error {
	csrName := syncCtx.QueueKey()
	klog.V(4).Infof("Reconciling CertificateSigningRequests %q", csrName)

	csr, err := c.getCSR(csrName)
	if csr == nil {
		return nil
	}
	if err != nil {
		return err
	}

	if isCSRApproved(csr) || IsCSRInTerminalState(csr) {
		return nil
	}

	addonName := csr.GetLabels()[constants.AddonLabel]
	agentAddon, ok := c.agentAddons[addonName]
	if !ok {
		return nil
	}

	registrationOption := agentAddon.GetAgentAddonOptions().Registration
	if registrationOption == nil {
		return nil
	}
	clusterName, ok := csr.GetLabels()[constants.ClusterLabel]
	if !ok {
		return nil
	}

	// Get ManagedCluster
	managedCluster, err := c.managedClusterLister.Get(clusterName)
	if errors.IsNotFound(err) {
		return nil
	}
	if err != nil {
		return err
	}

	// Get Addon
	managedClusterAddon, err := c.managedClusterAddonLister.ManagedClusterAddOns(clusterName).Get(addonName)
	if errors.IsNotFound(err) {
		return nil
	}
	if err != nil {
		return err
	}

	if registrationOption.CSRApproveCheck == nil {
		klog.V(4).Infof("addon csr %q cannont be auto approved due to approve check not defined", csr.GetName())
		return nil
	}

	if err := c.approve(ctx, registrationOption, managedCluster, managedClusterAddon, csr); err != nil {
		return err
	}

	c.eventRecorder.Eventf("AddonCSRAutoApproved", "addon csr %q is auto approved by addon csr controller", csr.GetName())
	return nil
}

func (c *csrApprovingController) getCSR(csrName string) (metav1.Object, error) {
	// TODO: remove the following block for deprecating V1beta1 CSR compatibility
	if EnableV1Beta1CSRCompatibility {
		if c.csrListerBeta != nil {
			csr, err := c.csrListerBeta.Get(csrName)
			if errors.IsNotFound(err) {
				return nil, nil
			}
			if err != nil {
				return nil, err
			}
			return csr, nil
		}
	}
	csr, err := c.csrLister.Get(csrName)
	if errors.IsNotFound(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return csr, nil
}

func (c *csrApprovingController) approve(
	ctx context.Context,
	registrationOption *agent.RegistrationOption,
	managedCluster *clusterv1.ManagedCluster,
	managedClusterAddon *addonv1alpha1.ManagedClusterAddOn,
	csr metav1.Object) error {

	switch t := csr.(type) {
	case *certificatesv1.CertificateSigningRequest:
		approve := registrationOption.CSRApproveCheck(managedCluster, managedClusterAddon, t)
		if !approve {
			klog.V(4).Infof("addon csr %q cannont be auto approved due to approve check fails", csr.GetName())
			return nil
		}
		return c.approveCSRV1(ctx, t)
	// TODO: remove the following block for deprecating V1beta1 CSR compatibility
	case *certificatesv1beta1.CertificateSigningRequest:
		v1CSR := unsafeConvertV1beta1CSRToV1CSR(t)
		approve := registrationOption.CSRApproveCheck(managedCluster, managedClusterAddon, v1CSR)
		if !approve {
			klog.V(4).Infof("addon csr %q cannont be auto approved due to approve check fails", csr.GetName())
			return nil
		}
		return c.approveCSRV1Beta1(ctx, t)
	default:
		return fmt.Errorf("unknown csr object type: %t", csr)
	}
}

func (c *csrApprovingController) approveCSRV1(ctx context.Context, v1CSR *certificatesv1.CertificateSigningRequest) error {
	v1CSR.Status.Conditions = append(v1CSR.Status.Conditions, certificatesv1.CertificateSigningRequestCondition{
		Type:    certificatesv1.CertificateApproved,
		Status:  corev1.ConditionTrue,
		Reason:  "AutoApprovedByHubCSRApprovingController",
		Message: "Auto approving addon agent certificate.",
	})
	_, err := c.kubeClient.CertificatesV1().CertificateSigningRequests().UpdateApproval(ctx, v1CSR.GetName(), v1CSR, metav1.UpdateOptions{})
	if err != nil {
		return err
	}
	return nil
}

func (c *csrApprovingController) approveCSRV1Beta1(ctx context.Context, v1beta1CSR *certificatesv1beta1.CertificateSigningRequest) error {
	v1beta1CSR.Status.Conditions = append(v1beta1CSR.Status.Conditions, certificatesv1beta1.CertificateSigningRequestCondition{
		Type:    certificatesv1beta1.CertificateApproved,
		Status:  corev1.ConditionTrue,
		Reason:  "AutoApprovedByHubCSRApprovingController",
		Message: "Auto approving addon agent certificate.",
	})
	_, err := c.kubeClient.CertificatesV1beta1().CertificateSigningRequests().UpdateApproval(ctx, v1beta1CSR, metav1.UpdateOptions{})
	if err != nil {
		return err
	}
	return nil
}

// Check whether a CSR is in terminal state
func IsCSRInTerminalState(csr metav1.Object) bool {
	if v1CSR, ok := csr.(*certificatesv1.CertificateSigningRequest); ok {
		for _, c := range v1CSR.Status.Conditions {
			if c.Type == certificatesv1.CertificateApproved {
				return true
			}
			if c.Type == certificatesv1.CertificateDenied {
				return true
			}
		}
	}
	// TODO: remove the following block for deprecating V1beta1 CSR compatibility
	if EnableV1Beta1CSRCompatibility {
		if v1beta1CSR, ok := csr.(*certificatesv1beta1.CertificateSigningRequest); ok {
			for _, c := range v1beta1CSR.Status.Conditions {
				if c.Type == certificatesv1beta1.CertificateApproved {
					return true
				}
				if c.Type == certificatesv1beta1.CertificateDenied {
					return true
				}
			}
		}
	}
	return false
}

func isCSRApproved(csr metav1.Object) bool {
	approved := false
	if v1CSR, ok := csr.(*certificatesv1.CertificateSigningRequest); ok {
		for _, condition := range v1CSR.Status.Conditions {
			if condition.Type == certificatesv1.CertificateDenied {
				return false
			} else if condition.Type == certificatesv1.CertificateApproved {
				approved = true
			}
		}
	}
	// TODO: remove the following block for deprecating V1beta1 CSR compatibility
	if EnableV1Beta1CSRCompatibility {
		if v1beta1CSR, ok := csr.(*certificatesv1beta1.CertificateSigningRequest); ok {
			for _, condition := range v1beta1CSR.Status.Conditions {
				if condition.Type == certificatesv1beta1.CertificateDenied {
					return false
				} else if condition.Type == certificatesv1beta1.CertificateApproved {
					approved = true
				}
			}
		}
	}
	return approved
}

// TODO: remove the following block for deprecating V1beta1 CSR compatibility
func unsafeConvertV1beta1CSRToV1CSR(v1beta1CSR *certificatesv1beta1.CertificateSigningRequest) *certificatesv1.CertificateSigningRequest {
	v1CSR := &certificatesv1.CertificateSigningRequest{
		TypeMeta: metav1.TypeMeta{
			APIVersion: certificatesv1.SchemeGroupVersion.String(),
			Kind:       "CertificateSigningRequest",
		},
		ObjectMeta: *v1beta1CSR.ObjectMeta.DeepCopy(),
		Spec: certificatesv1.CertificateSigningRequestSpec{
			Request:           v1beta1CSR.Spec.Request,
			ExpirationSeconds: v1beta1CSR.Spec.ExpirationSeconds,
			Usages:            unsafeCovertV1beta1KeyUsageToV1KeyUsage(v1beta1CSR.Spec.Usages),
			Username:          v1beta1CSR.Spec.Username,
			UID:               v1beta1CSR.Spec.UID,
			Groups:            v1beta1CSR.Spec.Groups,
			Extra:             unsafeCovertV1beta1ExtraValueToV1ExtraValue(v1beta1CSR.Spec.Extra),
		},
		Status: certificatesv1.CertificateSigningRequestStatus{
			Certificate: v1beta1CSR.Status.Certificate,
			Conditions:  unsafeCovertV1beta1ConditionsToV1Conditions(v1beta1CSR.Status.Conditions),
		},
	}
	if v1beta1CSR.Spec.SignerName != nil {
		v1CSR.Spec.SignerName = *v1beta1CSR.Spec.SignerName
	}
	return v1CSR
}

// TODO: remove the following block for deprecating V1beta1 CSR compatibility
func unsafeCovertV1beta1KeyUsageToV1KeyUsage(usages []certificatesv1beta1.KeyUsage) []certificatesv1.KeyUsage {
	v1Usages := make([]certificatesv1.KeyUsage, len(usages))
	for i := range usages {
		v1Usages[i] = certificatesv1.KeyUsage(usages[i])
	}
	return v1Usages
}

// TODO: remove the following block for deprecating V1beta1 CSR compatibility
func unsafeCovertV1beta1ExtraValueToV1ExtraValue(extraValues map[string]certificatesv1beta1.ExtraValue) map[string]certificatesv1.ExtraValue {
	v1Values := make(map[string]certificatesv1.ExtraValue)
	for k := range extraValues {
		v1Values[k] = certificatesv1.ExtraValue(extraValues[k])
	}
	return v1Values
}

// TODO: remove the following block for deprecating V1beta1 CSR compatibility
func unsafeCovertV1beta1ConditionsToV1Conditions(conditions []certificatesv1beta1.CertificateSigningRequestCondition) []certificatesv1.CertificateSigningRequestCondition {
	v1Conditions := make([]certificatesv1.CertificateSigningRequestCondition, len(conditions))
	for i := range conditions {
		v1Conditions[i] = certificatesv1.CertificateSigningRequestCondition{
			Type:               certificatesv1.RequestConditionType(conditions[i].Type),
			Status:             conditions[i].Status,
			Reason:             conditions[i].Reason,
			Message:            conditions[i].Message,
			LastTransitionTime: conditions[i].LastTransitionTime,
			LastUpdateTime:     conditions[i].LastUpdateTime,
		}
	}
	return v1Conditions
}
