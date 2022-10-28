package util

import (
	"encoding/json"

	"k8s.io/apimachinery/pkg/runtime"
)

func Convert_runtime_Object_To_runtime_RawExtension_Raw(in *runtime.Object, out *runtime.RawExtension) error {
	if in == nil {
		out.Raw = []byte("null")
		return nil
	}
	raw, err := json.Marshal(*in)
	if err != nil {
		return err
	}

	out.Raw = raw
	return nil
}
