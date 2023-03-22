/*
Copyright The Kubernetes Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

// Code generated by lister-gen. DO NOT EDIT.

package v1alpha1

import (
	v1alpha1 "github.com/alexzielenski/cel_polyfill/pkg/apis/admissionregistration.polyfill.sigs.k8s.io/v1alpha1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/tools/cache"
)

// ValidatingAdmissionPolicyLister helps list ValidatingAdmissionPolicies.
// All objects returned here must be treated as read-only.
type ValidatingAdmissionPolicyLister interface {
	// List lists all ValidatingAdmissionPolicies in the indexer.
	// Objects returned here must be treated as read-only.
	List(selector labels.Selector) (ret []*v1alpha1.ValidatingAdmissionPolicy, err error)
	// Get retrieves the ValidatingAdmissionPolicy from the index for a given name.
	// Objects returned here must be treated as read-only.
	Get(name string) (*v1alpha1.ValidatingAdmissionPolicy, error)
	ValidatingAdmissionPolicyListerExpansion
}

// validatingAdmissionPolicyLister implements the ValidatingAdmissionPolicyLister interface.
type validatingAdmissionPolicyLister struct {
	indexer cache.Indexer
}

// NewValidatingAdmissionPolicyLister returns a new ValidatingAdmissionPolicyLister.
func NewValidatingAdmissionPolicyLister(indexer cache.Indexer) ValidatingAdmissionPolicyLister {
	return &validatingAdmissionPolicyLister{indexer: indexer}
}

// List lists all ValidatingAdmissionPolicies in the indexer.
func (s *validatingAdmissionPolicyLister) List(selector labels.Selector) (ret []*v1alpha1.ValidatingAdmissionPolicy, err error) {
	err = cache.ListAll(s.indexer, selector, func(m interface{}) {
		ret = append(ret, m.(*v1alpha1.ValidatingAdmissionPolicy))
	})
	return ret, err
}

// Get retrieves the ValidatingAdmissionPolicy from the index for a given name.
func (s *validatingAdmissionPolicyLister) Get(name string) (*v1alpha1.ValidatingAdmissionPolicy, error) {
	obj, exists, err := s.indexer.GetByKey(name)
	if err != nil {
		return nil, err
	}
	if !exists {
		return nil, errors.NewNotFound(v1alpha1.Resource("validatingadmissionpolicy"), name)
	}
	return obj.(*v1alpha1.ValidatingAdmissionPolicy), nil
}
