// Code generated for package bindata by go-bindata DO NOT EDIT. (@generated)
// sources:
// manifests/cluster-manager/0000_00_clusters.open-cluster-management.io_spokeclusters.crd.yaml
// manifests/cluster-manager/0000_00_work.open-cluster-management.io_manifestworks.crd.yaml
// manifests/cluster-manager/cluster-manager-clusterrolebinding.yaml
// manifests/cluster-manager/cluster-manager-namespace.yaml
// manifests/cluster-manager/cluster-manager-registration-clusterrole.yaml
// manifests/cluster-manager/cluster-manager-registration-clusterrolebinding.yaml
// manifests/cluster-manager/cluster-manager-registration-deployment.yaml
// manifests/cluster-manager/cluster-manager-registration-serviceaccount.yaml
// manifests/cluster-manager/cluster-manager-registration-webhook-apiservice.yaml
// manifests/cluster-manager/cluster-manager-registration-webhook-clusterrole.yaml
// manifests/cluster-manager/cluster-manager-registration-webhook-clusterrolebinding.yaml
// manifests/cluster-manager/cluster-manager-registration-webhook-deployment.yaml
// manifests/cluster-manager/cluster-manager-registration-webhook-secret.yaml
// manifests/cluster-manager/cluster-manager-registration-webhook-service.yaml
// manifests/cluster-manager/cluster-manager-registration-webhook-serviceaccount.yaml
// manifests/cluster-manager/cluster-manager-registration-webhook-validatingconfiguration.yaml
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

var _manifestsClusterManager0000_00_clustersOpenClusterManagementIo_spokeclustersCrdYaml = []byte(`apiVersion: apiextensions.k8s.io/v1beta1
kind: CustomResourceDefinition
metadata:
  name: spokeclusters.cluster.open-cluster-management.io
spec:
  group: cluster.open-cluster-management.io
  names:
    kind: SpokeCluster
    listKind: SpokeClusterList
    plural: spokeclusters
    singular: spokecluster
  scope: "Cluster"
  subresources:
    status: {}
  preserveUnknownFields: false
  validation:
    openAPIV3Schema:
      description: "SpokeCluster represents the desired state and current status of
        spoke cluster. SpokeCluster is a cluster scoped resource. The name is the
        cluster UID. \n The cluster join process follows a double opt-in process:
        \n 1. agent on spoke cluster creates CSR on hub with cluster UID and agent
        name. 2. agent on spoke cluster creates spokecluster on hub. 3. cluster admin
        on hub approves the CSR for the spoke's cluster UID and agent name. 4. cluster
        admin set spec.acceptSpokeCluster of spokecluster to true. 5. cluster admin
        on spoke creates credential of kubeconfig to spoke. \n Once the hub creates
        the cluster namespace, the spoke agent pushes the credential to the hub to
        use against the spoke's kube-apiserver."
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
          description: Spec represents a desired configuration for the agent on the
            spoke cluster.
          type: object
          properties:
            hubAcceptsClient:
              description: AcceptSpokeCluster reprsents that hub accepts the join
                of spoke agent. Its default value is false, and can only be set true
                when the user on hub has an RBAC rule to UPDATE on the virtual subresource
                of spokeclusters/accept. When the vaule is set true, a namespace whose
                name is same as the name of SpokeCluster is created on hub representing
                the spoke cluster, also role/rolebinding is created on the namespace
                to grant the permision of access from agent on spoke. When the value
                is set false, the namespace representing the spoke cluster is deleted.
              type: boolean
            leaseDurationSeconds:
              description: LeaseDurationSeconds is used to coordinate the lease update
                time of spoke agents. If its value is zero, the spoke agent will update
                its lease per 60s by default
              type: integer
              format: int32
            spokeClientConfigs:
              description: SpokeClientConfigs represents a list of the apiserver address
                of the spoke cluster. If it is empty, spoke cluster has no accessible
                address to be visited from hub.
              type: array
              items:
                description: ClientConfig represents the apiserver address of the
                  spoke cluster. TODO include credential to connect to spoke cluster
                  kube-apiserver
                type: object
                properties:
                  caBundle:
                    description: CABundle is the ca bundle to connect to apiserver
                      of the spoke cluster. System certs are used if it is not set.
                    type: string
                    format: byte
                  url:
                    description: URL is the url of apiserver endpoint of the spoke
                      cluster.
                    type: string
        status:
          description: Status represents the current status of joined spoke cluster
          type: object
          properties:
            allocatable:
              description: Allocatable represents the total allocatable resources
                on the spoke cluster.
              type: object
              additionalProperties:
                type: string
            capacity:
              description: Capacity represents the total resource capacity from all
                nodeStatuses on the spoke cluster.
              type: object
              additionalProperties:
                type: string
            conditions:
              description: Conditions contains the different condition statuses for
                this spoke cluster.
              type: array
              items:
                description: StatusCondition contains condition information for a
                  spoke cluster.
                type: object
                properties:
                  lastTransitionTime:
                    description: LastTransitionTime is the last time the condition
                      changed from one status to another.
                    type: string
                    format: date-time
                  message:
                    description: Message is a human-readable message indicating details
                      about the last status change.
                    type: string
                  reason:
                    description: Reason is a (brief) reason for the condition's last
                      status change.
                    type: string
                  status:
                    description: Status is the status of the condition. One of True,
                      False, Unknown.
                    type: string
                  type:
                    description: Type is the type of the cluster condition.
                    type: string
            version:
              description: Version represents the kubernetes version of the spoke
                cluster.
              type: object
              properties:
                kubernetes:
                  description: Kubernetes is the kubernetes version of spoke cluster
                  type: string
  version: v1
  versions:
  - name: v1
    served: true
    storage: true
status:
  acceptedNames:
    kind: ""
    plural: ""
  conditions: []
  storedVersions: []
`)

func manifestsClusterManager0000_00_clustersOpenClusterManagementIo_spokeclustersCrdYamlBytes() ([]byte, error) {
	return _manifestsClusterManager0000_00_clustersOpenClusterManagementIo_spokeclustersCrdYaml, nil
}

func manifestsClusterManager0000_00_clustersOpenClusterManagementIo_spokeclustersCrdYaml() (*asset, error) {
	bytes, err := manifestsClusterManager0000_00_clustersOpenClusterManagementIo_spokeclustersCrdYamlBytes()
	if err != nil {
		return nil, err
	}

	info := bindataFileInfo{name: "manifests/cluster-manager/0000_00_clusters.open-cluster-management.io_spokeclusters.crd.yaml", size: 0, mode: os.FileMode(0), modTime: time.Unix(0, 0)}
	a := &asset{bytes: bytes, info: info}
	return a, nil
}

var _manifestsClusterManager0000_00_workOpenClusterManagementIo_manifestworksCrdYaml = []byte(`apiVersion: apiextensions.k8s.io/v1beta1
kind: CustomResourceDefinition
metadata:
  creationTimestamp: null
  name: manifestworks.work.open-cluster-management.io
spec:
  group: work.open-cluster-management.io
  names:
    kind: ManifestWork
    listKind: ManifestWorkList
    plural: manifestworks
    singular: manifestwork
  scope: "Namespaced"
  preserveUnknownFields: false
  subresources:
    status: {}
  validation:
    openAPIV3Schema:
      description: ManifestWork represents a manifests workload that hub wants to
        deploy on the spoke cluster. A manifest workload is defined as a set of kubernetes
        resources. ManifestWork must be created in the cluster namespace on the hub,
        so that agent on the corresponding spoke cluster can access this resource
        and deploy on the spoke cluster.
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
            on the spoke cluster.
          type: object
          properties:
            workload:
              description: Workload represents the manifest workload to be deployed
                on spoke cluster
              type: object
              properties:
                manifests:
                  description: Manifests represents a list of kuberenetes resources
                    to be deployed on the spoke cluster.
                  type: array
                  items:
                    description: Manifest represents a resource to be deployed on
                      spoke cluster
                    type: object
                    x-kubernetes-preserve-unknown-fields: true
                    x-kubernetes-embedded-resource: true
        status:
          description: Status represents the current status of work
          type: object
          properties:
            appliedResources:
              description: AppliedResources represents a list of resources defined
                within the manifestwork that are applied. Only resources with valid
                GroupVersionResource, namespace, and name are suitable. An item in
                this slice is deleted when there is no mapped manifest in manifestwork.Spec
                or by finalizer. The resource relating to the item will also be removed
                from spoke cluster. The deleted resource may still be present until
                the finalizers for that resource are finished. However, the resource
                will not be undeleted, so it can be removed from this list and eventual
                consistency is preserved.
              type: array
              items:
                description: AppliedManifestResourceMeta represents the gvr, name
                  and namespace of a resource. Since these resources have been created,
                  they must have valid group, version, resource, namespace, and name.
                type: object
                properties:
                  group:
                    description: Group is the API Group of the kubernetes resource
                    type: string
                  name:
                    description: Name is the name of the kubernetes resource
                    type: string
                  namespace:
                    description: Name is the namespace of the kubernetes resource,
                      empty string indicates it is a cluster scoped resource.
                    type: string
                  resource:
                    description: Resource is the resource name of the kubernetes resource
                    type: string
                  version:
                    description: Version is the version of the kubernetes resource
                    type: string
            conditions:
              description: 'Conditions contains the different condition statuses for
                this work. Valid condition types are: 1. Applied represents workload
                in ManifestWork is applied successfully on spoke cluster. 2. Progressing
                represents workload in ManifestWork is being applied on spoke cluster.
                3. Available represents workload in ManifestWork exists on the spoke
                cluster. 4. Degraded represents the current state of workload does
                not match the desired state for a certain period.'
              type: array
              items:
                description: StatusCondition contains condition information for a
                  spoke work.
                type: object
                properties:
                  lastTransitionTime:
                    description: LastTransitionTime is the last time the condition
                      changed from one status to another.
                    type: string
                    format: date-time
                  message:
                    description: Message is a human-readable message indicating details
                      about the last status change.
                    type: string
                  reason:
                    description: Reason is a (brief) reason for the condition's last
                      status change.
                    type: string
                  status:
                    description: Status is the status of the condition. One of True,
                      False, Unknown.
                    type: string
                  type:
                    description: Type is the type of the spoke work condition.
                    type: string
            resourceStatus:
              description: ResourceStatus represents the status of each resource in
                manifestwork deployed on spoke cluster. The agent on spoke cluster
                syncs the condition from spoke to the hub.
              type: object
              properties:
                manifests:
                  description: 'Manifests represents the condition of manifests deployed
                    on spoke cluster. Valid condition types are: 1. Progressing represents
                    the resource is being applied on spoke cluster. 2. Applied represents
                    the resource is applied successfully on spoke cluster. 3. Available
                    represents the resource exists on the spoke cluster. 4. Degraded
                    represents the current state of resource does not match the desired
                    state for a certain period.'
                  type: array
                  items:
                    description: ManifestCondition represents the conditions of the
                      resources deployed on spoke cluster
                    type: object
                    properties:
                      conditions:
                        description: Conditions represents the conditions of this
                          resource on spoke cluster
                        type: array
                        items:
                          description: StatusCondition contains condition information
                            for a spoke work.
                          type: object
                          properties:
                            lastTransitionTime:
                              description: LastTransitionTime is the last time the
                                condition changed from one status to another.
                              type: string
                              format: date-time
                            message:
                              description: Message is a human-readable message indicating
                                details about the last status change.
                              type: string
                            reason:
                              description: Reason is a (brief) reason for the condition's
                                last status change.
                              type: string
                            status:
                              description: Status is the status of the condition.
                                One of True, False, Unknown.
                              type: string
                            type:
                              description: Type is the type of the spoke work condition.
                              type: string
                      resourceMeta:
                        description: ResourceMeta represents the gvk, name and namespace
                          of a resoure
                        type: object
                        properties:
                          group:
                            description: Group is the API Group of the kubernetes
                              resource
                            type: string
                          kind:
                            description: Kind is the kind of the kubernetes resource
                            type: string
                          name:
                            description: Name is the name of the kubernetes resource
                            type: string
                          namespace:
                            description: Name is the namespace of the kubernetes resource
                            type: string
                          ordinal:
                            description: Ordinal represents the index of the manifest
                              on spec
                            type: integer
                            format: int32
                          resource:
                            description: Resource is the resource name of the kubernetes
                              resource
                            type: string
                          version:
                            description: Version is the version of the kubernetes
                              resource
                            type: string
  version: v1
  versions:
  - name: v1
    served: true
    storage: true
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

var _manifestsClusterManagerClusterManagerClusterrolebindingYaml = []byte(`apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRoleBinding
metadata:
  name: system:open-cluster-management:{{ .ClusterManagerName }}
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: ClusterRole
  name: system:open-cluster-management:{{ .ClusterManagerName }}
subjects:
- kind: ServiceAccount
  namespace: {{ .ClusterManagerNamespace }}
  name: {{ .ClusterManagerName }}-sa
`)

func manifestsClusterManagerClusterManagerClusterrolebindingYamlBytes() ([]byte, error) {
	return _manifestsClusterManagerClusterManagerClusterrolebindingYaml, nil
}

func manifestsClusterManagerClusterManagerClusterrolebindingYaml() (*asset, error) {
	bytes, err := manifestsClusterManagerClusterManagerClusterrolebindingYamlBytes()
	if err != nil {
		return nil, err
	}

	info := bindataFileInfo{name: "manifests/cluster-manager/cluster-manager-clusterrolebinding.yaml", size: 0, mode: os.FileMode(0), modTime: time.Unix(0, 0)}
	a := &asset{bytes: bytes, info: info}
	return a, nil
}

var _manifestsClusterManagerClusterManagerNamespaceYaml = []byte(`apiVersion: v1
kind: Namespace
metadata:
  name: {{ .ClusterManagerNamespace }}
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

var _manifestsClusterManagerClusterManagerRegistrationClusterroleYaml = []byte(`apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
  name: system:open-cluster-management:{{ .ClusterManagerName }}-registration-controller
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
# Allow hub to manage spokeclusters
- apiGroups: ["cluster.open-cluster-management.io"]
  resources: ["spokeclusters"]
  verbs: ["get", "list", "watch", "update", "patch"]
- apiGroups: ["cluster.open-cluster-management.io"]
  resources: ["spokeclusters/status"]
  verbs: ["update", "patch"]
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
  name: system:open-cluster-management:{{ .ClusterManagerName }}-registration-controller
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: ClusterRole
  name: system:open-cluster-management:{{ .ClusterManagerName }}-registration-controller
subjects:
- kind: ServiceAccount
  namespace: {{ .ClusterManagerNamespace }}
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
  namespace: {{ .ClusterManagerNamespace }}
  labels:
    app: clustermanager-controller
spec:
  replicas: 3
  selector:
    matchLabels:
      app: clustermanager-registration-controller
  template:
    metadata:
      labels:
        app: clustermanager-registration-controller
    spec:
      serviceAccountName: {{ .ClusterManagerName }}-registration-controller-sa
      containers:
      - name: hub-registration-controller
        image: {{ .RegistrationImage }}
        imagePullPolicy: IfNotPresent
        args:
          - "/registration"
          - "controller"
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
  namespace: {{ .ClusterManagerNamespace }}
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
    name: {{ .ClusterManagerWebhookRegistrationService }}
    namespace: {{ .ClusterManagerNamespace }}
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
  name: system:open-cluster-management:{{ .ClusterManagerName }}-registration-webhook
rules:
# Allow spokecluster admission to get/list/watch configmaps
- apiGroups: [""]
  resources: ["configmaps"]
  verbs: ["get", "list", "watch"]
# Allow spokecluster admission to create subjectaccessreviews
- apiGroups: ["authorization.k8s.io"]
  resources: ["subjectaccessreviews"]
  verbs: ["create"]
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
  name: system:open-cluster-management:{{ .ClusterManagerName }}-registration-webhook
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: ClusterRole
  name: system:open-cluster-management:{{ .ClusterManagerName }}-registration-webhook
subjects:
  - kind: ServiceAccount
    name: {{ .ClusterManagerName }}-registration-webhook-sa
    namespace: {{ .ClusterManagerNamespace }}
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

var _manifestsClusterManagerClusterManagerRegistrationWebhookDeploymentYaml = []byte(`apiVersion: apps/v1
kind: Deployment
metadata:
  name: {{ .ClusterManagerName }}-registration-webhook
  namespace: {{ .ClusterManagerNamespace }}
  labels:
    app: {{ .ClusterManagerName }}-registration-webhook
spec:
  replicas: 3
  selector:
    matchLabels:
      app: {{ .ClusterManagerName }}-registration-webhook
  template:
    metadata:
      labels:
        app: {{ .ClusterManagerName }}-registration-webhook
    spec:
      serviceAccountName: {{ .ClusterManagerName }}-registration-webhook-sa
      containers:
      - name: {{ .ClusterManagerName }}-registration-webhook-sa
        image: {{ .RegistrationImage }}
        imagePullPolicy: IfNotPresent
        args:
          - "/registration"
          - "webhook"
          - "--secure-port=6443"
          - "--tls-cert-file=/serving-cert/tls.crt"
          - "--tls-private-key-file=/serving-cert/tls.key"
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
        volumeMounts:
        - name: webhook-secret
          mountPath: "/serving-cert"
          readOnly: true
      volumes:
      - name: webhook-secret
        secret:
          secretName: {{ .ClusterManagerWebhookSecret }}

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

var _manifestsClusterManagerClusterManagerRegistrationWebhookSecretYaml = []byte(`apiVersion: v1
kind: Secret
metadata:
  name: {{ .ClusterManagerWebhookSecret }}
  namespace: {{ .ClusterManagerNamespace }}
data:
  tls.crt: {{ .RegistrationServingCert }}
  tls.key: {{ .RegistrationServingKey }}
  ca.crt: {{ .RegistrationAPIServiceCABundle }}
type: Opaque
`)

func manifestsClusterManagerClusterManagerRegistrationWebhookSecretYamlBytes() ([]byte, error) {
	return _manifestsClusterManagerClusterManagerRegistrationWebhookSecretYaml, nil
}

func manifestsClusterManagerClusterManagerRegistrationWebhookSecretYaml() (*asset, error) {
	bytes, err := manifestsClusterManagerClusterManagerRegistrationWebhookSecretYamlBytes()
	if err != nil {
		return nil, err
	}

	info := bindataFileInfo{name: "manifests/cluster-manager/cluster-manager-registration-webhook-secret.yaml", size: 0, mode: os.FileMode(0), modTime: time.Unix(0, 0)}
	a := &asset{bytes: bytes, info: info}
	return a, nil
}

var _manifestsClusterManagerClusterManagerRegistrationWebhookServiceYaml = []byte(`apiVersion: v1
kind: Service
metadata:
  name: {{ .ClusterManagerWebhookRegistrationService }}
  namespace: {{ .ClusterManagerNamespace }}
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
  namespace: {{ .ClusterManagerNamespace }}
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
  name: spokeclustervalidators.admission.cluster.open-cluster-management.io
webhooks:
- name: spokeclustervalidators.admission.cluster.open-cluster-management.io
  failurePolicy: Fail
  clientConfig:
    service:
      # reach the webhook via the registered aggregated API
      namespace: default
      name: kubernetes
      path: /apis/admission.cluster.open-cluster-management.io/v1/spokeclustervalidators
  rules:
  - operations:
    - CREATE
    - UPDATE
    apiGroups:
    - cluster.open-cluster-management.io
    apiVersions:
    - "*"
    resources:
    - spokeclusters
  admissionReviewVersions: ["v1beta1"]
  sideEffects: None
  timeoutSeconds: 3
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
	"manifests/cluster-manager/0000_00_clusters.open-cluster-management.io_spokeclusters.crd.yaml": manifestsClusterManager0000_00_clustersOpenClusterManagementIo_spokeclustersCrdYaml,
	"manifests/cluster-manager/0000_00_work.open-cluster-management.io_manifestworks.crd.yaml":     manifestsClusterManager0000_00_workOpenClusterManagementIo_manifestworksCrdYaml,
	"manifests/cluster-manager/cluster-manager-clusterrolebinding.yaml":                            manifestsClusterManagerClusterManagerClusterrolebindingYaml,
	"manifests/cluster-manager/cluster-manager-namespace.yaml":                                     manifestsClusterManagerClusterManagerNamespaceYaml,
	"manifests/cluster-manager/cluster-manager-registration-clusterrole.yaml":                      manifestsClusterManagerClusterManagerRegistrationClusterroleYaml,
	"manifests/cluster-manager/cluster-manager-registration-clusterrolebinding.yaml":               manifestsClusterManagerClusterManagerRegistrationClusterrolebindingYaml,
	"manifests/cluster-manager/cluster-manager-registration-deployment.yaml":                       manifestsClusterManagerClusterManagerRegistrationDeploymentYaml,
	"manifests/cluster-manager/cluster-manager-registration-serviceaccount.yaml":                   manifestsClusterManagerClusterManagerRegistrationServiceaccountYaml,
	"manifests/cluster-manager/cluster-manager-registration-webhook-apiservice.yaml":               manifestsClusterManagerClusterManagerRegistrationWebhookApiserviceYaml,
	"manifests/cluster-manager/cluster-manager-registration-webhook-clusterrole.yaml":              manifestsClusterManagerClusterManagerRegistrationWebhookClusterroleYaml,
	"manifests/cluster-manager/cluster-manager-registration-webhook-clusterrolebinding.yaml":       manifestsClusterManagerClusterManagerRegistrationWebhookClusterrolebindingYaml,
	"manifests/cluster-manager/cluster-manager-registration-webhook-deployment.yaml":               manifestsClusterManagerClusterManagerRegistrationWebhookDeploymentYaml,
	"manifests/cluster-manager/cluster-manager-registration-webhook-secret.yaml":                   manifestsClusterManagerClusterManagerRegistrationWebhookSecretYaml,
	"manifests/cluster-manager/cluster-manager-registration-webhook-service.yaml":                  manifestsClusterManagerClusterManagerRegistrationWebhookServiceYaml,
	"manifests/cluster-manager/cluster-manager-registration-webhook-serviceaccount.yaml":           manifestsClusterManagerClusterManagerRegistrationWebhookServiceaccountYaml,
	"manifests/cluster-manager/cluster-manager-registration-webhook-validatingconfiguration.yaml":  manifestsClusterManagerClusterManagerRegistrationWebhookValidatingconfigurationYaml,
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
			"0000_00_clusters.open-cluster-management.io_spokeclusters.crd.yaml": {manifestsClusterManager0000_00_clustersOpenClusterManagementIo_spokeclustersCrdYaml, map[string]*bintree{}},
			"0000_00_work.open-cluster-management.io_manifestworks.crd.yaml":     {manifestsClusterManager0000_00_workOpenClusterManagementIo_manifestworksCrdYaml, map[string]*bintree{}},
			"cluster-manager-clusterrolebinding.yaml":                            {manifestsClusterManagerClusterManagerClusterrolebindingYaml, map[string]*bintree{}},
			"cluster-manager-namespace.yaml":                                     {manifestsClusterManagerClusterManagerNamespaceYaml, map[string]*bintree{}},
			"cluster-manager-registration-clusterrole.yaml":                      {manifestsClusterManagerClusterManagerRegistrationClusterroleYaml, map[string]*bintree{}},
			"cluster-manager-registration-clusterrolebinding.yaml":               {manifestsClusterManagerClusterManagerRegistrationClusterrolebindingYaml, map[string]*bintree{}},
			"cluster-manager-registration-deployment.yaml":                       {manifestsClusterManagerClusterManagerRegistrationDeploymentYaml, map[string]*bintree{}},
			"cluster-manager-registration-serviceaccount.yaml":                   {manifestsClusterManagerClusterManagerRegistrationServiceaccountYaml, map[string]*bintree{}},
			"cluster-manager-registration-webhook-apiservice.yaml":               {manifestsClusterManagerClusterManagerRegistrationWebhookApiserviceYaml, map[string]*bintree{}},
			"cluster-manager-registration-webhook-clusterrole.yaml":              {manifestsClusterManagerClusterManagerRegistrationWebhookClusterroleYaml, map[string]*bintree{}},
			"cluster-manager-registration-webhook-clusterrolebinding.yaml":       {manifestsClusterManagerClusterManagerRegistrationWebhookClusterrolebindingYaml, map[string]*bintree{}},
			"cluster-manager-registration-webhook-deployment.yaml":               {manifestsClusterManagerClusterManagerRegistrationWebhookDeploymentYaml, map[string]*bintree{}},
			"cluster-manager-registration-webhook-secret.yaml":                   {manifestsClusterManagerClusterManagerRegistrationWebhookSecretYaml, map[string]*bintree{}},
			"cluster-manager-registration-webhook-service.yaml":                  {manifestsClusterManagerClusterManagerRegistrationWebhookServiceYaml, map[string]*bintree{}},
			"cluster-manager-registration-webhook-serviceaccount.yaml":           {manifestsClusterManagerClusterManagerRegistrationWebhookServiceaccountYaml, map[string]*bintree{}},
			"cluster-manager-registration-webhook-validatingconfiguration.yaml":  {manifestsClusterManagerClusterManagerRegistrationWebhookValidatingconfigurationYaml, map[string]*bintree{}},
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
