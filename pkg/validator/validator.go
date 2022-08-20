package validator

import (
	"context"
	"sync"

	polyfillv1 "github.com/alexzielenski/cel_polyfill/pkg/apis/celadmissionpolyfill.k8s.io/v1alpha1"
	"github.com/alexzielenski/cel_polyfill/pkg/controller/structuralschema"
	admissionregistrationv1 "k8s.io/api/admissionregistration/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	apiserverschema "k8s.io/apiextensions-apiserver/pkg/apiserver/schema"
	"k8s.io/apiextensions-apiserver/pkg/apiserver/schema/cel"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/validation/field"
	"k8s.io/klog/v2"
)

type RuleSetValidator interface {
	Interface

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
	structuralSchemaController structuralschema.Controller

	//!TODO: refactor Validate and change to RWMutex
	lock               sync.Mutex
	registeredRuleSets map[string]ruleSetCacheEntry
}

type compileRule struct {
	validator  *cel.Validator
	structural *apiserverschema.Structural
}

type ruleSetCacheEntry struct {
	source        *polyfillv1.ValidationRuleSet
	compiledRules map[metav1.GroupVersionResource]compileRule
}

func (r ruleSetCacheEntry) Matches(gvr metav1.GroupVersionResource) bool {
	isWildcard := func(s []string) bool {
		return len(s) == 1 && s[0] == "*"
	}

	hasMatch := func(within []string, search string) bool {
		for _, q := range within {
			if q == search {
				return true
			}
		}
		return false
	}

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
	structuralSchemaController structuralschema.Controller,
) RuleSetValidator {
	return &validator{
		registeredRuleSets:         make(map[string]ruleSetCacheEntry),
		structuralSchemaController: structuralSchemaController,
	}
}

func (v *validator) Validate(gvr metav1.GroupVersionResource, oldObj, obj interface{}) ValidationResult {
	// 1. Find rules which match against this object
	// 2. Find compiled CEL rules for this object's type. If not yet
	//	seen, compile for this type and save.
	//	TODO: use bounded LRU cache?
	// 3. Ask all CEL rules to validate for us
	var failures field.ErrorList

	//!TODO: expensive to compute. change to computing lazily only if there
	// ended up being a match for this gvk among the registered rules
	structural, err := v.structuralSchemaController.Get(gvr)
	if err != nil {
		return ValidationResult{Status: ValidationOK}
	}

	v.lock.Lock()
	defer v.lock.Unlock()

	var celBudget int64 = cel.RuntimeCELCostBudget
	for _, entry := range v.registeredRuleSets {
		if entry.Matches(gvr) {
			compiled, exists := entry.compiledRules[gvr]
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
				entry.compiledRules[gvr] = compiled
			}

			var errorList field.ErrorList

			o, _ := runtime.DefaultUnstructuredConverter.ToUnstructured(obj)
			old, _ := runtime.DefaultUnstructuredConverter.ToUnstructured(oldObj)

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

		return ValidationResult{
			Status: ValidationForbidden,
			Error:  failures.ToAggregate(),
		}
	}

	return ValidationResult{Status: ValidationOK}
}

func (v *validator) AddRuleSet(ruleSet *polyfillv1.ValidationRuleSet) {
	v.lock.Lock()
	defer v.lock.Unlock()

	v.registeredRuleSets[ruleSet.Namespace+"/"+ruleSet.Name] = ruleSetCacheEntry{
		source:        ruleSet,
		compiledRules: make(map[metav1.GroupVersionResource]compileRule),
	}
}

func (v *validator) RemoveRuleSet(namespace string, name string) {
	v.lock.Lock()
	defer v.lock.Unlock()

	delete(v.registeredRuleSets, namespace+"/"+name)
}
