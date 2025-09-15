package addonfactory

import (
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apimachinery/pkg/runtime"
)

func trimCRDDescription(objects []runtime.Object) []runtime.Object {
	rstObjects := []runtime.Object{}
	for _, o := range objects {
		switch object := o.(type) {
		case *apiextensionsv1.CustomResourceDefinition:
			trimCRDv1Description(object)
			rstObjects = append(rstObjects, object)
		default:
			rstObjects = append(rstObjects, object)
		}
	}

	return rstObjects
}

// trimCRDv1Description is to remove the description info in the versions of CRD spec
func trimCRDv1Description(crd *apiextensionsv1.CustomResourceDefinition) {
	versions := crd.Spec.Versions
	for i := range versions {
		if versions[i].Schema != nil {
			removeDescriptionV1(versions[i].Schema.OpenAPIV3Schema)
		}
	}
}

func removeDescriptionV1(p *apiextensionsv1.JSONSchemaProps) {
	if p == nil {
		return
	}

	p.Description = ""

	if p.Items != nil {
		removeDescriptionV1(p.Items.Schema)
		for i := range p.Items.JSONSchemas {
			removeDescriptionV1(&p.Items.JSONSchemas[i])
		}
	}

	if len(p.AllOf) != 0 {
		for i := range p.AllOf {
			removeDescriptionV1(&p.AllOf[i])
		}
	}

	if len(p.OneOf) != 0 {
		for i := range p.OneOf {
			removeDescriptionV1(&p.OneOf[i])
		}
	}

	if len(p.AnyOf) != 0 {
		for i := range p.AnyOf {
			removeDescriptionV1(&p.AnyOf[i])
		}
	}

	if p.Not != nil {
		removeDescriptionV1(p.Not)
	}

	if len(p.Properties) != 0 {
		newProperties := map[string]apiextensionsv1.JSONSchemaProps{}
		for k := range p.Properties {
			v := p.Properties[k]
			removeDescriptionV1(&v)
			newProperties[k] = v
		}
		p.Properties = newProperties
	}

	if len(p.PatternProperties) != 0 {
		newProperties := map[string]apiextensionsv1.JSONSchemaProps{}
		for k := range p.PatternProperties {
			v := p.PatternProperties[k]
			removeDescriptionV1(&v)
			newProperties[k] = v
		}
		p.PatternProperties = newProperties
	}

	if p.AdditionalProperties != nil {
		removeDescriptionV1(p.AdditionalProperties.Schema)
	}

	if len(p.Dependencies) != 0 {
		newDependencies := map[string]apiextensionsv1.JSONSchemaPropsOrStringArray{}
		for k := range p.Dependencies {
			v := p.Dependencies[k]
			removeDescriptionV1(v.Schema)
			newDependencies[k] = v
		}
		p.Dependencies = newDependencies
	}

	if p.AdditionalItems != nil {
		removeDescriptionV1(p.AdditionalItems.Schema)
	}

	if len(p.Definitions) != 0 {
		newDefinitions := map[string]apiextensionsv1.JSONSchemaProps{}
		for k := range p.Definitions {
			v := p.Definitions[k]
			removeDescriptionV1(&v)
			newDefinitions[k] = v
		}
		p.Definitions = newDefinitions
	}

	if p.ExternalDocs != nil {
		p.ExternalDocs.Description = ""
	}
}
