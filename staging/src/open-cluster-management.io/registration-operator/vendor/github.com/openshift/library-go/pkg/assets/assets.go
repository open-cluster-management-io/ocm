package assets

import (
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"

	"k8s.io/apimachinery/pkg/util/errors"

	"github.com/openshift/library-go/pkg/operator/resource/resourceread"
)

type Permission os.FileMode

const (
	PermissionDirectoryDefault Permission = 0755
	PermissionFileDefault      Permission = 0644
	PermissionFileRestricted   Permission = 0600
)

// Asset defines a single static asset.
type Asset struct {
	Name           string
	FilePermission Permission
	Data           []byte
}

// Assets is a list of assets.
type Assets []Asset

// New walks through a directory recursively and renders each file as asset. Only those files
// are rendered that make all predicates true.
func New(dir string, data interface{}, manifestPredicates []FileContentsPredicate, predicates ...FileInfoPredicate) (Assets, error) {
	files, err := LoadFilesRecursively(dir, predicates...)
	if err != nil {
		return nil, err
	}

	var as Assets
	var errs []error
	for path, bs := range files {
		a, err := assetFromTemplate(path, bs, data)
		if err != nil {
			return nil, fmt.Errorf("failed to render %q: %v", path, err)
		}

		skipManifest := false
		for _, manifestPredicate := range manifestPredicates {
			shouldInclude, err := manifestPredicate(a.Data)
			if err != nil {
				return nil, fmt.Errorf("failed to check manifest filter %q: %v", path, err)
			}
			if !shouldInclude {
				skipManifest = true
				break
			}
		}
		if skipManifest {
			continue
		}

		as = append(as, *a)
	}

	if len(errs) > 0 {
		return nil, errors.NewAggregate(errs)
	}

	return as, nil
}

// WriteFiles writes the assets to specified path.
func (as Assets) WriteFiles(path string) error {
	if err := os.MkdirAll(path, os.FileMode(PermissionDirectoryDefault)); err != nil {
		return err
	}
	for _, asset := range as {
		if _, err := os.Stat(path); os.IsExist(err) {
			fmt.Printf("WARNING: File %s already exists, content will be replaced\n", path)
		}
		if err := asset.WriteFile(path); err != nil {
			return err
		}
	}
	return nil
}

// WriteFile writes a single asset into specified path.
func (a Asset) WriteFile(path string) error {
	f := filepath.Join(path, a.Name)
	perms := PermissionFileDefault
	if err := os.MkdirAll(filepath.Dir(f), os.FileMode(PermissionDirectoryDefault)); err != nil {
		return err
	}
	if a.FilePermission != 0 {
		perms = a.FilePermission
	}
	fmt.Printf("Writing asset: %s\n", f)
	return ioutil.WriteFile(f, a.Data, os.FileMode(perms))
}

// MustCreateAssetFromTemplate process the given template using and return an asset.
func MustCreateAssetFromTemplate(name string, template []byte, config interface{}) Asset {
	asset, err := assetFromTemplate(name, template, config)
	if err != nil {
		panic(err)
	}
	return *asset
}

func assetFromTemplate(name string, tb []byte, data interface{}) (*Asset, error) {
	bs, err := renderFile(name, tb, data)
	if err != nil {
		return nil, err
	}
	return &Asset{Name: name, Data: bs}, nil
}

type FileInfoPredicate func(path string, info os.FileInfo) (bool, error)

type FileContentsPredicate func(manifest []byte) (bool, error)

// OnlyYaml is a predicate for LoadFilesRecursively filters out non-yaml files.
func OnlyYaml(_ string, info os.FileInfo) (bool, error) {
	return strings.HasSuffix(info.Name(), ".yaml") || strings.HasSuffix(info.Name(), ".yml"), nil
}

// InstallerFeatureSet returns a predicate for LoadFilesRecursively that filters manifests
// based on the specified FeatureSet.
func InstallerFeatureSet(featureSet string) FileContentsPredicate {
	targetFeatureSet := "Default"
	if len(featureSet) > 0 {
		targetFeatureSet = featureSet
	}
	return func(manifest []byte) (bool, error) {
		manifestFeatureSets := resourceread.ReadUnstructuredOrDie(manifest).GetAnnotations()["release.openshift.io/feature-set"]
		if len(manifestFeatureSets) == 0 {
			return true, nil
		}
		for _, manifestFeatureSet := range strings.Split(manifestFeatureSets, ",") {
			if manifestFeatureSet == targetFeatureSet {
				return true, nil
			}
		}
		return false, nil
	}
}

// LoadFilesRecursively returns a map from relative path names to file content.
func LoadFilesRecursively(dir string, predicates ...FileInfoPredicate) (map[string][]byte, error) {
	files := map[string][]byte{}
	err := filepath.Walk(dir,
		func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return err
			}
			if info.IsDir() {
				return nil
			}

			for _, p := range predicates {
				include, err := p(path, info)
				if err != nil {
					return err
				}
				if !include {
					return nil
				}
			}

			bs, err := ioutil.ReadFile(path)
			if err != nil {
				return err
			}

			// make path relative to dir
			rel, err := filepath.Rel(dir, path)
			if err != nil {
				return err
			}

			files[rel] = bs
			return nil
		},
	)
	if err != nil {
		return nil, err
	}

	return files, nil
}
