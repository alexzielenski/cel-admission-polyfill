package validator

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type ValidationStatus uint

const (
	// The requested resource passed all policies without issue
	ValidationOK ValidationStatus = iota

	// The requested resource was rejected by one of the policies' rules
	ValidationForbidden

	// An internal error prevented one of the policies from being enforced,
	// so Status is inconclusive
	ValidationInternalError
)

type ValidationResult struct {
	Status ValidationStatus
	Error  error
}

type Interface interface {
	// Validates the object and returns nil if it succeeds
	// or an error explaining why the object fails validation
	// Thread-Safe
	//
	// Returns a boolean `explicitlyForbidden` if this Validator is actively
	//	attempting to block this operation. If `explicitlyForbidden` is true,
	//  then `err` MUST be non-nil.
	//
	//  If `explicitlyForbidden` is false
	Validate(gvr metav1.GroupVersionResource, oldObj, obj interface{}) ValidationResult
}
