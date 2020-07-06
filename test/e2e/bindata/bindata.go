// Code generated for package bindata by go-bindata DO NOT EDIT. (@generated)
// sources:
// deploy/spoke/clusterrole.yaml
// deploy/spoke/clusterrole_binding.yaml
// deploy/spoke/deployment.yaml
// deploy/spoke/kustomization.yaml
// deploy/spoke/namespace.yaml
// deploy/spoke/role.yaml
// deploy/spoke/role_binding.yaml
// deploy/spoke/secret.yaml
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

var _deploySpokeClusterroleYaml = []byte(`apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
  name: open-cluster-management:spoke
rules:
- apiGroups: [""]
  resources: ["nodes", "configmaps", "secrets"]
  verbs: ["get", "list", "watch"]
- apiGroups: ["authorization.k8s.io"]
  resources: ["subjectaccessreviews"]
  verbs: ["create"]
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
  name: open-cluster-management:spoke
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: ClusterRole
  name: open-cluster-management:spoke
subjects:
  - kind: ServiceAccount
    name: spoke-agent-sa
    namespace: open-cluster-management
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

var _deploySpokeDeploymentYaml = []byte(`kind: Deployment
apiVersion: apps/v1
metadata:
  name: spoke-agent
  labels:
    app: spoke-agent
spec:
  replicas: 1
  selector:
    matchLabels:
      app: spoke-agent
  template:
    metadata:
      labels:
        app: spoke-agent
    spec:
      serviceAccountName: spoke-agent-sa
      containers:
      - name: spoke-agent
        image: quay.io/open-cluster-management/registration:latest
        imagePullPolicy: IfNotPresent
        args:
          - "/registration"
          - "agent"
          - "--cluster-name=local-development"
          - "--bootstrap-kubeconfig=/spoke/bootstrap/kubeconfig"
        volumeMounts:
        - name: bootstrap-secret
          mountPath: "/spoke/bootstrap"
          readOnly: true
        - name: hub-kubeconfig-secret
          mountPath: "/spoke/hub-kubeconfig"
          readOnly: true
      volumes:
      - name: bootstrap-secret
        secret:
          secretName: bootstrap-secret
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
namespace: open-cluster-management

# Value of this field is prepended to the
# names of all resources, e.g. a deployment named
# "wordpress" becomes "alices-wordpress".
# Note that it should also match with the prefix (text before '-') of the namespace
# field above.
#namePrefix: multicloud-

# Labels to add to all resources and selectors.
#commonLabels:
#  someName: someValue

# Each entry in this list must resolve to an existing
# resource definition in YAML.  These are the resource
# files that kustomize reads, modifies and emits as a
# YAML string, with resources separated by document
# markers ("---").
#
# General rule here is anything deployed by OLM bundles should go here as well,
# this is used in "make deploy" for developers and should mimic what OLM deploys
# for you. CRDs are an exception to this as we don't want to have to list them all
# here. These are deployed via a "make install" dependency.

resources:
- ./namespace.yaml
- ./service_account.yaml
- ./clusterrole.yaml
- ./clusterrole_binding.yaml
- ./role.yaml
- ./role_binding.yaml
- ./deployment.yaml
- ./secret.yaml

images:
- name: quay.io/open-cluster-management/registration:latest
  newName: quay.io/open-cluster-management/registration
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

var _deploySpokeNamespaceYaml = []byte(`apiVersion: v1
kind: Namespace
metadata:
  name: open-cluster-management
`)

func deploySpokeNamespaceYamlBytes() ([]byte, error) {
	return _deploySpokeNamespaceYaml, nil
}

func deploySpokeNamespaceYaml() (*asset, error) {
	bytes, err := deploySpokeNamespaceYamlBytes()
	if err != nil {
		return nil, err
	}

	info := bindataFileInfo{name: "deploy/spoke/namespace.yaml", size: 0, mode: os.FileMode(0), modTime: time.Unix(0, 0)}
	a := &asset{bytes: bytes, info: info}
	return a, nil
}

var _deploySpokeRoleYaml = []byte(`apiVersion: rbac.authorization.k8s.io/v1
kind: Role
metadata:
  name: open-cluster-management:registration-agent
  namespace: open-cluster-management
rules:
- apiGroups: [""]
  resources: ["configmaps", "secrets"]
  verbs: ["get", "list", "watch", "create", "delete", "update", "patch"]
- apiGroups: ["", "events.k8s.io"]
  resources: ["events"]
  verbs: ["create", "patch", "update"]
`)

func deploySpokeRoleYamlBytes() ([]byte, error) {
	return _deploySpokeRoleYaml, nil
}

func deploySpokeRoleYaml() (*asset, error) {
	bytes, err := deploySpokeRoleYamlBytes()
	if err != nil {
		return nil, err
	}

	info := bindataFileInfo{name: "deploy/spoke/role.yaml", size: 0, mode: os.FileMode(0), modTime: time.Unix(0, 0)}
	a := &asset{bytes: bytes, info: info}
	return a, nil
}

var _deploySpokeRole_bindingYaml = []byte(`apiVersion: rbac.authorization.k8s.io/v1
kind: RoleBinding
metadata:
  name: open-cluster-management:registration-agent
  namespace: open-cluster-management
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: Role
  name: open-cluster-management:registration-agent
subjects:
  - kind: ServiceAccount
    name: spoke-agent-sa
    namespace: open-cluster-management
`)

func deploySpokeRole_bindingYamlBytes() ([]byte, error) {
	return _deploySpokeRole_bindingYaml, nil
}

func deploySpokeRole_bindingYaml() (*asset, error) {
	bytes, err := deploySpokeRole_bindingYamlBytes()
	if err != nil {
		return nil, err
	}

	info := bindataFileInfo{name: "deploy/spoke/role_binding.yaml", size: 0, mode: os.FileMode(0), modTime: time.Unix(0, 0)}
	a := &asset{bytes: bytes, info: info}
	return a, nil
}

var _deploySpokeSecretYaml = []byte(`apiVersion: v1
kind: Secret
metadata:
  name: hub-kubeconfig-secret
type: Opaque
data:
  placeholder: YWRtaW4=
`)

func deploySpokeSecretYamlBytes() ([]byte, error) {
	return _deploySpokeSecretYaml, nil
}

func deploySpokeSecretYaml() (*asset, error) {
	bytes, err := deploySpokeSecretYamlBytes()
	if err != nil {
		return nil, err
	}

	info := bindataFileInfo{name: "deploy/spoke/secret.yaml", size: 0, mode: os.FileMode(0), modTime: time.Unix(0, 0)}
	a := &asset{bytes: bytes, info: info}
	return a, nil
}

var _deploySpokeService_accountYaml = []byte(`apiVersion: v1
kind: ServiceAccount
metadata:
  name: spoke-agent-sa
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
	"deploy/spoke/clusterrole.yaml":         deploySpokeClusterroleYaml,
	"deploy/spoke/clusterrole_binding.yaml": deploySpokeClusterrole_bindingYaml,
	"deploy/spoke/deployment.yaml":          deploySpokeDeploymentYaml,
	"deploy/spoke/kustomization.yaml":       deploySpokeKustomizationYaml,
	"deploy/spoke/namespace.yaml":           deploySpokeNamespaceYaml,
	"deploy/spoke/role.yaml":                deploySpokeRoleYaml,
	"deploy/spoke/role_binding.yaml":        deploySpokeRole_bindingYaml,
	"deploy/spoke/secret.yaml":              deploySpokeSecretYaml,
	"deploy/spoke/service_account.yaml":     deploySpokeService_accountYaml,
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
			"clusterrole.yaml":         {deploySpokeClusterroleYaml, map[string]*bintree{}},
			"clusterrole_binding.yaml": {deploySpokeClusterrole_bindingYaml, map[string]*bintree{}},
			"deployment.yaml":          {deploySpokeDeploymentYaml, map[string]*bintree{}},
			"kustomization.yaml":       {deploySpokeKustomizationYaml, map[string]*bintree{}},
			"namespace.yaml":           {deploySpokeNamespaceYaml, map[string]*bintree{}},
			"role.yaml":                {deploySpokeRoleYaml, map[string]*bintree{}},
			"role_binding.yaml":        {deploySpokeRole_bindingYaml, map[string]*bintree{}},
			"secret.yaml":              {deploySpokeSecretYaml, map[string]*bintree{}},
			"service_account.yaml":     {deploySpokeService_accountYaml, map[string]*bintree{}},
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
