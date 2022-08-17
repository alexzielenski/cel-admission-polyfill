package validator

import (
	"context"
	"errors"
	"fmt"
	"sync"

	polyfillv1 "github.com/alexzielenski/cel_polyfill/pkg/apis/celadmissionpolyfill.k8s.io/v1alpha1"
	admissionregistrationv1 "k8s.io/api/admissionregistration/v1"
	apiextensionshelpers "k8s.io/apiextensions-apiserver/pkg/apihelpers"
	apiextensionsinternal "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	apiserverschema "k8s.io/apiextensions-apiserver/pkg/apiserver/schema"
	"k8s.io/apiextensions-apiserver/pkg/apiserver/schema/cel"
	structuraldefaulting "k8s.io/apiextensions-apiserver/pkg/apiserver/schema/defaulting"
	apiextensionsv1listers "k8s.io/apiextensions-apiserver/pkg/client/listers/apiextensions/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	runtimeschema "k8s.io/apimachinery/pkg/runtime/schema"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/util/validation/field"
	"k8s.io/klog/v2"
)

type Interface interface {
	// Validates the object and returns nil if it succeeds
	// or an error explaining why the object fails validation
	// Thread-Safe
	Validate(oldObj, obj runtime.Object) error

	// Adds a Ruleset to be enforced by the Validate function.
	// If there is an existing ruleset with the same namespace/name, if is
	// replaced.
	// Thread-Safe
	AddRuleSet(ruleSet *polyfillv1.ValidationRuleSet)

	// Removes all rules associated with the given namespace/name of an added
	// ValidationRuleSet
	// Does nothing if it does not exist.
	// Thread-Safe
	RemoveRuleSet(namespace string, name string)
}

type validator struct {
	crdLister  apiextensionsv1listers.CustomResourceDefinitionLister
	restMapper meta.RESTMapper

	//!TODO: refactor Validate and change to RWMutex
	lock               sync.Mutex
	registeredRuleSets map[string]ruleSetCacheEntry
	structuralSchemas  map[runtimeschema.GroupVersionKind]*apiserverschema.Structural
}

type compileRule struct {
	validator  *cel.Validator
	structural *apiserverschema.Structural
}

type ruleSetCacheEntry struct {
	source        *polyfillv1.ValidationRuleSet
	compiledRules map[runtimeschema.GroupVersionKind]compileRule
}

func isWildcard(s []string) bool {
	return len(s) == 1 && s[0] == "*"
}

func hasMatch(within []string, search string) bool {
	for _, q := range within {
		if q == search {
			return true
		}
	}
	return false
}

func (r ruleSetCacheEntry) Matches(gvr schema.GroupVersionResource) bool {
	if len(r.source.Spec.Match) == 0 {
		return false
	}

	for _, match := range r.source.Spec.Match {
		if !isWildcard(match.APIGroups) && !hasMatch(match.APIGroups, gvr.Group) {
			return false
		}

		if !isWildcard(match.APIVersions) && !hasMatch(match.APIVersions, gvr.Version) {
			return false
		}

		if !isWildcard(match.Resources) && !hasMatch(match.Resources, gvr.Resource) {
			return false
		}

		//!TODO: support non-wildcard
		if len(match.Operations) != 1 || match.Operations[0] != admissionregistrationv1.OperationAll {
			return false
		}

		//!TODO: support non-wildcard
		if match.Scope == nil || *match.Scope != "*" {
			return false
		}
	}
	return true
}

func New(
	crdLister apiextensionsv1listers.CustomResourceDefinitionLister,
	restMapper meta.RESTMapper,
) Interface {
	return &validator{
		crdLister:          crdLister,
		restMapper:         restMapper,
		structuralSchemas:  make(map[runtimeschema.GroupVersionKind]*apiserverschema.Structural),
		registeredRuleSets: make(map[string]ruleSetCacheEntry),
	}
}

func (val *validator) getStructuralSchema(gvk schema.GroupVersionKind) (*apiserverschema.Structural, error) {
	// how to get canonical GVK for GVK?

	if existing, exists := val.structuralSchemas[gvk]; exists {
		return existing, nil
	}

	rsrc, err := val.restMapper.RESTMapping(runtimeschema.GroupKind{
		Group: gvk.Group,
		Kind:  gvk.Kind,
	}, gvk.Version)

	name := rsrc.Resource.Resource + "." + rsrc.Resource.Group

	crd, err := val.crdLister.Get(name)
	if err != nil {
		return nil, errors.New("crd not found")
	}

	// copied from https://github.com/kubernetes/kubernetes/blob/01cf641ffbb3c876c4fc6c3e53a0613356f883e5/staging/src/k8s.io/apiextensions-apiserver/pkg/apiserver/customresource_handler.go#L650-L651
	for _, v := range crd.Spec.Versions {
		sch, err := apiextensionshelpers.GetSchemaForVersion(crd, v.Name)
		if err != nil {
			utilruntime.HandleError(err)
			return nil, fmt.Errorf("the server could not properly serve the CR schema")
		}
		if sch == nil {
			continue
		}
		internalValidation := &apiextensionsinternal.CustomResourceValidation{}
		if err := apiextensionsv1.Convert_v1_CustomResourceValidation_To_apiextensions_CustomResourceValidation(sch, internalValidation, nil); err != nil {
			return nil, fmt.Errorf("failed converting CRD validation to internal version: %v", err)
		}
		s, err := apiserverschema.NewStructural(internalValidation.OpenAPIV3Schema)
		if crd.Spec.PreserveUnknownFields == false && err != nil {
			// This should never happen. If it does, it is a programming error.
			utilruntime.HandleError(fmt.Errorf("failed to convert schema to structural: %v", err))
			return nil, fmt.Errorf("the server could not properly serve the CR schema") // validation should avoid this
		}

		if crd.Spec.PreserveUnknownFields == false {
			// we don't own s completely, e.g. defaults are not deep-copied. So better make a copy here.
			s = s.DeepCopy()

			if err := structuraldefaulting.PruneDefaults(s); err != nil {
				// This should never happen. If it does, it is a programming error.
				utilruntime.HandleError(fmt.Errorf("failed to prune defaults: %v", err))
				return nil, fmt.Errorf("the server could not properly serve the CR schema") // validation should avoid this
			}
		}

		val.structuralSchemas[schema.GroupVersionKind{
			Group:   gvk.Group,
			Version: v.Name,
			Kind:    gvk.Kind,
		}] = s
	}

	if existing, exists := val.structuralSchemas[gvk]; exists {
		return existing, nil
	}

	return nil, errors.New("version not found")
}

func (v *validator) Validate(oldObj, obj runtime.Object) error {
	// 1. Find rules which match against this object
	// 2. Find compiled CEL rules for this object's type. If not yet
	//	seen, compile for this type and save.
	//	TODO: use bounded LRU cache?
	// 3. Ask all CEL rules to validate for us
	var failures field.ErrorList
	gvk := obj.GetObjectKind().GroupVersionKind()

	//!TODO: not this. the admissionreview has access to the requested resource
	// already. also we are unsure if that is an appropriate thing to filter by?
	gvr, err := v.restMapper.RESTMapping(runtimeschema.GroupKind{
		Group: gvk.Group,
		Kind:  gvk.Kind,
	}, gvk.Version)

	if err != nil {
		return err
	}
	//!TODO: expensive to compute. change to computing lazily only if there
	// ended up being a match for this gvk among the registered rules
	structural, err := v.getStructuralSchema(gvk)
	if err != nil {
		return err
	}

	v.lock.Lock()
	defer v.lock.Unlock()

	var celBudget int64 = cel.RuntimeCELCostBudget
	for _, entry := range v.registeredRuleSets {
		if entry.Matches(gvr.Resource) {
			compiled, exists := entry.compiledRules[gvk]
			if !exists {
				// Compile the rules
				// Get JSONSchemaProps from the CRD
				// Fetch CRD
				// apiserverschema.NewStructural()

				// Imbue the structural schema with our validations

				var wipeOutXValidations func(*apiserverschema.Structural) *apiserverschema.Structural
				wipeOutXValidations = func(s *apiserverschema.Structural) *apiserverschema.Structural {
					if s == nil {
						return nil
					}

					newS := *s
					newS.Items = wipeOutXValidations(s.Items)

					if newS.AdditionalProperties != nil {
						copied := *newS.AdditionalProperties
						copied.Structural = wipeOutXValidations(s.AdditionalProperties.Structural)

						newS.AdditionalProperties = &copied
					}

					if newS.Properties != nil {
						newProperties := map[string]apiserverschema.Structural{}
						newS.Properties = newProperties

						for prop, schema := range s.Properties {
							newS.Properties[prop] = *wipeOutXValidations(&schema)
						}
					}

					return &newS
				}

				var xvalidations apiextensionsv1.ValidationRules
				for _, rule := range entry.source.Spec.Rules {
					xvalidations = append(xvalidations, apiextensionsv1.ValidationRule{
						Rule:    rule.Rule,
						Message: rule.Message,
					})
				}
				copied := wipeOutXValidations(structural)

				// Imbue our validations unto the schema
				//!TODO: allow different locations that choose field paths to
				// apply stuff to?
				copied.XValidations = xvalidations
				compiled = compileRule{
					validator:  cel.NewValidator(copied, cel.PerCallLimit),
					structural: copied,
				}
				entry.compiledRules[gvk] = compiled
			}
			var errorList field.ErrorList

			var o interface{} = obj
			var old interface{} = oldObj
			if uns, ok := obj.(runtime.Unstructured); ok && uns != nil {
				o = uns.UnstructuredContent()
			}

			if uns, ok := oldObj.(runtime.Unstructured); ok && uns != nil {
				old = uns.UnstructuredContent()
			}

			errorList, celBudget = compiled.validator.Validate(
				context.TODO(),
				nil,
				compiled.structural,
				o,
				old,
				celBudget,
			)

			if len(errorList) > 0 {
				failures = append(failures, errorList...)
			}

		}
	}

	if failures != nil {
		for _, e := range failures {
			klog.Error(e)
		}
		return errors.New("validation failed")
	}

	return nil
}

func (v *validator) AddRuleSet(ruleSet *polyfillv1.ValidationRuleSet) {
	v.lock.Lock()
	defer v.lock.Unlock()

	v.registeredRuleSets[ruleSet.Namespace+"/"+ruleSet.Name] = ruleSetCacheEntry{
		source:        ruleSet,
		compiledRules: make(map[runtimeschema.GroupVersionKind]compileRule),
	}
}

func (v *validator) RemoveRuleSet(namespace string, name string) {
	v.lock.Lock()
	defer v.lock.Unlock()

	delete(v.registeredRuleSets, namespace+"/"+name)
}
