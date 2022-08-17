package v1alpha1

import (
	admissionregistrationv1 "k8s.io/api/admissionregistration/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

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

	// +listType=map
	// +listMapKey=name
	Rules []ValidationRule `json:"rules"`

	// +listType=atomic
	Match []admissionregistrationv1.RuleWithOperations `json:"match"`
}

type ValidationRuleSetStatus struct {
	//!TODO: surface errors/status for each rule
}

type ValidationRule struct {
	Name    string `json:"name"`
	Rule    string `json:"rule"`
	Message string `json:"message"`
}
