package v1alpha2

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
)

// +genclient
// +geninformer
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

type Environment struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Container string                   `json:"container,omitempty"`
	Variables map[string]OpenAPISchema `json:"variables,omitempty"`
	Functions EnvironmentFunctions     `json:"functions,omitempty"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
type EnvironmentList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`

	Items []Environment `json:"items"`
}

type Overload struct {
	FreeFunction bool            `json:"free_function,omitempty"`
	Args         []OpenAPISchema `json:"args,omitempty"`
	Return       OpenAPISchema   `json:"return,omitempty"`
}

type EnvironmentFunctions struct {
	Extensions map[string]map[string]Overload `json:"extensions,omitempty"`
}

// +genclient
// +geninformer
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

type PolicyTemplate struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	// +required
	Schema OpenAPISchema `json:"schema,omitempty"`

	// +required
	Evaluator `json:"evaluator,omitempty"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
type PolicyTemplateList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []PolicyTemplate `json:"items"`
}

type Evaluator struct {
	// +optional
	Environment string `json:"environment,omitempty"`

	// +required
	Productions []Production `json:"productions,omitempty"`

	// +optional
	Terms map[string]string `json:"terms,omitempty"`

	// +optional
	Ranges []EvaluatorRange `json:"ranges,omitempty"`
}

type EvaluatorRange struct {
	In    string `json:"in,omitempty"`
	Key   string `json:"key,omitempty"`
	Index string `json:"index,omitempty"`
	Value string `json:"value,omitempty"`
}

type Production struct {
	Match string `json:"match"`

	Decision  Decision   `json:",inline"`
	Decisions []Decision `json:"decisions"`
}

type Decision struct {
	Output      runtime.RawExtension `json:"output,omitempty"`
	Decision    string               `json:"decisionRef,omitempty"`
	DecisoinRef string               `json:"decision,omitempty"`
}

// Copy-pasted from upstream cel-policy-template. Needed to add JSON tags to use
// with CRD generator.
type OpenAPISchema struct {
	Title                string                    `json:"title,omitempty"`
	Description          string                    `json:"description,omitempty"`
	Type                 string                    `json:"type,omitempty"`
	TypeParam            string                    `json:"type_param,omitempty"`
	TypeRef              string                    `json:"$ref,omitempty"`
	DefaultValue         runtime.RawExtension      `json:"default,omitempty"`
	Enum                 []runtime.RawExtension    `json:"enum,omitempty"`
	Format               string                    `json:"format,omitempty"`
	Items                *OpenAPISchema            `json:"items,omitempty"`
	Metadata             map[string]string         `json:"metadata,omitempty"`
	Required             []string                  `json:"required,omitempty"`
	Properties           map[string]*OpenAPISchema `json:"properties,omitempty"`
	AdditionalProperties *OpenAPISchema            `json:"additionalProperties,omitempty"`
}
