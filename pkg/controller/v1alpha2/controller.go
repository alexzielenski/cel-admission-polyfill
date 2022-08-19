package v1alpha2

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/google/cel-go/cel"
	"github.com/google/cel-go/checker/decls"
	"github.com/google/cel-policy-templates-go/policy/compiler"
	"github.com/google/cel-policy-templates-go/policy/limits"
	"github.com/google/cel-policy-templates-go/policy/model"
	"github.com/google/cel-policy-templates-go/policy/parser"
	"github.com/google/cel-policy-templates-go/policy/runtime"
	expr "google.golang.org/genproto/googleapis/api/expr/v1alpha1"

	"github.com/alexzielenski/cel_polyfill/pkg/apis/celadmissionpolyfill.k8s.io/v1alpha2"
	"github.com/alexzielenski/cel_polyfill/pkg/controller"
	"github.com/alexzielenski/cel_polyfill/pkg/controller/structuralschema"
	"github.com/alexzielenski/cel_polyfill/pkg/validator"
	corev1 "k8s.io/api/core/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apiextensions-apiserver/pkg/apiserver/schema/cel/library"
	celmodel "k8s.io/apiextensions-apiserver/third_party/forked/celopenapi/model"

	apiextensionsclientset "k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/dynamic/dynamicinformer"
	"k8s.io/client-go/tools/cache"
)

var _ validator.Interface = &templateController{}

func NewPolicyTemplateController(
	dynamicClient dynamic.Interface,
	policyTemplatesInformer cache.SharedIndexInformer,
	structuralSchemaController structuralschema.Controller,
	crdClient apiextensionsclientset.Interface,
) controller.Interface {

	result := &templateController{
		structuralSchemaController: structuralSchemaController,
		crdClient:                  crdClient,
		dynamicClient:              dynamicClient,
		templates:                  make(map[string]templateInfo),
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
	dynamicClient              dynamic.Interface

	lock sync.RWMutex

	// map of policy template name
	templates map[string]templateInfo

	runningContext context.Context
}

type templateInfo struct {
	template *v1alpha2.PolicyTemplate

	// map from resource to compiled policy template for that resource
	//!TODO: whenever underlying CRD schema is changed this needs to be wiped
	//!TODO: whenever underlying policy changes this needs to be wiped
	compiledTemplates map[metav1.GroupVersionResource]*runtime.Template

	instances map[string]instanceInfo

	compiler *compiler.Compiler

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
					Name:                     template.GroupVersionKind().Version,
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

	//!TODO: dont use create if object exists. Unfortunately Patch can't
	// be used to create object if it doesnt already exist?
	_, err := c.crdClient.
		ApiextensionsV1().
		CustomResourceDefinitions().Create(context.TODO(), &crd, metav1.CreateOptions{
		FieldManager: "template-controller",
	})

	if err != nil {
		utilruntime.HandleError(err)
		return nil
	}

	c.lock.Lock()
	defer c.lock.Unlock()

	// Make sure we have an instance watcher for this CRD
	if _, exists := c.templates[template.Name]; !exists {
		instanceContext, instanceCancel := context.WithCancel(c.runningContext)
		c.templates[template.Name] = templateInfo{
			cancelFunc: instanceCancel,
		}

		// Watch for new instances of this policy
		instanceGVR := metav1.GroupVersionResource{
			Group:    template.GroupVersionKind().Group,
			Version:  template.GroupVersionKind().Version,
			Resource: template.Name,
		}
		informer := dynamicinformer.NewFilteredDynamicInformer(c.dynamicClient, schema.GroupVersionResource{
			Group:    instanceGVR.Group,
			Version:  instanceGVR.Version,
			Resource: instanceGVR.Resource,
		}, corev1.NamespaceAll, 30*time.Second, cache.Indexers{cache.NamespaceIndex: cache.MetaNamespaceIndexFunc}, nil)

		controller := controller.New(
			controller.NewInformer[*unstructured.Unstructured](informer.Informer()),
			func(namepace, name string, newObj *unstructured.Unstructured) error {
				c.lock.Lock()
				defer c.lock.Unlock()

				if newObj == nil {
					// Instance was removed
					if info, exists := c.templates[template.Name]; exists {
						delete(info.instances, name)
					}
				} else {
					// Instance was added/updated
					if templateInfo, exists := c.templates[template.Name]; exists {
						yamled, err := json.MarshalIndent(newObj, "", "    ")
						if err != nil {
							// hmm what to do in this case?
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
			controller.ControllerOptions{},
		)
		go informer.Informer().Run(instanceContext.Done())
		go controller.Run(instanceContext)
	}

	return err
}

func (c *templateController) Validate(gvr metav1.GroupVersionResource, oldObj, obj interface{}) error {
	//!TODO use RWMutex features
	c.lock.Lock()
	defer c.lock.Unlock()

	// Loop through all policies
	//	Compile for this resource
	// Loop through all instances of all policies
	//	Also compile for this resource? (dont think so?)
	// Evaluate in runtime
	for _, info := range c.templates {
		// Compiler gets wiped whenever CRD changes
		// Since "resource" is part of env which is used in compiler constructor
		//!TODO: Can this be changed so we can reused compiler with different envs?
		if info.compiler == nil {
			// Create type adaptor for the custom schema type

			lims := limits.NewLimits()
			env, err := cel.NewEnv(
				cel.HomogeneousAggregateLiterals(),
			)
			if err != nil {
				// Processing the template again won't solve this error
				utilruntime.HandleError(err)
				return nil
			}

			reg := celmodel.NewRegistry(env)

			// Add the target CRD definition to the env
			//!TODO: is there a way to do this so we can share the compiler for
			// all types, and only use the env while compiling specific templates
			// or instances?
			structural, err := c.structuralSchemaController.Get(gvr)
			if err != nil {
				utilruntime.HandleError(err)
				return nil
			}

			scopedTypeName := fmt.Sprintf("resourceType%d", time.Now().Nanosecond())
			rt, err := celmodel.NewRuleTypes(scopedTypeName, structural, true, reg)
			if err != nil {
				utilruntime.HandleError(err)
				return nil
			}

			opts, err := rt.EnvOptions(env.TypeProvider())
			if err != nil {
				utilruntime.HandleError(err)
				return nil
			}

			// wipe out the "rule" option
			opts = opts[:len(opts)-1]

			root, ok := rt.FindDeclType(scopedTypeName)
			if !ok {
				rootDecl := celmodel.SchemaDeclType(structural, true)
				if rootDecl == nil {
					return nil
				}

				root = rootDecl.MaybeAssignTypeName(scopedTypeName)
			}

			var propDecls []*expr.Decl
			propDecls = append(propDecls, decls.NewVar("resource", root.ExprType()))
			opts = append(opts, cel.Declarations(propDecls...), cel.HomogeneousAggregateLiterals())
			opts = append(opts, library.ExtensionLibs...)
			env, err = env.Extend(opts...)
			if err != nil {
				utilruntime.HandleError(err)
				continue
			}

			registry := model.NewRegistry(env)
			info.compiler = compiler.NewCompiler(registry, lims)
		}

		// Check if template is compiler for this GVR
		template, exists := info.compiledTemplates[gvr]
		if !exists {
			// Fetch structural schema for this GVR, turn it into a decl type or whatever
			// Add resource to it
			// vendor/k8s.io/apiextensions-apiserver/pkg/apiserver/schema/cel/compilation.go
			// for example
			// see env.Extend with Decl opts

			//!TODO: only lazily compile template if there is a matching instance?
			// Convert input template into a model.Value
			// TODO: where does structural Schema fit into this? seems like cel-policy-templates
			// handles openapi schemas itself? How does that fit within k8s? Does it matter?
			// What did k8s cel lib provide that we are missing if using cel-policy-templates directly
			//
			//
			// Below code is not working. When compiling required_labels template,
			// Undeclared reference to "resource" and "rule"
			// 	or "rule.labels" (part of rule schema)
			// 	as well as terms defined within template such as "want"
			source, celConversion, err := v1alpha2.PolicyTemplateToCELPolicyTemplate(info.template)
			if err != nil {
				// Processing the template again won't solve this error
				utilruntime.HandleError(err)
				return nil
			}

			modelTemplate, issues := info.compiler.CompileTemplate(source, celConversion)
			if issues != nil {
				utilruntime.HandleError(issues.Err())
				return nil
			}

			template, err = runtime.NewTemplate(nil, modelTemplate)
			if err != nil {
				utilruntime.HandleError(err)
				continue
			}

			info.compiledTemplates[gvr] = template
		}

		// Recompile any instances

		for _, instance := range info.instances {
			if instance.compiled == nil {
				source := model.StringSource(instance.raw, "")
				parsed, issues := parser.ParseYaml(source)
				if issues != nil {
					utilruntime.HandleError(issues.Err())
					continue
				}
				compiledInstance, issues := info.compiler.CompileInstance(source, parsed)
				if issues != nil {
					utilruntime.HandleError(issues.Err())
					continue
				}

				instance.compiled = compiledInstance
			}

			decisions, err := template.Eval(instance.compiled, nil, nil)
			if err != nil {
				utilruntime.HandleError(err)
				continue
			}

			println(decisions)
		}
	}

	return nil
}
