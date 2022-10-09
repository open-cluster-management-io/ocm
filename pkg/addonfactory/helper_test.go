package addonfactory

import (
	"testing"

	"k8s.io/apimachinery/pkg/api/equality"
)

func TestGetValuesFromAddonAnnotation(t *testing.T) {
	cases := []struct {
		name             string
		annotationValues string
		expectedValues   Values
		expectedErr      bool
	}{
		{
			name:             "get correct values from annotation",
			annotationValues: `{"Name":"test","TestGlobal":{"Image":"test","Tag":"test"}}`,
			expectedValues: Values{
				"Name": "test",
				"TestGlobal": map[string]interface{}{
					"Image": "test",
					"Tag":   "test",
				},
			},
			expectedErr: false,
		},
		{
			name:             "get no values from annotation",
			annotationValues: "",
			expectedValues:   Values{},
			expectedErr:      false,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			values, err := GetValuesFromAddonAnnotation(NewFakeManagedCluster("test"),
				NewFakeManagedClusterAddon("test", "test", "test", c.annotationValues))
			if !c.expectedErr && err != nil {
				t.Errorf("expected no error, bug got err %v", err)
			}
			if !equality.Semantic.DeepEqual(values, c.expectedValues) {
				t.Errorf("expected values %v, but got values %v", c.expectedValues, values)
			}
		})
	}
}

func TestMergeStructValues(t *testing.T) {
	type global struct {
		Image string `json:"image"`
		Tag   string `json:"tag"`
	}
	type config struct {
		Name   string `json:"name"`
		Global global `json:"global"`
	}
	cases := []struct {
		name           string
		valuesList     []Values
		expectedValues Values
	}{
		{
			name: "mutilpe merge",
			valuesList: []Values{
				func() Values {
					values, err := JsonStructToValues(config{
						Name:   "test",
						Global: global{Image: "test1"},
					})
					if err != nil {
						t.Fatalf("failed to struct to values %v", err)
					}
					return values
				}(),
				{"name": "dev", "label": "dev"},
				{"global": map[string]interface{}{"image": "test2", "tag": "test"}},
			},
			expectedValues: Values{"name": "dev", "label": "dev", "global": map[string]interface{}{"image": "test2", "tag": "test"}},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			overrideValues := Values{}
			for _, values := range c.valuesList {
				overrideValues = MergeValues(overrideValues, values)
			}

			if len(overrideValues) != len(c.expectedValues) {
				t.Errorf("expected values %v, but got values %v", c.expectedValues, overrideValues)
			}
		})
	}
}

func TestStripPrefix(t *testing.T) {
	cases := []struct {
		name   string
		prefix string
		path   string
		expect string
	}{
		{
			name:   "path file prefix without / suffix",
			prefix: "manifests/chart-management",
			path:   "manifests/chart-management/values.yaml",
			expect: "values.yaml",
		},
		{
			name:   "path file prefix with / suffix",
			prefix: "manifests/chart-management/",
			path:   "manifests/chart-management/values.yaml",
			expect: "values.yaml",
		},
		{
			name:   "path folder prefix without / suffix",
			prefix: "manifests/chart-management",
			path:   "manifests/chart-management/templates/service_account.yaml",
			expect: "templates/service_account.yaml",
		},
		{
			name:   "path folder prefix with / suffix",
			prefix: "manifests/chart-management/",
			path:   "manifests/chart-management/templates/service_account.yaml",
			expect: "templates/service_account.yaml",
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			result := stripPrefix(c.prefix, c.path)
			if result != c.expect {
				t.Errorf("name %s: expected values %v, but got values %v", c.name, c.expect, result)
			}
		})
	}
}
