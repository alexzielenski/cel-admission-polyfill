package v0alpha2

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"sync"
	"time"

	"github.com/google/cel-go/cel"
	"github.com/google/cel-go/common/types/ref"
	"github.com/google/cel-policy-templates-go/policy"
	"github.com/google/cel-policy-templates-go/policy/model"

	"github.com/alexzielenski/cel_polyfill/pkg/apis/celadmissionpolyfill.k8s.io/v0alpha2"
	"github.com/alexzielenski/cel_polyfill/pkg/controller"
	"github.com/alexzielenski/cel_polyfill/pkg/controller/structuralschema"
	"github.com/alexzielenski/cel_polyfill/pkg/validator"
	corev1 "k8s.io/api/core/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	structuralschemas "k8s.io/apiextensions-apiserver/pkg/apiserver/schema"
	kcel "k8s.io/apiextensions-apiserver/pkg/apiserver/schema/cel"
	celmodel "k8s.io/apiextensions-apiserver/pkg/apiserver/schema/cel/model"
	"k8s.io/apiserver/pkg/cel/library"
	"k8s.io/klog/v2"

	apiextensionsclientset "k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	runtimeschema "k8s.io/apimachinery/pkg/runtime/schema"
	patchtypes "k8s.io/apimachinery/pkg/types"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/dynamic/dynamicinformer"
	"k8s.io/client-go/tools/cache"
)

var _ validator.Interface = &templateController{}
var _ controller.Interface = &templateController{}

type PolicyTemplateController interface {
	controller.Interface
	validator.Interface
}

func NewPolicyTemplateController(
	dynamicClient dynamic.Interface,
	policyTemplatesInformer cache.SharedIndexInformer,
	structuralSchemaController structuralschema.Controller,
	crdClient apiextensionsclientset.Interface,
) PolicyTemplateController {
	// Resource type is determined at runtime rather than compile time
	var opts []cel.EnvOption
	opts = append(opts, cel.Variable("resource", cel.DynType))

	// cel.HomogeneousAggregateLiterals() makes `output.details.data` type
	// validation fail since sibling `output.message` key is a string (and details is a map)
	// opts = append(opts, cel.HomogeneousAggregateLiterals())
	opts = append(opts, library.ExtensionLibs...)

	env, err := cel.NewEnv(opts...)
	if err != nil {
		// why can this return a nerr
		panic(err)
	}

	engine, err := policy.NewEngine(policy.StandardExprEnv(env))
	if err != nil {
		// why can this return a nerr
		panic(err)
	}

	result := &templateController{
		structuralSchemaController: structuralSchemaController,
		crdClient:                  crdClient,
		dynamicClient:              dynamicClient,
		policyEngine:               engine,
		templates:                  make(map[string]templateInfo),
	}
	result.policyTemplatesController = controller.New(
		controller.NewInformer[*v0alpha2.PolicyTemplate](policyTemplatesInformer),
		result.reconcilePolicyTemplate,
		controller.ControllerOptions{},
	)
	return result
}

type templateController struct {
	policyTemplatesController  controller.Interface
	structuralSchemaController structuralschema.Controller
	crdClient                  apiextensionsclientset.Interface
	dynamicClient              dynamic.Interface

	lock sync.RWMutex

	// map of policy template name
	templates map[string]templateInfo

	policyEngine *policy.Engine

	runningContext context.Context
}

func (t *templateController) GetNumberInstances(templateName string) int {
	t.lock.RLock()
	defer t.lock.RUnlock()

	info, exists := t.templates[templateName]
	if !exists {
		return 0
	}
	return len(info.instances)
}

type templateInfo struct {
	template  *v0alpha2.PolicyTemplate
	instances map[string]instanceInfo

	// Stops this template watching for instances
	cancelFunc func()
}

type instanceInfo struct {
	raw      string
	compiled *model.Instance
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
	template *v0alpha2.PolicyTemplate,
) error {
	if template == nil {
		// Delete CRD with same name owned by this guy
		// k8s garbage collection via owner references might work

		//!TODO: Remove this template from the policy engine as well
		// as any of its instances
		panic("deletion not implemented")
	}

	// Regenerate the CRD
	// 1. Each policy template in turn owns a CRD
	crdGVR := metav1.GroupVersionResource{
		//!TODO: decide policy for which group to use
		//	example: OPA gatekeeper uses specialized constraints group for all
		//		constraints
		Group:    "policy.acme.co",
		Version:  "v1",
		Resource: template.Name,
	}

	// Sometimes these are empty???
	template.APIVersion = "celadmissionpolyfill.k8s.io/v0alpha2"
	template.Kind = "PolicyTemplate"

	crd := apiextensionsv1.CustomResourceDefinition{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "apiextensions.k8s.io/v1",
			Kind:       "CustomResourceDefinition",
		},
		ObjectMeta: metav1.ObjectMeta{
			//TODO: too naive. Need enforcement from apiserver of uniqueness
			// constraint that follows from CRD names through to policy template names
			Name: template.Name + "." + crdGVR.Group,
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
			Group: crdGVR.Group,
			Names: apiextensionsv1.CustomResourceDefinitionNames{
				//!TOOD: Validation that requires template name to be lowercase
				// required for uniqueness (since all names are lowercase)
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
					Name:                     crdGVR.Version,
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
								"rule":        template.Schema,
								"rules":       {Type: "array", Items: &apiextensionsv1.JSONSchemaPropsOrArray{Schema: &template.Schema}},
							},
						},
					},
				},
			},
		},
	}

	crdJSON, err := json.Marshal(crd)
	if err != nil {
		utilruntime.HandleError(err)
		return nil
	}

	//!TODO: dont use create if object exists. Unfortunately Patch can't
	// be used to create object if it doesnt already exist?
	_, err = c.crdClient.ApiextensionsV1().CustomResourceDefinitions().Patch(
		c.runningContext,
		crd.Name,
		patchtypes.ApplyPatchType,
		crdJSON,
		metav1.PatchOptions{
			FieldManager: "cel-polyfill-controller",
		},
	)

	// _, err = c.crdClient.
	// 	ApiextensionsV1().
	// 	CustomResourceDefinitions().Create(context.TODO(), &crd, metav1.CreateOptions{
	// 	FieldManager: "template-controller",
	// })

	if err != nil {
		utilruntime.HandleError(err)
		return nil
	}

	klog.Infof("created crd for policy: %s", crd.Name)

	// Compile template and throw it into the env
	source, _, err := v0alpha2.PolicyTemplateToCELPolicyTemplate(template)
	if err != nil {
		utilruntime.HandleError(err)
		return nil
	}
	compiledTemplate, issues := c.policyEngine.CompileTemplate(source)
	if issues != nil {
		utilruntime.HandleError(issues.Err())
		return nil
	}

	c.lock.Lock()
	defer c.lock.Unlock()

	err = c.policyEngine.SetTemplate(template.Name, compiledTemplate)
	if err != nil {
		utilruntime.HandleError(err)
		return nil
	}

	// Make sure we have an instance watcher for this CRD
	if _, exists := c.templates[template.Name]; !exists {
		instanceContext, instanceCancel := context.WithCancel(c.runningContext)
		c.templates[template.Name] = templateInfo{
			cancelFunc: instanceCancel,
			instances:  make(map[string]instanceInfo),
			template:   template,
		}

		// Watch for new instances of this policy
		informer := dynamicinformer.NewFilteredDynamicInformer(c.dynamicClient, runtimeschema.GroupVersionResource{
			Group:    crdGVR.Group,
			Version:  crdGVR.Version,
			Resource: crdGVR.Resource,
		}, corev1.NamespaceAll, 30*time.Second, cache.Indexers{cache.NamespaceIndex: cache.MetaNamespaceIndexFunc}, nil)

		controller := controller.New(
			controller.NewInformer[*unstructured.Unstructured](informer.Informer()),
			func(namepace, name string, newObj *unstructured.Unstructured) error {
				c.lock.Lock()
				defer c.lock.Unlock()

				if newObj == nil {
					// Instance was removed
					panic("deletion not implemented")
				} else {
					// Instance was added/updated
					if templateInfo, exists := c.templates[template.Name]; exists {
						yamled, err := json.MarshalIndent(newObj, "", "    ")
						if err != nil {
							// hmm what to do in this case?
							utilruntime.HandleError(err)
							return err
						}

						instanceSource := model.ByteSource(yamled, "")
						compiled, issues := c.policyEngine.CompileInstance(instanceSource)
						if issues != nil {
							utilruntime.HandleError(issues.Err())
							return nil
						}
						err = c.policyEngine.AddInstance(compiled)
						if err != nil {
							utilruntime.HandleError(err)
							return err
						}
						templateInfo.instances[name] = instanceInfo{
							compiled: nil,
							raw:      string(yamled),
						}
					}
				}

				println(name + " reconcile")

				return nil
			},
			controller.ControllerOptions{
				Name: fmt.Sprintf("%s.%s-instance-controller", template.GroupVersionKind().Version, template.Name),
			},
		)
		go informer.Informer().Run(instanceContext.Done())
		go controller.Run(instanceContext)
	}

	return err
}

type DecisionError struct {
	Decisions []model.DecisionValue
}

func ValToMap(val interface{}) interface{} {
	if val == nil {
		return nil
	}

	switch val := val.(type) {
	case ref.Val:
		nested := val.Value()
		if nested != val {
			return ValToMap(nested)
		}

		converted, err := val.ConvertToNative(reflect.TypeOf([]interface{}{}))
		if err == nil {
			return converted
		}

		converted, err = val.ConvertToNative(reflect.TypeOf(map[string]interface{}{}))
		if err == nil {
			return converted
		}

		converted, err = val.ConvertToNative(reflect.TypeOf([]interface{}{}).Elem())
		if err == nil {
			return converted
		}

		return val
	case map[ref.Val]ref.Val:
		converted := map[string]interface{}{}
		for k, v := range val {
			nativeKey := ValToMap(k)
			keyStr, ok := nativeKey.(string)
			if !ok {
				keyStr = fmt.Sprint(nativeKey)
			}
			converted[keyStr] = ValToMap(v)
		}
		return converted
	case []ref.Val:
		converted := []interface{}{}
		for _, v := range val {
			converted = append(converted, ValToMap(v))
		}
		return converted
	default:
		return val
	}
}

func (de DecisionError) ErrorJSON() []interface{} {
	vs := []interface{}{}
	for _, d := range de.Decisions {
		if l, ok := d.(*model.ListDecisionValue); ok {
			vals := l.Values()
			for _, v := range vals {
				vs = append(vs, ValToMap(v))
			}
		}
	}

	return vs
}

func (de DecisionError) Error() string {
	vs := de.ErrorJSON()
	js, err := json.MarshalIndent(vs, "", "    ")
	if err != nil {
		return fmt.Sprint(vs)
	}
	return string(js)
}

var _ error = DecisionError{}

func (c *templateController) Validate(gvr metav1.GroupVersionResource, oldObj, obj interface{}) validator.ValidationResult {
	obj, err := runtime.DefaultUnstructuredConverter.ToUnstructured(obj)
	if err != nil {
		utilruntime.HandleError(err)
		return validator.ValidationResult{Status: validator.ValidationInternalError, Error: err}
	}

	//!TODO use RWMutex features
	c.lock.RLock()
	defer c.lock.RUnlock()

	structural, err := c.structuralSchemaController.Get(gvr)
	if err != nil {
		utilruntime.HandleError(err)
		return validator.ValidationResult{Status: validator.ValidationOK, Error: err}
	}

	structural = celmodel.WithTypeAndObjectMeta(structural)

	// 	// WithTypeAndObjectMeta does not include labels
	// 	//!TODO: should upstream?
	structural.Properties["metadata"].Properties["labels"] =
		structuralschemas.Structural{
			Generic: structuralschemas.Generic{
				Type: "object",
				AdditionalProperties: &structuralschemas.StructuralOrBool{
					Structural: &structuralschemas.Structural{Generic: structuralschemas.Generic{Type: "string"}},
				},
			},
		}

	decisions, err := c.policyEngine.EvalAll(map[string]interface{}{
		"resource": kcel.UnstructuredToVal(obj, structural),
	})
	if err != nil {
		utilruntime.HandleError(err)
		return validator.ValidationResult{Status: validator.ValidationInternalError, Error: err}
	}

	if len(decisions) > 0 {
		err := DecisionError{Decisions: decisions}
		utilruntime.HandleError(err)
		return validator.ValidationResult{Status: validator.ValidationForbidden, Error: err}
	}

	return validator.ValidationResult{Status: validator.ValidationOK}
}
