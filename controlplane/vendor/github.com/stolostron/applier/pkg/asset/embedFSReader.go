// Copyright Contributors to the Open Cluster Management project

package asset

import (
	"embed"
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"

	"github.com/ghodss/yaml"
)

type ScenarioReader interface {
	//Retrieve an asset from the data source
	Asset(templatePath string) ([]byte, error)
	//List all available assets in the data source
	AssetNames(excluded []string) ([]string, error)
	ExtractAssets(prefix, dir string, excluded []string) error
	ToJSON(b []byte) ([]byte, error)
}

type ScenarioResourcesReader struct {
	files *embed.FS
}

var _ ScenarioReader = &ScenarioResourcesReader{
	files: nil,
}

func NewScenarioResourcesReader(files *embed.FS) *ScenarioResourcesReader {
	return &ScenarioResourcesReader{
		files: files,
	}
}

func (r *ScenarioResourcesReader) Asset(name string) ([]byte, error) {
	return r.files.ReadFile(name)
}

func (r *ScenarioResourcesReader) AssetNames(excluded []string) ([]string, error) {
	assetNames := make([]string, 0)
	got, err := r.assetWalk(".")
	if err != nil {
		return nil, err
	}
	for _, f := range got {
		if !isExcluded(f, excluded) {
			assetNames = append(assetNames, f)
		}
	}
	return assetNames, nil
}

func (r *ScenarioResourcesReader) assetWalk(f string) ([]string, error) {
	assets := make([]string, 0)
	file, err := r.files.Open(f)
	if err != nil {
		return assets, err
	}
	fs, err := file.Stat()
	if err != nil {
		return assets, err
	}
	if fs.IsDir() {
		de, err := r.files.ReadDir(f)
		if err != nil {
			return assets, err
		}
		for _, d := range de {
			di, err := d.Info()
			if err != nil {
				return assets, nil
			}
			assetsDir, err := r.assetWalk(filepath.Join(f, di.Name()))
			if err != nil {
				return assets, err
			}
			assets = append(assets, assetsDir...)
		}
		return assets, nil
	}
	return append(assets, f), nil
}

func isExcluded(f string, excluded []string) bool {
	for _, e := range excluded {
		if f == e {
			return true
		}
	}
	return false
}

func (r *ScenarioResourcesReader) ExtractAssets(prefix, dir string, excluded []string) error {
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

func (r *ScenarioResourcesReader) ToJSON(b []byte) ([]byte, error) {
	return yaml.YAMLToJSON(b)
}
