//go:generate go run sigs.k8s.io/controller-tools/cmd/controller-gen crd paths=. output:dir=.

// +groupName=stable.example.com
// +versionName=v1
package union_basic

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// +kubebuilder:storageversion
// +kubebuilder:subresource:status
type BasicUnion struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   BasicUnionSpec   `json:"spec,omitempty"`
	Status BasicUnionStatus `json:"status,omitempty"`
}

type BasicUnionStatus struct{}
type BasicUnionSpec struct {
	Discriminator string `json:"discriminator"`

	// +optional
	Mode1 string `json:"mode1"`

	// +optional
	Mode2 string `json:"mode2"`

	// +optional
	Value string `json:"value,omitempty"`
}
