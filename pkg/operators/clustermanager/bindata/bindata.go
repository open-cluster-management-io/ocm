// Code generated for package bindata by go-bindata DO NOT EDIT. (@generated)
// sources:
// manifests/cluster-manager/0000_00_addon.open-cluster-management.io_clustermanagementaddons.crd.yaml
// manifests/cluster-manager/0000_00_clusters.open-cluster-management.io_managedclusters.crd.yaml
// manifests/cluster-manager/0000_00_clusters.open-cluster-management.io_managedclustersets.crd.yaml
// manifests/cluster-manager/0000_00_work.open-cluster-management.io_manifestworks.crd.yaml
// manifests/cluster-manager/0000_01_addon.open-cluster-management.io_managedclusteraddons.crd.yaml
// manifests/cluster-manager/0000_01_clusters.open-cluster-management.io_managedclustersetbindings.crd.yaml
// manifests/cluster-manager/0000_03_clusters.open-cluster-management.io_placements.crd.yaml
// manifests/cluster-manager/0000_04_clusters.open-cluster-management.io_placementdecisions.crd.yaml
// manifests/cluster-manager/cluster-manager-namespace.yaml
// manifests/cluster-manager/cluster-manager-placement-clusterrole.yaml
// manifests/cluster-manager/cluster-manager-placement-clusterrolebinding.yaml
// manifests/cluster-manager/cluster-manager-placement-deployment.yaml
// manifests/cluster-manager/cluster-manager-placement-serviceaccount.yaml
// manifests/cluster-manager/cluster-manager-registration-clusterrole.yaml
// manifests/cluster-manager/cluster-manager-registration-clusterrolebinding.yaml
// manifests/cluster-manager/cluster-manager-registration-deployment.yaml
// manifests/cluster-manager/cluster-manager-registration-serviceaccount.yaml
// manifests/cluster-manager/cluster-manager-registration-webhook-apiservice.yaml
// manifests/cluster-manager/cluster-manager-registration-webhook-clusterrole.yaml
// manifests/cluster-manager/cluster-manager-registration-webhook-clusterrolebinding.yaml
// manifests/cluster-manager/cluster-manager-registration-webhook-clustersetbinding-validatingconfiguration.yaml
// manifests/cluster-manager/cluster-manager-registration-webhook-deployment.yaml
// manifests/cluster-manager/cluster-manager-registration-webhook-mutatingconfiguration.yaml
// manifests/cluster-manager/cluster-manager-registration-webhook-service.yaml
// manifests/cluster-manager/cluster-manager-registration-webhook-serviceaccount.yaml
// manifests/cluster-manager/cluster-manager-registration-webhook-validatingconfiguration.yaml
// manifests/cluster-manager/cluster-manager-work-webhook-apiservice.yaml
// manifests/cluster-manager/cluster-manager-work-webhook-clusterrole.yaml
// manifests/cluster-manager/cluster-manager-work-webhook-clusterrolebinding.yaml
// manifests/cluster-manager/cluster-manager-work-webhook-deployment.yaml
// manifests/cluster-manager/cluster-manager-work-webhook-service.yaml
// manifests/cluster-manager/cluster-manager-work-webhook-serviceaccount.yaml
// manifests/cluster-manager/cluster-manager-work-webhook-validatingconfiguration.yaml
package bindata

import (
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type asset struct {
	bytes []byte
	info  os.FileInfo
}

type bindataFileInfo struct {
	name    string
	size    int64
	mode    os.FileMode
	modTime time.Time
}

// Name return file name
func (fi bindataFileInfo) Name() string {
	return fi.name
}

// Size return file size
func (fi bindataFileInfo) Size() int64 {
	return fi.size
}

// Mode return file mode
func (fi bindataFileInfo) Mode() os.FileMode {
	return fi.mode
}

// Mode return file modify time
func (fi bindataFileInfo) ModTime() time.Time {
	return fi.modTime
}

// IsDir return file whether a directory
func (fi bindataFileInfo) IsDir() bool {
	return fi.mode&os.ModeDir != 0
}

// Sys return file is sys mode
func (fi bindataFileInfo) Sys() interface{} {
	return nil
}

var _manifestsClusterManager0000_00_addonOpenClusterManagementIo_clustermanagementaddonsCrdYaml = []byte(`apiVersion: apiextensions.k8s.io/v1
kind: CustomResourceDefinition
metadata:
  name: clustermanagementaddons.addon.open-cluster-management.io
spec:
  group: addon.open-cluster-management.io
  names:
    kind: ClusterManagementAddOn
    listKind: ClusterManagementAddOnList
    plural: clustermanagementaddons
    singular: clustermanagementaddon
  scope: Cluster
  preserveUnknownFields: false
  versions:
  - additionalPrinterColumns:
    - jsonPath: .spec.addOnMeta.displayName
      name: DISPLAY NAME
      type: string
    - jsonPath: .spec.addOnConfiguration.crdName
      name: CRD NAME
      type: string
    name: v1alpha1
    schema:
      openAPIV3Schema:
        description: ClusterManagementAddOn represents the registration of an add-on
          to the cluster manager. This resource allows the user to discover which
          add-on is available for the cluster manager and also provides metadata information
          about the add-on. This resource also provides a linkage to ManagedClusterAddOn,
          the name of the ClusterManagementAddOn resource will be used for the namespace-scoped
          ManagedClusterAddOn resource. ClusterManagementAddOn is a cluster-scoped
          resource.
        type: object
        properties:
          apiVersion:
            description: 'APIVersion defines the versioned schema of this representation
              of an object. Servers should convert recognized schemas to the latest
              internal value, and may reject unrecognized values. More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#resources'
            type: string
          kind:
            description: 'Kind is a string value representing the REST resource this
              object represents. Servers may infer this from the endpoint the client
              submits requests to. Cannot be updated. In CamelCase. More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#types-kinds'
            type: string
          metadata:
            type: object
          spec:
            description: spec represents a desired configuration for the agent on
              the cluster management add-on.
            type: object
            properties:
              addOnConfiguration:
                description: addOnConfiguration is a reference to configuration information
                  for the add-on. In scenario where a multiple add-ons share the same
                  add-on CRD, multiple ClusterManagementAddOn resources need to be
                  created and reference the same AddOnConfiguration.
                type: object
                properties:
                  crName:
                    description: crName is the name of the CR used to configure instances
                      of the managed add-on. This field should be configured if add-on
                      CR have a consistent name across the all of the ManagedCluster
                      instaces.
                    type: string
                  crdName:
                    description: crdName is the name of the CRD used to configure
                      instances of the managed add-on. This field should be configured
                      if the add-on have a CRD that controls the configuration of
                      the add-on.
                    type: string
              addOnMeta:
                description: addOnMeta is a reference to the metadata information
                  for the add-on.
                type: object
                properties:
                  description:
                    description: description represents the detailed description of
                      the add-on.
                    type: string
                  displayName:
                    description: displayName represents the name of add-on that will
                      be displayed.
                    type: string
          status:
            description: status represents the current status of cluster management
              add-on.
            type: object
    served: true
    storage: true
    subresources:
      status: {}
status:
  acceptedNames:
    kind: ""
    plural: ""
  conditions: []
  storedVersions: []
`)

func manifestsClusterManager0000_00_addonOpenClusterManagementIo_clustermanagementaddonsCrdYamlBytes() ([]byte, error) {
	return _manifestsClusterManager0000_00_addonOpenClusterManagementIo_clustermanagementaddonsCrdYaml, nil
}

func manifestsClusterManager0000_00_addonOpenClusterManagementIo_clustermanagementaddonsCrdYaml() (*asset, error) {
	bytes, err := manifestsClusterManager0000_00_addonOpenClusterManagementIo_clustermanagementaddonsCrdYamlBytes()
	if err != nil {
		return nil, err
	}

	info := bindataFileInfo{name: "manifests/cluster-manager/0000_00_addon.open-cluster-management.io_clustermanagementaddons.crd.yaml", size: 0, mode: os.FileMode(0), modTime: time.Unix(0, 0)}
	a := &asset{bytes: bytes, info: info}
	return a, nil
}

var _manifestsClusterManager0000_00_clustersOpenClusterManagementIo_managedclustersCrdYaml = []byte(`apiVersion: apiextensions.k8s.io/v1
kind: CustomResourceDefinition
metadata:
  name: managedclusters.cluster.open-cluster-management.io
spec:
  group: cluster.open-cluster-management.io
  names:
    kind: ManagedCluster
    listKind: ManagedClusterList
    plural: managedclusters
    shortNames:
    - mcl
    - mcls
    singular: managedcluster
  scope: Cluster
  preserveUnknownFields: false
  versions:
  - additionalPrinterColumns:
    - jsonPath: .spec.hubAcceptsClient
      name: Hub Accepted
      type: boolean
    - jsonPath: .spec.managedClusterClientConfigs[*].url
      name: Managed Cluster URLs
      type: string
    - jsonPath: .status.conditions[?(@.type=="ManagedClusterJoined")].status
      name: Joined
      type: string
    - jsonPath: .status.conditions[?(@.type=="ManagedClusterConditionAvailable")].status
      name: Available
      type: string
    - jsonPath: .metadata.creationTimestamp
      name: Age
      type: date
    name: v1
    schema:
      openAPIV3Schema:
        description: "ManagedCluster represents the desired state and current status
          of managed cluster. ManagedCluster is a cluster scoped resource. The name
          is the cluster UID. \n The cluster join process follows a double opt-in
          process: \n 1. Agent on managed cluster creates CSR on hub with cluster
          UID and agent name. 2. Agent on managed cluster creates ManagedCluster on
          hub. 3. Cluster admin on hub approves the CSR for UID and agent name of
          the ManagedCluster. 4. Cluster admin sets spec.acceptClient of ManagedCluster
          to true. 5. Cluster admin on managed cluster creates credential of kubeconfig
          to hub. \n Once the hub creates the cluster namespace, the Klusterlet agent
          on the ManagedCluster pushes the credential to the hub to use against the
          kube-apiserver of the ManagedCluster."
        type: object
        properties:
          apiVersion:
            description: 'APIVersion defines the versioned schema of this representation
              of an object. Servers should convert recognized schemas to the latest
              internal value, and may reject unrecognized values. More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#resources'
            type: string
          kind:
            description: 'Kind is a string value representing the REST resource this
              object represents. Servers may infer this from the endpoint the client
              submits requests to. Cannot be updated. In CamelCase. More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#types-kinds'
            type: string
          metadata:
            type: object
          spec:
            description: Spec represents a desired configuration for the agent on
              the managed cluster.
            type: object
            properties:
              hubAcceptsClient:
                description: hubAcceptsClient represents that hub accepts the joining
                  of Klusterlet agent on the managed cluster with the hub. The default
                  value is false, and can only be set true when the user on hub has
                  an RBAC rule to UPDATE on the virtual subresource of managedclusters/accept.
                  When the value is set true, a namespace whose name is the same as
                  the name of ManagedCluster is created on the hub. This namespace
                  represents the managed cluster, also role/rolebinding is created
                  on the namespace to grant the permision of access from the agent
                  on the managed cluster. When the value is set to false, the namespace
                  representing the managed cluster is deleted.
                type: boolean
              leaseDurationSeconds:
                description: LeaseDurationSeconds is used to coordinate the lease
                  update time of Klusterlet agents on the managed cluster. If its
                  value is zero, the Klusterlet agent will update its lease every
                  60 seconds by default
                type: integer
                format: int32
              managedClusterClientConfigs:
                description: ManagedClusterClientConfigs represents a list of the
                  apiserver address of the managed cluster. If it is empty, the managed
                  cluster has no accessible address for the hub to connect with it.
                type: array
                items:
                  description: ClientConfig represents the apiserver address of the
                    managed cluster. TODO include credential to connect to managed
                    cluster kube-apiserver
                  type: object
                  properties:
                    caBundle:
                      description: CABundle is the ca bundle to connect to apiserver
                        of the managed cluster. System certs are used if it is not
                        set.
                      type: string
                      format: byte
                    url:
                      description: URL is the URL of apiserver endpoint of the managed
                        cluster.
                      type: string
          status:
            description: Status represents the current status of joined managed cluster
            type: object
            properties:
              allocatable:
                description: Allocatable represents the total allocatable resources
                  on the managed cluster.
                type: object
                additionalProperties:
                  pattern: ^(\+|-)?(([0-9]+(\.[0-9]*)?)|(\.[0-9]+))(([KMGTPE]i)|[numkMGTPE]|([eE](\+|-)?(([0-9]+(\.[0-9]*)?)|(\.[0-9]+))))?$
                  anyOf:
                  - type: integer
                  - type: string
                  x-kubernetes-int-or-string: true
              capacity:
                description: Capacity represents the total resource capacity from
                  all nodeStatuses on the managed cluster.
                type: object
                additionalProperties:
                  pattern: ^(\+|-)?(([0-9]+(\.[0-9]*)?)|(\.[0-9]+))(([KMGTPE]i)|[numkMGTPE]|([eE](\+|-)?(([0-9]+(\.[0-9]*)?)|(\.[0-9]+))))?$
                  anyOf:
                  - type: integer
                  - type: string
                  x-kubernetes-int-or-string: true
              clusterClaims:
                description: ClusterClaims represents cluster information that a managed
                  cluster claims, for example a unique cluster identifier (id.k8s.io)
                  and kubernetes version (kubeversion.open-cluster-management.io).
                  They are written from the managed cluster. The set of claims is
                  not uniform across a fleet, some claims can be vendor or version
                  specific and may not be included from all managed clusters.
                type: array
                items:
                  description: ManagedClusterClaim represents a ClusterClaim collected
                    from a managed cluster.
                  type: object
                  properties:
                    name:
                      description: Name is the name of a ClusterClaim resource on
                        managed cluster. It's a well known or customized name to identify
                        the claim.
                      type: string
                      maxLength: 253
                      minLength: 1
                    value:
                      description: Value is a claim-dependent string
                      type: string
                      maxLength: 1024
                      minLength: 1
              conditions:
                description: Conditions contains the different condition statuses
                  for this managed cluster.
                type: array
                items:
                  description: "Condition contains details for one aspect of the current
                    state of this API Resource. --- This struct is intended for direct
                    use as an array at the field path .status.conditions.  For example,
                    type FooStatus struct{     // Represents the observations of a
                    foo's current state.     // Known .status.conditions.type are:
                    \"Available\", \"Progressing\", and \"Degraded\"     // +patchMergeKey=type
                    \    // +patchStrategy=merge     // +listType=map     // +listMapKey=type
                    \    Conditions []metav1.Condition ` + "`" + `json:\"conditions,omitempty\"
                    patchStrategy:\"merge\" patchMergeKey:\"type\" protobuf:\"bytes,1,rep,name=conditions\"` + "`" + `
                    \n     // other fields }"
                  type: object
                  required:
                  - lastTransitionTime
                  - message
                  - reason
                  - status
                  - type
                  properties:
                    lastTransitionTime:
                      description: lastTransitionTime is the last time the condition
                        transitioned from one status to another. This should be when
                        the underlying condition changed.  If that is not known, then
                        using the time when the API field changed is acceptable.
                      type: string
                      format: date-time
                    message:
                      description: message is a human readable message indicating
                        details about the transition. This may be an empty string.
                      type: string
                      maxLength: 32768
                    observedGeneration:
                      description: observedGeneration represents the .metadata.generation
                        that the condition was set based upon. For instance, if .metadata.generation
                        is currently 12, but the .status.conditions[x].observedGeneration
                        is 9, the condition is out of date with respect to the current
                        state of the instance.
                      type: integer
                      format: int64
                      minimum: 0
                    reason:
                      description: reason contains a programmatic identifier indicating
                        the reason for the condition's last transition. Producers
                        of specific condition types may define expected values and
                        meanings for this field, and whether the values are considered
                        a guaranteed API. The value should be a CamelCase string.
                        This field may not be empty.
                      type: string
                      maxLength: 1024
                      minLength: 1
                      pattern: ^[A-Za-z]([A-Za-z0-9_,:]*[A-Za-z0-9_])?$
                    status:
                      description: status of the condition, one of True, False, Unknown.
                      type: string
                      enum:
                      - "True"
                      - "False"
                      - Unknown
                    type:
                      description: type of condition in CamelCase or in foo.example.com/CamelCase.
                        --- Many .condition.type values are consistent across resources
                        like Available, but because arbitrary conditions can be useful
                        (see .node.status.conditions), the ability to deconflict is
                        important. The regex it matches is (dns1123SubdomainFmt/)?(qualifiedNameFmt)
                      type: string
                      maxLength: 316
                      pattern: ^([a-z0-9]([-a-z0-9]*[a-z0-9])?(\.[a-z0-9]([-a-z0-9]*[a-z0-9])?)*/)?(([A-Za-z0-9][-A-Za-z0-9_.]*)?[A-Za-z0-9])$
              version:
                description: Version represents the kubernetes version of the managed
                  cluster.
                type: object
                properties:
                  kubernetes:
                    description: Kubernetes is the kubernetes version of managed cluster.
                    type: string
    served: true
    storage: true
    subresources:
      status: {}
status:
  acceptedNames:
    kind: ""
    plural: ""
  conditions: []
  storedVersions: []
`)

func manifestsClusterManager0000_00_clustersOpenClusterManagementIo_managedclustersCrdYamlBytes() ([]byte, error) {
	return _manifestsClusterManager0000_00_clustersOpenClusterManagementIo_managedclustersCrdYaml, nil
}

func manifestsClusterManager0000_00_clustersOpenClusterManagementIo_managedclustersCrdYaml() (*asset, error) {
	bytes, err := manifestsClusterManager0000_00_clustersOpenClusterManagementIo_managedclustersCrdYamlBytes()
	if err != nil {
		return nil, err
	}

	info := bindataFileInfo{name: "manifests/cluster-manager/0000_00_clusters.open-cluster-management.io_managedclusters.crd.yaml", size: 0, mode: os.FileMode(0), modTime: time.Unix(0, 0)}
	a := &asset{bytes: bytes, info: info}
	return a, nil
}

var _manifestsClusterManager0000_00_clustersOpenClusterManagementIo_managedclustersetsCrdYaml = []byte(`apiVersion: apiextensions.k8s.io/v1
kind: CustomResourceDefinition
metadata:
  name: managedclustersets.cluster.open-cluster-management.io
spec:
  group: cluster.open-cluster-management.io
  names:
    kind: ManagedClusterSet
    listKind: ManagedClusterSetList
    plural: managedclustersets
    shortNames:
    - mclset
    - mclsets
    singular: managedclusterset
  scope: Cluster
  preserveUnknownFields: false
  versions:
  - name: v1alpha1
    schema:
      openAPIV3Schema:
        description: "ManagedClusterSet defines a group of ManagedClusters that user's
          workload can run on. A workload can be defined to deployed on a ManagedClusterSet,
          which mean:   1. The workload can run on any ManagedCluster in the ManagedClusterSet
          \  2. The workload cannot run on any ManagedCluster outside the ManagedClusterSet
          \  3. The service exposed by the workload can be shared in any ManagedCluster
          in the ManagedClusterSet \n In order to assign a ManagedCluster to a certian
          ManagedClusterSet, add a label with name ` + "`" + `cluster.open-cluster-management.io/clusterset` + "`" + `
          on the ManagedCluster to refers to the ManagedClusterSet. User is not allow
          to add/remove this label on a ManagedCluster unless they have a RBAC rule
          to CREATE on a virtual subresource of managedclustersets/join. In order
          to update this label, user must have the permission on both the old and
          new ManagedClusterSet."
        type: object
        properties:
          apiVersion:
            description: 'APIVersion defines the versioned schema of this representation
              of an object. Servers should convert recognized schemas to the latest
              internal value, and may reject unrecognized values. More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#resources'
            type: string
          kind:
            description: 'Kind is a string value representing the REST resource this
              object represents. Servers may infer this from the endpoint the client
              submits requests to. Cannot be updated. In CamelCase. More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#types-kinds'
            type: string
          metadata:
            type: object
          spec:
            description: Spec defines the attributes of the ManagedClusterSet
            type: object
          status:
            description: Status represents the current status of the ManagedClusterSet
            type: object
            properties:
              conditions:
                description: Conditions contains the different condition statuses
                  for this ManagedClusterSet.
                type: array
                items:
                  description: "Condition contains details for one aspect of the current
                    state of this API Resource. --- This struct is intended for direct
                    use as an array at the field path .status.conditions.  For example,
                    type FooStatus struct{     // Represents the observations of a
                    foo's current state.     // Known .status.conditions.type are:
                    \"Available\", \"Progressing\", and \"Degraded\"     // +patchMergeKey=type
                    \    // +patchStrategy=merge     // +listType=map     // +listMapKey=type
                    \    Conditions []metav1.Condition ` + "`" + `json:\"conditions,omitempty\"
                    patchStrategy:\"merge\" patchMergeKey:\"type\" protobuf:\"bytes,1,rep,name=conditions\"` + "`" + `
                    \n     // other fields }"
                  type: object
                  required:
                  - lastTransitionTime
                  - message
                  - reason
                  - status
                  - type
                  properties:
                    lastTransitionTime:
                      description: lastTransitionTime is the last time the condition
                        transitioned from one status to another. This should be when
                        the underlying condition changed.  If that is not known, then
                        using the time when the API field changed is acceptable.
                      type: string
                      format: date-time
                    message:
                      description: message is a human readable message indicating
                        details about the transition. This may be an empty string.
                      type: string
                      maxLength: 32768
                    observedGeneration:
                      description: observedGeneration represents the .metadata.generation
                        that the condition was set based upon. For instance, if .metadata.generation
                        is currently 12, but the .status.conditions[x].observedGeneration
                        is 9, the condition is out of date with respect to the current
                        state of the instance.
                      type: integer
                      format: int64
                      minimum: 0
                    reason:
                      description: reason contains a programmatic identifier indicating
                        the reason for the condition's last transition. Producers
                        of specific condition types may define expected values and
                        meanings for this field, and whether the values are considered
                        a guaranteed API. The value should be a CamelCase string.
                        This field may not be empty.
                      type: string
                      maxLength: 1024
                      minLength: 1
                      pattern: ^[A-Za-z]([A-Za-z0-9_,:]*[A-Za-z0-9_])?$
                    status:
                      description: status of the condition, one of True, False, Unknown.
                      type: string
                      enum:
                      - "True"
                      - "False"
                      - Unknown
                    type:
                      description: type of condition in CamelCase or in foo.example.com/CamelCase.
                        --- Many .condition.type values are consistent across resources
                        like Available, but because arbitrary conditions can be useful
                        (see .node.status.conditions), the ability to deconflict is
                        important. The regex it matches is (dns1123SubdomainFmt/)?(qualifiedNameFmt)
                      type: string
                      maxLength: 316
                      pattern: ^([a-z0-9]([-a-z0-9]*[a-z0-9])?(\.[a-z0-9]([-a-z0-9]*[a-z0-9])?)*/)?(([A-Za-z0-9][-A-Za-z0-9_.]*)?[A-Za-z0-9])$
    served: true
    storage: true
    subresources:
      status: {}
status:
  acceptedNames:
    kind: ""
    plural: ""
  conditions: []
  storedVersions: []
`)

func manifestsClusterManager0000_00_clustersOpenClusterManagementIo_managedclustersetsCrdYamlBytes() ([]byte, error) {
	return _manifestsClusterManager0000_00_clustersOpenClusterManagementIo_managedclustersetsCrdYaml, nil
}

func manifestsClusterManager0000_00_clustersOpenClusterManagementIo_managedclustersetsCrdYaml() (*asset, error) {
	bytes, err := manifestsClusterManager0000_00_clustersOpenClusterManagementIo_managedclustersetsCrdYamlBytes()
	if err != nil {
		return nil, err
	}

	info := bindataFileInfo{name: "manifests/cluster-manager/0000_00_clusters.open-cluster-management.io_managedclustersets.crd.yaml", size: 0, mode: os.FileMode(0), modTime: time.Unix(0, 0)}
	a := &asset{bytes: bytes, info: info}
	return a, nil
}

var _manifestsClusterManager0000_00_workOpenClusterManagementIo_manifestworksCrdYaml = []byte(`apiVersion: apiextensions.k8s.io/v1
kind: CustomResourceDefinition
metadata:
  name: manifestworks.work.open-cluster-management.io
spec:
  group: work.open-cluster-management.io
  names:
    kind: ManifestWork
    listKind: ManifestWorkList
    plural: manifestworks
    singular: manifestwork
  scope: Namespaced
  preserveUnknownFields: false
  versions:
  - name: v1
    schema:
      openAPIV3Schema:
        description: ManifestWork represents a manifests workload that hub wants to
          deploy on the managed cluster. A manifest workload is defined as a set of
          Kubernetes resources. ManifestWork must be created in the cluster namespace
          on the hub, so that agent on the corresponding managed cluster can access
          this resource and deploy on the managed cluster.
        type: object
        properties:
          apiVersion:
            description: 'APIVersion defines the versioned schema of this representation
              of an object. Servers should convert recognized schemas to the latest
              internal value, and may reject unrecognized values. More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#resources'
            type: string
          kind:
            description: 'Kind is a string value representing the REST resource this
              object represents. Servers may infer this from the endpoint the client
              submits requests to. Cannot be updated. In CamelCase. More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#types-kinds'
            type: string
          metadata:
            type: object
          spec:
            description: Spec represents a desired configuration of work to be deployed
              on the managed cluster.
            type: object
            properties:
              workload:
                description: Workload represents the manifest workload to be deployed
                  on a managed cluster.
                type: object
                properties:
                  manifests:
                    description: Manifests represents a list of kuberenetes resources
                      to be deployed on a managed cluster.
                    type: array
                    items:
                      description: Manifest represents a resource to be deployed on
                        managed cluster.
                      type: object
                      x-kubernetes-preserve-unknown-fields: true
                      x-kubernetes-embedded-resource: true
          status:
            description: Status represents the current status of work.
            type: object
            properties:
              conditions:
                description: 'Conditions contains the different condition statuses
                  for this work. Valid condition types are: 1. Applied represents
                  workload in ManifestWork is applied successfully on managed cluster.
                  2. Progressing represents workload in ManifestWork is being applied
                  on managed cluster. 3. Available represents workload in ManifestWork
                  exists on the managed cluster. 4. Degraded represents the current
                  state of workload does not match the desired state for a certain
                  period.'
                type: array
                items:
                  description: "Condition contains details for one aspect of the current
                    state of this API Resource. --- This struct is intended for direct
                    use as an array at the field path .status.conditions.  For example,
                    type FooStatus struct{     // Represents the observations of a
                    foo's current state.     // Known .status.conditions.type are:
                    \"Available\", \"Progressing\", and \"Degraded\"     // +patchMergeKey=type
                    \    // +patchStrategy=merge     // +listType=map     // +listMapKey=type
                    \    Conditions []metav1.Condition ` + "`" + `json:\"conditions,omitempty\"
                    patchStrategy:\"merge\" patchMergeKey:\"type\" protobuf:\"bytes,1,rep,name=conditions\"` + "`" + `
                    \n     // other fields }"
                  type: object
                  required:
                  - lastTransitionTime
                  - message
                  - reason
                  - status
                  - type
                  properties:
                    lastTransitionTime:
                      description: lastTransitionTime is the last time the condition
                        transitioned from one status to another. This should be when
                        the underlying condition changed.  If that is not known, then
                        using the time when the API field changed is acceptable.
                      type: string
                      format: date-time
                    message:
                      description: message is a human readable message indicating
                        details about the transition. This may be an empty string.
                      type: string
                      maxLength: 32768
                    observedGeneration:
                      description: observedGeneration represents the .metadata.generation
                        that the condition was set based upon. For instance, if .metadata.generation
                        is currently 12, but the .status.conditions[x].observedGeneration
                        is 9, the condition is out of date with respect to the current
                        state of the instance.
                      type: integer
                      format: int64
                      minimum: 0
                    reason:
                      description: reason contains a programmatic identifier indicating
                        the reason for the condition's last transition. Producers
                        of specific condition types may define expected values and
                        meanings for this field, and whether the values are considered
                        a guaranteed API. The value should be a CamelCase string.
                        This field may not be empty.
                      type: string
                      maxLength: 1024
                      minLength: 1
                      pattern: ^[A-Za-z]([A-Za-z0-9_,:]*[A-Za-z0-9_])?$
                    status:
                      description: status of the condition, one of True, False, Unknown.
                      type: string
                      enum:
                      - "True"
                      - "False"
                      - Unknown
                    type:
                      description: type of condition in CamelCase or in foo.example.com/CamelCase.
                        --- Many .condition.type values are consistent across resources
                        like Available, but because arbitrary conditions can be useful
                        (see .node.status.conditions), the ability to deconflict is
                        important. The regex it matches is (dns1123SubdomainFmt/)?(qualifiedNameFmt)
                      type: string
                      maxLength: 316
                      pattern: ^([a-z0-9]([-a-z0-9]*[a-z0-9])?(\.[a-z0-9]([-a-z0-9]*[a-z0-9])?)*/)?(([A-Za-z0-9][-A-Za-z0-9_.]*)?[A-Za-z0-9])$
              resourceStatus:
                description: ResourceStatus represents the status of each resource
                  in manifestwork deployed on a managed cluster. The Klusterlet agent
                  on managed cluster syncs the condition from the managed cluster
                  to the hub.
                type: object
                properties:
                  manifests:
                    description: 'Manifests represents the condition of manifests
                      deployed on managed cluster. Valid condition types are: 1. Progressing
                      represents the resource is being applied on managed cluster.
                      2. Applied represents the resource is applied successfully on
                      managed cluster. 3. Available represents the resource exists
                      on the managed cluster. 4. Degraded represents the current state
                      of resource does not match the desired state for a certain period.'
                    type: array
                    items:
                      description: ManifestCondition represents the conditions of
                        the resources deployed on a managed cluster.
                      type: object
                      properties:
                        conditions:
                          description: Conditions represents the conditions of this
                            resource on a managed cluster.
                          type: array
                          items:
                            description: "Condition contains details for one aspect
                              of the current state of this API Resource. --- This
                              struct is intended for direct use as an array at the
                              field path .status.conditions.  For example, type FooStatus
                              struct{     // Represents the observations of a foo's
                              current state.     // Known .status.conditions.type
                              are: \"Available\", \"Progressing\", and \"Degraded\"
                              \    // +patchMergeKey=type     // +patchStrategy=merge
                              \    // +listType=map     // +listMapKey=type     Conditions
                              []metav1.Condition ` + "`" + `json:\"conditions,omitempty\" patchStrategy:\"merge\"
                              patchMergeKey:\"type\" protobuf:\"bytes,1,rep,name=conditions\"` + "`" + `
                              \n     // other fields }"
                            type: object
                            required:
                            - lastTransitionTime
                            - message
                            - reason
                            - status
                            - type
                            properties:
                              lastTransitionTime:
                                description: lastTransitionTime is the last time the
                                  condition transitioned from one status to another.
                                  This should be when the underlying condition changed.  If
                                  that is not known, then using the time when the
                                  API field changed is acceptable.
                                type: string
                                format: date-time
                              message:
                                description: message is a human readable message indicating
                                  details about the transition. This may be an empty
                                  string.
                                type: string
                                maxLength: 32768
                              observedGeneration:
                                description: observedGeneration represents the .metadata.generation
                                  that the condition was set based upon. For instance,
                                  if .metadata.generation is currently 12, but the
                                  .status.conditions[x].observedGeneration is 9, the
                                  condition is out of date with respect to the current
                                  state of the instance.
                                type: integer
                                format: int64
                                minimum: 0
                              reason:
                                description: reason contains a programmatic identifier
                                  indicating the reason for the condition's last transition.
                                  Producers of specific condition types may define
                                  expected values and meanings for this field, and
                                  whether the values are considered a guaranteed API.
                                  The value should be a CamelCase string. This field
                                  may not be empty.
                                type: string
                                maxLength: 1024
                                minLength: 1
                                pattern: ^[A-Za-z]([A-Za-z0-9_,:]*[A-Za-z0-9_])?$
                              status:
                                description: status of the condition, one of True,
                                  False, Unknown.
                                type: string
                                enum:
                                - "True"
                                - "False"
                                - Unknown
                              type:
                                description: type of condition in CamelCase or in
                                  foo.example.com/CamelCase. --- Many .condition.type
                                  values are consistent across resources like Available,
                                  but because arbitrary conditions can be useful (see
                                  .node.status.conditions), the ability to deconflict
                                  is important. The regex it matches is (dns1123SubdomainFmt/)?(qualifiedNameFmt)
                                type: string
                                maxLength: 316
                                pattern: ^([a-z0-9]([-a-z0-9]*[a-z0-9])?(\.[a-z0-9]([-a-z0-9]*[a-z0-9])?)*/)?(([A-Za-z0-9][-A-Za-z0-9_.]*)?[A-Za-z0-9])$
                        resourceMeta:
                          description: ResourceMeta represents the group, version,
                            kind, name and namespace of a resoure.
                          type: object
                          properties:
                            group:
                              description: Group is the API Group of the Kubernetes
                                resource.
                              type: string
                            kind:
                              description: Kind is the kind of the Kubernetes resource.
                              type: string
                            name:
                              description: Name is the name of the Kubernetes resource.
                              type: string
                            namespace:
                              description: Name is the namespace of the Kubernetes
                                resource.
                              type: string
                            ordinal:
                              description: Ordinal represents the index of the manifest
                                on spec.
                              type: integer
                              format: int32
                            resource:
                              description: Resource is the resource name of the Kubernetes
                                resource.
                              type: string
                            version:
                              description: Version is the version of the Kubernetes
                                resource.
                              type: string
    served: true
    storage: true
    subresources:
      status: {}
status:
  acceptedNames:
    kind: ""
    plural: ""
  conditions: []
  storedVersions: []
`)

func manifestsClusterManager0000_00_workOpenClusterManagementIo_manifestworksCrdYamlBytes() ([]byte, error) {
	return _manifestsClusterManager0000_00_workOpenClusterManagementIo_manifestworksCrdYaml, nil
}

func manifestsClusterManager0000_00_workOpenClusterManagementIo_manifestworksCrdYaml() (*asset, error) {
	bytes, err := manifestsClusterManager0000_00_workOpenClusterManagementIo_manifestworksCrdYamlBytes()
	if err != nil {
		return nil, err
	}

	info := bindataFileInfo{name: "manifests/cluster-manager/0000_00_work.open-cluster-management.io_manifestworks.crd.yaml", size: 0, mode: os.FileMode(0), modTime: time.Unix(0, 0)}
	a := &asset{bytes: bytes, info: info}
	return a, nil
}

var _manifestsClusterManager0000_01_addonOpenClusterManagementIo_managedclusteraddonsCrdYaml = []byte(`apiVersion: apiextensions.k8s.io/v1
kind: CustomResourceDefinition
metadata:
  name: managedclusteraddons.addon.open-cluster-management.io
spec:
  group: addon.open-cluster-management.io
  names:
    kind: ManagedClusterAddOn
    listKind: ManagedClusterAddOnList
    plural: managedclusteraddons
    singular: managedclusteraddon
  scope: Namespaced
  preserveUnknownFields: false
  versions:
  - additionalPrinterColumns:
    - jsonPath: .status.conditions[?(@.type=="Available")].status
      name: Available
      type: string
    - jsonPath: .status.conditions[?(@.type=="Degraded")].status
      name: Degraded
      type: string
    - jsonPath: .status.conditions[?(@.type=="Progressing")].status
      name: Progressing
      type: string
    name: v1alpha1
    schema:
      openAPIV3Schema:
        description: ManagedClusterAddOn is the Custom Resource object which holds
          the current state of an add-on. This object is used by add-on operators
          to convey their state. This resource should be created in the ManagedCluster
          namespace.
        type: object
        required:
        - spec
        properties:
          apiVersion:
            description: 'APIVersion defines the versioned schema of this representation
              of an object. Servers should convert recognized schemas to the latest
              internal value, and may reject unrecognized values. More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#resources'
            type: string
          kind:
            description: 'Kind is a string value representing the REST resource this
              object represents. Servers may infer this from the endpoint the client
              submits requests to. Cannot be updated. In CamelCase. More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#types-kinds'
            type: string
          metadata:
            type: object
          spec:
            description: spec holds configuration that could apply to any operator.
            type: object
            properties:
              installNamespace:
                description: installNamespace is the namespace on the managed cluster
                  to install the addon agent. If it is not set, open-cluster-management-agent-addon
                  namespace is used to install the addon agent.
                type: string
                maxLength: 63
                pattern: ^[a-z0-9]([-a-z0-9]*[a-z0-9])?$
          status:
            description: status holds the information about the state of an operator.  It
              is consistent with status information across the Kubernetes ecosystem.
            type: object
            properties:
              addOnConfiguration:
                description: addOnConfiguration is a reference to configuration information
                  for the add-on. This resource is use to locate the configuration
                  resource for the add-on.
                type: object
                properties:
                  crName:
                    description: crName is the name of the CR used to configure instances
                      of the managed add-on. This field should be configured if add-on
                      CR have a consistent name across the all of the ManagedCluster
                      instaces.
                    type: string
                  crdName:
                    description: crdName is the name of the CRD used to configure
                      instances of the managed add-on. This field should be configured
                      if the add-on have a CRD that controls the configuration of
                      the add-on.
                    type: string
              addOnMeta:
                description: addOnMeta is a reference to the metadata information
                  for the add-on. This should be same as the addOnMeta for the corresponding
                  ClusterManagementAddOn resource.
                type: object
                properties:
                  description:
                    description: description represents the detailed description of
                      the add-on.
                    type: string
                  displayName:
                    description: displayName represents the name of add-on that will
                      be displayed.
                    type: string
              conditions:
                description: conditions describe the state of the managed and monitored
                  components for the operator.
                type: array
                items:
                  description: "Condition contains details for one aspect of the current
                    state of this API Resource. --- This struct is intended for direct
                    use as an array at the field path .status.conditions.  For example,
                    type FooStatus struct{     // Represents the observations of a
                    foo's current state.     // Known .status.conditions.type are:
                    \"Available\", \"Progressing\", and \"Degraded\"     // +patchMergeKey=type
                    \    // +patchStrategy=merge     // +listType=map     // +listMapKey=type
                    \    Conditions []metav1.Condition ` + "`" + `json:\"conditions,omitempty\"
                    patchStrategy:\"merge\" patchMergeKey:\"type\" protobuf:\"bytes,1,rep,name=conditions\"` + "`" + `
                    \n     // other fields }"
                  type: object
                  required:
                  - lastTransitionTime
                  - message
                  - reason
                  - status
                  - type
                  properties:
                    lastTransitionTime:
                      description: lastTransitionTime is the last time the condition
                        transitioned from one status to another. This should be when
                        the underlying condition changed.  If that is not known, then
                        using the time when the API field changed is acceptable.
                      type: string
                      format: date-time
                    message:
                      description: message is a human readable message indicating
                        details about the transition. This may be an empty string.
                      type: string
                      maxLength: 32768
                    observedGeneration:
                      description: observedGeneration represents the .metadata.generation
                        that the condition was set based upon. For instance, if .metadata.generation
                        is currently 12, but the .status.conditions[x].observedGeneration
                        is 9, the condition is out of date with respect to the current
                        state of the instance.
                      type: integer
                      format: int64
                      minimum: 0
                    reason:
                      description: reason contains a programmatic identifier indicating
                        the reason for the condition's last transition. Producers
                        of specific condition types may define expected values and
                        meanings for this field, and whether the values are considered
                        a guaranteed API. The value should be a CamelCase string.
                        This field may not be empty.
                      type: string
                      maxLength: 1024
                      minLength: 1
                      pattern: ^[A-Za-z]([A-Za-z0-9_,:]*[A-Za-z0-9_])?$
                    status:
                      description: status of the condition, one of True, False, Unknown.
                      type: string
                      enum:
                      - "True"
                      - "False"
                      - Unknown
                    type:
                      description: type of condition in CamelCase or in foo.example.com/CamelCase.
                        --- Many .condition.type values are consistent across resources
                        like Available, but because arbitrary conditions can be useful
                        (see .node.status.conditions), the ability to deconflict is
                        important. The regex it matches is (dns1123SubdomainFmt/)?(qualifiedNameFmt)
                      type: string
                      maxLength: 316
                      pattern: ^([a-z0-9]([-a-z0-9]*[a-z0-9])?(\.[a-z0-9]([-a-z0-9]*[a-z0-9])?)*/)?(([A-Za-z0-9][-A-Za-z0-9_.]*)?[A-Za-z0-9])$
              registrations:
                description: registrations is the conifigurations for the addon agent
                  to register to hub. It should be set by each addon controller on
                  hub to define how the addon agent on managedcluster is registered.
                  With the registration defined, The addon agent can access to kube
                  apiserver with kube style API or other endpoints on hub cluster
                  with client certificate authentication. A csr will be created per
                  registration configuration. If more than one registrationConfig
                  is defined, a csr will be created for each registration configuration.
                  It is not allowed that multiple registrationConfigs have the same
                  signer name. After the csr is approved on the hub cluster, the klusterlet
                  agent will create a secret in the installNamespace for the registrationConfig.
                  If the signerName is "kubernetes.io/kube-apiserver-client", the
                  secret name will be "{addon name}-hub-kubeconfig" whose contents
                  includes key/cert and kubeconfig. Otherwise, the secret name will
                  be "{addon name}-{signer name}-client-cert" whose contents includes
                  key/cert.
                type: array
                items:
                  description: RegistrationConfig defines the configuration of the
                    addon agent to register to hub. The Klusterlet agent will create
                    a csr for the addon agent with the registrationConfig.
                  type: object
                  properties:
                    signerName:
                      description: signerName is the name of signer that addon agent
                        will use to create csr.
                      type: string
                      maxLength: 571
                      minLength: 5
                    subject:
                      description: "subject is the user subject of the addon agent
                        to be registered to the hub. If it is not set, the addon agent
                        will have the default subject \"subject\": { \t\"user\": \"system:open-cluster-management:addon:{addonName}:{clusterName}:{agentName}\",
                        \t\"groups: [\"system:open-cluster-management:addon\", \"system:open-cluster-management:addon:{addonName}\",
                        \"system:authenticated\"] }"
                      type: object
                      properties:
                        groups:
                          description: groups is the user group of the addon agent.
                          type: array
                          items:
                            type: string
                        organizationUnit:
                          description: organizationUnit is the ou of the addon agent
                          type: array
                          items:
                            type: string
                        user:
                          description: user is the user name of the addon agent.
                          type: string
              relatedObjects:
                description: 'relatedObjects is a list of objects that are "interesting"
                  or related to this operator. Common uses are: 1. the detailed resource
                  driving the operator 2. operator namespaces 3. operand namespaces
                  4. related ClusterManagementAddon resource'
                type: array
                items:
                  description: ObjectReference contains enough information to let
                    you inspect or modify the referred object.
                  type: object
                  required:
                  - group
                  - name
                  - resource
                  properties:
                    group:
                      description: group of the referent.
                      type: string
                    name:
                      description: name of the referent.
                      type: string
                    resource:
                      description: resource of the referent.
                      type: string
    served: true
    storage: true
    subresources:
      status: {}
status:
  acceptedNames:
    kind: ""
    plural: ""
  conditions: []
  storedVersions: []
`)

func manifestsClusterManager0000_01_addonOpenClusterManagementIo_managedclusteraddonsCrdYamlBytes() ([]byte, error) {
	return _manifestsClusterManager0000_01_addonOpenClusterManagementIo_managedclusteraddonsCrdYaml, nil
}

func manifestsClusterManager0000_01_addonOpenClusterManagementIo_managedclusteraddonsCrdYaml() (*asset, error) {
	bytes, err := manifestsClusterManager0000_01_addonOpenClusterManagementIo_managedclusteraddonsCrdYamlBytes()
	if err != nil {
		return nil, err
	}

	info := bindataFileInfo{name: "manifests/cluster-manager/0000_01_addon.open-cluster-management.io_managedclusteraddons.crd.yaml", size: 0, mode: os.FileMode(0), modTime: time.Unix(0, 0)}
	a := &asset{bytes: bytes, info: info}
	return a, nil
}

var _manifestsClusterManager0000_01_clustersOpenClusterManagementIo_managedclustersetbindingsCrdYaml = []byte(`apiVersion: apiextensions.k8s.io/v1
kind: CustomResourceDefinition
metadata:
  name: managedclustersetbindings.cluster.open-cluster-management.io
spec:
  group: cluster.open-cluster-management.io
  names:
    kind: ManagedClusterSetBinding
    listKind: ManagedClusterSetBindingList
    plural: managedclustersetbindings
    shortNames:
    - mclsetbinding
    - mclsetbindings
    singular: managedclustersetbinding
  scope: Namespaced
  preserveUnknownFields: false
  versions:
  - name: v1alpha1
    schema:
      openAPIV3Schema:
        description: ManagedClusterSetBinding projects a ManagedClusterSet into a
          certain namespace. User is able to create a ManagedClusterSetBinding in
          a namespace and bind it to a ManagedClusterSet if they have an RBAC rule
          to CREATE on the virtual subresource of managedclustersets/bind. Workloads
          created in the same namespace can only be distributed to ManagedClusters
          in ManagedClusterSets bound in this namespace by higher level controllers.
        type: object
        properties:
          apiVersion:
            description: 'APIVersion defines the versioned schema of this representation
              of an object. Servers should convert recognized schemas to the latest
              internal value, and may reject unrecognized values. More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#resources'
            type: string
          kind:
            description: 'Kind is a string value representing the REST resource this
              object represents. Servers may infer this from the endpoint the client
              submits requests to. Cannot be updated. In CamelCase. More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#types-kinds'
            type: string
          metadata:
            type: object
          spec:
            description: Spec defines the attributes of ManagedClusterSetBinding.
            type: object
            properties:
              clusterSet:
                description: ClusterSet is the name of the ManagedClusterSet to bind.
                  It must match the instance name of the ManagedClusterSetBinding
                  and cannot change once created. User is allowed to set this field
                  if they have an RBAC rule to CREATE on the virtual subresource of
                  managedclustersets/bind.
                type: string
                minLength: 1
    served: true
    storage: true
status:
  acceptedNames:
    kind: ""
    plural: ""
  conditions: []
  storedVersions: []
`)

func manifestsClusterManager0000_01_clustersOpenClusterManagementIo_managedclustersetbindingsCrdYamlBytes() ([]byte, error) {
	return _manifestsClusterManager0000_01_clustersOpenClusterManagementIo_managedclustersetbindingsCrdYaml, nil
}

func manifestsClusterManager0000_01_clustersOpenClusterManagementIo_managedclustersetbindingsCrdYaml() (*asset, error) {
	bytes, err := manifestsClusterManager0000_01_clustersOpenClusterManagementIo_managedclustersetbindingsCrdYamlBytes()
	if err != nil {
		return nil, err
	}

	info := bindataFileInfo{name: "manifests/cluster-manager/0000_01_clusters.open-cluster-management.io_managedclustersetbindings.crd.yaml", size: 0, mode: os.FileMode(0), modTime: time.Unix(0, 0)}
	a := &asset{bytes: bytes, info: info}
	return a, nil
}

var _manifestsClusterManager0000_03_clustersOpenClusterManagementIo_placementsCrdYaml = []byte(`apiVersion: apiextensions.k8s.io/v1
kind: CustomResourceDefinition
metadata:
  name: placements.cluster.open-cluster-management.io
spec:
  group: cluster.open-cluster-management.io
  names:
    kind: Placement
    listKind: PlacementList
    plural: placements
    singular: placement
  scope: Namespaced
  preserveUnknownFields: false
  versions:
  - name: v1alpha1
    schema:
      openAPIV3Schema:
        description: "Placement defines a rule to select a set of ManagedClusters
          from the ManagedClusterSets bound to the placement namespace. \n Here is
          how the placement policy combines with other selection methods to determine
          a matching list of ManagedClusters: 1) Kubernetes clusters are registered
          with hub as cluster-scoped ManagedClusters; 2) ManagedClusters are organized
          into cluster-scoped ManagedClusterSets; 3) ManagedClusterSets are bound
          to workload namespaces; 4) Namespace-scoped Placements specify a slice of
          ManagedClusterSets which select a working set    of potential ManagedClusters;
          5) Then Placements subselect from that working set using label/claim selection.
          \n No ManagedCluster will be selected if no ManagedClusterSet is bound to
          the placement namespace. User is able to bind a ManagedClusterSet to a namespace
          by creating a ManagedClusterSetBinding in that namespace if they have a
          RBAC rule to CREATE on the virtual subresource of ` + "`" + `managedclustersets/bind` + "`" + `.
          \n A slice of PlacementDecisions with label cluster.open-cluster-management.io/placement={placement
          name} will be created to represent the ManagedClusters selected by this
          placement. \n If a ManagedCluster is selected and added into the PlacementDecisions,
          other components may apply workload on it; once it is removed from the PlacementDecisions,
          the workload applied on this ManagedCluster should be evicted accordingly."
        type: object
        required:
        - spec
        properties:
          apiVersion:
            description: 'APIVersion defines the versioned schema of this representation
              of an object. Servers should convert recognized schemas to the latest
              internal value, and may reject unrecognized values. More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#resources'
            type: string
          kind:
            description: 'Kind is a string value representing the REST resource this
              object represents. Servers may infer this from the endpoint the client
              submits requests to. Cannot be updated. In CamelCase. More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#types-kinds'
            type: string
          metadata:
            type: object
          spec:
            description: Spec defines the attributes of Placement.
            type: object
            properties:
              clusterSets:
                description: ClusterSets represent the ManagedClusterSets from which
                  the ManagedClusters are selected. If the slice is empty, ManagedClusters
                  will be selected from the ManagedClusterSets bound to the placement
                  namespace, otherwise ManagedClusters will be selected from the intersection
                  of this slice and the ManagedClusterSets bound to the placement
                  namespace.
                type: array
                items:
                  type: string
              numberOfClusters:
                description: NumberOfClusters represents the desired number of ManagedClusters
                  to be selected which meet the placement requirements. 1) If not
                  specified, all ManagedClusters which meet the placement requirements
                  (including ClusterSets,    and Predicates) will be selected; 2)
                  Otherwise if the nubmer of ManagedClusters meet the placement requirements
                  is larger than    NumberOfClusters, a random subset with desired
                  number of ManagedClusters will be selected; 3) If the nubmer of
                  ManagedClusters meet the placement requirements is equal to NumberOfClusters,    all
                  of them will be selected; 4) If the nubmer of ManagedClusters meet
                  the placement requirements is less than NumberOfClusters,    all
                  of them will be selected, and the status of condition ` + "`" + `PlacementConditionSatisfied` + "`" + `
                  will be    set to false;
                type: integer
                format: int32
              predicates:
                description: Predicates represent a slice of predicates to select
                  ManagedClusters. The predicates are ORed.
                type: array
                items:
                  description: ClusterPredicate represents a predicate to select ManagedClusters.
                  type: object
                  properties:
                    requiredClusterSelector:
                      description: RequiredClusterSelector represents a selector of
                        ManagedClusters by label and claim. If specified, 1) Any ManagedCluster,
                        which does not match the selector, should not be selected
                        by this ClusterPredicate; 2) If a selected ManagedCluster
                        (of this ClusterPredicate) ceases to match the selector (e.g.
                        due to    an update) of any ClusterPredicate, it will be eventually
                        removed from the placement decisions; 3) If a ManagedCluster
                        (not selected previously) starts to match the selector, it
                        will either    be selected or at least has a chance to be
                        selected (when NumberOfClusters is specified);
                      type: object
                      properties:
                        claimSelector:
                          description: ClaimSelector represents a selector of ManagedClusters
                            by clusterClaims in status
                          type: object
                          properties:
                            matchExpressions:
                              description: matchExpressions is a list of cluster claim
                                selector requirements. The requirements are ANDed.
                              type: array
                              items:
                                description: A label selector requirement is a selector
                                  that contains values, a key, and an operator that
                                  relates the key and values.
                                type: object
                                required:
                                - key
                                - operator
                                properties:
                                  key:
                                    description: key is the label key that the selector
                                      applies to.
                                    type: string
                                  operator:
                                    description: operator represents a key's relationship
                                      to a set of values. Valid operators are In,
                                      NotIn, Exists and DoesNotExist.
                                    type: string
                                  values:
                                    description: values is an array of string values.
                                      If the operator is In or NotIn, the values array
                                      must be non-empty. If the operator is Exists
                                      or DoesNotExist, the values array must be empty.
                                      This array is replaced during a strategic merge
                                      patch.
                                    type: array
                                    items:
                                      type: string
                        labelSelector:
                          description: LabelSelector represents a selector of ManagedClusters
                            by label
                          type: object
                          properties:
                            matchExpressions:
                              description: matchExpressions is a list of label selector
                                requirements. The requirements are ANDed.
                              type: array
                              items:
                                description: A label selector requirement is a selector
                                  that contains values, a key, and an operator that
                                  relates the key and values.
                                type: object
                                required:
                                - key
                                - operator
                                properties:
                                  key:
                                    description: key is the label key that the selector
                                      applies to.
                                    type: string
                                  operator:
                                    description: operator represents a key's relationship
                                      to a set of values. Valid operators are In,
                                      NotIn, Exists and DoesNotExist.
                                    type: string
                                  values:
                                    description: values is an array of string values.
                                      If the operator is In or NotIn, the values array
                                      must be non-empty. If the operator is Exists
                                      or DoesNotExist, the values array must be empty.
                                      This array is replaced during a strategic merge
                                      patch.
                                    type: array
                                    items:
                                      type: string
                            matchLabels:
                              description: matchLabels is a map of {key,value} pairs.
                                A single {key,value} in the matchLabels map is equivalent
                                to an element of matchExpressions, whose key field
                                is "key", the operator is "In", and the values array
                                contains only "value". The requirements are ANDed.
                              type: object
                              additionalProperties:
                                type: string
          status:
            description: Status represents the current status of the Placement
            type: object
            properties:
              conditions:
                description: Conditions contains the different condition statuses
                  for this Placement.
                type: array
                items:
                  description: "Condition contains details for one aspect of the current
                    state of this API Resource. --- This struct is intended for direct
                    use as an array at the field path .status.conditions.  For example,
                    type FooStatus struct{     // Represents the observations of a
                    foo's current state.     // Known .status.conditions.type are:
                    \"Available\", \"Progressing\", and \"Degraded\"     // +patchMergeKey=type
                    \    // +patchStrategy=merge     // +listType=map     // +listMapKey=type
                    \    Conditions []metav1.Condition ` + "`" + `json:\"conditions,omitempty\"
                    patchStrategy:\"merge\" patchMergeKey:\"type\" protobuf:\"bytes,1,rep,name=conditions\"` + "`" + `
                    \n     // other fields }"
                  type: object
                  required:
                  - lastTransitionTime
                  - message
                  - reason
                  - status
                  - type
                  properties:
                    lastTransitionTime:
                      description: lastTransitionTime is the last time the condition
                        transitioned from one status to another. This should be when
                        the underlying condition changed.  If that is not known, then
                        using the time when the API field changed is acceptable.
                      type: string
                      format: date-time
                    message:
                      description: message is a human readable message indicating
                        details about the transition. This may be an empty string.
                      type: string
                      maxLength: 32768
                    observedGeneration:
                      description: observedGeneration represents the .metadata.generation
                        that the condition was set based upon. For instance, if .metadata.generation
                        is currently 12, but the .status.conditions[x].observedGeneration
                        is 9, the condition is out of date with respect to the current
                        state of the instance.
                      type: integer
                      format: int64
                      minimum: 0
                    reason:
                      description: reason contains a programmatic identifier indicating
                        the reason for the condition's last transition. Producers
                        of specific condition types may define expected values and
                        meanings for this field, and whether the values are considered
                        a guaranteed API. The value should be a CamelCase string.
                        This field may not be empty.
                      type: string
                      maxLength: 1024
                      minLength: 1
                      pattern: ^[A-Za-z]([A-Za-z0-9_,:]*[A-Za-z0-9_])?$
                    status:
                      description: status of the condition, one of True, False, Unknown.
                      type: string
                      enum:
                      - "True"
                      - "False"
                      - Unknown
                    type:
                      description: type of condition in CamelCase or in foo.example.com/CamelCase.
                        --- Many .condition.type values are consistent across resources
                        like Available, but because arbitrary conditions can be useful
                        (see .node.status.conditions), the ability to deconflict is
                        important. The regex it matches is (dns1123SubdomainFmt/)?(qualifiedNameFmt)
                      type: string
                      maxLength: 316
                      pattern: ^([a-z0-9]([-a-z0-9]*[a-z0-9])?(\.[a-z0-9]([-a-z0-9]*[a-z0-9])?)*/)?(([A-Za-z0-9][-A-Za-z0-9_.]*)?[A-Za-z0-9])$
              numberOfSelectedClusters:
                description: NumberOfSelectedClusters represents the number of selected
                  ManagedClusters
                type: integer
                format: int32
    served: true
    storage: true
    subresources:
      status: {}
status:
  acceptedNames:
    kind: ""
    plural: ""
  conditions: []
  storedVersions: []
`)

func manifestsClusterManager0000_03_clustersOpenClusterManagementIo_placementsCrdYamlBytes() ([]byte, error) {
	return _manifestsClusterManager0000_03_clustersOpenClusterManagementIo_placementsCrdYaml, nil
}

func manifestsClusterManager0000_03_clustersOpenClusterManagementIo_placementsCrdYaml() (*asset, error) {
	bytes, err := manifestsClusterManager0000_03_clustersOpenClusterManagementIo_placementsCrdYamlBytes()
	if err != nil {
		return nil, err
	}

	info := bindataFileInfo{name: "manifests/cluster-manager/0000_03_clusters.open-cluster-management.io_placements.crd.yaml", size: 0, mode: os.FileMode(0), modTime: time.Unix(0, 0)}
	a := &asset{bytes: bytes, info: info}
	return a, nil
}

var _manifestsClusterManager0000_04_clustersOpenClusterManagementIo_placementdecisionsCrdYaml = []byte(`apiVersion: apiextensions.k8s.io/v1
kind: CustomResourceDefinition
metadata:
  name: placementdecisions.cluster.open-cluster-management.io
spec:
  group: cluster.open-cluster-management.io
  names:
    kind: PlacementDecision
    listKind: PlacementDecisionList
    plural: placementdecisions
    singular: placementdecision
  scope: Namespaced
  preserveUnknownFields: false
  versions:
  - name: v1alpha1
    schema:
      openAPIV3Schema:
        description: "PlacementDecision indicates a decision from a placement PlacementDecision
          should has a label cluster.open-cluster-management.io/placement={placement
          name} to reference a certain placement. \n If a placement has spec.numberOfClusters
          specified, the total number of decisions contained in status.decisions of
          PlacementDecisions should always be NumberOfClusters; otherwise, the total
          number of decisions should be the number of ManagedClusters which match
          the placement requirements. \n Some of the decisions might be empty when
          there are no enough ManagedClusters meet the placement requirements."
        type: object
        properties:
          apiVersion:
            description: 'APIVersion defines the versioned schema of this representation
              of an object. Servers should convert recognized schemas to the latest
              internal value, and may reject unrecognized values. More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#resources'
            type: string
          kind:
            description: 'Kind is a string value representing the REST resource this
              object represents. Servers may infer this from the endpoint the client
              submits requests to. Cannot be updated. In CamelCase. More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#types-kinds'
            type: string
          metadata:
            type: object
          status:
            description: Status represents the current status of the PlacementDecision
            type: object
            required:
            - decisions
            properties:
              decisions:
                description: Decisions is a slice of decisions according to a placement
                  The number of decisions should not be larger than 100
                type: array
                items:
                  description: ClusterDecision represents a decision from a placement
                    An empty ClusterDecision indicates it is not scheduled yet.
                  type: object
                  required:
                  - clusterName
                  - reason
                  properties:
                    clusterName:
                      description: ClusterName is the name of the ManagedCluster.
                        If it is not empty, its value should be unique cross all placement
                        decisions for the Placement.
                      type: string
                    reason:
                      description: Reason represents the reason why the ManagedCluster
                        is selected.
                      type: string
    served: true
    storage: true
    subresources:
      status: {}
status:
  acceptedNames:
    kind: ""
    plural: ""
  conditions: []
  storedVersions: []
`)

func manifestsClusterManager0000_04_clustersOpenClusterManagementIo_placementdecisionsCrdYamlBytes() ([]byte, error) {
	return _manifestsClusterManager0000_04_clustersOpenClusterManagementIo_placementdecisionsCrdYaml, nil
}

func manifestsClusterManager0000_04_clustersOpenClusterManagementIo_placementdecisionsCrdYaml() (*asset, error) {
	bytes, err := manifestsClusterManager0000_04_clustersOpenClusterManagementIo_placementdecisionsCrdYamlBytes()
	if err != nil {
		return nil, err
	}

	info := bindataFileInfo{name: "manifests/cluster-manager/0000_04_clusters.open-cluster-management.io_placementdecisions.crd.yaml", size: 0, mode: os.FileMode(0), modTime: time.Unix(0, 0)}
	a := &asset{bytes: bytes, info: info}
	return a, nil
}

var _manifestsClusterManagerClusterManagerNamespaceYaml = []byte(`apiVersion: v1
kind: Namespace
metadata:
  name: open-cluster-management-hub
`)

func manifestsClusterManagerClusterManagerNamespaceYamlBytes() ([]byte, error) {
	return _manifestsClusterManagerClusterManagerNamespaceYaml, nil
}

func manifestsClusterManagerClusterManagerNamespaceYaml() (*asset, error) {
	bytes, err := manifestsClusterManagerClusterManagerNamespaceYamlBytes()
	if err != nil {
		return nil, err
	}

	info := bindataFileInfo{name: "manifests/cluster-manager/cluster-manager-namespace.yaml", size: 0, mode: os.FileMode(0), modTime: time.Unix(0, 0)}
	a := &asset{bytes: bytes, info: info}
	return a, nil
}

var _manifestsClusterManagerClusterManagerPlacementClusterroleYaml = []byte(`apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
  name: open-cluster-management:{{ .ClusterManagerName }}-placement:controller
rules:
# Allow controller to get/list/watch/create/delete configmaps
- apiGroups: [""]
  resources: ["configmaps"]
  verbs: ["get", "list", "watch", "create", "delete", "update"]
# Allow controller to create/patch/update events
- apiGroups: ["", "events.k8s.io"]
  resources: ["events"]
  verbs: ["create", "patch", "update"]
# Allow controller to view managedclusters/managedclustersets/managedclustersetbindings
- apiGroups: ["cluster.open-cluster-management.io"]
  resources: ["managedclusters", "managedclustersets", "managedclustersetbindings"]
  verbs: ["get", "list", "watch"]
# Allow controller to manage placements/placementdecisions
- apiGroups: ["cluster.open-cluster-management.io"]
  resources: ["placements", "placementdecisions"]
  verbs: ["get", "list", "watch", "create", "update", "patch"]
- apiGroups: ["cluster.open-cluster-management.io"]
  resources: ["placements/status", "placementdecisions/status"]
  verbs: ["update", "patch"]
- apiGroups: ["cluster.open-cluster-management.io"]
  resources: ["placements/finalizers"]
  verbs: ["update"]
`)

func manifestsClusterManagerClusterManagerPlacementClusterroleYamlBytes() ([]byte, error) {
	return _manifestsClusterManagerClusterManagerPlacementClusterroleYaml, nil
}

func manifestsClusterManagerClusterManagerPlacementClusterroleYaml() (*asset, error) {
	bytes, err := manifestsClusterManagerClusterManagerPlacementClusterroleYamlBytes()
	if err != nil {
		return nil, err
	}

	info := bindataFileInfo{name: "manifests/cluster-manager/cluster-manager-placement-clusterrole.yaml", size: 0, mode: os.FileMode(0), modTime: time.Unix(0, 0)}
	a := &asset{bytes: bytes, info: info}
	return a, nil
}

var _manifestsClusterManagerClusterManagerPlacementClusterrolebindingYaml = []byte(`apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRoleBinding
metadata:
  name: open-cluster-management:{{ .ClusterManagerName }}-placement:controller
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: ClusterRole
  name: open-cluster-management:{{ .ClusterManagerName }}-placement:controller
subjects:
- kind: ServiceAccount
  namespace: open-cluster-management-hub
  name: {{ .ClusterManagerName }}-placement-controller-sa
`)

func manifestsClusterManagerClusterManagerPlacementClusterrolebindingYamlBytes() ([]byte, error) {
	return _manifestsClusterManagerClusterManagerPlacementClusterrolebindingYaml, nil
}

func manifestsClusterManagerClusterManagerPlacementClusterrolebindingYaml() (*asset, error) {
	bytes, err := manifestsClusterManagerClusterManagerPlacementClusterrolebindingYamlBytes()
	if err != nil {
		return nil, err
	}

	info := bindataFileInfo{name: "manifests/cluster-manager/cluster-manager-placement-clusterrolebinding.yaml", size: 0, mode: os.FileMode(0), modTime: time.Unix(0, 0)}
	a := &asset{bytes: bytes, info: info}
	return a, nil
}

var _manifestsClusterManagerClusterManagerPlacementDeploymentYaml = []byte(`kind: Deployment
apiVersion: apps/v1
metadata:
  name: {{ .ClusterManagerName }}-placement-controller
  namespace: open-cluster-management-hub
  labels:
    app: clustermanager-controller
spec:
  replicas: {{ .Replica }}
  selector:
    matchLabels:
      app: clustermanager-placement-controller
  template:
    metadata:
      labels:
        app: clustermanager-placement-controller
    spec:
      affinity:
        podAntiAffinity:
          preferredDuringSchedulingIgnoredDuringExecution:
          - weight: 70
            podAffinityTerm:
              topologyKey: failure-domain.beta.kubernetes.io/zone
              labelSelector:
                matchExpressions:
                - key: app
                  operator: In
                  values:
                  - clustermanager-placement-controller
          - weight: 30
            podAffinityTerm:
              topologyKey: kubernetes.io/hostname
              labelSelector:
                matchExpressions:
                - key: app
                  operator: In
                  values:
                  - clustermanager-placement-controller
      serviceAccountName: {{ .ClusterManagerName }}-placement-controller-sa
      containers:
      - name: placement-controller
        image: {{ .PlacementImage }}
        args:
          - "/placement"
          - "controller"
        securityContext:
          allowPrivilegeEscalation: false
          capabilities:
            drop:
              - ALL
          privileged: false
          runAsNonRoot: true
        livenessProbe:
          httpGet:
            path: /healthz
            scheme: HTTPS
            port: 8443
          initialDelaySeconds: 2
          periodSeconds: 10
        readinessProbe:
          httpGet:
            path: /healthz
            scheme: HTTPS
            port: 8443
          initialDelaySeconds: 2
        resources:
          requests:
            cpu: 100m
            memory: 128Mi
`)

func manifestsClusterManagerClusterManagerPlacementDeploymentYamlBytes() ([]byte, error) {
	return _manifestsClusterManagerClusterManagerPlacementDeploymentYaml, nil
}

func manifestsClusterManagerClusterManagerPlacementDeploymentYaml() (*asset, error) {
	bytes, err := manifestsClusterManagerClusterManagerPlacementDeploymentYamlBytes()
	if err != nil {
		return nil, err
	}

	info := bindataFileInfo{name: "manifests/cluster-manager/cluster-manager-placement-deployment.yaml", size: 0, mode: os.FileMode(0), modTime: time.Unix(0, 0)}
	a := &asset{bytes: bytes, info: info}
	return a, nil
}

var _manifestsClusterManagerClusterManagerPlacementServiceaccountYaml = []byte(`apiVersion: v1
kind: ServiceAccount
metadata:
  name: {{ .ClusterManagerName }}-placement-controller-sa
  namespace: open-cluster-management-hub
`)

func manifestsClusterManagerClusterManagerPlacementServiceaccountYamlBytes() ([]byte, error) {
	return _manifestsClusterManagerClusterManagerPlacementServiceaccountYaml, nil
}

func manifestsClusterManagerClusterManagerPlacementServiceaccountYaml() (*asset, error) {
	bytes, err := manifestsClusterManagerClusterManagerPlacementServiceaccountYamlBytes()
	if err != nil {
		return nil, err
	}

	info := bindataFileInfo{name: "manifests/cluster-manager/cluster-manager-placement-serviceaccount.yaml", size: 0, mode: os.FileMode(0), modTime: time.Unix(0, 0)}
	a := &asset{bytes: bytes, info: info}
	return a, nil
}

var _manifestsClusterManagerClusterManagerRegistrationClusterroleYaml = []byte(`apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
  name: open-cluster-management:{{ .ClusterManagerName }}-registration:controller
rules:
# Allow hub to monitor and update status of csr
- apiGroups: ["certificates.k8s.io"]
  resources: ["certificatesigningrequests"]
  verbs: ["create", "get", "list", "watch"]
- apiGroups: ["certificates.k8s.io"]
  resources: ["certificatesigningrequests/status", "certificatesigningrequests/approval"]
  verbs: ["update"]
# Allow hub to get/list/watch/create/delete namespace and service account
- apiGroups: [""]
  resources: ["namespaces", "serviceaccounts", "configmaps"]
  verbs: ["get", "list", "watch", "create", "delete", "update"]
- apiGroups: ["", "events.k8s.io"]
  resources: ["events"]
  verbs: ["create", "patch", "update"]
- apiGroups: ["authorization.k8s.io"]
  resources: ["subjectaccessreviews"]
  verbs: ["create"]
# Allow hub to manage clusterrole/clusterrolebinding/role/rolebinding
- apiGroups: ["rbac.authorization.k8s.io"]
  resources: ["clusterrolebindings", "rolebindings"]
  verbs: ["get", "list", "watch", "create", "update", "patch", "delete"]
- apiGroups: ["rbac.authorization.k8s.io"]
  resources: ["clusterroles", "roles"]
  verbs: ["get", "list", "watch", "create", "update", "patch", "delete", "escalate", "bind"]
# Allow hub to manage coordination.k8s.io/lease
- apiGroups: ["coordination.k8s.io"]
  resources: ["leases"]
  verbs: ["get", "list", "watch", "create", "delete", "update"]
# Allow hub to manage managedclusters
- apiGroups: ["cluster.open-cluster-management.io"]
  resources: ["managedclusters"]
  verbs: ["get", "list", "watch", "update", "patch"]
- apiGroups: ["cluster.open-cluster-management.io"]
  resources: ["managedclusters/status"]
  verbs: ["update", "patch"]
# Allow hub to monitor manifestworks
- apiGroups: ["work.open-cluster-management.io"]
  resources: ["manifestworks"]
  verbs: ["get", "list", "watch"]
# Allow hub to approve certificates that are signed by kubernetes.io/kube-apiserver-client (kube1.18.3+ needs)
- apiGroups: ["certificates.k8s.io"]
  resources: ["signers"]
  resourceNames: ["kubernetes.io/kube-apiserver-client"]
  verbs: ["approve"]
# Allow hub to manage managedclustersets
- apiGroups: ["cluster.open-cluster-management.io"]
  resources: ["managedclustersets"]
  verbs: ["get", "list", "watch", "create", "update", "patch", "delete"]
- apiGroups: ["cluster.open-cluster-management.io"]
  resources: ["managedclustersets/status"]
  verbs: ["update", "patch"]
# Allow to access metrics API
- apiGroups: ["authentication.k8s.io"]
  resources: ["tokenreviews"]
  verbs: ["create"]
- apiGroups: ["authorization.k8s.io"]
  resources: ["subjectaccessreviews"]
  verbs: ["create"]
# Allow hub to manage managed cluster addons
- apiGroups: ["addon.open-cluster-management.io"]
  resources: ["managedclusteraddons"]
  verbs: ["get", "list", "watch"]
- apiGroups: ["addon.open-cluster-management.io"]
  resources: ["managedclusteraddons/status"]
  verbs: ["patch", "update"]
`)

func manifestsClusterManagerClusterManagerRegistrationClusterroleYamlBytes() ([]byte, error) {
	return _manifestsClusterManagerClusterManagerRegistrationClusterroleYaml, nil
}

func manifestsClusterManagerClusterManagerRegistrationClusterroleYaml() (*asset, error) {
	bytes, err := manifestsClusterManagerClusterManagerRegistrationClusterroleYamlBytes()
	if err != nil {
		return nil, err
	}

	info := bindataFileInfo{name: "manifests/cluster-manager/cluster-manager-registration-clusterrole.yaml", size: 0, mode: os.FileMode(0), modTime: time.Unix(0, 0)}
	a := &asset{bytes: bytes, info: info}
	return a, nil
}

var _manifestsClusterManagerClusterManagerRegistrationClusterrolebindingYaml = []byte(`apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRoleBinding
metadata:
  name: open-cluster-management:{{ .ClusterManagerName }}-registration:controller
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: ClusterRole
  name: open-cluster-management:{{ .ClusterManagerName }}-registration:controller
subjects:
- kind: ServiceAccount
  namespace: open-cluster-management-hub
  name: {{ .ClusterManagerName }}-registration-controller-sa
`)

func manifestsClusterManagerClusterManagerRegistrationClusterrolebindingYamlBytes() ([]byte, error) {
	return _manifestsClusterManagerClusterManagerRegistrationClusterrolebindingYaml, nil
}

func manifestsClusterManagerClusterManagerRegistrationClusterrolebindingYaml() (*asset, error) {
	bytes, err := manifestsClusterManagerClusterManagerRegistrationClusterrolebindingYamlBytes()
	if err != nil {
		return nil, err
	}

	info := bindataFileInfo{name: "manifests/cluster-manager/cluster-manager-registration-clusterrolebinding.yaml", size: 0, mode: os.FileMode(0), modTime: time.Unix(0, 0)}
	a := &asset{bytes: bytes, info: info}
	return a, nil
}

var _manifestsClusterManagerClusterManagerRegistrationDeploymentYaml = []byte(`kind: Deployment
apiVersion: apps/v1
metadata:
  name: {{ .ClusterManagerName }}-registration-controller
  namespace: open-cluster-management-hub
  labels:
    app: clustermanager-controller
spec:
  replicas: {{ .Replica }}
  selector:
    matchLabels:
      app: clustermanager-registration-controller
  template:
    metadata:
      labels:
        app: clustermanager-registration-controller
    spec:
      affinity:
        podAntiAffinity:
          preferredDuringSchedulingIgnoredDuringExecution:
          - weight: 70
            podAffinityTerm:
              topologyKey: failure-domain.beta.kubernetes.io/zone
              labelSelector:
                matchExpressions:
                - key: app
                  operator: In
                  values:
                  - clustermanager-registration-controller
          - weight: 30
            podAffinityTerm:
              topologyKey: kubernetes.io/hostname
              labelSelector:
                matchExpressions:
                - key: app
                  operator: In
                  values:
                  - clustermanager-registration-controller
      serviceAccountName: {{ .ClusterManagerName }}-registration-controller-sa
      containers:
      - name: hub-registration-controller
        image: {{ .RegistrationImage }}
        args:
          - "/registration"
          - "controller"
        securityContext:
          allowPrivilegeEscalation: false
          capabilities:
            drop:
              - ALL
          privileged: false
          runAsNonRoot: true
        livenessProbe:
          httpGet:
            path: /healthz
            scheme: HTTPS
            port: 8443
          initialDelaySeconds: 2
          periodSeconds: 10
        readinessProbe:
          httpGet:
            path: /healthz
            scheme: HTTPS
            port: 8443
          initialDelaySeconds: 2
        resources:
          requests:
            cpu: 100m
            memory: 128Mi
`)

func manifestsClusterManagerClusterManagerRegistrationDeploymentYamlBytes() ([]byte, error) {
	return _manifestsClusterManagerClusterManagerRegistrationDeploymentYaml, nil
}

func manifestsClusterManagerClusterManagerRegistrationDeploymentYaml() (*asset, error) {
	bytes, err := manifestsClusterManagerClusterManagerRegistrationDeploymentYamlBytes()
	if err != nil {
		return nil, err
	}

	info := bindataFileInfo{name: "manifests/cluster-manager/cluster-manager-registration-deployment.yaml", size: 0, mode: os.FileMode(0), modTime: time.Unix(0, 0)}
	a := &asset{bytes: bytes, info: info}
	return a, nil
}

var _manifestsClusterManagerClusterManagerRegistrationServiceaccountYaml = []byte(`apiVersion: v1
kind: ServiceAccount
metadata:
  name: {{ .ClusterManagerName }}-registration-controller-sa
  namespace: open-cluster-management-hub
`)

func manifestsClusterManagerClusterManagerRegistrationServiceaccountYamlBytes() ([]byte, error) {
	return _manifestsClusterManagerClusterManagerRegistrationServiceaccountYaml, nil
}

func manifestsClusterManagerClusterManagerRegistrationServiceaccountYaml() (*asset, error) {
	bytes, err := manifestsClusterManagerClusterManagerRegistrationServiceaccountYamlBytes()
	if err != nil {
		return nil, err
	}

	info := bindataFileInfo{name: "manifests/cluster-manager/cluster-manager-registration-serviceaccount.yaml", size: 0, mode: os.FileMode(0), modTime: time.Unix(0, 0)}
	a := &asset{bytes: bytes, info: info}
	return a, nil
}

var _manifestsClusterManagerClusterManagerRegistrationWebhookApiserviceYaml = []byte(`apiVersion: apiregistration.k8s.io/v1
kind: APIService
metadata:
  name: v1.admission.cluster.open-cluster-management.io
spec:
  group: admission.cluster.open-cluster-management.io
  version: v1
  service:
    name: cluster-manager-registration-webhook
    namespace: open-cluster-management-hub
  caBundle: {{ .RegistrationAPIServiceCABundle }}
  groupPriorityMinimum: 10000
  versionPriority: 20
`)

func manifestsClusterManagerClusterManagerRegistrationWebhookApiserviceYamlBytes() ([]byte, error) {
	return _manifestsClusterManagerClusterManagerRegistrationWebhookApiserviceYaml, nil
}

func manifestsClusterManagerClusterManagerRegistrationWebhookApiserviceYaml() (*asset, error) {
	bytes, err := manifestsClusterManagerClusterManagerRegistrationWebhookApiserviceYamlBytes()
	if err != nil {
		return nil, err
	}

	info := bindataFileInfo{name: "manifests/cluster-manager/cluster-manager-registration-webhook-apiservice.yaml", size: 0, mode: os.FileMode(0), modTime: time.Unix(0, 0)}
	a := &asset{bytes: bytes, info: info}
	return a, nil
}

var _manifestsClusterManagerClusterManagerRegistrationWebhookClusterroleYaml = []byte(`apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
  name: open-cluster-management:{{ .ClusterManagerName }}-registration:webhook
rules:
# Allow managedcluster admission to get/list/watch configmaps
- apiGroups: [""]
  resources: ["configmaps"]
  verbs: ["get", "list", "watch"]
# Allow managedcluster admission to create subjectaccessreviews
- apiGroups: ["authorization.k8s.io"]
  resources: ["subjectaccessreviews"]
  verbs: ["create"]
# API priority and fairness
- apiGroups: ["flowcontrol.apiserver.k8s.io"]
  resources: ["prioritylevelconfigurations", "flowschemas"]
  verbs: ["get", "list", "watch"]
`)

func manifestsClusterManagerClusterManagerRegistrationWebhookClusterroleYamlBytes() ([]byte, error) {
	return _manifestsClusterManagerClusterManagerRegistrationWebhookClusterroleYaml, nil
}

func manifestsClusterManagerClusterManagerRegistrationWebhookClusterroleYaml() (*asset, error) {
	bytes, err := manifestsClusterManagerClusterManagerRegistrationWebhookClusterroleYamlBytes()
	if err != nil {
		return nil, err
	}

	info := bindataFileInfo{name: "manifests/cluster-manager/cluster-manager-registration-webhook-clusterrole.yaml", size: 0, mode: os.FileMode(0), modTime: time.Unix(0, 0)}
	a := &asset{bytes: bytes, info: info}
	return a, nil
}

var _manifestsClusterManagerClusterManagerRegistrationWebhookClusterrolebindingYaml = []byte(`apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRoleBinding
metadata:
  name: open-cluster-management:{{ .ClusterManagerName }}-registration:webhook
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: ClusterRole
  name: open-cluster-management:{{ .ClusterManagerName }}-registration:webhook
subjects:
  - kind: ServiceAccount
    name: {{ .ClusterManagerName }}-registration-webhook-sa
    namespace: open-cluster-management-hub
`)

func manifestsClusterManagerClusterManagerRegistrationWebhookClusterrolebindingYamlBytes() ([]byte, error) {
	return _manifestsClusterManagerClusterManagerRegistrationWebhookClusterrolebindingYaml, nil
}

func manifestsClusterManagerClusterManagerRegistrationWebhookClusterrolebindingYaml() (*asset, error) {
	bytes, err := manifestsClusterManagerClusterManagerRegistrationWebhookClusterrolebindingYamlBytes()
	if err != nil {
		return nil, err
	}

	info := bindataFileInfo{name: "manifests/cluster-manager/cluster-manager-registration-webhook-clusterrolebinding.yaml", size: 0, mode: os.FileMode(0), modTime: time.Unix(0, 0)}
	a := &asset{bytes: bytes, info: info}
	return a, nil
}

var _manifestsClusterManagerClusterManagerRegistrationWebhookClustersetbindingValidatingconfigurationYaml = []byte(`apiVersion: admissionregistration.k8s.io/v1
kind: ValidatingWebhookConfiguration
metadata:
  name: managedclustersetbindingvalidators.admission.cluster.open-cluster-management.io
webhooks:
- name: managedclustersetbindingvalidators.admission.cluster.open-cluster-management.io
  failurePolicy: Fail
  clientConfig:
    service:
      # reach the webhook via the registered aggregated API
      namespace: default
      name: kubernetes
      path: /apis/admission.cluster.open-cluster-management.io/v1/managedclustersetbindingvalidators
  rules:
  - operations:
    - CREATE
    - UPDATE
    apiGroups:
    - cluster.open-cluster-management.io
    apiVersions:
    - "*"
    resources:
    - managedclustersetbindings
  admissionReviewVersions: ["v1beta1"]
  sideEffects: None
  timeoutSeconds: 10
`)

func manifestsClusterManagerClusterManagerRegistrationWebhookClustersetbindingValidatingconfigurationYamlBytes() ([]byte, error) {
	return _manifestsClusterManagerClusterManagerRegistrationWebhookClustersetbindingValidatingconfigurationYaml, nil
}

func manifestsClusterManagerClusterManagerRegistrationWebhookClustersetbindingValidatingconfigurationYaml() (*asset, error) {
	bytes, err := manifestsClusterManagerClusterManagerRegistrationWebhookClustersetbindingValidatingconfigurationYamlBytes()
	if err != nil {
		return nil, err
	}

	info := bindataFileInfo{name: "manifests/cluster-manager/cluster-manager-registration-webhook-clustersetbinding-validatingconfiguration.yaml", size: 0, mode: os.FileMode(0), modTime: time.Unix(0, 0)}
	a := &asset{bytes: bytes, info: info}
	return a, nil
}

var _manifestsClusterManagerClusterManagerRegistrationWebhookDeploymentYaml = []byte(`apiVersion: apps/v1
kind: Deployment
metadata:
  name: {{ .ClusterManagerName }}-registration-webhook
  namespace: open-cluster-management-hub
  labels:
    app: {{ .ClusterManagerName }}-registration-webhook
spec:
  replicas: {{ .Replica }}
  selector:
    matchLabels:
      app: {{ .ClusterManagerName }}-registration-webhook
  template:
    metadata:
      labels:
        app: {{ .ClusterManagerName }}-registration-webhook
    spec:
      affinity:
        podAntiAffinity:
          preferredDuringSchedulingIgnoredDuringExecution:
          - weight: 70
            podAffinityTerm:
              topologyKey: failure-domain.beta.kubernetes.io/zone
              labelSelector:
                matchExpressions:
                - key: app
                  operator: In
                  values:
                  - {{ .ClusterManagerName }}-registration-webhook
          - weight: 30
            podAffinityTerm:
              topologyKey: kubernetes.io/hostname
              labelSelector:
                matchExpressions:
                - key: app
                  operator: In
                  values:
                  - {{ .ClusterManagerName }}-registration-webhook
      serviceAccountName: {{ .ClusterManagerName }}-registration-webhook-sa
      containers:
      - name: {{ .ClusterManagerName }}-registration-webhook-sa
        image: {{ .RegistrationImage }}
        args:
          - "/registration"
          - "webhook"
          - "--secure-port=6443"
          - "--tls-cert-file=/serving-cert/tls.crt"
          - "--tls-private-key-file=/serving-cert/tls.key"
        securityContext:
          allowPrivilegeEscalation: false
          capabilities:
            drop:
              - ALL
          privileged: false
          runAsNonRoot: true
        livenessProbe:
          httpGet:
            path: /healthz
            scheme: HTTPS
            port: 6443
          initialDelaySeconds: 2
          periodSeconds: 10
        readinessProbe:
          httpGet:
            path: /healthz
            scheme: HTTPS
            port: 6443
          initialDelaySeconds: 2
        resources:
          requests:
            cpu: 100m
            memory: 128Mi
        volumeMounts:
        - name: webhook-secret
          mountPath: "/serving-cert"
          readOnly: true
      volumes:
      - name: webhook-secret
        secret:
          secretName: registration-webhook-serving-cert

`)

func manifestsClusterManagerClusterManagerRegistrationWebhookDeploymentYamlBytes() ([]byte, error) {
	return _manifestsClusterManagerClusterManagerRegistrationWebhookDeploymentYaml, nil
}

func manifestsClusterManagerClusterManagerRegistrationWebhookDeploymentYaml() (*asset, error) {
	bytes, err := manifestsClusterManagerClusterManagerRegistrationWebhookDeploymentYamlBytes()
	if err != nil {
		return nil, err
	}

	info := bindataFileInfo{name: "manifests/cluster-manager/cluster-manager-registration-webhook-deployment.yaml", size: 0, mode: os.FileMode(0), modTime: time.Unix(0, 0)}
	a := &asset{bytes: bytes, info: info}
	return a, nil
}

var _manifestsClusterManagerClusterManagerRegistrationWebhookMutatingconfigurationYaml = []byte(`apiVersion: admissionregistration.k8s.io/v1
kind: MutatingWebhookConfiguration
metadata:
  name: managedclustermutators.admission.cluster.open-cluster-management.io
webhooks:
- name: managedclustermutators.admission.cluster.open-cluster-management.io
  failurePolicy: Fail
  clientConfig:
    service:
      # reach the webhook via the registered aggregated API
      namespace: default
      name: kubernetes
      path: /apis/admission.cluster.open-cluster-management.io/v1/managedclustermutators
  rules:
  - operations:
    - CREATE
    - UPDATE
    apiGroups:
    - cluster.open-cluster-management.io
    apiVersions:
    - "*"
    resources:
    - managedclusters
  admissionReviewVersions: ["v1beta1"]
  sideEffects: None
  timeoutSeconds: 10
`)

func manifestsClusterManagerClusterManagerRegistrationWebhookMutatingconfigurationYamlBytes() ([]byte, error) {
	return _manifestsClusterManagerClusterManagerRegistrationWebhookMutatingconfigurationYaml, nil
}

func manifestsClusterManagerClusterManagerRegistrationWebhookMutatingconfigurationYaml() (*asset, error) {
	bytes, err := manifestsClusterManagerClusterManagerRegistrationWebhookMutatingconfigurationYamlBytes()
	if err != nil {
		return nil, err
	}

	info := bindataFileInfo{name: "manifests/cluster-manager/cluster-manager-registration-webhook-mutatingconfiguration.yaml", size: 0, mode: os.FileMode(0), modTime: time.Unix(0, 0)}
	a := &asset{bytes: bytes, info: info}
	return a, nil
}

var _manifestsClusterManagerClusterManagerRegistrationWebhookServiceYaml = []byte(`apiVersion: v1
kind: Service
metadata:
  name: cluster-manager-registration-webhook
  namespace: open-cluster-management-hub
spec:
  selector:
    app: {{ .ClusterManagerName }}-registration-webhook
  ports:
  - port: 443
    targetPort: 6443
`)

func manifestsClusterManagerClusterManagerRegistrationWebhookServiceYamlBytes() ([]byte, error) {
	return _manifestsClusterManagerClusterManagerRegistrationWebhookServiceYaml, nil
}

func manifestsClusterManagerClusterManagerRegistrationWebhookServiceYaml() (*asset, error) {
	bytes, err := manifestsClusterManagerClusterManagerRegistrationWebhookServiceYamlBytes()
	if err != nil {
		return nil, err
	}

	info := bindataFileInfo{name: "manifests/cluster-manager/cluster-manager-registration-webhook-service.yaml", size: 0, mode: os.FileMode(0), modTime: time.Unix(0, 0)}
	a := &asset{bytes: bytes, info: info}
	return a, nil
}

var _manifestsClusterManagerClusterManagerRegistrationWebhookServiceaccountYaml = []byte(`apiVersion: v1
kind: ServiceAccount
metadata:
  name: {{ .ClusterManagerName }}-registration-webhook-sa
  namespace: open-cluster-management-hub
`)

func manifestsClusterManagerClusterManagerRegistrationWebhookServiceaccountYamlBytes() ([]byte, error) {
	return _manifestsClusterManagerClusterManagerRegistrationWebhookServiceaccountYaml, nil
}

func manifestsClusterManagerClusterManagerRegistrationWebhookServiceaccountYaml() (*asset, error) {
	bytes, err := manifestsClusterManagerClusterManagerRegistrationWebhookServiceaccountYamlBytes()
	if err != nil {
		return nil, err
	}

	info := bindataFileInfo{name: "manifests/cluster-manager/cluster-manager-registration-webhook-serviceaccount.yaml", size: 0, mode: os.FileMode(0), modTime: time.Unix(0, 0)}
	a := &asset{bytes: bytes, info: info}
	return a, nil
}

var _manifestsClusterManagerClusterManagerRegistrationWebhookValidatingconfigurationYaml = []byte(`apiVersion: admissionregistration.k8s.io/v1
kind: ValidatingWebhookConfiguration
metadata:
  name: managedclustervalidators.admission.cluster.open-cluster-management.io
webhooks:
- name: managedclustervalidators.admission.cluster.open-cluster-management.io
  failurePolicy: Fail
  clientConfig:
    service:
      # reach the webhook via the registered aggregated API
      namespace: default
      name: kubernetes
      path: /apis/admission.cluster.open-cluster-management.io/v1/managedclustervalidators
  rules:
  - operations:
    - CREATE
    - UPDATE
    apiGroups:
    - cluster.open-cluster-management.io
    apiVersions:
    - "*"
    resources:
    - managedclusters
  admissionReviewVersions: ["v1beta1"]
  sideEffects: None
  timeoutSeconds: 10
`)

func manifestsClusterManagerClusterManagerRegistrationWebhookValidatingconfigurationYamlBytes() ([]byte, error) {
	return _manifestsClusterManagerClusterManagerRegistrationWebhookValidatingconfigurationYaml, nil
}

func manifestsClusterManagerClusterManagerRegistrationWebhookValidatingconfigurationYaml() (*asset, error) {
	bytes, err := manifestsClusterManagerClusterManagerRegistrationWebhookValidatingconfigurationYamlBytes()
	if err != nil {
		return nil, err
	}

	info := bindataFileInfo{name: "manifests/cluster-manager/cluster-manager-registration-webhook-validatingconfiguration.yaml", size: 0, mode: os.FileMode(0), modTime: time.Unix(0, 0)}
	a := &asset{bytes: bytes, info: info}
	return a, nil
}

var _manifestsClusterManagerClusterManagerWorkWebhookApiserviceYaml = []byte(`apiVersion: apiregistration.k8s.io/v1
kind: APIService
metadata:
  name: v1.admission.work.open-cluster-management.io
spec:
  group: admission.work.open-cluster-management.io
  version: v1
  service:
    name: cluster-manager-work-webhook
    namespace: open-cluster-management-hub
  caBundle: {{ .WorkAPIServiceCABundle }}
  groupPriorityMinimum: 10000
  versionPriority: 20
`)

func manifestsClusterManagerClusterManagerWorkWebhookApiserviceYamlBytes() ([]byte, error) {
	return _manifestsClusterManagerClusterManagerWorkWebhookApiserviceYaml, nil
}

func manifestsClusterManagerClusterManagerWorkWebhookApiserviceYaml() (*asset, error) {
	bytes, err := manifestsClusterManagerClusterManagerWorkWebhookApiserviceYamlBytes()
	if err != nil {
		return nil, err
	}

	info := bindataFileInfo{name: "manifests/cluster-manager/cluster-manager-work-webhook-apiservice.yaml", size: 0, mode: os.FileMode(0), modTime: time.Unix(0, 0)}
	a := &asset{bytes: bytes, info: info}
	return a, nil
}

var _manifestsClusterManagerClusterManagerWorkWebhookClusterroleYaml = []byte(`apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
  name: open-cluster-management:{{ .ClusterManagerName }}-work:webhook
rules:
# Allow managedcluster admission to get/list/watch configmaps
- apiGroups: [""]
  resources: ["configmaps"]
  verbs: ["get", "list", "watch"]
# Allow managedcluster admission to create subjectaccessreviews
- apiGroups: ["authorization.k8s.io"]
  resources: ["subjectaccessreviews"]
  verbs: ["create"]
# API priority and fairness
- apiGroups: ["flowcontrol.apiserver.k8s.io"]
  resources: ["prioritylevelconfigurations", "flowschemas"]
  verbs: ["get", "list", "watch"]
`)

func manifestsClusterManagerClusterManagerWorkWebhookClusterroleYamlBytes() ([]byte, error) {
	return _manifestsClusterManagerClusterManagerWorkWebhookClusterroleYaml, nil
}

func manifestsClusterManagerClusterManagerWorkWebhookClusterroleYaml() (*asset, error) {
	bytes, err := manifestsClusterManagerClusterManagerWorkWebhookClusterroleYamlBytes()
	if err != nil {
		return nil, err
	}

	info := bindataFileInfo{name: "manifests/cluster-manager/cluster-manager-work-webhook-clusterrole.yaml", size: 0, mode: os.FileMode(0), modTime: time.Unix(0, 0)}
	a := &asset{bytes: bytes, info: info}
	return a, nil
}

var _manifestsClusterManagerClusterManagerWorkWebhookClusterrolebindingYaml = []byte(`apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRoleBinding
metadata:
  name: open-cluster-management:{{ .ClusterManagerName }}-work:webhook
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: ClusterRole
  name: open-cluster-management:{{ .ClusterManagerName }}-work:webhook
subjects:
  - kind: ServiceAccount
    name: {{ .ClusterManagerName }}-work-webhook-sa
    namespace: open-cluster-management-hub
`)

func manifestsClusterManagerClusterManagerWorkWebhookClusterrolebindingYamlBytes() ([]byte, error) {
	return _manifestsClusterManagerClusterManagerWorkWebhookClusterrolebindingYaml, nil
}

func manifestsClusterManagerClusterManagerWorkWebhookClusterrolebindingYaml() (*asset, error) {
	bytes, err := manifestsClusterManagerClusterManagerWorkWebhookClusterrolebindingYamlBytes()
	if err != nil {
		return nil, err
	}

	info := bindataFileInfo{name: "manifests/cluster-manager/cluster-manager-work-webhook-clusterrolebinding.yaml", size: 0, mode: os.FileMode(0), modTime: time.Unix(0, 0)}
	a := &asset{bytes: bytes, info: info}
	return a, nil
}

var _manifestsClusterManagerClusterManagerWorkWebhookDeploymentYaml = []byte(`apiVersion: apps/v1
kind: Deployment
metadata:
  name: {{ .ClusterManagerName }}-work-webhook
  namespace: open-cluster-management-hub
  labels:
    app: {{ .ClusterManagerName }}-work-webhook
spec:
  replicas: {{ .Replica }}
  selector:
    matchLabels:
      app: {{ .ClusterManagerName }}-work-webhook
  template:
    metadata:
      labels:
        app: {{ .ClusterManagerName }}-work-webhook
    spec:
      affinity:
        podAntiAffinity:
          preferredDuringSchedulingIgnoredDuringExecution:
          - weight: 70
            podAffinityTerm:
              topologyKey: failure-domain.beta.kubernetes.io/zone
              labelSelector:
                matchExpressions:
                - key: app
                  operator: In
                  values:
                  - {{ .ClusterManagerName }}-work-webhook
          - weight: 30
            podAffinityTerm:
              topologyKey: kubernetes.io/hostname
              labelSelector:
                matchExpressions:
                - key: app
                  operator: In
                  values:
                  - {{ .ClusterManagerName }}-work-webhook
      serviceAccountName: {{ .ClusterManagerName }}-work-webhook-sa
      containers:
      - name: {{ .ClusterManagerName }}-work-webhook-sa
        image: {{ .WorkImage }}
        args:
          - "/work"
          - "webhook"
          - "--secure-port=6443"
          - "--tls-cert-file=/serving-cert/tls.crt"
          - "--tls-private-key-file=/serving-cert/tls.key"
        securityContext:
          allowPrivilegeEscalation: false
          capabilities:
            drop:
              - ALL
          privileged: false
          runAsNonRoot: true
        livenessProbe:
          httpGet:
            path: /healthz
            scheme: HTTPS
            port: 6443
          initialDelaySeconds: 2
          periodSeconds: 10
        readinessProbe:
          httpGet:
            path: /healthz
            scheme: HTTPS
            port: 6443
          initialDelaySeconds: 2
        resources:
          requests:
            cpu: 100m
            memory: 128Mi
        volumeMounts:
        - name: webhook-secret
          mountPath: "/serving-cert"
          readOnly: true
      volumes:
      - name: webhook-secret
        secret:
          secretName: work-webhook-serving-cert

`)

func manifestsClusterManagerClusterManagerWorkWebhookDeploymentYamlBytes() ([]byte, error) {
	return _manifestsClusterManagerClusterManagerWorkWebhookDeploymentYaml, nil
}

func manifestsClusterManagerClusterManagerWorkWebhookDeploymentYaml() (*asset, error) {
	bytes, err := manifestsClusterManagerClusterManagerWorkWebhookDeploymentYamlBytes()
	if err != nil {
		return nil, err
	}

	info := bindataFileInfo{name: "manifests/cluster-manager/cluster-manager-work-webhook-deployment.yaml", size: 0, mode: os.FileMode(0), modTime: time.Unix(0, 0)}
	a := &asset{bytes: bytes, info: info}
	return a, nil
}

var _manifestsClusterManagerClusterManagerWorkWebhookServiceYaml = []byte(`apiVersion: v1
kind: Service
metadata:
  name: cluster-manager-work-webhook
  namespace: open-cluster-management-hub
spec:
  selector:
    app: {{ .ClusterManagerName }}-work-webhook
  ports:
  - port: 443
    targetPort: 6443
`)

func manifestsClusterManagerClusterManagerWorkWebhookServiceYamlBytes() ([]byte, error) {
	return _manifestsClusterManagerClusterManagerWorkWebhookServiceYaml, nil
}

func manifestsClusterManagerClusterManagerWorkWebhookServiceYaml() (*asset, error) {
	bytes, err := manifestsClusterManagerClusterManagerWorkWebhookServiceYamlBytes()
	if err != nil {
		return nil, err
	}

	info := bindataFileInfo{name: "manifests/cluster-manager/cluster-manager-work-webhook-service.yaml", size: 0, mode: os.FileMode(0), modTime: time.Unix(0, 0)}
	a := &asset{bytes: bytes, info: info}
	return a, nil
}

var _manifestsClusterManagerClusterManagerWorkWebhookServiceaccountYaml = []byte(`apiVersion: v1
kind: ServiceAccount
metadata:
  name: {{ .ClusterManagerName }}-work-webhook-sa
  namespace: open-cluster-management-hub
`)

func manifestsClusterManagerClusterManagerWorkWebhookServiceaccountYamlBytes() ([]byte, error) {
	return _manifestsClusterManagerClusterManagerWorkWebhookServiceaccountYaml, nil
}

func manifestsClusterManagerClusterManagerWorkWebhookServiceaccountYaml() (*asset, error) {
	bytes, err := manifestsClusterManagerClusterManagerWorkWebhookServiceaccountYamlBytes()
	if err != nil {
		return nil, err
	}

	info := bindataFileInfo{name: "manifests/cluster-manager/cluster-manager-work-webhook-serviceaccount.yaml", size: 0, mode: os.FileMode(0), modTime: time.Unix(0, 0)}
	a := &asset{bytes: bytes, info: info}
	return a, nil
}

var _manifestsClusterManagerClusterManagerWorkWebhookValidatingconfigurationYaml = []byte(`apiVersion: admissionregistration.k8s.io/v1
kind: ValidatingWebhookConfiguration
metadata:
  name: manifestworkvalidators.admission.work.open-cluster-management.io
webhooks:
- name: manifestworkvalidators.admission.work.open-cluster-management.io
  failurePolicy: Fail
  clientConfig:
    service:
      # reach the webhook via the registered aggregated API
      namespace: default
      name: kubernetes
      path: /apis/admission.work.open-cluster-management.io/v1/manifestworkvalidators
  rules:
  - operations:
    - CREATE
    - UPDATE
    apiGroups:
    - work.open-cluster-management.io
    apiVersions:
    - "*"
    resources:
    - manifestworks
  admissionReviewVersions: ["v1beta1"]
  sideEffects: None
  timeoutSeconds: 10
`)

func manifestsClusterManagerClusterManagerWorkWebhookValidatingconfigurationYamlBytes() ([]byte, error) {
	return _manifestsClusterManagerClusterManagerWorkWebhookValidatingconfigurationYaml, nil
}

func manifestsClusterManagerClusterManagerWorkWebhookValidatingconfigurationYaml() (*asset, error) {
	bytes, err := manifestsClusterManagerClusterManagerWorkWebhookValidatingconfigurationYamlBytes()
	if err != nil {
		return nil, err
	}

	info := bindataFileInfo{name: "manifests/cluster-manager/cluster-manager-work-webhook-validatingconfiguration.yaml", size: 0, mode: os.FileMode(0), modTime: time.Unix(0, 0)}
	a := &asset{bytes: bytes, info: info}
	return a, nil
}

// Asset loads and returns the asset for the given name.
// It returns an error if the asset could not be found or
// could not be loaded.
func Asset(name string) ([]byte, error) {
	cannonicalName := strings.Replace(name, "\\", "/", -1)
	if f, ok := _bindata[cannonicalName]; ok {
		a, err := f()
		if err != nil {
			return nil, fmt.Errorf("Asset %s can't read by error: %v", name, err)
		}
		return a.bytes, nil
	}
	return nil, fmt.Errorf("Asset %s not found", name)
}

// MustAsset is like Asset but panics when Asset would return an error.
// It simplifies safe initialization of global variables.
func MustAsset(name string) []byte {
	a, err := Asset(name)
	if err != nil {
		panic("asset: Asset(" + name + "): " + err.Error())
	}

	return a
}

// AssetInfo loads and returns the asset info for the given name.
// It returns an error if the asset could not be found or
// could not be loaded.
func AssetInfo(name string) (os.FileInfo, error) {
	cannonicalName := strings.Replace(name, "\\", "/", -1)
	if f, ok := _bindata[cannonicalName]; ok {
		a, err := f()
		if err != nil {
			return nil, fmt.Errorf("AssetInfo %s can't read by error: %v", name, err)
		}
		return a.info, nil
	}
	return nil, fmt.Errorf("AssetInfo %s not found", name)
}

// AssetNames returns the names of the assets.
func AssetNames() []string {
	names := make([]string, 0, len(_bindata))
	for name := range _bindata {
		names = append(names, name)
	}
	return names
}

// _bindata is a table, holding each asset generator, mapped to its name.
var _bindata = map[string]func() (*asset, error){
	"manifests/cluster-manager/0000_00_addon.open-cluster-management.io_clustermanagementaddons.crd.yaml":           manifestsClusterManager0000_00_addonOpenClusterManagementIo_clustermanagementaddonsCrdYaml,
	"manifests/cluster-manager/0000_00_clusters.open-cluster-management.io_managedclusters.crd.yaml":                manifestsClusterManager0000_00_clustersOpenClusterManagementIo_managedclustersCrdYaml,
	"manifests/cluster-manager/0000_00_clusters.open-cluster-management.io_managedclustersets.crd.yaml":             manifestsClusterManager0000_00_clustersOpenClusterManagementIo_managedclustersetsCrdYaml,
	"manifests/cluster-manager/0000_00_work.open-cluster-management.io_manifestworks.crd.yaml":                      manifestsClusterManager0000_00_workOpenClusterManagementIo_manifestworksCrdYaml,
	"manifests/cluster-manager/0000_01_addon.open-cluster-management.io_managedclusteraddons.crd.yaml":              manifestsClusterManager0000_01_addonOpenClusterManagementIo_managedclusteraddonsCrdYaml,
	"manifests/cluster-manager/0000_01_clusters.open-cluster-management.io_managedclustersetbindings.crd.yaml":      manifestsClusterManager0000_01_clustersOpenClusterManagementIo_managedclustersetbindingsCrdYaml,
	"manifests/cluster-manager/0000_03_clusters.open-cluster-management.io_placements.crd.yaml":                     manifestsClusterManager0000_03_clustersOpenClusterManagementIo_placementsCrdYaml,
	"manifests/cluster-manager/0000_04_clusters.open-cluster-management.io_placementdecisions.crd.yaml":             manifestsClusterManager0000_04_clustersOpenClusterManagementIo_placementdecisionsCrdYaml,
	"manifests/cluster-manager/cluster-manager-namespace.yaml":                                                      manifestsClusterManagerClusterManagerNamespaceYaml,
	"manifests/cluster-manager/cluster-manager-placement-clusterrole.yaml":                                          manifestsClusterManagerClusterManagerPlacementClusterroleYaml,
	"manifests/cluster-manager/cluster-manager-placement-clusterrolebinding.yaml":                                   manifestsClusterManagerClusterManagerPlacementClusterrolebindingYaml,
	"manifests/cluster-manager/cluster-manager-placement-deployment.yaml":                                           manifestsClusterManagerClusterManagerPlacementDeploymentYaml,
	"manifests/cluster-manager/cluster-manager-placement-serviceaccount.yaml":                                       manifestsClusterManagerClusterManagerPlacementServiceaccountYaml,
	"manifests/cluster-manager/cluster-manager-registration-clusterrole.yaml":                                       manifestsClusterManagerClusterManagerRegistrationClusterroleYaml,
	"manifests/cluster-manager/cluster-manager-registration-clusterrolebinding.yaml":                                manifestsClusterManagerClusterManagerRegistrationClusterrolebindingYaml,
	"manifests/cluster-manager/cluster-manager-registration-deployment.yaml":                                        manifestsClusterManagerClusterManagerRegistrationDeploymentYaml,
	"manifests/cluster-manager/cluster-manager-registration-serviceaccount.yaml":                                    manifestsClusterManagerClusterManagerRegistrationServiceaccountYaml,
	"manifests/cluster-manager/cluster-manager-registration-webhook-apiservice.yaml":                                manifestsClusterManagerClusterManagerRegistrationWebhookApiserviceYaml,
	"manifests/cluster-manager/cluster-manager-registration-webhook-clusterrole.yaml":                               manifestsClusterManagerClusterManagerRegistrationWebhookClusterroleYaml,
	"manifests/cluster-manager/cluster-manager-registration-webhook-clusterrolebinding.yaml":                        manifestsClusterManagerClusterManagerRegistrationWebhookClusterrolebindingYaml,
	"manifests/cluster-manager/cluster-manager-registration-webhook-clustersetbinding-validatingconfiguration.yaml": manifestsClusterManagerClusterManagerRegistrationWebhookClustersetbindingValidatingconfigurationYaml,
	"manifests/cluster-manager/cluster-manager-registration-webhook-deployment.yaml":                                manifestsClusterManagerClusterManagerRegistrationWebhookDeploymentYaml,
	"manifests/cluster-manager/cluster-manager-registration-webhook-mutatingconfiguration.yaml":                     manifestsClusterManagerClusterManagerRegistrationWebhookMutatingconfigurationYaml,
	"manifests/cluster-manager/cluster-manager-registration-webhook-service.yaml":                                   manifestsClusterManagerClusterManagerRegistrationWebhookServiceYaml,
	"manifests/cluster-manager/cluster-manager-registration-webhook-serviceaccount.yaml":                            manifestsClusterManagerClusterManagerRegistrationWebhookServiceaccountYaml,
	"manifests/cluster-manager/cluster-manager-registration-webhook-validatingconfiguration.yaml":                   manifestsClusterManagerClusterManagerRegistrationWebhookValidatingconfigurationYaml,
	"manifests/cluster-manager/cluster-manager-work-webhook-apiservice.yaml":                                        manifestsClusterManagerClusterManagerWorkWebhookApiserviceYaml,
	"manifests/cluster-manager/cluster-manager-work-webhook-clusterrole.yaml":                                       manifestsClusterManagerClusterManagerWorkWebhookClusterroleYaml,
	"manifests/cluster-manager/cluster-manager-work-webhook-clusterrolebinding.yaml":                                manifestsClusterManagerClusterManagerWorkWebhookClusterrolebindingYaml,
	"manifests/cluster-manager/cluster-manager-work-webhook-deployment.yaml":                                        manifestsClusterManagerClusterManagerWorkWebhookDeploymentYaml,
	"manifests/cluster-manager/cluster-manager-work-webhook-service.yaml":                                           manifestsClusterManagerClusterManagerWorkWebhookServiceYaml,
	"manifests/cluster-manager/cluster-manager-work-webhook-serviceaccount.yaml":                                    manifestsClusterManagerClusterManagerWorkWebhookServiceaccountYaml,
	"manifests/cluster-manager/cluster-manager-work-webhook-validatingconfiguration.yaml":                           manifestsClusterManagerClusterManagerWorkWebhookValidatingconfigurationYaml,
}

// AssetDir returns the file names below a certain
// directory embedded in the file by go-bindata.
// For example if you run go-bindata on data/... and data contains the
// following hierarchy:
//     data/
//       foo.txt
//       img/
//         a.png
//         b.png
// then AssetDir("data") would return []string{"foo.txt", "img"}
// AssetDir("data/img") would return []string{"a.png", "b.png"}
// AssetDir("foo.txt") and AssetDir("notexist") would return an error
// AssetDir("") will return []string{"data"}.
func AssetDir(name string) ([]string, error) {
	node := _bintree
	if len(name) != 0 {
		cannonicalName := strings.Replace(name, "\\", "/", -1)
		pathList := strings.Split(cannonicalName, "/")
		for _, p := range pathList {
			node = node.Children[p]
			if node == nil {
				return nil, fmt.Errorf("Asset %s not found", name)
			}
		}
	}
	if node.Func != nil {
		return nil, fmt.Errorf("Asset %s not found", name)
	}
	rv := make([]string, 0, len(node.Children))
	for childName := range node.Children {
		rv = append(rv, childName)
	}
	return rv, nil
}

type bintree struct {
	Func     func() (*asset, error)
	Children map[string]*bintree
}

var _bintree = &bintree{nil, map[string]*bintree{
	"manifests": {nil, map[string]*bintree{
		"cluster-manager": {nil, map[string]*bintree{
			"0000_00_addon.open-cluster-management.io_clustermanagementaddons.crd.yaml":           {manifestsClusterManager0000_00_addonOpenClusterManagementIo_clustermanagementaddonsCrdYaml, map[string]*bintree{}},
			"0000_00_clusters.open-cluster-management.io_managedclusters.crd.yaml":                {manifestsClusterManager0000_00_clustersOpenClusterManagementIo_managedclustersCrdYaml, map[string]*bintree{}},
			"0000_00_clusters.open-cluster-management.io_managedclustersets.crd.yaml":             {manifestsClusterManager0000_00_clustersOpenClusterManagementIo_managedclustersetsCrdYaml, map[string]*bintree{}},
			"0000_00_work.open-cluster-management.io_manifestworks.crd.yaml":                      {manifestsClusterManager0000_00_workOpenClusterManagementIo_manifestworksCrdYaml, map[string]*bintree{}},
			"0000_01_addon.open-cluster-management.io_managedclusteraddons.crd.yaml":              {manifestsClusterManager0000_01_addonOpenClusterManagementIo_managedclusteraddonsCrdYaml, map[string]*bintree{}},
			"0000_01_clusters.open-cluster-management.io_managedclustersetbindings.crd.yaml":      {manifestsClusterManager0000_01_clustersOpenClusterManagementIo_managedclustersetbindingsCrdYaml, map[string]*bintree{}},
			"0000_03_clusters.open-cluster-management.io_placements.crd.yaml":                     {manifestsClusterManager0000_03_clustersOpenClusterManagementIo_placementsCrdYaml, map[string]*bintree{}},
			"0000_04_clusters.open-cluster-management.io_placementdecisions.crd.yaml":             {manifestsClusterManager0000_04_clustersOpenClusterManagementIo_placementdecisionsCrdYaml, map[string]*bintree{}},
			"cluster-manager-namespace.yaml":                                                      {manifestsClusterManagerClusterManagerNamespaceYaml, map[string]*bintree{}},
			"cluster-manager-placement-clusterrole.yaml":                                          {manifestsClusterManagerClusterManagerPlacementClusterroleYaml, map[string]*bintree{}},
			"cluster-manager-placement-clusterrolebinding.yaml":                                   {manifestsClusterManagerClusterManagerPlacementClusterrolebindingYaml, map[string]*bintree{}},
			"cluster-manager-placement-deployment.yaml":                                           {manifestsClusterManagerClusterManagerPlacementDeploymentYaml, map[string]*bintree{}},
			"cluster-manager-placement-serviceaccount.yaml":                                       {manifestsClusterManagerClusterManagerPlacementServiceaccountYaml, map[string]*bintree{}},
			"cluster-manager-registration-clusterrole.yaml":                                       {manifestsClusterManagerClusterManagerRegistrationClusterroleYaml, map[string]*bintree{}},
			"cluster-manager-registration-clusterrolebinding.yaml":                                {manifestsClusterManagerClusterManagerRegistrationClusterrolebindingYaml, map[string]*bintree{}},
			"cluster-manager-registration-deployment.yaml":                                        {manifestsClusterManagerClusterManagerRegistrationDeploymentYaml, map[string]*bintree{}},
			"cluster-manager-registration-serviceaccount.yaml":                                    {manifestsClusterManagerClusterManagerRegistrationServiceaccountYaml, map[string]*bintree{}},
			"cluster-manager-registration-webhook-apiservice.yaml":                                {manifestsClusterManagerClusterManagerRegistrationWebhookApiserviceYaml, map[string]*bintree{}},
			"cluster-manager-registration-webhook-clusterrole.yaml":                               {manifestsClusterManagerClusterManagerRegistrationWebhookClusterroleYaml, map[string]*bintree{}},
			"cluster-manager-registration-webhook-clusterrolebinding.yaml":                        {manifestsClusterManagerClusterManagerRegistrationWebhookClusterrolebindingYaml, map[string]*bintree{}},
			"cluster-manager-registration-webhook-clustersetbinding-validatingconfiguration.yaml": {manifestsClusterManagerClusterManagerRegistrationWebhookClustersetbindingValidatingconfigurationYaml, map[string]*bintree{}},
			"cluster-manager-registration-webhook-deployment.yaml":                                {manifestsClusterManagerClusterManagerRegistrationWebhookDeploymentYaml, map[string]*bintree{}},
			"cluster-manager-registration-webhook-mutatingconfiguration.yaml":                     {manifestsClusterManagerClusterManagerRegistrationWebhookMutatingconfigurationYaml, map[string]*bintree{}},
			"cluster-manager-registration-webhook-service.yaml":                                   {manifestsClusterManagerClusterManagerRegistrationWebhookServiceYaml, map[string]*bintree{}},
			"cluster-manager-registration-webhook-serviceaccount.yaml":                            {manifestsClusterManagerClusterManagerRegistrationWebhookServiceaccountYaml, map[string]*bintree{}},
			"cluster-manager-registration-webhook-validatingconfiguration.yaml":                   {manifestsClusterManagerClusterManagerRegistrationWebhookValidatingconfigurationYaml, map[string]*bintree{}},
			"cluster-manager-work-webhook-apiservice.yaml":                                        {manifestsClusterManagerClusterManagerWorkWebhookApiserviceYaml, map[string]*bintree{}},
			"cluster-manager-work-webhook-clusterrole.yaml":                                       {manifestsClusterManagerClusterManagerWorkWebhookClusterroleYaml, map[string]*bintree{}},
			"cluster-manager-work-webhook-clusterrolebinding.yaml":                                {manifestsClusterManagerClusterManagerWorkWebhookClusterrolebindingYaml, map[string]*bintree{}},
			"cluster-manager-work-webhook-deployment.yaml":                                        {manifestsClusterManagerClusterManagerWorkWebhookDeploymentYaml, map[string]*bintree{}},
			"cluster-manager-work-webhook-service.yaml":                                           {manifestsClusterManagerClusterManagerWorkWebhookServiceYaml, map[string]*bintree{}},
			"cluster-manager-work-webhook-serviceaccount.yaml":                                    {manifestsClusterManagerClusterManagerWorkWebhookServiceaccountYaml, map[string]*bintree{}},
			"cluster-manager-work-webhook-validatingconfiguration.yaml":                           {manifestsClusterManagerClusterManagerWorkWebhookValidatingconfigurationYaml, map[string]*bintree{}},
		}},
	}},
}}

// RestoreAsset restores an asset under the given directory
func RestoreAsset(dir, name string) error {
	data, err := Asset(name)
	if err != nil {
		return err
	}
	info, err := AssetInfo(name)
	if err != nil {
		return err
	}
	err = os.MkdirAll(_filePath(dir, filepath.Dir(name)), os.FileMode(0755))
	if err != nil {
		return err
	}
	err = ioutil.WriteFile(_filePath(dir, name), data, info.Mode())
	if err != nil {
		return err
	}
	err = os.Chtimes(_filePath(dir, name), info.ModTime(), info.ModTime())
	if err != nil {
		return err
	}
	return nil
}

// RestoreAssets restores an asset under the given directory recursively
func RestoreAssets(dir, name string) error {
	children, err := AssetDir(name)
	// File
	if err != nil {
		return RestoreAsset(dir, name)
	}
	// Dir
	for _, child := range children {
		err = RestoreAssets(dir, filepath.Join(name, child))
		if err != nil {
			return err
		}
	}
	return nil
}

func _filePath(dir, name string) string {
	cannonicalName := strings.Replace(name, "\\", "/", -1)
	return filepath.Join(append([]string{dir}, strings.Split(cannonicalName, "/")...)...)
}
