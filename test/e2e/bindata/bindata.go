// Code generated for package bindata by go-bindata DO NOT EDIT. (@generated)
// sources:
// deploy/spoke/appliedmanifestworks.crd.yaml
// deploy/spoke/cluster_namespace.yaml
// deploy/spoke/clusterrole.yaml
// deploy/spoke/clusterrole_binding.yaml
// deploy/spoke/clusterrole_binding_addition.yaml
// deploy/spoke/component_namespace.yaml
// deploy/spoke/deployment.yaml
// deploy/spoke/kustomization.yaml
// deploy/spoke/manifestworks.crd.yaml
// deploy/spoke/service_account.yaml
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

var _deploySpokeAppliedmanifestworksCrdYaml = []byte(`apiVersion: apiextensions.k8s.io/v1
kind: CustomResourceDefinition
metadata:
  name: appliedmanifestworks.work.open-cluster-management.io
spec:
  group: work.open-cluster-management.io
  names:
    kind: AppliedManifestWork
    listKind: AppliedManifestWorkList
    plural: appliedmanifestworks
    singular: appliedmanifestwork
  scope: Cluster
  preserveUnknownFields: false
  versions:
  - name: v1
    schema:
      openAPIV3Schema:
        description: AppliedManifestWork represents an applied manifestwork on managed
          cluster that is placed on a managed cluster. An AppliedManifestWork links
          to a manifestwork on a hub recording resources deployed in the managed cluster.
          When the agent is removed from managed cluster, cluster-admin on managed
          cluster can delete appliedmanifestwork to remove resources deployed by the
          agent. The name of the appliedmanifestwork must be in the format of {hash
          of hub's first kube-apiserver url}-{manifestwork name}
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
            description: Spec represents the desired configuration of AppliedManifestWork.
            type: object
            properties:
              hubHash:
                description: HubHash represents the hash of the first hub kube apiserver
                  to identify which hub this AppliedManifestWork links to.
                type: string
              manifestWorkName:
                description: ManifestWorkName represents the name of the related manifestwork
                  on the hub.
                type: string
          status:
            description: Status represents the current status of AppliedManifestWork.
            type: object
            properties:
              appliedResources:
                description: AppliedResources represents a list of resources defined
                  within the manifestwork that are applied. Only resources with valid
                  GroupVersionResource, namespace, and name are suitable. An item
                  in this slice is deleted when there is no mapped manifest in manifestwork.Spec
                  or by finalizer. The resource relating to the item will also be
                  removed from managed cluster. The deleted resource may still be
                  present until the finalizers for that resource are finished. However,
                  the resource will not be undeleted, so it can be removed from this
                  list and eventual consistency is preserved.
                type: array
                items:
                  description: AppliedManifestResourceMeta represents the group, version,
                    resource, name and namespace of a resource. Since these resources
                    have been created, they must have valid group, version, resource,
                    namespace, and name.
                  type: object
                  properties:
                    group:
                      description: Group is the API Group of the Kubernetes resource.
                      type: string
                    name:
                      description: Name is the name of the Kubernetes resource.
                      type: string
                    namespace:
                      description: Name is the namespace of the Kubernetes resource,
                        empty string indicates it is a cluster scoped resource.
                      type: string
                    resource:
                      description: Resource is the resource name of the Kubernetes
                        resource.
                      type: string
                    uid:
                      description: UID is set on successful deletion of the Kubernetes
                        resource by controller. The resource might be still visible
                        on the managed cluster after this field is set. It is not
                        directly settable by a client.
                      type: string
                    version:
                      description: Version is the version of the Kubernetes resource.
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

func deploySpokeAppliedmanifestworksCrdYamlBytes() ([]byte, error) {
	return _deploySpokeAppliedmanifestworksCrdYaml, nil
}

func deploySpokeAppliedmanifestworksCrdYaml() (*asset, error) {
	bytes, err := deploySpokeAppliedmanifestworksCrdYamlBytes()
	if err != nil {
		return nil, err
	}

	info := bindataFileInfo{name: "deploy/spoke/appliedmanifestworks.crd.yaml", size: 0, mode: os.FileMode(0), modTime: time.Unix(0, 0)}
	a := &asset{bytes: bytes, info: info}
	return a, nil
}

var _deploySpokeCluster_namespaceYaml = []byte(`apiVersion: v1
kind: Namespace
metadata:
  name: cluster1
`)

func deploySpokeCluster_namespaceYamlBytes() ([]byte, error) {
	return _deploySpokeCluster_namespaceYaml, nil
}

func deploySpokeCluster_namespaceYaml() (*asset, error) {
	bytes, err := deploySpokeCluster_namespaceYamlBytes()
	if err != nil {
		return nil, err
	}

	info := bindataFileInfo{name: "deploy/spoke/cluster_namespace.yaml", size: 0, mode: os.FileMode(0), modTime: time.Unix(0, 0)}
	a := &asset{bytes: bytes, info: info}
	return a, nil
}

var _deploySpokeClusterroleYaml = []byte(`# Clusterrole for work agent in addition to admin clusterrole.
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
  name: open-cluster-management:work:agent
rules:
# Allow agent to get/list/watch/create/delete crds.
- apiGroups: ["apiextensions.k8s.io"]
  resources: ["customresourcedefinitions"]
  verbs: ["get", "list", "watch", "create", "delete", "update"]
# Allow agent to create/delete namespaces, get/list are contained in admin role already
- apiGroups: [""]
  resources: ["namespaces"]
  verbs: ["create", "delete"]
# Allow agent to manage role/rolebinding/clusterrole/clusterrolebinding
- apiGroups: ["rbac.authorization.k8s.io"]
  resources: ["clusterrolebindings", "rolebindings"]
  verbs: ["get", "list", "watch", "create", "update", "patch", "delete"]
- apiGroups: ["rbac.authorization.k8s.io"]
  resources: ["clusterroles", "roles"]
  verbs: ["get", "list", "watch", "create", "update", "patch", "delete", "escalate", "bind"]
# Allow agent to create sar
- apiGroups: ["authorization.k8s.io"]
  resources: ["subjectaccessreviews"]
  verbs: ["create"]
# Allow agent to create events
- apiGroups: ["", "events.k8s.io"]
  resources: ["events"]
  verbs: ["get", "list", "watch", "create", "patch", "update"]
# Allow agent to managed appliedmanifestworks
- apiGroups: ["work.open-cluster-management.io"]
  resources: ["appliedmanifestworks"]
  verbs: ["get", "list", "watch", "create", "update", "patch", "delete"]
- apiGroups: ["work.open-cluster-management.io"]
  resources: ["appliedmanifestworks/status"]
  verbs: ["patch", "update"]
- apiGroups: ["work.open-cluster-management.io"]
  resources: ["appliedmanifestworks/finalizers"]
  verbs: ["update"]
  `)

func deploySpokeClusterroleYamlBytes() ([]byte, error) {
	return _deploySpokeClusterroleYaml, nil
}

func deploySpokeClusterroleYaml() (*asset, error) {
	bytes, err := deploySpokeClusterroleYamlBytes()
	if err != nil {
		return nil, err
	}

	info := bindataFileInfo{name: "deploy/spoke/clusterrole.yaml", size: 0, mode: os.FileMode(0), modTime: time.Unix(0, 0)}
	a := &asset{bytes: bytes, info: info}
	return a, nil
}

var _deploySpokeClusterrole_bindingYaml = []byte(`apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRoleBinding
metadata:
  name: open-cluster-management:work:agent
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: ClusterRole
  # We deploy a controller that could work with permission lower than cluster-admin, the tradeoff is
  # responsivity because list/watch cannot be maintained over too many namespaces.
  name: admin
subjects:
  - kind: ServiceAccount
    name: work-agent-sa
    namespace: open-cluster-management-agent
`)

func deploySpokeClusterrole_bindingYamlBytes() ([]byte, error) {
	return _deploySpokeClusterrole_bindingYaml, nil
}

func deploySpokeClusterrole_bindingYaml() (*asset, error) {
	bytes, err := deploySpokeClusterrole_bindingYamlBytes()
	if err != nil {
		return nil, err
	}

	info := bindataFileInfo{name: "deploy/spoke/clusterrole_binding.yaml", size: 0, mode: os.FileMode(0), modTime: time.Unix(0, 0)}
	a := &asset{bytes: bytes, info: info}
	return a, nil
}

var _deploySpokeClusterrole_binding_additionYaml = []byte(`apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRoleBinding
metadata:
  name: open-cluster-management:work:agent-addition
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: ClusterRole
  name: open-cluster-management:work:agent
subjects:
  - kind: ServiceAccount
    name: work-agent-sa
    namespace: open-cluster-management-agent
`)

func deploySpokeClusterrole_binding_additionYamlBytes() ([]byte, error) {
	return _deploySpokeClusterrole_binding_additionYaml, nil
}

func deploySpokeClusterrole_binding_additionYaml() (*asset, error) {
	bytes, err := deploySpokeClusterrole_binding_additionYamlBytes()
	if err != nil {
		return nil, err
	}

	info := bindataFileInfo{name: "deploy/spoke/clusterrole_binding_addition.yaml", size: 0, mode: os.FileMode(0), modTime: time.Unix(0, 0)}
	a := &asset{bytes: bytes, info: info}
	return a, nil
}

var _deploySpokeComponent_namespaceYaml = []byte(`apiVersion: v1
kind: Namespace
metadata:
  name: open-cluster-management-agent
`)

func deploySpokeComponent_namespaceYamlBytes() ([]byte, error) {
	return _deploySpokeComponent_namespaceYaml, nil
}

func deploySpokeComponent_namespaceYaml() (*asset, error) {
	bytes, err := deploySpokeComponent_namespaceYamlBytes()
	if err != nil {
		return nil, err
	}

	info := bindataFileInfo{name: "deploy/spoke/component_namespace.yaml", size: 0, mode: os.FileMode(0), modTime: time.Unix(0, 0)}
	a := &asset{bytes: bytes, info: info}
	return a, nil
}

var _deploySpokeDeploymentYaml = []byte(`kind: Deployment
apiVersion: apps/v1
metadata:
  name: work-agent
  labels:
    app: work-agent
spec:
  replicas: 1
  selector:
    matchLabels:
      app: work-agent
  template:
    metadata:
      labels:
        app: work-agent
    spec:
      serviceAccountName: work-agent-sa
      containers:
      - name: work-agent
        image: quay.io/open-cluster-management/work:latest
        imagePullPolicy: IfNotPresent
        args:
          - "/work"
          - "agent"
          - "--spoke-cluster-name=cluster1"
          - "--hub-kubeconfig=/spoke/hub-kubeconfig/kubeconfig"
        volumeMounts:
        - name: hub-kubeconfig-secret
          mountPath: "/spoke/hub-kubeconfig"
          readOnly: true
      volumes:
      - name: hub-kubeconfig-secret
        secret:
          secretName: hub-kubeconfig-secret
`)

func deploySpokeDeploymentYamlBytes() ([]byte, error) {
	return _deploySpokeDeploymentYaml, nil
}

func deploySpokeDeploymentYaml() (*asset, error) {
	bytes, err := deploySpokeDeploymentYamlBytes()
	if err != nil {
		return nil, err
	}

	info := bindataFileInfo{name: "deploy/spoke/deployment.yaml", size: 0, mode: os.FileMode(0), modTime: time.Unix(0, 0)}
	a := &asset{bytes: bytes, info: info}
	return a, nil
}

var _deploySpokeKustomizationYaml = []byte(`
# Adds namespace to all resources.
namespace: open-cluster-management-agent

resources:
- ./appliedmanifestworks.crd.yaml
- ./component_namespace.yaml
- ./cluster_namespace.yaml
- ./service_account.yaml
- ./clusterrole.yaml
- ./clusterrole_binding.yaml
- ./clusterrole_binding_addition.yaml
- ./deployment.yaml

images:
- name: quay.io/open-cluster-management/work:latest
  newName: quay.io/open-cluster-management/work
  newTag: latest
apiVersion: kustomize.config.k8s.io/v1beta1
kind: Kustomization
`)

func deploySpokeKustomizationYamlBytes() ([]byte, error) {
	return _deploySpokeKustomizationYaml, nil
}

func deploySpokeKustomizationYaml() (*asset, error) {
	bytes, err := deploySpokeKustomizationYamlBytes()
	if err != nil {
		return nil, err
	}

	info := bindataFileInfo{name: "deploy/spoke/kustomization.yaml", size: 0, mode: os.FileMode(0), modTime: time.Unix(0, 0)}
	a := &asset{bytes: bytes, info: info}
	return a, nil
}

var _deploySpokeManifestworksCrdYaml = []byte(`apiVersion: apiextensions.k8s.io/v1
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

func deploySpokeManifestworksCrdYamlBytes() ([]byte, error) {
	return _deploySpokeManifestworksCrdYaml, nil
}

func deploySpokeManifestworksCrdYaml() (*asset, error) {
	bytes, err := deploySpokeManifestworksCrdYamlBytes()
	if err != nil {
		return nil, err
	}

	info := bindataFileInfo{name: "deploy/spoke/manifestworks.crd.yaml", size: 0, mode: os.FileMode(0), modTime: time.Unix(0, 0)}
	a := &asset{bytes: bytes, info: info}
	return a, nil
}

var _deploySpokeService_accountYaml = []byte(`apiVersion: v1
kind: ServiceAccount
metadata:
  name: work-agent-sa
  namespace: open-cluster-management-agent
`)

func deploySpokeService_accountYamlBytes() ([]byte, error) {
	return _deploySpokeService_accountYaml, nil
}

func deploySpokeService_accountYaml() (*asset, error) {
	bytes, err := deploySpokeService_accountYamlBytes()
	if err != nil {
		return nil, err
	}

	info := bindataFileInfo{name: "deploy/spoke/service_account.yaml", size: 0, mode: os.FileMode(0), modTime: time.Unix(0, 0)}
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
	"deploy/spoke/appliedmanifestworks.crd.yaml":     deploySpokeAppliedmanifestworksCrdYaml,
	"deploy/spoke/cluster_namespace.yaml":            deploySpokeCluster_namespaceYaml,
	"deploy/spoke/clusterrole.yaml":                  deploySpokeClusterroleYaml,
	"deploy/spoke/clusterrole_binding.yaml":          deploySpokeClusterrole_bindingYaml,
	"deploy/spoke/clusterrole_binding_addition.yaml": deploySpokeClusterrole_binding_additionYaml,
	"deploy/spoke/component_namespace.yaml":          deploySpokeComponent_namespaceYaml,
	"deploy/spoke/deployment.yaml":                   deploySpokeDeploymentYaml,
	"deploy/spoke/kustomization.yaml":                deploySpokeKustomizationYaml,
	"deploy/spoke/manifestworks.crd.yaml":            deploySpokeManifestworksCrdYaml,
	"deploy/spoke/service_account.yaml":              deploySpokeService_accountYaml,
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
	"deploy": {nil, map[string]*bintree{
		"spoke": {nil, map[string]*bintree{
			"appliedmanifestworks.crd.yaml":     {deploySpokeAppliedmanifestworksCrdYaml, map[string]*bintree{}},
			"cluster_namespace.yaml":            {deploySpokeCluster_namespaceYaml, map[string]*bintree{}},
			"clusterrole.yaml":                  {deploySpokeClusterroleYaml, map[string]*bintree{}},
			"clusterrole_binding.yaml":          {deploySpokeClusterrole_bindingYaml, map[string]*bintree{}},
			"clusterrole_binding_addition.yaml": {deploySpokeClusterrole_binding_additionYaml, map[string]*bintree{}},
			"component_namespace.yaml":          {deploySpokeComponent_namespaceYaml, map[string]*bintree{}},
			"deployment.yaml":                   {deploySpokeDeploymentYaml, map[string]*bintree{}},
			"kustomization.yaml":                {deploySpokeKustomizationYaml, map[string]*bintree{}},
			"manifestworks.crd.yaml":            {deploySpokeManifestworksCrdYaml, map[string]*bintree{}},
			"service_account.yaml":              {deploySpokeService_accountYaml, map[string]*bintree{}},
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
