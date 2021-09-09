package certificate

import (
	"context"
	"fmt"
	"strings"

	"github.com/openshift/library-go/pkg/controller/factory"
	"github.com/openshift/library-go/pkg/operator/events"
	certificatesv1 "k8s.io/api/certificates/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	certificatesinformers "k8s.io/client-go/informers/certificates/v1"
	"k8s.io/client-go/kubernetes"
	certificateslisters "k8s.io/client-go/listers/certificates/v1"
	"k8s.io/klog/v2"
	"open-cluster-management.io/addon-framework/pkg/addonmanager/constants"
	"open-cluster-management.io/addon-framework/pkg/agent"
	addoninformerv1alpha1 "open-cluster-management.io/api/client/addon/informers/externalversions/addon/v1alpha1"
	addonlisterv1alpha1 "open-cluster-management.io/api/client/addon/listers/addon/v1alpha1"
	clusterinformers "open-cluster-management.io/api/client/cluster/informers/externalversions/cluster/v1"
	clusterlister "open-cluster-management.io/api/client/cluster/listers/cluster/v1"
)

// csrApprovingController auto approve the renewal CertificateSigningRequests for an accepted spoke cluster on the hub.
type csrSignController struct {
	kubeClient                kubernetes.Interface
	agentAddons               map[string]agent.AgentAddon
	eventRecorder             events.Recorder
	managedClusterLister      clusterlister.ManagedClusterLister
	managedClusterAddonLister addonlisterv1alpha1.ManagedClusterAddOnLister
	csrLister                 certificateslisters.CertificateSigningRequestLister
}

// NewCSRApprovingController creates a new csr approving controller
func NewCSRSignController(
	kubeClient kubernetes.Interface,
	clusterInformers clusterinformers.ManagedClusterInformer,
	csrInformer certificatesinformers.CertificateSigningRequestInformer,
	addonInformers addoninformerv1alpha1.ManagedClusterAddOnInformer,
	agentAddons map[string]agent.AgentAddon,
	recorder events.Recorder,
) factory.Controller {
	c := &csrSignController{
		kubeClient:                kubeClient,
		agentAddons:               agentAddons,
		managedClusterLister:      clusterInformers.Lister(),
		managedClusterAddonLister: addonInformers.Lister(),
		csrLister:                 csrInformer.Lister(),
		eventRecorder:             recorder.WithComponentSuffix(fmt.Sprintf("csr-signing-controller")),
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
			csrInformer.Informer()).
		WithSync(c.sync).
		ToController("CSRApprovingController", recorder)
}

func (c *csrSignController) sync(ctx context.Context, syncCtx factory.SyncContext) error {
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

	if !isCSRApproved(csr) {
		return nil
	}

	if len(csr.Status.Certificate) > 0 {
		return nil
	}

	// Do not sigh apiserver cert
	if csr.Spec.SignerName == certificatesv1.KubeAPIServerClientSignerName {
		return nil
	}

	addonName := csr.Labels[constants.AddonLabel]
	agentAddon, ok := c.agentAddons[addonName]
	if !ok {
		return nil
	}

	registrationOption := agentAddon.GetAgentAddonOptions().Registration
	if registrationOption == nil {
		return nil
	}
	clusterName, ok := csr.Labels[constants.ClusterLabel]
	if !ok {
		return nil
	}

	// Get ManagedCluster
	_, err = c.managedClusterLister.Get(clusterName)
	if errors.IsNotFound(err) {
		return nil
	}
	if err != nil {
		return err
	}

	_, err = c.managedClusterAddonLister.ManagedClusterAddOns(clusterName).Get(addonName)
	if errors.IsNotFound(err) {
		return nil
	}
	if err != nil {
		return err
	}

	if registrationOption.CSRSign == nil {
		return nil
	}

	csr.Status.Certificate = registrationOption.CSRSign(csr)
	if len(csr.Status.Certificate) == 0 {
		return fmt.Errorf("invalid client certificate generated for addon csr %q", csr.Name)
	}

	_, err = c.kubeClient.CertificatesV1().CertificateSigningRequests().UpdateStatus(ctx, csr, metav1.UpdateOptions{})
	if err != nil {
		return err
	}
	c.eventRecorder.Eventf("AddonCSRAutoApproved", "addon csr %q is signedr", csr.Name)
	return nil
}
