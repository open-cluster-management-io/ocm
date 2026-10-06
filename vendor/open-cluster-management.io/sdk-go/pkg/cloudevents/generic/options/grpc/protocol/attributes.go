package protocol

import (
	"fmt"
	"strings"

	pbv1 "open-cluster-management.io/sdk-go/pkg/cloudevents/generic/options/grpc/protobuf/v1"
)

const (
	specNameSpecVersion     = "specversion"
	specNameID              = "id"
	specNameSource          = "source"
	specNameType            = "type"
	specNameDataContentType = "datacontenttype"
)

func validateAttributeNames(attributes map[string]*pbv1.CloudEventAttributeValue) error {
	for name := range attributes {
		if name != strings.ToLower(name) {
			return fmt.Errorf("invalid cloud event attribute name %q: attribute names must be lower-case", name)
		}

		if name == contenttype {
			continue
		}

		if !strings.HasPrefix(name, prefix) {
			return fmt.Errorf("invalid cloud event attribute name %q: expected the %q prefix", name, prefix)
		}

		switch specName := strings.TrimPrefix(name, prefix); specName {
		case specNameSpecVersion, specNameID, specNameSource, specNameType:
			return fmt.Errorf("invalid cloud event attribute name %q: the %q attribute is carried in a dedicated field",
				name, specName)
		case specNameDataContentType:
			return fmt.Errorf("invalid cloud event attribute name %q: the %q attribute is carried as %q",
				name, specName, contenttype)
		}
	}

	return nil
}
