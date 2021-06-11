// Code generated for package bindata by go-bindata DO NOT EDIT. (@generated)
// sources:
// deploy/spoke/appliedmanifestworks.crd.yaml
// deploy/spoke/clusterrole.yaml
// deploy/spoke/clusterrole_binding.yaml
// deploy/spoke/clusterrole_binding_addition.yaml
// deploy/spoke/component_namespace.yaml
// deploy/spoke/deployment.yaml
// deploy/spoke/kustomization.yaml
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
        securityContext:
          allowPrivilegeEscalation: false
          capabilities:
            drop:
              - ALL
          privileged: false
          runAsNonRoot: true
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
	"deploy/spoke/clusterrole.yaml":                  deploySpokeClusterroleYaml,
	"deploy/spoke/clusterrole_binding.yaml":          deploySpokeClusterrole_bindingYaml,
	"deploy/spoke/clusterrole_binding_addition.yaml": deploySpokeClusterrole_binding_additionYaml,
	"deploy/spoke/component_namespace.yaml":          deploySpokeComponent_namespaceYaml,
	"deploy/spoke/deployment.yaml":                   deploySpokeDeploymentYaml,
	"deploy/spoke/kustomization.yaml":                deploySpokeKustomizationYaml,
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
			"clusterrole.yaml":                  {deploySpokeClusterroleYaml, map[string]*bintree{}},
			"clusterrole_binding.yaml":          {deploySpokeClusterrole_bindingYaml, map[string]*bintree{}},
			"clusterrole_binding_addition.yaml": {deploySpokeClusterrole_binding_additionYaml, map[string]*bintree{}},
			"component_namespace.yaml":          {deploySpokeComponent_namespaceYaml, map[string]*bintree{}},
			"deployment.yaml":                   {deploySpokeDeploymentYaml, map[string]*bintree{}},
			"kustomization.yaml":                {deploySpokeKustomizationYaml, map[string]*bintree{}},
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
