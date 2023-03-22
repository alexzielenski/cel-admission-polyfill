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

// Code generated by client-gen. DO NOT EDIT.

package fake

import (
	"context"

	v1alpha1 "github.com/alexzielenski/cel_polyfill/pkg/apis/admissionregistration.polyfill.sigs.k8s.io/v1alpha1"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	labels "k8s.io/apimachinery/pkg/labels"
	types "k8s.io/apimachinery/pkg/types"
	watch "k8s.io/apimachinery/pkg/watch"
	testing "k8s.io/client-go/testing"
)

// FakeValidatingAdmissionPolicyBindings implements ValidatingAdmissionPolicyBindingInterface
type FakeValidatingAdmissionPolicyBindings struct {
	Fake *FakeAdmissionregistrationV1alpha1
}

var validatingadmissionpolicybindingsResource = v1alpha1.SchemeGroupVersion.WithResource("validatingadmissionpolicybindings")

var validatingadmissionpolicybindingsKind = v1alpha1.SchemeGroupVersion.WithKind("ValidatingAdmissionPolicyBinding")

// Get takes name of the validatingAdmissionPolicyBinding, and returns the corresponding validatingAdmissionPolicyBinding object, and an error if there is any.
func (c *FakeValidatingAdmissionPolicyBindings) Get(ctx context.Context, name string, options v1.GetOptions) (result *v1alpha1.ValidatingAdmissionPolicyBinding, err error) {
	obj, err := c.Fake.
		Invokes(testing.NewRootGetAction(validatingadmissionpolicybindingsResource, name), &v1alpha1.ValidatingAdmissionPolicyBinding{})
	if obj == nil {
		return nil, err
	}
	return obj.(*v1alpha1.ValidatingAdmissionPolicyBinding), err
}

// List takes label and field selectors, and returns the list of ValidatingAdmissionPolicyBindings that match those selectors.
func (c *FakeValidatingAdmissionPolicyBindings) List(ctx context.Context, opts v1.ListOptions) (result *v1alpha1.ValidatingAdmissionPolicyBindingList, err error) {
	obj, err := c.Fake.
		Invokes(testing.NewRootListAction(validatingadmissionpolicybindingsResource, validatingadmissionpolicybindingsKind, opts), &v1alpha1.ValidatingAdmissionPolicyBindingList{})
	if obj == nil {
		return nil, err
	}

	label, _, _ := testing.ExtractFromListOptions(opts)
	if label == nil {
		label = labels.Everything()
	}
	list := &v1alpha1.ValidatingAdmissionPolicyBindingList{ListMeta: obj.(*v1alpha1.ValidatingAdmissionPolicyBindingList).ListMeta}
	for _, item := range obj.(*v1alpha1.ValidatingAdmissionPolicyBindingList).Items {
		if label.Matches(labels.Set(item.Labels)) {
			list.Items = append(list.Items, item)
		}
	}
	return list, err
}

// Watch returns a watch.Interface that watches the requested validatingAdmissionPolicyBindings.
func (c *FakeValidatingAdmissionPolicyBindings) Watch(ctx context.Context, opts v1.ListOptions) (watch.Interface, error) {
	return c.Fake.
		InvokesWatch(testing.NewRootWatchAction(validatingadmissionpolicybindingsResource, opts))
}

// Create takes the representation of a validatingAdmissionPolicyBinding and creates it.  Returns the server's representation of the validatingAdmissionPolicyBinding, and an error, if there is any.
func (c *FakeValidatingAdmissionPolicyBindings) Create(ctx context.Context, validatingAdmissionPolicyBinding *v1alpha1.ValidatingAdmissionPolicyBinding, opts v1.CreateOptions) (result *v1alpha1.ValidatingAdmissionPolicyBinding, err error) {
	obj, err := c.Fake.
		Invokes(testing.NewRootCreateAction(validatingadmissionpolicybindingsResource, validatingAdmissionPolicyBinding), &v1alpha1.ValidatingAdmissionPolicyBinding{})
	if obj == nil {
		return nil, err
	}
	return obj.(*v1alpha1.ValidatingAdmissionPolicyBinding), err
}

// Update takes the representation of a validatingAdmissionPolicyBinding and updates it. Returns the server's representation of the validatingAdmissionPolicyBinding, and an error, if there is any.
func (c *FakeValidatingAdmissionPolicyBindings) Update(ctx context.Context, validatingAdmissionPolicyBinding *v1alpha1.ValidatingAdmissionPolicyBinding, opts v1.UpdateOptions) (result *v1alpha1.ValidatingAdmissionPolicyBinding, err error) {
	obj, err := c.Fake.
		Invokes(testing.NewRootUpdateAction(validatingadmissionpolicybindingsResource, validatingAdmissionPolicyBinding), &v1alpha1.ValidatingAdmissionPolicyBinding{})
	if obj == nil {
		return nil, err
	}
	return obj.(*v1alpha1.ValidatingAdmissionPolicyBinding), err
}

// Delete takes name of the validatingAdmissionPolicyBinding and deletes it. Returns an error if one occurs.
func (c *FakeValidatingAdmissionPolicyBindings) Delete(ctx context.Context, name string, opts v1.DeleteOptions) error {
	_, err := c.Fake.
		Invokes(testing.NewRootDeleteActionWithOptions(validatingadmissionpolicybindingsResource, name, opts), &v1alpha1.ValidatingAdmissionPolicyBinding{})
	return err
}

// DeleteCollection deletes a collection of objects.
func (c *FakeValidatingAdmissionPolicyBindings) DeleteCollection(ctx context.Context, opts v1.DeleteOptions, listOpts v1.ListOptions) error {
	action := testing.NewRootDeleteCollectionAction(validatingadmissionpolicybindingsResource, listOpts)

	_, err := c.Fake.Invokes(action, &v1alpha1.ValidatingAdmissionPolicyBindingList{})
	return err
}

// Patch applies the patch and returns the patched validatingAdmissionPolicyBinding.
func (c *FakeValidatingAdmissionPolicyBindings) Patch(ctx context.Context, name string, pt types.PatchType, data []byte, opts v1.PatchOptions, subresources ...string) (result *v1alpha1.ValidatingAdmissionPolicyBinding, err error) {
	obj, err := c.Fake.
		Invokes(testing.NewRootPatchSubresourceAction(validatingadmissionpolicybindingsResource, name, pt, data, subresources...), &v1alpha1.ValidatingAdmissionPolicyBinding{})
	if obj == nil {
		return nil, err
	}
	return obj.(*v1alpha1.ValidatingAdmissionPolicyBinding), err
}
