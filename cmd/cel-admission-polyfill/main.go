package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/signal"
	"reflect"
	"sync"
	"time"

	"github.com/alexzielenski/cel_polyfill"
	"github.com/alexzielenski/cel_polyfill/pkg/apis/admissionregistration.polyfill.sigs.k8s.io/v1alpha1"
	controllerv0alpha1 "github.com/alexzielenski/cel_polyfill/pkg/controller/celadmissionpolyfill.k8s.io/v0alpha1"
	controllerv0alpha2 "github.com/alexzielenski/cel_polyfill/pkg/controller/celadmissionpolyfill.k8s.io/v0alpha2"
	"github.com/alexzielenski/cel_polyfill/pkg/controller/schemaresolver"
	"github.com/alexzielenski/cel_polyfill/pkg/controller/structuralschema"
	"github.com/alexzielenski/cel_polyfill/pkg/generated/clientset/versioned"
	"github.com/alexzielenski/cel_polyfill/pkg/generated/clientset/versioned/scheme"
	admissionregistrationpolyfillclient "github.com/alexzielenski/cel_polyfill/pkg/generated/clientset/versioned/typed/admissionregistration.polyfill.sigs.k8s.io/v1alpha1"
	"github.com/alexzielenski/cel_polyfill/pkg/generated/informers/externalversions"
	"github.com/alexzielenski/cel_polyfill/pkg/generated/informers/externalversions/celadmissionpolyfill.k8s.io/v0alpha1"
	"github.com/alexzielenski/cel_polyfill/pkg/validator"
	"github.com/alexzielenski/cel_polyfill/pkg/webhook"
	apiextensionsclientset "k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset"
	apiextensionsclientsetscheme "k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset/scheme"
	apiextensionsinformers "k8s.io/apiextensions-apiserver/pkg/client/informers/externalversions"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/apimachinery/pkg/watch"
	admissionregistrationv1alpha1 "k8s.io/client-go/kubernetes/typed/admissionregistration/v1alpha1"
	aggregatorclientsetscheme "k8s.io/kube-aggregator/pkg/client/clientset_generated/clientset/scheme"

	admissionregistrationv1alpha1types "k8s.io/api/admissionregistration/v1alpha1"
	"k8s.io/apiserver/pkg/admission"
	"k8s.io/apiserver/pkg/admission/plugin/validatingadmissionpolicy"
	"k8s.io/apiserver/pkg/authorization/authorizer"
	"k8s.io/apiserver/pkg/cel/openapi/resolver"
	admissionregistrationv1alpha1apply "k8s.io/client-go/applyconfigurations/admissionregistration/v1alpha1"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	clientsetscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/restmapper"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/klog/v2"
)

type ClientInterface[T any, TList any] interface {
	Create(ctx context.Context, object *T, opts metav1.CreateOptions) (*T, error)
	Update(ctx context.Context, object *T, opts metav1.UpdateOptions) (*T, error)
	Delete(ctx context.Context, name string, opts metav1.DeleteOptions) error
	DeleteCollection(ctx context.Context, opts metav1.DeleteOptions, listOpts metav1.ListOptions) error
	Get(ctx context.Context, name string, opts metav1.GetOptions) (*T, error)
	List(ctx context.Context, opts metav1.ListOptions) (*TList, error)
	Watch(ctx context.Context, opts metav1.ListOptions) (watch.Interface, error)
	Patch(ctx context.Context, name string, pt types.PatchType, data []byte, opts metav1.PatchOptions, subresources ...string) (result *T, err error)
}

type ClientInterfaceWithApply[T any, TList any, TApplyConfiguration any] interface {
	ClientInterface[T, TList]
	Apply(ctx context.Context, object *TApplyConfiguration, opts metav1.ApplyOptions) (result *T, err error)
}

type ClientInterfaceWithStatus[T any, TList any] interface {
	ClientInterface[T, TList]
	UpdateStatus(ctx context.Context, object *T, opts metav1.UpdateOptions) (*T, error)
}

type ClientInterfaceWithStatusAndApply[T any, TList any, TApplyConfiguration any] interface {
	ClientInterfaceWithStatus[T, TList]
	ClientInterfaceWithApply[T, TList, TApplyConfiguration]
	ApplyStatus(ctx context.Context, object *TApplyConfiguration, opts metav1.ApplyOptions) (result *T, err error)
}

type TransformedClient[T any, TList any, TApplyConfiguration any, R any, RList any, RApplyConfiguration any] struct {
	TargetClient      ClientInterface[T, TList]
	ReplacementClient ClientInterface[R, RList]

	To   func(*R) (*T, error)
	From func(*T) (*R, error)
}

func (c TransformedClient[T, TList, TApplyConfiguration, R, RList, RApplyConfiguration]) Create(ctx context.Context, object *T, opts metav1.CreateOptions) (*T, error) {
	converted, err := c.From(object)
	if err != nil {
		return nil, err
	}

	replacementValue, err := c.ReplacementClient.Create(ctx, converted, opts)
	if err != nil {
		return nil, err
	}

	convertedResult, err := c.To(replacementValue)
	if err != nil {
		return nil, err
	}

	return convertedResult, nil
}

func (c TransformedClient[T, TList, TApplyConfiguration, R, RList, RApplyConfiguration]) Update(ctx context.Context, object *T, opts metav1.UpdateOptions) (*T, error) {
	converted, err := c.From(object)
	if err != nil {
		return nil, err
	}

	replacementValue, err := c.ReplacementClient.Update(ctx, converted, opts)
	if err != nil {
		return nil, err
	}

	convertedResult, err := c.To(replacementValue)
	if err != nil {
		return nil, err
	}

	return convertedResult, nil
}

func (c TransformedClient[T, TList, TApplyConfiguration, R, RList, RApplyConfiguration]) UpdateStatus(ctx context.Context, object *T, opts metav1.UpdateOptions) (*T, error) {
	converted, err := c.From(object)
	if err != nil {
		return nil, err
	}

	replacementValue, err := c.ReplacementClient.(ClientInterfaceWithStatus[R, RList]).UpdateStatus(ctx, converted, opts)
	if err != nil {
		return nil, err
	}

	convertedResult, err := c.To(replacementValue)
	if err != nil {
		return nil, err
	}

	return convertedResult, nil
}

func (c TransformedClient[T, TList, TApplyConfiguration, R, RList, RApplyConfiguration]) Delete(ctx context.Context, name string, opts metav1.DeleteOptions) error {
	return c.ReplacementClient.Delete(ctx, name, opts)
}

func (c TransformedClient[T, TList, TApplyConfiguration, R, RList, RApplyConfiguration]) DeleteCollection(ctx context.Context, opts metav1.DeleteOptions, listOpts metav1.ListOptions) error {
	return c.ReplacementClient.DeleteCollection(ctx, opts, listOpts)
}

func (c TransformedClient[T, TList, TApplyConfiguration, R, RList, RApplyConfiguration]) Get(ctx context.Context, name string, opts metav1.GetOptions) (*T, error) {
	replacementValue, err := c.ReplacementClient.Get(ctx, name, opts)
	if err != nil {
		return nil, err
	}

	convertedResult, err := c.To(replacementValue)
	if err != nil {
		return nil, err
	}

	return convertedResult, nil
}

func (c TransformedClient[T, TList, TApplyConfiguration, R, RList, RApplyConfiguration]) List(ctx context.Context, opts metav1.ListOptions) (*TList, error) {
	value, err := c.ReplacementClient.List(ctx, opts)
	if err != nil {
		return nil, err
	}

	items := getItems[R](value)

	newItems := make([]T, len(items))
	for i, v := range items {
		converted, err := c.To(&v)
		if err != nil {
			return nil, err
		}

		newItems[i] = *converted
	}

	return listWithItems[TList](newItems), nil
}

func (c TransformedClient[T, TList, TApplyConfiguration, R, RList, RApplyConfiguration]) Watch(ctx context.Context, opts metav1.ListOptions) (watch.Interface, error) {
	watcher, err := c.ReplacementClient.Watch(ctx, opts)
	if err != nil {
		return nil, err
	}

	return watch.Filter(watcher, func(in watch.Event) (out watch.Event, keep bool) {
		if asR, ok := in.Object.(any).(*R); ok {
			converted, err := c.To(asR)
			if err != nil {
				klog.Error(err)
				return in, false
			}
			var erasure any
			erasure = converted
			in.Object = erasure.(runtime.Object)
		} else {
			fmt.Println(in)
		}
		return in, true
	}), nil
}

// Ideally your replacement type is JSON compatible with your target type
// in case of validatingadmissionpolicy polyfill that is true.
// If we ever need this we can do something about it
func (c TransformedClient[T, TList, TApplyConfiguration, R, RList, RApplyConfiguration]) Patch(ctx context.Context, name string, pt types.PatchType, data []byte, opts metav1.PatchOptions, subresources ...string) (result *T, err error) {
	panic("transform patch unsupported")
}

func (c TransformedClient[T, TList, TApplyConfiguration, R, RList, RApplyConfiguration]) Apply(ctx context.Context, object *TApplyConfiguration, opts metav1.ApplyOptions) (result *T, err error) {
	panic("transform apply unsupported")
}

func (c TransformedClient[T, TList, TApplyConfiguration, R, RList, RApplyConfiguration]) ApplyStatus(ctx context.Context, object *TApplyConfiguration, opts metav1.ApplyOptions) (result *T, err error) {
	panic("transform applystatus unsupported")
}

// Given a list of []V and the type of a List type with Items field []V,
// create an instance of that List type, and set its Items to the given list
func listWithItems[TList any, V any](items []V) *TList {
	tZero := reflect.New(reflect.TypeOf((*TList)(nil)).Elem())
	itemsField := tZero.Elem().FieldByName("Items")
	itemsField.Set(reflect.ValueOf(items))
	return tZero.Interface().(*TList)
}

// Given a ListObj with Items []T
// Return the list of []T
func getItems[T any](listObj any) []T {
	// Rip items from list
	// Don't see a better way other than reflection
	rVal := reflect.ValueOf(listObj)
	itemsField := rVal.Elem().FieldByName("Items")
	if itemsField.IsNil() || itemsField.IsZero() {
		return nil
	}

	return itemsField.Interface().([]T)
}

// var x interface{}

// var _ ClientInterfaceWithStatus[admissionregistrationv1alpha1types.ValidatingAdmissionPolicy, admissionregistrationv1alpha1types.ValidatingAdmissionPolicyList] = (&admissionregistrationv1alpha1.AdmissionregistrationV1alpha1Client{}).ValidatingAdmissionPolicies()

// var _ admissionregistrationv1alpha1.ValidatingAdmissionPolicyInterface = (x.(interface{})).(ClientInterfaceWithStatusAndApply[admissionregistrationv1alpha1types.ValidatingAdmissionPolicy, admissionregistrationv1alpha1types.ValidatingAdmissionPolicyList, admissionregistrationv1alpha1apply.ValidatingAdmissionPolicyApplyConfiguration])

// type TransformClient[T runtime.Object, TList runtime.Object, ]

type replacedClient struct {
	admissionregistrationv1alpha1.AdmissionregistrationV1alpha1Interface
	replacement admissionregistrationpolyfillclient.AdmissionregistrationV1alpha1Interface
}

func (r replacedClient) ValidatingAdmissionPolicies() admissionregistrationv1alpha1.ValidatingAdmissionPolicyInterface {
	return TransformedClient[
		admissionregistrationv1alpha1types.ValidatingAdmissionPolicy, admissionregistrationv1alpha1types.ValidatingAdmissionPolicyList, admissionregistrationv1alpha1apply.ValidatingAdmissionPolicyApplyConfiguration,
		v1alpha1.ValidatingAdmissionPolicy, v1alpha1.ValidatingAdmissionPolicyList, any]{
		TargetClient:      r.AdmissionregistrationV1alpha1Interface.ValidatingAdmissionPolicies(),
		ReplacementClient: r.replacement.ValidatingAdmissionPolicies(),
		To:                CRDToNativePolicy,
		From:              NativeToCRDPolicy,
	}
}

func (r replacedClient) ValidatingAdmissionPolicyBindings() admissionregistrationv1alpha1.ValidatingAdmissionPolicyBindingInterface {
	return TransformedClient[
		admissionregistrationv1alpha1types.ValidatingAdmissionPolicyBinding, admissionregistrationv1alpha1types.ValidatingAdmissionPolicyBindingList, admissionregistrationv1alpha1apply.ValidatingAdmissionPolicyBindingApplyConfiguration,
		v1alpha1.ValidatingAdmissionPolicyBinding, v1alpha1.ValidatingAdmissionPolicyBindingList, any]{
		TargetClient:      r.AdmissionregistrationV1alpha1Interface.ValidatingAdmissionPolicyBindings(),
		ReplacementClient: r.replacement.ValidatingAdmissionPolicyBindings(),
		To:                CRDToNativePolicyBinding,
		From:              NativeToCRDPolicyBinding,
	}
}

type wrappedClient struct {
	kubernetes.Interface
	replacement admissionregistrationpolyfillclient.AdmissionregistrationV1alpha1Interface
}

func (w wrappedClient) AdmissionregistrationV1alpha1() admissionregistrationv1alpha1.AdmissionregistrationV1alpha1Interface {
	return replacedClient{
		replacement:                            w.replacement,
		AdmissionregistrationV1alpha1Interface: w.Interface.AdmissionregistrationV1alpha1(),
	}
}

func NativeToCRDPolicy(vap *admissionregistrationv1alpha1types.ValidatingAdmissionPolicy) (*v1alpha1.ValidatingAdmissionPolicy, error) {
	if vap == nil {
		return nil, nil
	}

	// I'm very lazy so let's just do JSON conversion for now :)
	toJson, err := json.Marshal(vap)
	if err != nil {
		return nil, err
	}
	var res v1alpha1.ValidatingAdmissionPolicy
	err = json.Unmarshal(toJson, &res)
	if len(res.APIVersion) > 0 {
		res.APIVersion = "admissionregistration.polyfill.sigs.k8s.io/v1alpha1"
	}
	return &res, err
}

func CRDToNativePolicy(vap *v1alpha1.ValidatingAdmissionPolicy) (*admissionregistrationv1alpha1types.ValidatingAdmissionPolicy, error) {
	if vap == nil {
		return nil, nil
	}

	// I'm very lazy so let's just do JSON conversion for now :)
	toJson, err := json.Marshal(vap)
	if err != nil {
		return nil, err
	}
	var res admissionregistrationv1alpha1types.ValidatingAdmissionPolicy
	err = json.Unmarshal(toJson, &res)
	if len(res.APIVersion) > 0 {
		res.APIVersion = "apiregistration.k8s.io/v1alpha1"
	}
	return &res, err
}

func NativeToCRDPolicyBinding(vap *admissionregistrationv1alpha1types.ValidatingAdmissionPolicyBinding) (*v1alpha1.ValidatingAdmissionPolicyBinding, error) {
	if vap == nil {
		return nil, nil
	}

	// I'm very lazy so let's just do JSON conversion for now :)
	toJson, err := json.Marshal(vap)
	if err != nil {
		return nil, err
	}
	var res v1alpha1.ValidatingAdmissionPolicyBinding
	err = json.Unmarshal(toJson, &res)
	if len(res.APIVersion) > 0 {
		res.APIVersion = "admissionregistration.polyfill.sigs.k8s.io/v1alpha1"
	}
	return &res, err
}

func CRDToNativePolicyBinding(vap *v1alpha1.ValidatingAdmissionPolicyBinding) (*admissionregistrationv1alpha1types.ValidatingAdmissionPolicyBinding, error) {
	if vap == nil {
		return nil, nil
	}

	// I'm very lazy so let's just do JSON conversion for now :)
	toJson, err := json.Marshal(vap)
	if err != nil {
		return nil, err
	}
	var res admissionregistrationv1alpha1types.ValidatingAdmissionPolicyBinding
	err = json.Unmarshal(toJson, &res)
	if len(res.APIVersion) > 0 {
		res.APIVersion = "apiregistration.k8s.io/v1alpha1"
	}
	return &res, err
}

// Global K8s webhook which respects policy rules

var DEBUG = true

func main() {
	klog.EnableContextualLogging(true)

	// Create an overarching context which is cancelled if there is ever an
	// OS interrupt (eg. Ctrl-C)
	mainContext, _ := signal.NotifyContext(context.Background(), os.Interrupt)
	restConfig, err := loadClientConfig()

	if err != nil {
		fmt.Printf("Failed to load Client Configuration: %v", err)
		return
	}

	// Make the kubernetes clientset scheme aware of all kubernetes types
	// and our custom CRD types
	scheme.AddToScheme(clientsetscheme.Scheme)
	apiextensionsclientsetscheme.AddToScheme(clientsetscheme.Scheme)
	aggregatorclientsetscheme.AddToScheme(clientsetscheme.Scheme)

	customClient, err := versioned.NewForConfig(restConfig)
	if err != nil {
		klog.Errorf("Failed to create crd client: %v", err)
		return
	}

	unwrappedKubeClient, err := kubernetes.NewForConfig(restConfig)
	// customClient := versioned.New(kubeClient.Discovery().RESTClient())
	if err != nil {
		fmt.Printf("Failed to create kubernetes client: %v", err)
		return
	}

	// Override the typed validating admission policy client in the kubeClient
	kubeClient := wrappedClient{
		Interface:   unwrappedKubeClient,
		replacement: customClient.AdmissionregistrationV1alpha1(),
	}

	dynamicClient, err := dynamic.NewForConfig(restConfig)
	if err != nil {
		klog.Errorf("Failed to create dynamic client: %v", err)
		return
	}

	apiextensionsClient, err := apiextensionsclientset.NewForConfig(restConfig)
	if err != nil {
		klog.Errorf("Failed to create apiextensions client: %v", err)
		return
	}

	cel_polyfill.DEBUG_InstallCRDs(mainContext, dynamicClient)

	// used to keep process alive until all workers are finished
	waitGroup := sync.WaitGroup{}
	serverContext, serverCancel := context.WithCancel(mainContext)

	// Start any informers
	// What is appropriate resync perriod?
	factory := informers.NewSharedInformerFactory(kubeClient, 30*time.Second)
	customFactory := externalversions.NewSharedInformerFactory(customClient, 30*time.Second)
	apiextensionsFactory := apiextensionsinformers.NewSharedInformerFactory(apiextensionsClient, 30*time.Second)

	restmapper := meta.NewLazyRESTMapperLoader(func() (meta.RESTMapper, error) {
		groupResources, err := restmapper.GetAPIGroupResources(kubeClient.Discovery())
		if err != nil {
			return nil, err
		}
		return restmapper.NewDiscoveryRESTMapper(groupResources), nil
	}).(meta.ResettableRESTMapper)

	go wait.PollUntil(1*time.Minute, func() (done bool, err error) {
		// Refresh restmapper every minute
		restmapper.Reset()
		return false, nil
	}, serverContext.Done())

	// structuralschemaController := structuralschema.NewController(
	// 	apiextensionsFactory.Apiextensions().V1().CustomResourceDefinitions().Informer(),
	// )

	// should by called by each worker when it has finished cleaning up
	// main function blocks on each worker being done
	cleanupWorker := func() {
		// Inform all other workers they should begin clean up if they have not
		serverCancel()

		// Decrement wait counter for main thread
		waitGroup.Done()
	}

	waitGroup.Add(1)
	validators := []admission.ValidationInterface{
		// StartV0Alpha1(serverContext, cleanupWorker, structuralschemaController, customFactory.Celadmissionpolyfill().V0alpha1().ValidationRuleSets()),
		// StartV0Alpha2(serverContext, cleanupWorker, dynamicClient, apiextensionsClient, structuralschemaController, customFactory.Celadmissionpolyfill().V0alpha2().PolicyTemplates().Informer()),
		StartV1Alpha1(serverContext, cleanupWorker, factory, kubeClient, restmapper, schemaresolver.New(apiextensionsFactory.Apiextensions().V1().CustomResourceDefinitions(), kubeClient.Discovery()), dynamicClient, nil),
	}

	// Start HTTP REST server for webhook
	waitGroup.Add(1)
	go func() {
		webhook := webhook.New(locateCertificates(), clientsetscheme.Scheme, validator.NewMulti(validators...))

		// Set up bindings with cluster automatically if debugging
		if DEBUG {
			// When debugging, expect server to be running already. Treat
			// as non-fatal error if it isn't.
			if err := webhook.Install(kubeClient); err != nil {
				klog.Error(err)
			} else {
				// Install webhook configuration
				klog.Info("updated webhook configuration")
			}
		}

		cancellationReason := webhook.Run(serverContext)
		klog.Infof("server closure reason: %v", cancellationReason)

		// Cancel the server context to stop other workers
		cleanupWorker()
	}()

	// Start after informers have been requested from factory
	factory.Start(serverContext.Done())
	apiextensionsFactory.Start(serverContext.Done())
	customFactory.Start(serverContext.Done())

	// Wait for controller and HTTP server to stop. They both signal to the other's
	// context that it is time to wrap up
	waitGroup.Wait()
}

func loadClientConfig() (*rest.Config, error) {
	// Use KubeConfig to find cluser if debugging, otherwise use the in cluser
	// configuration
	if DEBUG {
		// Connect to k8s
		loadingRules := clientcmd.NewDefaultClientConfigLoadingRules()
		// if you want to change the loading rules (which files in which order), you can do so here

		configOverrides := &clientcmd.ConfigOverrides{}
		// if you want to change override values or bind them to flags, there are methods to help you

		kubeConfig := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(loadingRules, configOverrides)

		return kubeConfig.ClientConfig()
	}

	// untested. assuming this is how it might work when run from inside clsuter
	return rest.InClusterConfig()
}

func locateCertificates() webhook.CertInfo {
	if DEBUG {
		// Check temp path and regenerate if not exist
		return webhook.CertInfo{
			CertFile: "/Users/alex/go/src/github.com/alexzielenski/cel_polyfill/certs/leaf.pem",
			KeyFile:  "/Users/alex/go/src/github.com/alexzielenski/cel_polyfill/certs/leaf.key",
			RootFile: "/Users/alex/go/src/github.com/alexzielenski/cel_polyfill/certs/root.pem",
		}
	} else {
		// Check hardcoded location/envars which are mounted via Secret
	}

	return webhook.CertInfo{}
}

func StartV0Alpha1(
	ctx context.Context,
	cancelFunc func(),
	structuralschemaController structuralschema.Controller,
	informer v0alpha1.ValidationRuleSetInformer,
) admission.ValidationInterface {

	if DEBUG {
		// Install latest CRD definitions
	}

	validator := controllerv0alpha1.NewValidator(structuralschemaController)

	// call outside of goroutine so that informer is requested before we start
	// factory. (for some reason factory doesn't start informers requested
	// after it was already started?)
	admissionRulesController := controllerv0alpha1.NewAdmissionRulesController(
		informer,
		validator,
	)

	go func() {
		// Start k8s operator for digesting validation configuration
		cancellationReason := admissionRulesController.Run(ctx)
		klog.Infof("controller closure reason: %v", cancellationReason)

		cancelFunc()
	}()

	return validator
}

func StartV0Alpha2(
	ctx context.Context,
	cancelFunc func(),
	dynamicClient dynamic.Interface,
	apiextensionsClient apiextensionsclientset.Interface,
	structuralschemaController structuralschema.Controller,
	policyTemplatesInformer cache.SharedIndexInformer,
) admission.ValidationInterface {
	if DEBUG {
		// Install latest CRD definitions
	}

	controller := controllerv0alpha2.NewPolicyTemplateController(
		dynamicClient,
		policyTemplatesInformer,
		structuralschemaController,
		apiextensionsClient,
	)

	go func() {
		// Start k8s operator for digesting validation configuration
		cancellationReason := controller.Run(ctx)
		klog.Infof("controller closure reason: %v", cancellationReason)

		cancelFunc()
	}()

	return controller
}

// !TODO: just use the already available plugin
type celAdmissionPlugin struct {
	evaluator validatingadmissionpolicy.CELPolicyEvaluator
}

func (c *celAdmissionPlugin) Handles(operation admission.Operation) bool {
	return true
}

func (c *celAdmissionPlugin) Validate(
	ctx context.Context,
	a admission.Attributes,
	o admission.ObjectInterfaces,
) (err error) {
	// isPolicyResource determines if an admission.Attributes object is describing
	// the admission of a ValidatingAdmissionPolicy or a ValidatingAdmissionPolicyBinding
	if isPolicyResource(a) {
		return
	}

	if !c.evaluator.HasSynced() {
		return admission.NewForbidden(a, fmt.Errorf("not yet ready to handle request"))
	}

	return c.evaluator.Validate(ctx, a, o)
}

func isPolicyResource(attr admission.Attributes) bool {
	gvk := attr.GetResource()
	if gvk.Group == "admissionregistration.k8s.io" || gvk.Group == "admissionregistration.polyfill.sigs.k8s.io" {
		if gvk.Resource == "validatingadmissionpolicies" || gvk.Resource == "validatingadmissionpolicybindings" {
			return true
		}
	}
	return false
}

func StartV1Alpha1(
	ctx context.Context,
	cancelFunc func(),
	factory informers.SharedInformerFactory,
	client kubernetes.Interface,
	restMapper meta.RESTMapper,
	schemaResolver resolver.SchemaResolver,
	dynamicClient dynamic.Interface,
	authorizer authorizer.Authorizer,
) admission.ValidationInterface {
	controller := validatingadmissionpolicy.NewAdmissionController(
		factory, client, restMapper, schemaResolver, dynamicClient, authorizer,
	)

	go func() {
		defer cancelFunc()
		controller.Run(ctx.Done())
	}()

	return &celAdmissionPlugin{controller}
}
