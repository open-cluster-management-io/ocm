package feature

import (
	"k8s.io/component-base/featuregate"
)

const (
	// Every feature gate should add method here following this template:
	//
	// // owner: @username
	// // alpha: v1.X
	// MyFeature featuregate.Feature = "MyFeature"

	// ClusterClaim will start a new controller in the spoke-agent to manage the cluster-claim
	// resources in the managed cluster.
	//
	// The cluster-claim controller is majorly for collecting claims and updating claims field
	// in managedcluster status. When it exceeds the limit specified by "--max-custom-cluster-claims",
	// the extra claims will be truncated.
	//
	// If it is disabled, the user will see empty claims field in managedcluster status. The
	// deployer who disable the feature may need to update claim field in managed cluster status
	// itself to avoid impact to users.
	ClusterClaim featuregate.Feature = "ClusterClaim"

	// ClusterProperty is a feature gate on hub controller and spoke-agent. When it is enabled on the
	// spoke agent, it will use the claim controller to manage the managed cluster property
	ClusterProperty featuregate.Feature = "ClusterProperty"

	// AddonManagement is a feature gate on hub controller and spoke-agent. When it is enabled on the
	// spoke agent, it will start a new controllers to manage the managed cluster addons
	// registration and maintains the status of managed cluster addons through watching their leases.
	// When it is enabled on hub controller, it will start a new controller to process addon automatic
	// installation and rolling out.
	AddonManagement featuregate.Feature = "AddonManagement"

	// DefaultClusterSet will make registration hub controller to maintain a default clusterset and a global clusterset.
	// All clusters without clusterset label will be automatically added into the default clusterset by adding a label
	// "cluster.open-cluster-management.io/clusterset=default" to the clusters.
	// All clusters will be included to the global clusterset
	DefaultClusterSet featuregate.Feature = "DefaultClusterSet"

	// V1beta1CSRAPICompatibility will make the spoke registration agent to issue CSR requests
	// via V1beta1 api, so that registration agent can still manage the certificate rotation for the
	// ManagedCluster and  ManagedClusterAddon.
	// Note that kubernetes release [1.12, 1.18)'s beta CSR api doesn't have the "signerName" field which
	// means that all the approved CSR objects will be signed by the built-in CSR controller in
	// kube-controller-manager.
	V1beta1CSRAPICompatibility featuregate.Feature = "V1beta1CSRAPICompatibility"

	// NilExecutorValidating will make the work-webhook to validate the manifest work even if its executor is nil, it
	// will check if the request user has the execute-as permission with the default executor
	// "system:serviceaccount::klusterlet-work-sa"
	NilExecutorValidating featuregate.Feature = "NilExecutorValidating"

	// ExecutorValidatingCaches will start a new controller in the wokrk agent to cache subject access review
	// validating results for executors.
	// When using the ManifestWork Executor feature, enabling this can reduce the number of subject access review
	// requests sent by the work agent to the managed cluster api server.
	ExecutorValidatingCaches featuregate.Feature = "ExecutorValidatingCaches"

	// ManagedClusterAutoApproval will approve a managed cluster registraion request automatically.
	// A registraion request to be automatically approved must be initiated by specific users, these users are
	// specifed with "--cluster-auto-approval-users" flag in the registration hub controller.
	ManagedClusterAutoApproval featuregate.Feature = "ManagedClusterAutoApproval"

	// ManifestWorkReplicaSet will start new controller in the Hub that can be used to deploy manifestWorks to group
	// of clusters selected by a placement. For more info check ManifestWorkReplicaSet APIs
	ManifestWorkReplicaSet featuregate.Feature = "ManifestWorkReplicaSet"

	// CloudEventsDrivers will enable the cloud events drivers (mqtt or grpc) for the hub controller,
	// so that the controller can deliver manifestworks to the managed clusters via cloud events.
	CloudEventsDrivers featuregate.Feature = "CloudEventsDrivers"

	// RawFeedbackJsonString will make the work agent to return the feedback result as a json string if the result
	// is not a scalar value.
	RawFeedbackJsonString featuregate.Feature = "RawFeedbackJsonString"

	// ResourceCleanup will start gc controller to clean up resources in cluster ns after cluster is deleted.
	ResourceCleanup featuregate.Feature = "ResourceCleanup"

	// MultipleHubs allows user to configure multiple bootstrapkubeconfig connecting to different hubs via Klusterlet and let agent decide which one to use
	MultipleHubs featuregate.Feature = "MultipleHubs"

	// ClusterProfile will start new controller in the Hub that can be used to sync ManagedCluster to ClusterProfile.
	ClusterProfile featuregate.Feature = "ClusterProfile"

	// ClusterImporter will enable the auto import of managed cluster for certain cluster providers, e.g. cluster-api.
	ClusterImporter featuregate.Feature = "ClusterImporter"
)

// DefaultSpokeRegistrationFeatureGates consists of all known ocm-registration
// feature keys for registration agent.  To add a new feature, define a key for it above and
// add it here.
var DefaultSpokeRegistrationFeatureGates = map[featuregate.Feature]featuregate.FeatureSpec{
	ClusterClaim:               {Default: true, PreRelease: featuregate.Beta},
	ClusterProperty:            {Default: false, PreRelease: featuregate.Alpha},
	AddonManagement:            {Default: true, PreRelease: featuregate.Beta},
	V1beta1CSRAPICompatibility: {Default: false, PreRelease: featuregate.Alpha},
	MultipleHubs:               {Default: false, PreRelease: featuregate.Alpha},
}

// DefaultHubRegistrationFeatureGates consists of all known ocm-registration
// feature keys for registration hub controller.  To add a new feature, define a key for it above and
// add it here.
var DefaultHubRegistrationFeatureGates = map[featuregate.Feature]featuregate.FeatureSpec{
	DefaultClusterSet:          {Default: true, PreRelease: featuregate.Alpha},
	V1beta1CSRAPICompatibility: {Default: false, PreRelease: featuregate.Alpha},
	ManagedClusterAutoApproval: {Default: false, PreRelease: featuregate.Alpha},
	ResourceCleanup:            {Default: true, PreRelease: featuregate.Beta},
	ClusterProfile:             {Default: false, PreRelease: featuregate.Alpha},
	ClusterImporter:            {Default: false, PreRelease: featuregate.Alpha},
}

var DefaultHubAddonManagerFeatureGates = map[featuregate.Feature]featuregate.FeatureSpec{
	AddonManagement: {Default: true, PreRelease: featuregate.Beta},
}

// DefaultHubWorkFeatureGates consists of all known acm work wehbook feature keys.
// To add a new feature, define a key for it above and add it here.
var DefaultHubWorkFeatureGates = map[featuregate.Feature]featuregate.FeatureSpec{
	NilExecutorValidating:  {Default: false, PreRelease: featuregate.Alpha},
	ManifestWorkReplicaSet: {Default: false, PreRelease: featuregate.Alpha},
	CloudEventsDrivers:     {Default: false, PreRelease: featuregate.Alpha},
}

// DefaultSpokeWorkFeatureGates consists of all known ocm work feature keys for work agent.
// To add a new feature, define a key for it above and add it here.
var DefaultSpokeWorkFeatureGates = map[featuregate.Feature]featuregate.FeatureSpec{
	ExecutorValidatingCaches: {Default: false, PreRelease: featuregate.Alpha},
	RawFeedbackJsonString:    {Default: false, PreRelease: featuregate.Alpha},
}
