package addonfactory

import (
	"reflect"
	"testing"
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
			if !reflect.DeepEqual(values, c.expectedValues) {
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
		jsonStruct     config
		values         Values
		expectedValues Values
	}{
		{
			name: "merge ok",
			jsonStruct: config{
				Name: "test",
				Global: global{
					Image: "test",
					Tag:   "test",
				},
			},
			values:         Values{"name": "dev", "label": "dev"},
			expectedValues: Values{"name": "dev", "label": "dev", "global": map[string]interface{}{"image": "test", "tag": "test"}},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			aValues, err := JsonStructToValues(c.jsonStruct)
			if err != nil {
				t.Fatalf("failed to struct to values %v", err)
			}
			mergedValues := MergeValues(aValues, c.values)
			if len(mergedValues) != len(c.expectedValues) {
				t.Errorf("expected values %v, but got values %v", c.expectedValues, mergedValues)
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
