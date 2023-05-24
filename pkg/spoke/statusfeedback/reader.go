package statusfeedback

import (
	"encoding/json"
	"fmt"
	"k8s.io/utils/pointer"
	ocmfeature "open-cluster-management.io/api/feature"
	"reflect"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	utilerrors "k8s.io/apimachinery/pkg/util/errors"
	"k8s.io/client-go/util/jsonpath"
	workapiv1 "open-cluster-management.io/api/work/v1"
	"open-cluster-management.io/work/pkg/features"
	"open-cluster-management.io/work/pkg/spoke/statusfeedback/rules"
)

const maxJsonRawLength = 1024

type StatusReader struct {
	wellKnownStatus rules.WellKnownStatusRuleResolver
}

func NewStatusReader() *StatusReader {
	return &StatusReader{
		wellKnownStatus: rules.DefaultWellKnownStatusRule(),
	}
}

func (s *StatusReader) GetValuesByRule(obj *unstructured.Unstructured, rule workapiv1.FeedbackRule) ([]workapiv1.FeedbackValue, error) {
	errs := []error{}
	values := []workapiv1.FeedbackValue{}

	switch rule.Type {
	case workapiv1.WellKnownStatusType:
		paths := s.wellKnownStatus.GetPathsByKind(obj.GroupVersionKind())
		if len(paths) == 0 {
			return values, fmt.Errorf("cannot find the wellknown statuses for resource with gvk %s", obj.GroupVersionKind().String())
		}

		for _, path := range paths {
			value, err := getValueByJsonPath(path.Name, path.Path, obj)
			if err != nil {
				errs = append(errs, err)
				continue
			}
			if value == nil {
				continue
			}
			values = append(values, *value)
		}
	case workapiv1.JSONPathsType:
		for _, path := range rule.JsonPaths {
			// skip if version is specified and the object version does not match
			if len(path.Version) != 0 && obj.GroupVersionKind().Version != path.Version {
				errs = append(errs, fmt.Errorf("version set in the path %s is not matched for the related resource", path.Name))
				continue
			}

			value, err := getValueByJsonPath(path.Name, path.Path, obj)
			if err != nil {
				errs = append(errs, err)
				continue
			}
			if value == nil {
				continue
			}
			values = append(values, *value)
		}
	}

	return values, utilerrors.NewAggregate(errs)
}

func getValueByJsonPath(name, path string, obj *unstructured.Unstructured) (*workapiv1.FeedbackValue, error) {
	j := jsonpath.New(name).AllowMissingKeys(true)
	err := j.Parse(fmt.Sprintf("{%s}", path))
	if err != nil {
		return nil, fmt.Errorf("failed to parse json path %s of %s with error: %v", path, name, err)
	}

	results, err := j.FindResults(obj.UnstructuredContent())

	if err != nil {
		return nil, fmt.Errorf("failed to find value for %s with error: %v", name, err)
	}

	if len(results) == 0 || len(results[0]) == 0 {
		// no results are found here.
		return nil, nil
	}

	// as we only support simple JSON path, we can assume to have only one result (or none, filtered out above)
	value := results[0][0].Interface()

	if value == nil {
		// ignore the result if it is nil
		return nil, nil
	}

	var fieldValue workapiv1.FieldValue
	switch t := value.(type) {
	case int64:
		fieldValue = workapiv1.FieldValue{
			Type:    workapiv1.Integer,
			Integer: &t,
		}
		return &workapiv1.FeedbackValue{
			Name:  name,
			Value: fieldValue,
		}, nil
	case string:
		fieldValue = workapiv1.FieldValue{
			Type:   workapiv1.String,
			String: &t,
		}
		return &workapiv1.FeedbackValue{
			Name:  name,
			Value: fieldValue,
		}, nil
	case bool:
		fieldValue = workapiv1.FieldValue{
			Type:    workapiv1.Boolean,
			Boolean: &t,
		}
		return &workapiv1.FeedbackValue{
			Name:  name,
			Value: fieldValue,
		}, nil
	default:
		if features.DefaultSpokeMutableFeatureGate.Enabled(ocmfeature.RawFeedbackJsonString) {
			jsonRaw, err := json.Marshal(&t)
			if err != nil {
				return nil, fmt.Errorf("failed to parse the resource to json string for name %s: %v", name, err)
			}
			if len(jsonRaw) > maxJsonRawLength {
				return nil, fmt.Errorf("the length of returned json raw string for name %s is larger than the maximum length %d", name, maxJsonRawLength)
			}
			fieldValue = workapiv1.FieldValue{
				Type:    workapiv1.JsonRaw,
				JsonRaw: pointer.String(string(jsonRaw)),
			}
			return &workapiv1.FeedbackValue{
				Name:  name,
				Value: fieldValue,
			}, nil
		}
	}

	return nil, fmt.Errorf("the type %v of the value for %s is not found", reflect.TypeOf(value), name)
}
