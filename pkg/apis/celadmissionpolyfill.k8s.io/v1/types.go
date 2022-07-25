package v1

import metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

// +genclient
// +geninformer
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

type ValidationRuleSet struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	// +optional
	Spec ValidationRuleSetSpec `json:"spec,omitempty"`

	// +optional
	Status ValidationRuleSetStatus `json:"status,omitempty"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
type ValidationRuleSetList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata"`

	Items []ValidationRuleSet `json:"items"`
}

type ValidationRuleSetSpec struct {
	// Associative on Name
	Rules []ValidationRule `json:"rules"`
}

type ValidationRuleSetStatus struct {
}

type ValidationRule struct {
	Name string `json:"name"`
}
