package validator

import metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

func NewMulti(validators ...Interface) Interface {
	return multi{validators: validators}
}

type multi struct {
	validators []Interface
}

func (m multi) Validate(gvr metav1.GroupVersionResource, oldObj, obj interface{}) error {
	for _, v := range m.validators {
		err := v.Validate(gvr, oldObj, obj)
		if err != nil {
			return err
		}
	}

	return nil
}
