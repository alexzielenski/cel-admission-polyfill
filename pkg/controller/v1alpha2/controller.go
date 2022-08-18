package v1alpha2

import (
	"context"
	"errors"
	"sync"

	"github.com/google/cel-go/cel"
	"github.com/google/cel-policy-templates-go/policy"
	"github.com/google/cel-policy-templates-go/policy/compiler"
	"github.com/google/cel-policy-templates-go/policy/limits"
	"github.com/google/cel-policy-templates-go/policy/model"

	"github.com/alexzielenski/cel_polyfill/pkg/apis/celadmissionpolyfill.k8s.io/v1alpha2"
	"github.com/alexzielenski/cel_polyfill/pkg/controller"
	"github.com/alexzielenski/cel_polyfill/pkg/controller/structuralschema"
	"github.com/alexzielenski/cel_polyfill/pkg/validator"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	apiextensionsclientset "k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/tools/cache"
)

var _ validator.Interface = &templateController{}

func NewPolicyTemplateController(
	policyTemplatesInformer cache.SharedIndexInformer,
	structuralSchemaController structuralschema.Controller,
	crdClient apiextensionsclientset.Interface,
) controller.Interface {
	engine, err := policy.NewEngine()
	if err != nil {
		utilruntime.HandleError(err)
	}

	result := &templateController{
		structuralSchemaController: structuralSchemaController,
		crdClient:                  crdClient,
		policyEngine:               engine,
	}
	result.policyTemplatesController = controller.New(
		controller.NewInformer[*v1alpha2.PolicyTemplate](policyTemplatesInformer),
		result.reconcilePolicyTemplate,
		controller.ControllerOptions{},
	)
	return result
}

type templateController struct {
	policyTemplatesController  controller.Interface
	structuralSchemaController structuralschema.Controller
	crdClient                  apiextensionsclientset.Interface

	lock      sync.Mutex
	instances map[string]instanceInfo

	runningContext context.Context

	policyEngine *policy.Engine
}

type instanceInfo struct {
	controller controller.Interface
	cancelFunc func()
}

func (c *templateController) Run(ctx context.Context) error {
	myCtx, myCancel := context.WithCancel(ctx)
	c.runningContext = myCtx

	go func() {
		c.policyTemplatesController.Run(myCtx)
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

		//!TODO: Remove this template from the policy engine as well
		// as any of its instances
		return nil
	}

	// Regenerate the CRD
	// 1. Each policy template in turn owns a CRD
	converted := v1alpha2.OpenAPISchemaTOJSONSchemaProps(&template.Schema)
	if converted == nil {
		utilruntime.HandleError(errors.New("failed to convert OpenAPISchema to JSONSchemaProps? THis hsould never happen"))
		return nil
	}

	crd := apiextensionsv1.CustomResourceDefinition{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "v1",
			Kind:       "CustomResourceDefinition",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: template.Name + "." + template.GroupVersionKind().Group,
			OwnerReferences: []metav1.OwnerReference{
				{
					APIVersion: template.APIVersion,
					Kind:       template.Kind,
					Name:       name,
					UID:        template.GetUID(),
				},
			},
		},
		Spec: apiextensionsv1.CustomResourceDefinitionSpec{
			Group: template.GroupVersionKind().Group,
			Names: apiextensionsv1.CustomResourceDefinitionNames{
				Plural:     template.Name,
				Singular:   template.Name,
				ShortNames: []string{},
				Kind:       template.Name,
				ListKind:   template.Name + "List",
				Categories: []string{"policy"},
			},
			Scope:                 apiextensionsv1.ClusterScoped,
			PreserveUnknownFields: false,
			Versions: []apiextensionsv1.CustomResourceDefinitionVersion{
				{
					Name:                     "v1",
					Served:                   true,
					Storage:                  true,
					Deprecated:               false,
					Subresources:             nil,
					AdditionalPrinterColumns: nil,

					Schema: &apiextensionsv1.CustomResourceValidation{
						OpenAPIV3Schema: &apiextensionsv1.JSONSchemaProps{
							Type:     "object",
							Required: []string{"apiVersion", "kind", "metadata"},
							Properties: map[string]apiextensionsv1.JSONSchemaProps{
								"apiVersion":  {Type: "string"},
								"kind":        {Type: "string"},
								"metadata":    {Type: "object"},
								"description": {Type: "string"},
								"selector":    {Type: "object", Properties: map[string]apiextensionsv1.JSONSchemaProps{}},
								"rule":        *converted,
								"rules":       {Type: "array", Items: &apiextensionsv1.JSONSchemaPropsOrArray{Schema: converted}},
							},
						},
					},
				},
			},
		},
	}

	// bytes, err := json.Marshal(crd)
	// if err != nil {
	// 	utilruntime.HandleError(err)
	// 	return nil
	// }

	_, err := c.crdClient.
		ApiextensionsV1().
		CustomResourceDefinitions().Create(context.TODO(), &crd, metav1.CreateOptions{
		FieldManager: "template-controller",
	})

	// _, err := c.crdClient.
	// 	ApiextensionsV1().
	// 	CustomResourceDefinitions().Update(context.TODO(), &crd, metav1.UpdateOptions{
	// 	FieldManager: "template-controller",
	// })

	// _, err = c.crdClient.
	// 	ApiextensionsV1().
	// 	CustomResourceDefinitions().
	// 	Patch(
	// 		context.TODO(),
	// 		template.Name+"."+template.GroupVersionKind().Group,
	// 		types.ApplyPatchType,
	// 		bytes,
	// 		metav1.PatchOptions{},
	// 	)

	if err != nil {
		utilruntime.HandleError(err)
		return nil
	}

	c.lock.Lock()
	defer c.lock.Unlock()

	// Convert input template into a model.Value
	//
	//!TODO NEXT: use PolicyTemplateToCELPolicyTemplate stub to create model.template
	// as compiledTemplate variable
	//
	// fill out stub.
	// test it out
	//
	// prob want to fork the policy stuff
	// need to be able to remove templates/instances
	// also compile template from model.Value instead of yaml
	//	or do we want to create yaml and reparse from there rather than going thru
	//	model.Value?
	//
	// Also where does structural Schema fit into this? seems like cel-policy-templates
	// handles openapi schemas itself? How does that fit within k8s? Does it matter?
	// What did k8s cel lib provide that we are missing if using cel-policy-templates directly
	source, celConversion, err := v1alpha2.PolicyTemplateToCELPolicyTemplate(template)
	if err != nil {
		// Processing the template again won't solve this error
		utilruntime.HandleError(err)
		return nil
	}

	lims := limits.NewLimits()
	env, err := cel.NewEnv()
	if err != nil {
		// Processing the template again won't solve this error
		utilruntime.HandleError(err)
		return nil
	}

	registry := model.NewRegistry(env)
	compiler := compiler.NewCompiler(registry, lims)
	compiledTemplate, issues := compiler.CompileTemplate(source, celConversion)

	if issues != nil {
		utilruntime.HandleError(issues.Err())
		return nil
	}

	// Make sure the engine is aware of this template
	err = c.policyEngine.SetTemplate(template.Name, compiledTemplate)
	if err != nil {
		// Processing the template again won't solve this error
		utilruntime.HandleError(err)
	}

	// Make sure we have an instance watcher for this CRD
	if _, exists := c.instances[template.Name]; !exists {
		instanceContext, instanceCancel := context.WithCancel(c.runningContext)
		info := instanceInfo{
			controller: NewInstanceController(),
			cancelFunc: instanceCancel,
		}
		c.instances[template.Name] = info
		go info.controller.Run(instanceContext)
	}

	return err
}

func (c *templateController) Validate(gvr metav1.GroupVersionResource, oldObj, obj interface{}) error {

	return nil
}
