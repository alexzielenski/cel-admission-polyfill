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

package v0alpha1

import (
	"context"
	"time"

	v0alpha1 "github.com/alexzielenski/cel_polyfill/pkg/apis/celadmissionpolyfill.k8s.io/v0alpha1"
	scheme "github.com/alexzielenski/cel_polyfill/pkg/generated/clientset/versioned/scheme"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	types "k8s.io/apimachinery/pkg/types"
	watch "k8s.io/apimachinery/pkg/watch"
	rest "k8s.io/client-go/rest"
)

// ValidationRuleSetsGetter has a method to return a ValidationRuleSetInterface.
// A group's client should implement this interface.
type ValidationRuleSetsGetter interface {
	ValidationRuleSets(namespace string) ValidationRuleSetInterface
}

// ValidationRuleSetInterface has methods to work with ValidationRuleSet resources.
type ValidationRuleSetInterface interface {
	Create(ctx context.Context, validationRuleSet *v0alpha1.ValidationRuleSet, opts v1.CreateOptions) (*v0alpha1.ValidationRuleSet, error)
	Update(ctx context.Context, validationRuleSet *v0alpha1.ValidationRuleSet, opts v1.UpdateOptions) (*v0alpha1.ValidationRuleSet, error)
	UpdateStatus(ctx context.Context, validationRuleSet *v0alpha1.ValidationRuleSet, opts v1.UpdateOptions) (*v0alpha1.ValidationRuleSet, error)
	Delete(ctx context.Context, name string, opts v1.DeleteOptions) error
	DeleteCollection(ctx context.Context, opts v1.DeleteOptions, listOpts v1.ListOptions) error
	Get(ctx context.Context, name string, opts v1.GetOptions) (*v0alpha1.ValidationRuleSet, error)
	List(ctx context.Context, opts v1.ListOptions) (*v0alpha1.ValidationRuleSetList, error)
	Watch(ctx context.Context, opts v1.ListOptions) (watch.Interface, error)
	Patch(ctx context.Context, name string, pt types.PatchType, data []byte, opts v1.PatchOptions, subresources ...string) (result *v0alpha1.ValidationRuleSet, err error)
	ValidationRuleSetExpansion
}

// validationRuleSets implements ValidationRuleSetInterface
type validationRuleSets struct {
	client rest.Interface
	ns     string
}

// newValidationRuleSets returns a ValidationRuleSets
func newValidationRuleSets(c *CeladmissionpolyfillV0alpha1Client, namespace string) *validationRuleSets {
	return &validationRuleSets{
		client: c.RESTClient(),
		ns:     namespace,
	}
}

// Get takes name of the validationRuleSet, and returns the corresponding validationRuleSet object, and an error if there is any.
func (c *validationRuleSets) Get(ctx context.Context, name string, options v1.GetOptions) (result *v0alpha1.ValidationRuleSet, err error) {
	result = &v0alpha1.ValidationRuleSet{}
	err = c.client.Get().
		Namespace(c.ns).
		Resource("validationrulesets").
		Name(name).
		VersionedParams(&options, scheme.ParameterCodec).
		Do(ctx).
		Into(result)
	return
}

// List takes label and field selectors, and returns the list of ValidationRuleSets that match those selectors.
func (c *validationRuleSets) List(ctx context.Context, opts v1.ListOptions) (result *v0alpha1.ValidationRuleSetList, err error) {
	var timeout time.Duration
	if opts.TimeoutSeconds != nil {
		timeout = time.Duration(*opts.TimeoutSeconds) * time.Second
	}
	result = &v0alpha1.ValidationRuleSetList{}
	err = c.client.Get().
		Namespace(c.ns).
		Resource("validationrulesets").
		VersionedParams(&opts, scheme.ParameterCodec).
		Timeout(timeout).
		Do(ctx).
		Into(result)
	return
}

// Watch returns a watch.Interface that watches the requested validationRuleSets.
func (c *validationRuleSets) Watch(ctx context.Context, opts v1.ListOptions) (watch.Interface, error) {
	var timeout time.Duration
	if opts.TimeoutSeconds != nil {
		timeout = time.Duration(*opts.TimeoutSeconds) * time.Second
	}
	opts.Watch = true
	return c.client.Get().
		Namespace(c.ns).
		Resource("validationrulesets").
		VersionedParams(&opts, scheme.ParameterCodec).
		Timeout(timeout).
		Watch(ctx)
}

// Create takes the representation of a validationRuleSet and creates it.  Returns the server's representation of the validationRuleSet, and an error, if there is any.
func (c *validationRuleSets) Create(ctx context.Context, validationRuleSet *v0alpha1.ValidationRuleSet, opts v1.CreateOptions) (result *v0alpha1.ValidationRuleSet, err error) {
	result = &v0alpha1.ValidationRuleSet{}
	err = c.client.Post().
		Namespace(c.ns).
		Resource("validationrulesets").
		VersionedParams(&opts, scheme.ParameterCodec).
		Body(validationRuleSet).
		Do(ctx).
		Into(result)
	return
}

// Update takes the representation of a validationRuleSet and updates it. Returns the server's representation of the validationRuleSet, and an error, if there is any.
func (c *validationRuleSets) Update(ctx context.Context, validationRuleSet *v0alpha1.ValidationRuleSet, opts v1.UpdateOptions) (result *v0alpha1.ValidationRuleSet, err error) {
	result = &v0alpha1.ValidationRuleSet{}
	err = c.client.Put().
		Namespace(c.ns).
		Resource("validationrulesets").
		Name(validationRuleSet.Name).
		VersionedParams(&opts, scheme.ParameterCodec).
		Body(validationRuleSet).
		Do(ctx).
		Into(result)
	return
}

// UpdateStatus was generated because the type contains a Status member.
// Add a +genclient:noStatus comment above the type to avoid generating UpdateStatus().
func (c *validationRuleSets) UpdateStatus(ctx context.Context, validationRuleSet *v0alpha1.ValidationRuleSet, opts v1.UpdateOptions) (result *v0alpha1.ValidationRuleSet, err error) {
	result = &v0alpha1.ValidationRuleSet{}
	err = c.client.Put().
		Namespace(c.ns).
		Resource("validationrulesets").
		Name(validationRuleSet.Name).
		SubResource("status").
		VersionedParams(&opts, scheme.ParameterCodec).
		Body(validationRuleSet).
		Do(ctx).
		Into(result)
	return
}

// Delete takes name of the validationRuleSet and deletes it. Returns an error if one occurs.
func (c *validationRuleSets) Delete(ctx context.Context, name string, opts v1.DeleteOptions) error {
	return c.client.Delete().
		Namespace(c.ns).
		Resource("validationrulesets").
		Name(name).
		Body(&opts).
		Do(ctx).
		Error()
}

// DeleteCollection deletes a collection of objects.
func (c *validationRuleSets) DeleteCollection(ctx context.Context, opts v1.DeleteOptions, listOpts v1.ListOptions) error {
	var timeout time.Duration
	if listOpts.TimeoutSeconds != nil {
		timeout = time.Duration(*listOpts.TimeoutSeconds) * time.Second
	}
	return c.client.Delete().
		Namespace(c.ns).
		Resource("validationrulesets").
		VersionedParams(&listOpts, scheme.ParameterCodec).
		Timeout(timeout).
		Body(&opts).
		Do(ctx).
		Error()
}

// Patch applies the patch and returns the patched validationRuleSet.
func (c *validationRuleSets) Patch(ctx context.Context, name string, pt types.PatchType, data []byte, opts v1.PatchOptions, subresources ...string) (result *v0alpha1.ValidationRuleSet, err error) {
	result = &v0alpha1.ValidationRuleSet{}
	err = c.client.Patch(pt).
		Namespace(c.ns).
		Resource("validationrulesets").
		Name(name).
		SubResource(subresources...).
		VersionedParams(&opts, scheme.ParameterCodec).
		Body(data).
		Do(ctx).
		Into(result)
	return
}