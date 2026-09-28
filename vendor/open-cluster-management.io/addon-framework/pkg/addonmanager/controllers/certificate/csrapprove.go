package certificate

import (
	"context"
	"strings"

	certificatesv1 "k8s.io/api/certificates/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	certificatesinformers "k8s.io/client-go/informers/certificates/v1"
	"k8s.io/client-go/kubernetes"
	certificateslisters "k8s.io/client-go/listers/certificates/v1"
	"k8s.io/klog/v2"
	addonv1beta1 "open-cluster-management.io/api/addon/v1beta1"
	addoninformerv1beta1 "open-cluster-management.io/api/client/addon/informers/externalversions/addon/v1beta1"
	addonlisterv1beta1 "open-cluster-management.io/api/client/addon/listers/addon/v1beta1"
	clusterinformers "open-cluster-management.io/api/client/cluster/informers/externalversions/cluster/v1"
	clusterlister "open-cluster-management.io/api/client/cluster/listers/cluster/v1"
	clusterv1 "open-cluster-management.io/api/cluster/v1"

	"open-cluster-management.io/addon-framework/pkg/agent"
	"open-cluster-management.io/addon-framework/pkg/utils"
	"open-cluster-management.io/sdk-go/pkg/basecontroller/factory"
)

// csrApprovingController auto approve the renewal CertificateSigningRequests for an accepted spoke cluster on the hub.
type csrApprovingController struct {
	kubeClient                kubernetes.Interface
	agentAddons               map[string]agent.AgentAddon
	managedClusterLister      clusterlister.ManagedClusterLister
	managedClusterAddonLister addonlisterv1beta1.ManagedClusterAddOnLister
	csrLister                 certificateslisters.CertificateSigningRequestLister
	mcaFilterFunc             utils.ManagedClusterAddOnFilterFunc
}

// NewCSRApprovingController creates a new csr approving controller
func NewCSRApprovingController(
	kubeClient kubernetes.Interface,
	clusterInformers clusterinformers.ManagedClusterInformer,
	csrV1Informer certificatesinformers.CertificateSigningRequestInformer,
	addonInformers addoninformerv1beta1.ManagedClusterAddOnInformer,
	agentAddons map[string]agent.AgentAddon,
	mcaFilterFunc utils.ManagedClusterAddOnFilterFunc,
) factory.Controller {
	c := &csrApprovingController{
		kubeClient:                kubeClient,
		agentAddons:               agentAddons,
		managedClusterLister:      clusterInformers.Lister(),
		managedClusterAddonLister: addonInformers.Lister(),
		csrLister:                 csrV1Informer.Lister(),
		mcaFilterFunc:             mcaFilterFunc,
	}

	return factory.New().
		WithFilteredEventsInformersQueueKeysFunc(
			func(obj runtime.Object) []string {
				accessor, _ := meta.Accessor(obj)
				return []string{accessor.GetName()}
			},
			func(obj interface{}) bool {
				accessor, _ := meta.Accessor(obj)
				if !strings.HasPrefix(accessor.GetName(), "addon") {
					return false
				}
				if len(accessor.GetLabels()) == 0 {
					return false
				}
				addonName := accessor.GetLabels()[addonv1beta1.AddonLabelKey]
				if _, ok := agentAddons[addonName]; !ok {
					return false
				}
				return true
			},
			csrV1Informer.Informer()).
		// clusterLister and addonLister are used, so wait for cache sync
		WithBareInformers(clusterInformers.Informer(), addonInformers.Informer()).
		WithSync(c.sync).
		ToController("CSRApprovingController")
}

func (c *csrApprovingController) sync(ctx context.Context, syncCtx factory.SyncContext, csrName string) error {
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

	addonName := csr.GetLabels()[addonv1beta1.AddonLabelKey]
	agentAddon, ok := c.agentAddons[addonName]
	if !ok {
		return nil
	}

	registrationOption := agentAddon.GetAgentAddonOptions().Registration
	if registrationOption == nil {
		return nil
	}
	clusterName, ok := csr.GetLabels()[clusterv1.ClusterNameLabelKey]
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
	if c.mcaFilterFunc != nil && !c.mcaFilterFunc(managedClusterAddon) {
		return nil
	}

	if registrationOption.CSRApproveCheck == nil {
		klog.V(4).Infof("addon csr %q cannont be auto approved due to approve check not defined", csr.GetName())
		return nil
	}

	if err := c.approve(ctx, registrationOption, managedCluster, managedClusterAddon, csr); err != nil {
		return err
	}

	return nil
}

func (c *csrApprovingController) getCSR(csrName string) (*certificatesv1.CertificateSigningRequest, error) {
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
	managedClusterAddon *addonv1beta1.ManagedClusterAddOn,
	csr *certificatesv1.CertificateSigningRequest) error {

	approve := registrationOption.CSRApproveCheck(ctx, managedCluster, managedClusterAddon, csr)
	if !approve {
		klog.V(4).Infof("addon csr %q cannont be auto approved due to approve check fails", csr.GetName())
		return nil
	}
	csr.Status.Conditions = append(csr.Status.Conditions, certificatesv1.CertificateSigningRequestCondition{
		Type:    certificatesv1.CertificateApproved,
		Status:  corev1.ConditionTrue,
		Reason:  "AutoApprovedByHubCSRApprovingController",
		Message: "Auto approving addon agent certificate.",
	})
	_, err := c.kubeClient.CertificatesV1().CertificateSigningRequests().UpdateApproval(ctx, csr.GetName(), csr, metav1.UpdateOptions{})
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
	return approved
}
