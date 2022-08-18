package v1alpha2

import (
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
)

// +genclient
// +geninformer
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

type PolicyTemplate struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	// +optional
	PolicyTemplateSpec `json:"spec,omitempty"`
}

type PolicyTemplateSpec struct {
	// +optional
	PluralName string `json:"pluralName,omitempty"`

	// +required
	Schema OpenAPISchema `json:"schema,omitempty"`

	// +required
	Evaluator `json:"evaluator,omitempty"`

	// +optional
	*Validator `json:"validator,omitempty"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
type PolicyTemplateList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []PolicyTemplate `json:"items"`
}

type Validator struct {
	// +optional
	Description string `json:"description,omitempty"`

	// +optional
	Environment string `json:"environment,omitempty"`

	// +required
	Productions []ValidatorProduction `json:"productions"`

	// +optional
	Terms TermMap `json:"terms,omitempty"`
}

type Evaluator struct {
	// +optional
	Description string `json:"description,omitempty"`

	// +optional
	Environment string `json:"environment,omitempty"`

	// +required
	Productions []Production `json:"productions,omitempty"`

	// +optional
	Terms TermMap `json:"terms,omitempty"`

	// +optional
	Ranges []EvaluatorRange `json:"ranges,omitempty"`
}

type Term struct {
	Name string `json:"name"`
	//!TODO: evaluator requires this is a string
	Value runtime.RawExtension `json:"value"`
}

// +listType=map
// +listMapType=granular
// +listMapKey=name
type TermMap []Term

type EvaluatorRange struct {
	// +required
	In string `json:"in"`

	// +optional
	Key string `json:"key,omitempty"`

	// +optional
	Index string `json:"index,omitempty"`

	// +optional
	Value string `json:"value,omitempty"`
}

type ValidatorProduction struct {
	// +optional
	Match string `json:"match,omitempty"`

	// +optional
	Field string `json:"field,omitempty"`

	// +required
	Message string `json:"message"`

	// +optional
	Details runtime.RawExtension `json:"details,omitempty"`
}

type Production struct {
	// +optional
	Match string `json:"match,omitempty"`

	// +optional
	Output runtime.RawExtension `json:"output,omitempty"`

	// +optional
	Decision string `json:"decision,omitempty"`

	// +optional
	DecisionRef string `json:"decisionRef,omitempty"`

	// +optional
	Decisions []Decision `json:"decisions,omitempty"`
}

type Decision struct {
	Output runtime.RawExtension `json:"output"`

	// +optional
	Decision string `json:"decision,omitempty"`

	// +optional
	DecisionRef string `json:"decisionRef,omitempty"`
}

// Copy-pasted from upstream cel-policy-template. Needed to add JSON tags to use
// with CRD generator.
// !TODO: Figure out if there is a way to reconcile this OpenAPI schema def with the
// upstream k8s version so a single type can be used.
// Converting different OpenAPI formats already is a huge perf hit in apiserver
type OpenAPISchema struct {
	Title                string                    `json:"title,omitempty"`
	Description          string                    `json:"description,omitempty"`
	Type                 string                    `json:"type,omitempty"`
	TypeParam            string                    `json:"type_param,omitempty"`
	TypeRef              string                    `json:"$ref,omitempty"`
	DefaultValue         *apiextensionsv1.JSON     `json:"default,omitempty"`
	Enum                 []apiextensionsv1.JSON    `json:"enum,omitempty"`
	Format               string                    `json:"format,omitempty"`
	Items                *OpenAPISchema            `json:"items,omitempty"`
	Metadata             map[string]string         `json:"metadata,omitempty"`
	Required             []string                  `json:"required,omitempty"`
	Properties           map[string]*OpenAPISchema `json:"properties,omitempty"`
	AdditionalProperties *OpenAPISchema            `json:"additionalProperties,omitempty"`
}
