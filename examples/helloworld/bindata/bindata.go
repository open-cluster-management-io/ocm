// Code generated for package bindata by go-bindata DO NOT EDIT. (@generated)
// sources:
// examples/helloworld/manifests/clusterrolebinding.yaml
// examples/helloworld/manifests/deployment.yaml
// examples/helloworld/manifests/role.yaml
// examples/helloworld/manifests/rolebinding.yaml
// examples/helloworld/manifests/serviceaccount.yaml
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

var _examplesHelloworldManifestsClusterrolebindingYaml = []byte(`kind: ClusterRoleBinding
apiVersion: rbac.authorization.k8s.io/v1
metadata:
  name: helloworld-agent
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: ClusterRole
  name: cluster-admin
subjects:
  - kind: ServiceAccount
    name: helloworld-agent-sa
    namespace: {{ .AddonInstallNamespace }}
`)

func examplesHelloworldManifestsClusterrolebindingYamlBytes() ([]byte, error) {
	return _examplesHelloworldManifestsClusterrolebindingYaml, nil
}

func examplesHelloworldManifestsClusterrolebindingYaml() (*asset, error) {
	bytes, err := examplesHelloworldManifestsClusterrolebindingYamlBytes()
	if err != nil {
		return nil, err
	}

	info := bindataFileInfo{name: "examples/helloworld/manifests/clusterrolebinding.yaml", size: 0, mode: os.FileMode(0), modTime: time.Unix(0, 0)}
	a := &asset{bytes: bytes, info: info}
	return a, nil
}

var _examplesHelloworldManifestsDeploymentYaml = []byte(`kind: Deployment
apiVersion: apps/v1
metadata:
  name: helloworld-agent
  namespace: {{ .AddonInstallNamespace }}
  labels:
    app: helloworld-agent
spec:
  replicas: 1
  selector:
    matchLabels:
      app: helloworld-agent
  template:
    metadata:
      labels:
        app: helloworld-agent
    spec:
      serviceAccountName: helloworld-agent-sa
      volumes:
      - name: hub-config
        secret:
          secretName: {{ .KubeConfigSecret }}
      containers:
      - name: helloworld-agent
        image: quay.io/open-cluster-management/helloworld-addon
        imagePullPolicy: IfNotPresent
        args:
          - "/helloworld"
          - "agent"
          - "--hub-kubeconfig=/var/run/hub/kubeconfig"
          - "--cluster-name={{ .ClusterName }}"
        volumeMounts:
          - name: hub-config
            mountPath: /var/run/hub
`)

func examplesHelloworldManifestsDeploymentYamlBytes() ([]byte, error) {
	return _examplesHelloworldManifestsDeploymentYaml, nil
}

func examplesHelloworldManifestsDeploymentYaml() (*asset, error) {
	bytes, err := examplesHelloworldManifestsDeploymentYamlBytes()
	if err != nil {
		return nil, err
	}

	info := bindataFileInfo{name: "examples/helloworld/manifests/deployment.yaml", size: 0, mode: os.FileMode(0), modTime: time.Unix(0, 0)}
	a := &asset{bytes: bytes, info: info}
	return a, nil
}

var _examplesHelloworldManifestsRoleYaml = []byte(`kind: Role
apiVersion: rbac.authorization.k8s.io/v1
metadata:
  name: open-cluster-management:helloworld:agent
  namespace: {{ .ClusterName }}
rules:
  - apiGroups: [""]
    resources: ["configmaps"]
    verbs: ["get", "list", "watch"]
`)

func examplesHelloworldManifestsRoleYamlBytes() ([]byte, error) {
	return _examplesHelloworldManifestsRoleYaml, nil
}

func examplesHelloworldManifestsRoleYaml() (*asset, error) {
	bytes, err := examplesHelloworldManifestsRoleYamlBytes()
	if err != nil {
		return nil, err
	}

	info := bindataFileInfo{name: "examples/helloworld/manifests/role.yaml", size: 0, mode: os.FileMode(0), modTime: time.Unix(0, 0)}
	a := &asset{bytes: bytes, info: info}
	return a, nil
}

var _examplesHelloworldManifestsRolebindingYaml = []byte(`apiVersion: rbac.authorization.k8s.io/v1
kind: RoleBinding
metadata:
  name: open-cluster-management:helloworld:agent
  namespace: {{ .ClusterName }}
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: Role
  name: open-cluster-management:helloworld:agent
subjects:
  - kind: Group
    apiGroup: rbac.authorization.k8s.io
    name: {{ .Group }}
`)

func examplesHelloworldManifestsRolebindingYamlBytes() ([]byte, error) {
	return _examplesHelloworldManifestsRolebindingYaml, nil
}

func examplesHelloworldManifestsRolebindingYaml() (*asset, error) {
	bytes, err := examplesHelloworldManifestsRolebindingYamlBytes()
	if err != nil {
		return nil, err
	}

	info := bindataFileInfo{name: "examples/helloworld/manifests/rolebinding.yaml", size: 0, mode: os.FileMode(0), modTime: time.Unix(0, 0)}
	a := &asset{bytes: bytes, info: info}
	return a, nil
}

var _examplesHelloworldManifestsServiceaccountYaml = []byte(`kind: ServiceAccount
apiVersion: v1
metadata:
  name: helloworld-agent-sa
  namespace: {{ .AddonInstallNamespace }}
`)

func examplesHelloworldManifestsServiceaccountYamlBytes() ([]byte, error) {
	return _examplesHelloworldManifestsServiceaccountYaml, nil
}

func examplesHelloworldManifestsServiceaccountYaml() (*asset, error) {
	bytes, err := examplesHelloworldManifestsServiceaccountYamlBytes()
	if err != nil {
		return nil, err
	}

	info := bindataFileInfo{name: "examples/helloworld/manifests/serviceaccount.yaml", size: 0, mode: os.FileMode(0), modTime: time.Unix(0, 0)}
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
	"examples/helloworld/manifests/clusterrolebinding.yaml": examplesHelloworldManifestsClusterrolebindingYaml,
	"examples/helloworld/manifests/deployment.yaml":         examplesHelloworldManifestsDeploymentYaml,
	"examples/helloworld/manifests/role.yaml":               examplesHelloworldManifestsRoleYaml,
	"examples/helloworld/manifests/rolebinding.yaml":        examplesHelloworldManifestsRolebindingYaml,
	"examples/helloworld/manifests/serviceaccount.yaml":     examplesHelloworldManifestsServiceaccountYaml,
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
	"examples": {nil, map[string]*bintree{
		"helloworld": {nil, map[string]*bintree{
			"manifests": {nil, map[string]*bintree{
				"clusterrolebinding.yaml": {examplesHelloworldManifestsClusterrolebindingYaml, map[string]*bintree{}},
				"deployment.yaml":         {examplesHelloworldManifestsDeploymentYaml, map[string]*bintree{}},
				"role.yaml":               {examplesHelloworldManifestsRoleYaml, map[string]*bintree{}},
				"rolebinding.yaml":        {examplesHelloworldManifestsRolebindingYaml, map[string]*bintree{}},
				"serviceaccount.yaml":     {examplesHelloworldManifestsServiceaccountYaml, map[string]*bintree{}},
			}},
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
