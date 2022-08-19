package v1alpha2

import (
	"encoding/json"
	"reflect"

	"github.com/google/cel-policy-templates-go/policy/model"
	"github.com/google/cel-policy-templates-go/policy/parser"
	metav1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/serializer"
)

type HackyTermMap struct {
	t TermMap
}

var _ json.Marshaler = HackyTermMap{t: nil}

func (h HackyTermMap) MarshalJSON() ([]byte, error) {
	t := h.t

	if t == nil {
		return []byte(`null`), nil
	} else if len(t) == 0 {
		return []byte(`{}`), nil
	}

	out := []byte(`{`)
	for _, e := range t {
		key, err := json.Marshal(e.Name)
		if err != nil {
			return nil, err
		}

		val, err := json.Marshal(e.Value)
		if err != nil {
			return nil, err
		}

		out = append(out, key...)
		out = append(out, ':')
		out = append(out, val...)
		out = append(out, ',')
	}
	out[len(out)-1] = '}'
	return out, nil
}

func wipeZeroes(m map[string]interface{}) {
	for k, v := range m {
		if reflect.ValueOf(v).IsZero() {
			delete(m, k)
		}

		if nextM, ok := v.(map[string]interface{}); ok {
			wipeZeroes(nextM)
		}
	}
}

func PolicyTemplateToCELPolicyTemplate(template *PolicyTemplate) (*model.Source, *model.ParsedValue, error) {
	// re-arrange document into format parser expects
	//!TODO: upstream a better way to get our input into policy template lib
	reformatted := map[string]interface{}{
		"apiVersion": template.APIVersion,
		"kind":       template.Kind,
		"schema":     template.Schema,
		"metadata": map[string]string{
			"name":      template.Name,
			"namespace": template.Namespace,
		},
		"evaluator": map[string]interface{}{
			"description": template.Evaluator.Description,
			"environment": template.Evaluator.Environment,
			"productions": template.Evaluator.Productions,
			"terms":       HackyTermMap{t: template.Evaluator.Terms},
			"ranges":      template.Evaluator.Ranges,
		},
		//!TODO: terms also busted in validator
		"validator": template.Validator,
	}

	scheme := runtime.NewScheme()
	AddToScheme(scheme)

	serializer.NewCodecFactory(scheme)

	wipeZeroes(reformatted)

	yamled, err := json.Marshal(reformatted)
	if err != nil {
		return nil, nil, err
	}

	println(string(yamled))
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
