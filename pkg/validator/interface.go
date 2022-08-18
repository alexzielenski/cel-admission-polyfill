package validator

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type Interface interface {
	// Validates the object and returns nil if it succeeds
	// or an error explaining why the object fails validation
	// Thread-Safe
	Validate(gvr metav1.GroupVersionResource, oldObj, obj interface{}) error
}
