package common

import (
	"reflect"

	"github.com/google/cel-go/cel"
	"github.com/google/cel-go/ext"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apiserver/pkg/cel/library"
)

var BaseEnvOpts = []cel.EnvOption{
	cel.OptionalTypes(),
	ext.Strings(),
	library.Lists(),
	library.Regex(),
	library.URLs(),
	library.Quantity(),
	library.IP(),
	library.CIDR(),
	library.Format(),
}

// ConvertObjectToUnstructured converts any object to an unstructured.Unstructured object.
func ConvertObjectToUnstructured(obj interface{}) (*unstructured.Unstructured, error) {
	if obj == nil || reflect.ValueOf(obj).IsNil() {
		return &unstructured.Unstructured{Object: nil}, nil
	}
	ret, err := runtime.DefaultUnstructuredConverter.ToUnstructured(obj)
	if err != nil {
		return nil, err
	}
	return &unstructured.Unstructured{Object: ret}, nil
}
