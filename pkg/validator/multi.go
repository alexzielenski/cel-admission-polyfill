package validator

import metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

func NewMulti(validators ...Interface) Interface {
	return multi{validators: validators}
}

type multi struct {
	validators []Interface
}

func (m multi) Validate(gvr metav1.GroupVersionResource, oldObj, obj interface{}) ValidationResult {
	for _, v := range m.validators {

		err := v.Validate(gvr, oldObj, obj)

		// TODO: Allow configuration of multivalidator to vary how it combines
		// validation
		//
		// For now, policy is to return Forbidden if any are explicitly forbidden
		// otherwise, OK
		if err.Status == ValidationForbidden {
			return err
		}
	}

	return ValidationResult{
		Status: ValidationOK,
	}
}
