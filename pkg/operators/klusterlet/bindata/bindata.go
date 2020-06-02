// Code generated for package bindata by go-bindata DO NOT EDIT. (@generated)
// sources:
// manifests/klusterlet/klusterlet-registration-clusterrole.yaml
// manifests/klusterlet/klusterlet-registration-clusterrolebinding.yaml
// manifests/klusterlet/klusterlet-registration-deployment.yaml
// manifests/klusterlet/klusterlet-registration-role.yaml
// manifests/klusterlet/klusterlet-registration-rolebinding.yaml
// manifests/klusterlet/klusterlet-registration-serviceaccount.yaml
// manifests/klusterlet/klusterlet-work-clusterrole.yaml
// manifests/klusterlet/klusterlet-work-clusterrolebinding-addition.yaml
// manifests/klusterlet/klusterlet-work-clusterrolebinding.yaml
// manifests/klusterlet/klusterlet-work-deployment.yaml
// manifests/klusterlet/klusterlet-work-serviceaccount.yaml
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

var _manifestsKlusterletKlusterletRegistrationClusterroleYaml = []byte(`# Clusterrole for work agent in addition to admin clusterrole.
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
  name: system:open-cluster-management:{{ .KlusterletName }}-registration-agent
rules:
# Allow agent to get/list/watch nodes.
- apiGroups: [""]
  resources: ["nodes", "configmaps", "secrets"]
  verbs: ["get", "list", "watch"]
- apiGroups: ["authorization.k8s.io"]
  resources: ["subjectaccessreviews"]
  verbs: ["create"]
`)

func manifestsKlusterletKlusterletRegistrationClusterroleYamlBytes() ([]byte, error) {
	return _manifestsKlusterletKlusterletRegistrationClusterroleYaml, nil
}

func manifestsKlusterletKlusterletRegistrationClusterroleYaml() (*asset, error) {
	bytes, err := manifestsKlusterletKlusterletRegistrationClusterroleYamlBytes()
	if err != nil {
		return nil, err
	}

	info := bindataFileInfo{name: "manifests/klusterlet/klusterlet-registration-clusterrole.yaml", size: 0, mode: os.FileMode(0), modTime: time.Unix(0, 0)}
	a := &asset{bytes: bytes, info: info}
	return a, nil
}

var _manifestsKlusterletKlusterletRegistrationClusterrolebindingYaml = []byte(`apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRoleBinding
metadata:
  name: system:open-cluster-management:{{ .KlusterletName }}-registration-agent
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: ClusterRole
  name: system:open-cluster-management:{{ .KlusterletName }}-registration-agent
subjects:
  - kind: ServiceAccount
    name: {{ .KlusterletName }}-registration-sa
    namespace: {{ .KlusterletNamespace }}
`)

func manifestsKlusterletKlusterletRegistrationClusterrolebindingYamlBytes() ([]byte, error) {
	return _manifestsKlusterletKlusterletRegistrationClusterrolebindingYaml, nil
}

func manifestsKlusterletKlusterletRegistrationClusterrolebindingYaml() (*asset, error) {
	bytes, err := manifestsKlusterletKlusterletRegistrationClusterrolebindingYamlBytes()
	if err != nil {
		return nil, err
	}

	info := bindataFileInfo{name: "manifests/klusterlet/klusterlet-registration-clusterrolebinding.yaml", size: 0, mode: os.FileMode(0), modTime: time.Unix(0, 0)}
	a := &asset{bytes: bytes, info: info}
	return a, nil
}

var _manifestsKlusterletKlusterletRegistrationDeploymentYaml = []byte(`kind: Deployment
apiVersion: apps/v1
metadata:
  name: {{ .KlusterletName }}-registration-agent
  namespace: {{ .KlusterletNamespace }}
  labels:
    app: klusterlet-registration-agent
spec:
  replicas: 3
  selector:
    matchLabels:
      app: klusterlet-registration-agent
  template:
    metadata:
      labels:
        app: klusterlet-registration-agent
    spec:
      serviceAccountName: {{ .KlusterletName }}-registration-sa
      containers:
      - name: spoke-agent
        image: {{ .RegistrationImage }}
        imagePullPolicy: IfNotPresent
        args:
          - "/registration"
          - "agent"
          - "--cluster-name={{ .ClusterName }}"
          - "--bootstrap-kubeconfig=/spoke/bootstrap/kubeconfig"
          - "--spoke-external-server-urls={{ .ExternalServerURL }}"
        volumeMounts:
        - name: bootstrap-secret
          mountPath: "/spoke/bootstrap"
          readOnly: true
        - name: hub-kubeconfig-secret
          mountPath: "/spoke/hub-kubeconfig"
          readOnly: true
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
      volumes:
      - name: bootstrap-secret
        secret:
          secretName: {{ .BootStrapKubeConfigSecret }}
      - name: hub-kubeconfig-secret
        secret:
          secretName: {{ .HubKubeConfigSecret }}
`)

func manifestsKlusterletKlusterletRegistrationDeploymentYamlBytes() ([]byte, error) {
	return _manifestsKlusterletKlusterletRegistrationDeploymentYaml, nil
}

func manifestsKlusterletKlusterletRegistrationDeploymentYaml() (*asset, error) {
	bytes, err := manifestsKlusterletKlusterletRegistrationDeploymentYamlBytes()
	if err != nil {
		return nil, err
	}

	info := bindataFileInfo{name: "manifests/klusterlet/klusterlet-registration-deployment.yaml", size: 0, mode: os.FileMode(0), modTime: time.Unix(0, 0)}
	a := &asset{bytes: bytes, info: info}
	return a, nil
}

var _manifestsKlusterletKlusterletRegistrationRoleYaml = []byte(`# Role for registration agent.
apiVersion: rbac.authorization.k8s.io/v1
kind: Role
metadata:
  name: system:open-cluster-management:{{ .KlusterletName }}-registration-agent
  namespace: {{ .KlusterletNamespace }}
rules:
- apiGroups: [""]
  resources: ["configmaps", "secrets"]
  verbs: ["get", "list", "watch", "create", "delete", "update", "patch"]
- apiGroups: ["", "events.k8s.io"]
  resources: ["events"]
  verbs: ["create", "patch", "update"]
`)

func manifestsKlusterletKlusterletRegistrationRoleYamlBytes() ([]byte, error) {
	return _manifestsKlusterletKlusterletRegistrationRoleYaml, nil
}

func manifestsKlusterletKlusterletRegistrationRoleYaml() (*asset, error) {
	bytes, err := manifestsKlusterletKlusterletRegistrationRoleYamlBytes()
	if err != nil {
		return nil, err
	}

	info := bindataFileInfo{name: "manifests/klusterlet/klusterlet-registration-role.yaml", size: 0, mode: os.FileMode(0), modTime: time.Unix(0, 0)}
	a := &asset{bytes: bytes, info: info}
	return a, nil
}

var _manifestsKlusterletKlusterletRegistrationRolebindingYaml = []byte(`apiVersion: rbac.authorization.k8s.io/v1
kind: RoleBinding
metadata:
  name: system:open-cluster-management:{{ .KlusterletName }}-registration-agent
  namespace: {{ .KlusterletNamespace }}
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: Role
  name: system:open-cluster-management:{{ .KlusterletName }}-registration-agent
subjects:
  - kind: ServiceAccount
    name: {{ .KlusterletName }}-registration-sa
    namespace: {{ .KlusterletNamespace }}
`)

func manifestsKlusterletKlusterletRegistrationRolebindingYamlBytes() ([]byte, error) {
	return _manifestsKlusterletKlusterletRegistrationRolebindingYaml, nil
}

func manifestsKlusterletKlusterletRegistrationRolebindingYaml() (*asset, error) {
	bytes, err := manifestsKlusterletKlusterletRegistrationRolebindingYamlBytes()
	if err != nil {
		return nil, err
	}

	info := bindataFileInfo{name: "manifests/klusterlet/klusterlet-registration-rolebinding.yaml", size: 0, mode: os.FileMode(0), modTime: time.Unix(0, 0)}
	a := &asset{bytes: bytes, info: info}
	return a, nil
}

var _manifestsKlusterletKlusterletRegistrationServiceaccountYaml = []byte(`apiVersion: v1
kind: ServiceAccount
metadata:
  name: {{ .KlusterletName }}-registration-sa
  namespace: {{ .KlusterletNamespace }}
`)

func manifestsKlusterletKlusterletRegistrationServiceaccountYamlBytes() ([]byte, error) {
	return _manifestsKlusterletKlusterletRegistrationServiceaccountYaml, nil
}

func manifestsKlusterletKlusterletRegistrationServiceaccountYaml() (*asset, error) {
	bytes, err := manifestsKlusterletKlusterletRegistrationServiceaccountYamlBytes()
	if err != nil {
		return nil, err
	}

	info := bindataFileInfo{name: "manifests/klusterlet/klusterlet-registration-serviceaccount.yaml", size: 0, mode: os.FileMode(0), modTime: time.Unix(0, 0)}
	a := &asset{bytes: bytes, info: info}
	return a, nil
}

var _manifestsKlusterletKlusterletWorkClusterroleYaml = []byte(`# Clusterrole for work agent in addition to admin clusterrole.
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
  name: system:open-cluster-management:{{ .KlusterletName }}-work-agent
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
  verbs: ["create", "patch", "update"]
`)

func manifestsKlusterletKlusterletWorkClusterroleYamlBytes() ([]byte, error) {
	return _manifestsKlusterletKlusterletWorkClusterroleYaml, nil
}

func manifestsKlusterletKlusterletWorkClusterroleYaml() (*asset, error) {
	bytes, err := manifestsKlusterletKlusterletWorkClusterroleYamlBytes()
	if err != nil {
		return nil, err
	}

	info := bindataFileInfo{name: "manifests/klusterlet/klusterlet-work-clusterrole.yaml", size: 0, mode: os.FileMode(0), modTime: time.Unix(0, 0)}
	a := &asset{bytes: bytes, info: info}
	return a, nil
}

var _manifestsKlusterletKlusterletWorkClusterrolebindingAdditionYaml = []byte(`apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRoleBinding
metadata:
  name: system:open-cluster-management:{{ .KlusterletName }}-work-agent-addition
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: ClusterRole
  name: system:open-cluster-management:{{ .KlusterletName }}-work-agent
subjects:
  - kind: ServiceAccount
    name: {{ .KlusterletName }}-work-sa
    namespace: {{ .KlusterletNamespace }}
`)

func manifestsKlusterletKlusterletWorkClusterrolebindingAdditionYamlBytes() ([]byte, error) {
	return _manifestsKlusterletKlusterletWorkClusterrolebindingAdditionYaml, nil
}

func manifestsKlusterletKlusterletWorkClusterrolebindingAdditionYaml() (*asset, error) {
	bytes, err := manifestsKlusterletKlusterletWorkClusterrolebindingAdditionYamlBytes()
	if err != nil {
		return nil, err
	}

	info := bindataFileInfo{name: "manifests/klusterlet/klusterlet-work-clusterrolebinding-addition.yaml", size: 0, mode: os.FileMode(0), modTime: time.Unix(0, 0)}
	a := &asset{bytes: bytes, info: info}
	return a, nil
}

var _manifestsKlusterletKlusterletWorkClusterrolebindingYaml = []byte(`apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRoleBinding
metadata:
  name: system:open-cluster-management:{{ .KlusterletName }}-work-agent
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: ClusterRole
  # We deploy a controller that could work with permission lower than cluster-admin, the tradeoff is
  # responsivity because list/watch cannot be maintained over too many namespaces.
  name: admin
subjects:
  - kind: ServiceAccount
    name: {{ .KlusterletName }}-work-sa
    namespace: {{ .KlusterletNamespace }}
`)

func manifestsKlusterletKlusterletWorkClusterrolebindingYamlBytes() ([]byte, error) {
	return _manifestsKlusterletKlusterletWorkClusterrolebindingYaml, nil
}

func manifestsKlusterletKlusterletWorkClusterrolebindingYaml() (*asset, error) {
	bytes, err := manifestsKlusterletKlusterletWorkClusterrolebindingYamlBytes()
	if err != nil {
		return nil, err
	}

	info := bindataFileInfo{name: "manifests/klusterlet/klusterlet-work-clusterrolebinding.yaml", size: 0, mode: os.FileMode(0), modTime: time.Unix(0, 0)}
	a := &asset{bytes: bytes, info: info}
	return a, nil
}

var _manifestsKlusterletKlusterletWorkDeploymentYaml = []byte(`kind: Deployment
apiVersion: apps/v1
metadata:
  name: {{ .KlusterletName }}-work-agent
  namespace: {{ .KlusterletNamespace }}
  labels:
    app: klusterlet-manifestwork-agent
spec:
  replicas: 3
  selector:
    matchLabels:
      app: klusterlet-manifestwork-agent
  template:
    metadata:
      labels:
        app: klusterlet-manifestwork-agent
    spec:
      serviceAccountName: {{ .KlusterletName }}-work-sa
      containers:
      - name: spoke-agent
        image: {{ .WorkImage }}
        imagePullPolicy: IfNotPresent
        args:
          - "/work"
          - "agent"
          - "--spoke-cluster-name={{ .ClusterName }}"
          - "--hub-kubeconfig=/spoke/hub-kubeconfig/kubeconfig"
        volumeMounts:
        - name: hub-kubeconfig-secret
          mountPath: "/spoke/hub-kubeconfig"
          readOnly: true
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
      volumes:
      - name: hub-kubeconfig-secret
        secret:
          secretName: {{ .HubKubeConfigSecret }}
`)

func manifestsKlusterletKlusterletWorkDeploymentYamlBytes() ([]byte, error) {
	return _manifestsKlusterletKlusterletWorkDeploymentYaml, nil
}

func manifestsKlusterletKlusterletWorkDeploymentYaml() (*asset, error) {
	bytes, err := manifestsKlusterletKlusterletWorkDeploymentYamlBytes()
	if err != nil {
		return nil, err
	}

	info := bindataFileInfo{name: "manifests/klusterlet/klusterlet-work-deployment.yaml", size: 0, mode: os.FileMode(0), modTime: time.Unix(0, 0)}
	a := &asset{bytes: bytes, info: info}
	return a, nil
}

var _manifestsKlusterletKlusterletWorkServiceaccountYaml = []byte(`apiVersion: v1
kind: ServiceAccount
metadata:
  name: {{ .KlusterletName }}-work-sa
  namespace: {{ .KlusterletNamespace }}
`)

func manifestsKlusterletKlusterletWorkServiceaccountYamlBytes() ([]byte, error) {
	return _manifestsKlusterletKlusterletWorkServiceaccountYaml, nil
}

func manifestsKlusterletKlusterletWorkServiceaccountYaml() (*asset, error) {
	bytes, err := manifestsKlusterletKlusterletWorkServiceaccountYamlBytes()
	if err != nil {
		return nil, err
	}

	info := bindataFileInfo{name: "manifests/klusterlet/klusterlet-work-serviceaccount.yaml", size: 0, mode: os.FileMode(0), modTime: time.Unix(0, 0)}
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
	"manifests/klusterlet/klusterlet-registration-clusterrole.yaml":         manifestsKlusterletKlusterletRegistrationClusterroleYaml,
	"manifests/klusterlet/klusterlet-registration-clusterrolebinding.yaml":  manifestsKlusterletKlusterletRegistrationClusterrolebindingYaml,
	"manifests/klusterlet/klusterlet-registration-deployment.yaml":          manifestsKlusterletKlusterletRegistrationDeploymentYaml,
	"manifests/klusterlet/klusterlet-registration-role.yaml":                manifestsKlusterletKlusterletRegistrationRoleYaml,
	"manifests/klusterlet/klusterlet-registration-rolebinding.yaml":         manifestsKlusterletKlusterletRegistrationRolebindingYaml,
	"manifests/klusterlet/klusterlet-registration-serviceaccount.yaml":      manifestsKlusterletKlusterletRegistrationServiceaccountYaml,
	"manifests/klusterlet/klusterlet-work-clusterrole.yaml":                 manifestsKlusterletKlusterletWorkClusterroleYaml,
	"manifests/klusterlet/klusterlet-work-clusterrolebinding-addition.yaml": manifestsKlusterletKlusterletWorkClusterrolebindingAdditionYaml,
	"manifests/klusterlet/klusterlet-work-clusterrolebinding.yaml":          manifestsKlusterletKlusterletWorkClusterrolebindingYaml,
	"manifests/klusterlet/klusterlet-work-deployment.yaml":                  manifestsKlusterletKlusterletWorkDeploymentYaml,
	"manifests/klusterlet/klusterlet-work-serviceaccount.yaml":              manifestsKlusterletKlusterletWorkServiceaccountYaml,
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
		"klusterlet": {nil, map[string]*bintree{
			"klusterlet-registration-clusterrole.yaml":         {manifestsKlusterletKlusterletRegistrationClusterroleYaml, map[string]*bintree{}},
			"klusterlet-registration-clusterrolebinding.yaml":  {manifestsKlusterletKlusterletRegistrationClusterrolebindingYaml, map[string]*bintree{}},
			"klusterlet-registration-deployment.yaml":          {manifestsKlusterletKlusterletRegistrationDeploymentYaml, map[string]*bintree{}},
			"klusterlet-registration-role.yaml":                {manifestsKlusterletKlusterletRegistrationRoleYaml, map[string]*bintree{}},
			"klusterlet-registration-rolebinding.yaml":         {manifestsKlusterletKlusterletRegistrationRolebindingYaml, map[string]*bintree{}},
			"klusterlet-registration-serviceaccount.yaml":      {manifestsKlusterletKlusterletRegistrationServiceaccountYaml, map[string]*bintree{}},
			"klusterlet-work-clusterrole.yaml":                 {manifestsKlusterletKlusterletWorkClusterroleYaml, map[string]*bintree{}},
			"klusterlet-work-clusterrolebinding-addition.yaml": {manifestsKlusterletKlusterletWorkClusterrolebindingAdditionYaml, map[string]*bintree{}},
			"klusterlet-work-clusterrolebinding.yaml":          {manifestsKlusterletKlusterletWorkClusterrolebindingYaml, map[string]*bintree{}},
			"klusterlet-work-deployment.yaml":                  {manifestsKlusterletKlusterletWorkDeploymentYaml, map[string]*bintree{}},
			"klusterlet-work-serviceaccount.yaml":              {manifestsKlusterletKlusterletWorkServiceaccountYaml, map[string]*bintree{}},
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
