// Copyright Contributors to the Open Cluster Management project

package asset

import (
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"

	"github.com/ghodss/yaml"
	"k8s.io/klog/v2"
)

//YamlFileReader defines a reader for yaml files
type YamlFileReader struct {
	header string
	paths  []string
	files  []string
}

var _ ScenarioReader = &YamlFileReader{
	header: "",
	paths:  []string{},
	files:  []string{},
}

//NewDirectoriesReader constructs a new YamlFileReader
func NewDirectoriesReader(
	header string,
	paths []string,
) *YamlFileReader {
	reader := &YamlFileReader{
		header: header,
		paths:  paths,
	}
	files, err := reader.AssetNames([]string{})
	if err != nil {
		klog.Fatal(err)
	}
	reader.files = files
	return reader
}

//Asset returns an asset
func (r *YamlFileReader) Asset(
	name string,
) ([]byte, error) {
	found := false
	for _, p := range r.files {
		if p == name {
			found = true
			break
		}
	}
	if !found {
		return nil, fmt.Errorf("file %s is not part of the assets", name)
	}
	return ioutil.ReadFile(filepath.Clean(name))
}

//AssetNames returns the name of all assets
func (r *YamlFileReader) AssetNames(excluded []string) ([]string, error) {
	files := make([]string, 0)
	if len(r.header) != 0 {
		files = append(files, r.header)
	}
	visit := func(path string, fileInfo os.FileInfo, err error) error {
		if fileInfo == nil {
			return fmt.Errorf("paths %s doesn't exist", path)
		}
		if fileInfo.IsDir() {
			return nil
		}
		if isExcluded(path, excluded) {
			return nil
		}
		files = append(files, path)
		return nil
	}

	for _, p := range r.paths {
		if err := filepath.Walk(p, visit); err != nil {
			return files, err
		}
	}
	return files, nil
}

//ToJSON converts to JSON
func (*YamlFileReader) ToJSON(
	b []byte,
) ([]byte, error) {
	b, err := yaml.YAMLToJSON(b)
	if err != nil {
		klog.Errorf("err:%s\nyaml:\n%s", err, string(b))
		return nil, err
	}
	return b, nil
}

func (r *YamlFileReader) ExtractAssets(prefix, dir string, excluded []string) error {
	assetNames, err := r.AssetNames(excluded)
	if err != nil {
		return err
	}
	for _, assetName := range assetNames {
		if !strings.HasPrefix(assetName, prefix) {
			continue
		}
		relPath, err := filepath.Rel(prefix, assetName)
		if err != nil {
			return err
		}
		path := filepath.Join(dir, relPath)

		if relPath == "." {
			path = filepath.Join(dir, filepath.Base(assetName))
		}
		err = os.MkdirAll(filepath.Dir(path), os.FileMode(0700))
		if err != nil {
			return err
		}
		data, err := r.Asset(assetName)
		if err != nil {
			return err
		}
		err = ioutil.WriteFile(path, data, os.FileMode(0600))
		if err != nil {
			return err
		}
	}
	return nil
}
