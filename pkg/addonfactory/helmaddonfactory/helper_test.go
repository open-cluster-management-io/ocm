package helmaddonfactory

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
			values, err := GetValuesFromAddonAnnotation(newManagedCluster("test"),
				newManagedClusterAddon("test", "test", "test", c.annotationValues))
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
		Image string
		Tag   string
	}
	type config struct {
		Name   string
		Global global
	}
	cases := []struct {
		name           string
		aStruct        config
		bValues        Values
		expectedValues Values
	}{
		{
			name: "merge ok",
			aStruct: config{
				Name: "test",
				Global: global{
					Image: "test",
					Tag:   "test",
				},
			},
			bValues:        Values{"Name": "dev", "label": "dev"},
			expectedValues: Values{"Name": "dev", "label": "dev", "global": map[string]interface{}{"Image": "test", "Tag": "test"}},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			aValues := StructToValues(c.aStruct)
			mergedValues := MergeValues(aValues, c.bValues)
			if len(mergedValues) != len(c.expectedValues) {
				t.Errorf("expected values %v, but got values %v", c.expectedValues, mergedValues)
			}
		})
	}
}
