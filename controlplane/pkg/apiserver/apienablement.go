package apiserver

import (
	admissionregistrationv1 "k8s.io/api/admissionregistration/v1"
	apiserverinternalv1alpha1 "k8s.io/api/apiserverinternal/v1alpha1"
	authenticationv1 "k8s.io/api/authentication/v1"
	authorizationapiv1 "k8s.io/api/authorization/v1"
	autoscalingapiv2beta1 "k8s.io/api/autoscaling/v2beta1"
	autoscalingapiv2beta2 "k8s.io/api/autoscaling/v2beta2"
	batchapiv1beta1 "k8s.io/api/batch/v1beta1"
	certificatesapiv1 "k8s.io/api/certificates/v1"
	coordinationapiv1 "k8s.io/api/coordination/v1"
	apiv1 "k8s.io/api/core/v1"
	discoveryv1 "k8s.io/api/discovery/v1"
	discoveryv1beta1 "k8s.io/api/discovery/v1beta1"
	eventsv1 "k8s.io/api/events/v1"
	eventsv1beta1 "k8s.io/api/events/v1beta1"
	flowcontrolv1alpha1 "k8s.io/api/flowcontrol/v1alpha1"
	nodev1beta1 "k8s.io/api/node/v1beta1"
	policyapiv1beta1 "k8s.io/api/policy/v1beta1"
	rbacv1 "k8s.io/api/rbac/v1"
	storageapiv1 "k8s.io/api/storage/v1"
	storageapiv1alpha1 "k8s.io/api/storage/v1alpha1"
	storageapiv1beta1 "k8s.io/api/storage/v1beta1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	serverstorage "k8s.io/apiserver/pkg/server/storage"
	flowcontrolv1beta1 "k8s.io/kubernetes/pkg/apis/flowcontrol/v1beta1"
	flowcontrolv1beta2 "k8s.io/kubernetes/pkg/apis/flowcontrol/v1beta2"
)

var (
	stableAPIGroupVersionsEnabledByDefault = []schema.GroupVersion{
		admissionregistrationv1.SchemeGroupVersion,
		apiv1.SchemeGroupVersion,
		authenticationv1.SchemeGroupVersion,
		authorizationapiv1.SchemeGroupVersion,
		certificatesapiv1.SchemeGroupVersion,
		coordinationapiv1.SchemeGroupVersion,
		discoveryv1.SchemeGroupVersion,
		eventsv1.SchemeGroupVersion,
		rbacv1.SchemeGroupVersion,
		storageapiv1.SchemeGroupVersion,
	}

	legacyBetaEnabledByDefaultResources = []schema.GroupVersionResource{
		discoveryv1beta1.SchemeGroupVersion.WithResource("endpointslices"),                // remove in 1.25
		eventsv1beta1.SchemeGroupVersion.WithResource("events"),                           // remove in 1.25
		flowcontrolv1beta1.SchemeGroupVersion.WithResource("flowschemas"),                 // remove in 1.26
		flowcontrolv1beta1.SchemeGroupVersion.WithResource("prioritylevelconfigurations"), // remove in 1.26
		flowcontrolv1beta2.SchemeGroupVersion.WithResource("flowschemas"),                 // remove in 1.29
		flowcontrolv1beta2.SchemeGroupVersion.WithResource("prioritylevelconfigurations"), // remove in 1.29
	}
	// betaAPIGroupVersionsDisabledByDefault is for all future beta groupVersions.
	betaAPIGroupVersionsDisabledByDefault = []schema.GroupVersion{
		autoscalingapiv2beta1.SchemeGroupVersion,
		autoscalingapiv2beta2.SchemeGroupVersion,
		batchapiv1beta1.SchemeGroupVersion,
		discoveryv1beta1.SchemeGroupVersion,
		eventsv1beta1.SchemeGroupVersion,
		nodev1beta1.SchemeGroupVersion, // remove in 1.26
		policyapiv1beta1.SchemeGroupVersion,
		storageapiv1beta1.SchemeGroupVersion,
		flowcontrolv1beta1.SchemeGroupVersion,
		flowcontrolv1beta2.SchemeGroupVersion,
	}

	// alphaAPIGroupVersionsDisabledByDefault holds the alpha APIs we have.  They are always disabled by default.
	alphaAPIGroupVersionsDisabledByDefault = []schema.GroupVersion{
		apiserverinternalv1alpha1.SchemeGroupVersion,
		storageapiv1alpha1.SchemeGroupVersion,
		flowcontrolv1alpha1.SchemeGroupVersion,
	}
)

func APIResourceConfigSource() *serverstorage.ResourceConfig {
	ret := serverstorage.NewResourceConfig()

	ret.EnableVersions(stableAPIGroupVersionsEnabledByDefault...)
	ret.DisableVersions(betaAPIGroupVersionsDisabledByDefault...)
	ret.DisableVersions(alphaAPIGroupVersionsDisabledByDefault...)

	ret.EnableResources(legacyBetaEnabledByDefaultResources...)

	return ret
}
