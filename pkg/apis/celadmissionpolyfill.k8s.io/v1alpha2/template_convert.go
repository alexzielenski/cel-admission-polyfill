package v1alpha2

import (
	"encoding/json"

	"github.com/google/cel-policy-templates-go/policy/model"
	"github.com/google/cel-policy-templates-go/policy/parser"
	metav1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
)

func PolicyTemplateToCELPolicyTemplate(template *PolicyTemplate) (*model.Source, *model.ParsedValue, error) {
	// re-arrange document into format parser expects
	//!TODO: upstream a better way to get our input into policy template lib
	reformatted := map[string]interface{}{
		"apiVersion": template.APIVersion,
		"kind":       template.Kind,
		"metadata": map[string]string{
			"name":      template.Name,
			"namespace": template.Namespace,
		},
		"evaluator": template.Evaluator,
	}

	if template.Validator != nil {
		reformatted["validator"] = template.Validator
	}

	yamled, err := json.MarshalIndent(reformatted, "", "    ")
	if err != nil {
		return nil, nil, err
	}

	source := model.ByteSource(yamled, "")
	parsed, issues := parser.ParseYaml(source)
	if issues != nil {
		return nil, nil, issues.Err()
	}

	// reparse
	// this cel model thing is too complicated to convert to

	return source, parsed, nil
}

func OpenAPISchemaTOJSONSchemaProps(schema *OpenAPISchema) *metav1.JSONSchemaProps {
	if schema == nil {
		return nil
	}

	var items *metav1.JSONSchemaPropsOrArray
	if schema.Items != nil {
		items = &metav1.JSONSchemaPropsOrArray{
			Schema: OpenAPISchemaTOJSONSchemaProps(schema.Items),
		}
	}

	var properties map[string]metav1.JSONSchemaProps
	if schema.Properties != nil {
		properties = make(map[string]metav1.JSONSchemaProps)
		for key, val := range schema.Properties {
			properties[key] = *OpenAPISchemaTOJSONSchemaProps(val)
		}
	}

	var additionalProperties *metav1.JSONSchemaPropsOrBool
	if schema.AdditionalProperties != nil {
		additionalProperties = &metav1.JSONSchemaPropsOrBool{
			Schema: OpenAPISchemaTOJSONSchemaProps(schema.AdditionalProperties),
		}
	}

	return &metav1.JSONSchemaProps{
		// No idea what this is. seems made up
		// TypeParam: schema.TypeParam

		// No idea what this is. seems made up
		// Metadata: schema.Metadata

		// Resolved shema should not have any type ref
		// TypeRef: schema.TypeRef

		Title:                schema.Title,
		Description:          schema.Description,
		Type:                 schema.Type,
		Default:              schema.DefaultValue,
		Enum:                 schema.Enum,
		Format:               schema.Format,
		Items:                items,
		Required:             schema.Required,
		Properties:           properties,
		AdditionalProperties: additionalProperties,
	}
}
