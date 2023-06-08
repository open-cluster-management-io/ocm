package addonfactory

import (
	"embed"
	"encoding/json"
	"io/fs"
	"path/filepath"
	"strings"

	"github.com/fatih/structs"

	"helm.sh/helm/v3/pkg/chart"
	"helm.sh/helm/v3/pkg/chart/loader"

	"k8s.io/klog/v2"

	addonapiv1alpha1 "open-cluster-management.io/api/addon/v1alpha1"
	clusterv1 "open-cluster-management.io/api/cluster/v1"
)

// GetValuesFromAddonAnnotation get the values in the annotation of addon cr.
// the key of the annotation is `addon.open-cluster-management.io/values`, the value is a json string which has the values.
// for example: "addon.open-cluster-management.io/values": `{"NodeSelector":{"host":"ssd"},"Image":"quay.io/helloworld:2.4"}`
func GetValuesFromAddonAnnotation(
	cluster *clusterv1.ManagedCluster,
	addon *addonapiv1alpha1.ManagedClusterAddOn) (Values, error) {
	values := map[string]interface{}{}
	annotations := addon.GetAnnotations()
	if len(annotations[AnnotationValuesName]) == 0 {
		return values, nil
	}

	err := json.Unmarshal([]byte(annotations[AnnotationValuesName]), &values)
	if err != nil {
		return values, err
	}

	return values, nil
}

// MergeValues merges the 2 given Values to a Values.
// the values of b will override that in a for the same fields.
func MergeValues(a, b Values) Values {
	out := Values{}
	for k, v := range a {
		out[k] = v
	}
	for bk, bv := range b {
		if bv, ok := bv.(map[string]interface{}); ok {
			if av, ok := out[bk]; ok {
				if av, ok := av.(map[string]interface{}); ok {
					out[bk] = mergeInterfaceMaps(av, bv)
					continue
				}
			}
		}
		out[bk] = bv
	}
	return out
}

// StructToValues converts the given struct to a Values
func StructToValues(a interface{}) Values {
	return structs.Map(a)
}

// JsonStructToValues converts the given json struct to a Values
func JsonStructToValues(a interface{}) (Values, error) {
	raw, err := json.Marshal(a)
	if err != nil {
		return nil, err
	}
	v := Values{}

	err = json.Unmarshal(raw, &v)
	if err != nil {
		return nil, err
	}
	return v, nil
}

func loadChart(chartFS embed.FS, chartPrefix string) (*chart.Chart, error) {
	files, err := getFiles(chartFS)
	if err != nil {
		return nil, err
	}

	var bfs []*loader.BufferedFile
	for _, fileName := range files {
		b, err := fs.ReadFile(chartFS, fileName)
		if err != nil {
			klog.Errorf("failed to read file %v. err:%v", fileName, err)
			return nil, err
		}
		if !strings.HasPrefix(fileName, chartPrefix) {
			continue
		}
		bf := &loader.BufferedFile{
			Name: stripPrefix(chartPrefix, fileName),
			Data: b,
		}
		bfs = append(bfs, bf)
	}

	userChart, err := loader.LoadFiles(bfs)
	if err != nil {
		klog.Errorf("failed to load chart. err:%v", err)
		return nil, err
	}
	return userChart, nil
}

func getTemplateFiles(templateFS embed.FS, dir string) ([]string, error) {
	files, err := getFiles(templateFS)
	if err != nil {
		return nil, err
	}
	if dir == "." || len(dir) == 0 {
		return files, nil
	}

	var templateFiles []string
	for _, f := range files {
		if strings.HasPrefix(f, dir) {
			templateFiles = append(templateFiles, f)
		}

	}
	return templateFiles, nil
}

func getFiles(manifestFS embed.FS) ([]string, error) {
	var res []string
	err := fs.WalkDir(manifestFS, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		res = append(res, path)
		return nil
	})
	return res, err
}

func stripPrefix(chartPrefix, path string) string {
	prefixNoPathSeparatorSuffix := strings.TrimSuffix(chartPrefix, string(filepath.Separator))
	chartPrefixLen := len(strings.Split(prefixNoPathSeparatorSuffix, string(filepath.Separator)))
	pathValues := strings.Split(path, string(filepath.Separator))
	return strings.Join(pathValues[chartPrefixLen:], string(filepath.Separator))
}

func mergeInterfaceMaps(a, b map[string]interface{}) map[string]interface{} {
	out := map[string]interface{}{}
	for k, v := range a {
		out[k] = v
	}

	for bk, bv := range b {
		if bv, ok := bv.(map[string]interface{}); ok {
			if av, ok := out[bk]; ok {
				if av, ok := av.(map[string]interface{}); ok {
					out[bk] = mergeInterfaceMaps(av, bv)
					continue
				}
			}
		}

		out[bk] = bv
	}

	return out
}
