package hub

import (
	"context"
	"time"

	ocmfeature "open-cluster-management.io/api/feature"

	"open-cluster-management.io/registration/pkg/features"
	"open-cluster-management.io/registration/pkg/helpers"
	"open-cluster-management.io/registration/pkg/hub/managedclustersetbinding"
	"open-cluster-management.io/registration/pkg/hub/taint"

	addonclient "open-cluster-management.io/api/client/addon/clientset/versioned"
	addoninformers "open-cluster-management.io/api/client/addon/informers/externalversions"
	clusterv1client "open-cluster-management.io/api/client/cluster/clientset/versioned"
	clusterv1informers "open-cluster-management.io/api/client/cluster/informers/externalversions"
	workv1client "open-cluster-management.io/api/client/work/clientset/versioned"
	workv1informers "open-cluster-management.io/api/client/work/informers/externalversions"
	"open-cluster-management.io/registration/pkg/hub/addon"
	"open-cluster-management.io/registration/pkg/hub/clusterrole"
	"open-cluster-management.io/registration/pkg/hub/csr"
	"open-cluster-management.io/registration/pkg/hub/lease"
	"open-cluster-management.io/registration/pkg/hub/managedcluster"
	"open-cluster-management.io/registration/pkg/hub/managedclusterset"
	"open-cluster-management.io/registration/pkg/hub/rbacfinalizerdeletion"

	"github.com/openshift/library-go/pkg/controller/controllercmd"
	"github.com/openshift/library-go/pkg/controller/factory"
	"github.com/pkg/errors"

	kubeinformers "k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/klog/v2"
)

var ResyncInterval = 5 * time.Minute

// RunControllerManager starts the controllers on hub to manage spoke cluster registration.
func RunControllerManager(ctx context.Context, controllerContext *controllercmd.ControllerContext) error {
	// If qps in kubconfig is not set, increase the qps and burst to enhance the ability of kube client to handle
	// requests in concurrent
	// TODO: Use ClientConnectionOverrides flags to change qps/burst when library-go exposes them in the future
	kubeConfig := rest.CopyConfig(controllerContext.KubeConfig)
	if kubeConfig.QPS == 0.0 {
		kubeConfig.QPS = 100.0
		kubeConfig.Burst = 200
	}

	kubeClient, err := kubernetes.NewForConfig(kubeConfig)
	if err != nil {
		return err
	}

	clusterClient, err := clusterv1client.NewForConfig(kubeConfig)
	if err != nil {
		return err
	}

	workClient, err := workv1client.NewForConfig(kubeConfig)
	if err != nil {
		return err
	}

	addOnClient, err := addonclient.NewForConfig(kubeConfig)
	if err != nil {
		return err
	}

	clusterInformers := clusterv1informers.NewSharedInformerFactory(clusterClient, 10*time.Minute)
	workInformers := workv1informers.NewSharedInformerFactory(workClient, 10*time.Minute)
	kubeInfomers := kubeinformers.NewSharedInformerFactory(kubeClient, 10*time.Minute)
	addOnInformers := addoninformers.NewSharedInformerFactory(addOnClient, 10*time.Minute)

	managedClusterController := managedcluster.NewManagedClusterController(
		kubeClient,
		clusterClient,
		clusterInformers.Cluster().V1().ManagedClusters(),
		controllerContext.EventRecorder,
	)

	taintController := taint.NewTaintController(
		clusterClient,
		clusterInformers.Cluster().V1().ManagedClusters(),
		controllerContext.EventRecorder,
	)

	var csrController factory.Controller
	if features.DefaultHubMutableFeatureGate.Enabled(ocmfeature.V1beta1CSRAPICompatibility) {
		v1CSRSupported, v1beta1CSRSupported, err := helpers.IsCSRSupported(kubeClient)
		if err != nil {
			return errors.Wrapf(err, "failed CSR api discovery")
		}

		if !v1CSRSupported && v1beta1CSRSupported {
			csrController = csr.NewV1beta1CSRApprovingController(
				kubeClient,
				kubeInfomers.Certificates().V1beta1().CertificateSigningRequests(),
				controllerContext.EventRecorder,
			)
			klog.Info("Using v1beta1 CSR api to manage spoke client certificate")
		}
	}
	if csrController == nil {
		csrController = csr.NewCSRApprovingController(
			kubeClient,
			kubeInfomers.Certificates().V1().CertificateSigningRequests(),
			controllerContext.EventRecorder,
		)
	}

	leaseController := lease.NewClusterLeaseController(
		kubeClient,
		clusterClient,
		clusterInformers.Cluster().V1().ManagedClusters(),
		kubeInfomers.Coordination().V1().Leases(),
		ResyncInterval, //TODO: this interval time should be allowed to change from outside
		controllerContext.EventRecorder,
	)

	rbacFinalizerController := rbacfinalizerdeletion.NewFinalizeController(
		kubeInfomers.Rbac().V1().Roles(),
		kubeInfomers.Rbac().V1().RoleBindings(),
		kubeInfomers.Core().V1().Namespaces().Lister(),
		clusterInformers.Cluster().V1().ManagedClusters().Lister(),
		workInformers.Work().V1().ManifestWorks().Lister(),
		kubeClient.RbacV1(),
		controllerContext.EventRecorder,
	)

	managedClusterSetController := managedclusterset.NewManagedClusterSetController(
		clusterClient,
		clusterInformers.Cluster().V1().ManagedClusters(),
		clusterInformers.Cluster().V1beta1().ManagedClusterSets(),
		controllerContext.EventRecorder,
	)

	managedClusterSetBindingController := managedclustersetbinding.NewManagedClusterSetBindingController(
		clusterClient,
		clusterInformers.Cluster().V1beta1().ManagedClusterSets(),
		clusterInformers.Cluster().V1beta1().ManagedClusterSetBindings(),
		controllerContext.EventRecorder,
	)

	clusterroleController := clusterrole.NewManagedClusterClusterroleController(
		kubeClient,
		clusterInformers.Cluster().V1().ManagedClusters(),
		kubeInfomers.Rbac().V1().ClusterRoles(),
		controllerContext.EventRecorder,
	)

	addOnHealthCheckController := addon.NewManagedClusterAddOnHealthCheckController(
		addOnClient,
		addOnInformers.Addon().V1alpha1().ManagedClusterAddOns(),
		clusterInformers.Cluster().V1().ManagedClusters(),
		controllerContext.EventRecorder,
	)

	addOnFeatureDiscoveryController := addon.NewAddOnFeatureDiscoveryController(
		clusterClient,
		clusterInformers.Cluster().V1().ManagedClusters(),
		addOnInformers.Addon().V1alpha1().ManagedClusterAddOns(),
		controllerContext.EventRecorder,
	)

	var defaultManagedClusterSetController, globalManagedClusterSetController factory.Controller
	if features.DefaultHubMutableFeatureGate.Enabled(ocmfeature.DefaultClusterSet) {
		defaultManagedClusterSetController = managedclusterset.NewDefaultManagedClusterSetController(
			clusterClient.ClusterV1beta1(),
			clusterInformers.Cluster().V1beta1().ManagedClusterSets(),
			controllerContext.EventRecorder,
		)
		globalManagedClusterSetController = managedclusterset.NewGlobalManagedClusterSetController(
			clusterClient.ClusterV1beta1(),
			clusterInformers.Cluster().V1beta1().ManagedClusterSets(),
			controllerContext.EventRecorder,
		)
	}

	go clusterInformers.Start(ctx.Done())
	go workInformers.Start(ctx.Done())
	go kubeInfomers.Start(ctx.Done())
	go addOnInformers.Start(ctx.Done())

	go managedClusterController.Run(ctx, 1)
	go taintController.Run(ctx, 1)
	go csrController.Run(ctx, 1)
	go leaseController.Run(ctx, 1)
	go rbacFinalizerController.Run(ctx, 1)
	go managedClusterSetController.Run(ctx, 1)
	go managedClusterSetBindingController.Run(ctx, 1)
	go clusterroleController.Run(ctx, 1)
	go addOnHealthCheckController.Run(ctx, 1)
	go addOnFeatureDiscoveryController.Run(ctx, 1)
	if features.DefaultHubMutableFeatureGate.Enabled(ocmfeature.DefaultClusterSet) {
		go defaultManagedClusterSetController.Run(ctx, 1)
		go globalManagedClusterSetController.Run(ctx, 1)
	}

	<-ctx.Done()
	return nil
}
