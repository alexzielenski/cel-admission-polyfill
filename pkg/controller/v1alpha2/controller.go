package v1alpha2

import (
	"context"

	"github.com/alexzielenski/cel_polyfill/pkg/apis/celadmissionpolyfill.k8s.io/v1alpha2"
	"github.com/alexzielenski/cel_polyfill/pkg/controller"
	"github.com/alexzielenski/cel_polyfill/pkg/validator"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/cache"
)

var _ validator.Interface = &templateController{}

func NewPolicyTemplateController(
	policyTemplatesInformer cache.SharedIndexInformer,
	crdInformer cache.SharedIndexInformer,
) controller.Interface {
	result := &templateController{}
	result.policyTemplatesController = controller.New(
		controller.NewInformer[*v1alpha2.PolicyTemplate](policyTemplatesInformer),
		result.reconcilePolicyTemplate,
		controller.ControllerOptions{},
	)
	result.crdController = controller.New(
		controller.NewInformer[*apiextensionsv1.CustomResourceDefinition](crdInformer),
		result.reconcileCRD,
		controller.ControllerOptions{},
	)
	return result
}

type templateController struct {
	policyTemplatesController controller.Interface
	crdController             controller.Interface
}

func (c *templateController) Run(ctx context.Context) error {
	myCtx, myCancel := context.WithCancel(ctx)

	go func() {
		c.policyTemplatesController.Run(myCtx)
		myCancel()
	}()

	go func() {
		c.crdController.Run(myCtx)
		myCancel()
	}()

	<-myCtx.Done()
	return nil
}

func (c *templateController) reconcilePolicyTemplate(
	namespace, name string,
	template *v1alpha2.PolicyTemplate,
) error {
	if template == nil {
		// Delete CRD with same name owned by this guy
		// k8s garbage collection via owner references might work
		return nil
	}

	// 1. Each policy template in turn owns a CRD
	// 2.

	return nil
}

func (c *templateController) reconcileCRD(
	namespace, name string,
	template *apiextensionsv1.CustomResourceDefinition,
) error {
	if template == nil {
		// Delete CRD with same name owned by this guy
		// k8s garbage collection via owner references might work
		return nil
	}

	// 1. Each policy template in turn owns a CRD
	// 2.

	return nil
}

func (c *templateController) Validate(gvr metav1.GroupVersionResource, oldObj, obj interface{}) error {
	return nil
}
