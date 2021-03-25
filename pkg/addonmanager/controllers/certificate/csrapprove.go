package certificate

import (
	"context"
	"fmt"
	"strings"

	"github.com/open-cluster-management/addon-framework/pkg/agent"
	addoninformerv1alpha1 "github.com/open-cluster-management/api/client/addon/informers/externalversions/addon/v1alpha1"
	addonlisterv1alpha1 "github.com/open-cluster-management/api/client/addon/listers/addon/v1alpha1"
	clusterinformers "github.com/open-cluster-management/api/client/cluster/informers/externalversions/cluster/v1"
	clusterlister "github.com/open-cluster-management/api/client/cluster/listers/cluster/v1"
	"github.com/openshift/library-go/pkg/controller/factory"
	"github.com/openshift/library-go/pkg/operator/events"
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
)

const (
	spokeClusterNameLabel = "open-cluster-management.io/cluster-name"
	spokeAddonNameLabel   = "open-cluster-management.io/addon-name"
)

// csrApprovingController auto approve the renewal CertificateSigningRequests for an accepted spoke cluster on the hub.
type csrApprovingController struct {
	kubeClient                kubernetes.Interface
	agentAddons               map[string]agent.AgentAddon
	eventRecorder             events.Recorder
	managedClusterLister      clusterlister.ManagedClusterLister
	managedClusterAddonLister addonlisterv1alpha1.ManagedClusterAddOnLister
	csrLister                 certificateslisters.CertificateSigningRequestLister
}

// NewCSRApprovingController creates a new csr approving controller
func NewCSRApprovingController(
	kubeClient kubernetes.Interface,
	clusterInformers clusterinformers.ManagedClusterInformer,
	csrInformer certificatesinformers.CertificateSigningRequestInformer,
	addonInformers addoninformerv1alpha1.ManagedClusterAddOnInformer,
	agentAddons map[string]agent.AgentAddon,
	recorder events.Recorder,
) factory.Controller {
	c := &csrApprovingController{
		kubeClient:                kubeClient,
		agentAddons:               agentAddons,
		managedClusterLister:      clusterInformers.Lister(),
		csrLister:                 csrInformer.Lister(),
		managedClusterAddonLister: addonInformers.Lister(),
		eventRecorder:             recorder.WithComponentSuffix(fmt.Sprintf("csr-approving-controller")),
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
				addonName := accessor.GetLabels()[spokeAddonNameLabel]
				if _, ok := agentAddons[addonName]; !ok {
					return false
				}
				return true
			},
			csrInformer.Informer()).
		WithSync(c.sync).
		ToController("CSRApprovingController", recorder)
}

func (c *csrApprovingController) sync(ctx context.Context, syncCtx factory.SyncContext) error {
	csrName := syncCtx.QueueKey()
	klog.V(4).Infof("Reconciling CertificateSigningRequests %q", csrName)
	csr, err := c.csrLister.Get(csrName)
	if errors.IsNotFound(err) {
		return nil
	}
	if err != nil {
		return err
	}
	csr = csr.DeepCopy()

	if isCSRApproved(csr) || IsCSRInTerminalState(csr) {
		return nil
	}

	addonName := csr.Labels[spokeAddonNameLabel]
	agentAddon, ok := c.agentAddons[addonName]
	if !ok {
		return nil
	}

	registrationOption := agentAddon.GetAgentAddonOptions().Registration
	if registrationOption == nil {
		return nil
	}
	clusterName, ok := csr.Labels[spokeClusterNameLabel]
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
		klog.V(4).Infof("addon csr %q cannont be auto approved due to approve check not defined", csr.Name)
		return nil
	}

	approved := registrationOption.CSRApproveCheck(managedCluster, managedClusterAddon, csr)

	// Do not approve if csr check fails
	if !approved {
		klog.V(4).Infof("addon csr %q cannont be auto approved due to approve check fails", csr.Name)
		return nil
	}

	// Auto approve the spoke cluster csr
	csr.Status.Conditions = append(csr.Status.Conditions, certificatesv1.CertificateSigningRequestCondition{
		Type:    certificatesv1.CertificateApproved,
		Status:  corev1.ConditionTrue,
		Reason:  "AutoApprovedByHubCSRApprovingController",
		Message: "Auto approving addon agent certificate.",
	})
	_, err = c.kubeClient.CertificatesV1().CertificateSigningRequests().UpdateApproval(ctx, csr.Name, csr, metav1.UpdateOptions{})
	if err != nil {
		return err
	}
	c.eventRecorder.Eventf("AddonCSRAutoApproved", "addon csr %q is auto approved by addon csr controller", csr.Name)
	return nil
}

// Check whether a CSR is in terminal state
func IsCSRInTerminalState(csr *certificatesv1.CertificateSigningRequest) bool {
	for _, c := range csr.Status.Conditions {
		if c.Type == certificatesv1.CertificateApproved {
			return true
		}
		if c.Type == certificatesv1.CertificateDenied {
			return true
		}
	}
	return false
}

func isCSRApproved(csr *certificatesv1.CertificateSigningRequest) bool {
	// TODO: need to make it work in csr v1 as well
	approved := false
	for _, condition := range csr.Status.Conditions {
		if condition.Type == certificatesv1.CertificateDenied {
			return false
		} else if condition.Type == certificatesv1.CertificateApproved {
			approved = true
		}
	}

	return approved
}
