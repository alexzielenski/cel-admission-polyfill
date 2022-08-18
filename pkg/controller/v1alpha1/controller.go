package v1alpha1

import (
	"context"

	"github.com/alexzielenski/cel_polyfill/pkg/apis/celadmissionpolyfill.k8s.io/v1alpha1"
	"github.com/alexzielenski/cel_polyfill/pkg/controller"
	informers "github.com/alexzielenski/cel_polyfill/pkg/generated/informers/externalversions/celadmissionpolyfill.k8s.io/v1alpha1"
	"github.com/alexzielenski/cel_polyfill/pkg/validator"
)

func NewAdmissionRulesController(
	ruleSetsInformer informers.ValidationRuleSetInformer,
	validator validator.Interface,
) controller.Interface {
	name := "admissionRulesController"
	result := &admissionRulesController{
		name:       name,
		controller: nil,
		validator:  validator,
	}

	result.controller = controller.New(
		controller.NewInformer[*v1alpha1.ValidationRuleSet](ruleSetsInformer.Informer()),
		result.reconcile,
		controller.ControllerOptions{},
	)

	return result
}

type admissionRulesController struct {
	name       string
	validator  validator.Interface
	controller controller.Interface
}

func (c *admissionRulesController) Run(ctx context.Context) error {
	return c.controller.Run(ctx)
}

func (c *admissionRulesController) reconcile(namespace, name string, ruleSet *v1alpha1.ValidationRuleSet) error {
	if ruleSet == nil {
		// Rule was deleted. Remove it from our database of enforced rules
		c.validator.RemoveRuleSet(namespace, name)
		return nil
	}
	// Commit the ruleSet to our local data structure for enforcing validation
	// rules
	c.validator.AddRuleSet(ruleSet)
	return nil
}
