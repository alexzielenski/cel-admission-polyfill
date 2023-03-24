package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"sync"
	"time"

	"github.com/alexzielenski/cel_polyfill"
	"github.com/alexzielenski/cel_polyfill/pkg/controller/admissionregistration.polyfill.sigs.k8s.io/v1alpha1"
	controllerv0alpha1 "github.com/alexzielenski/cel_polyfill/pkg/controller/celadmissionpolyfill.k8s.io/v0alpha1"
	controllerv0alpha2 "github.com/alexzielenski/cel_polyfill/pkg/controller/celadmissionpolyfill.k8s.io/v0alpha2"
	"github.com/alexzielenski/cel_polyfill/pkg/controller/schemaresolver"
	"github.com/alexzielenski/cel_polyfill/pkg/controller/structuralschema"
	"github.com/alexzielenski/cel_polyfill/pkg/generated/clientset/versioned"
	"github.com/alexzielenski/cel_polyfill/pkg/generated/clientset/versioned/scheme"
	"github.com/alexzielenski/cel_polyfill/pkg/generated/informers/externalversions"
	"github.com/alexzielenski/cel_polyfill/pkg/generated/informers/externalversions/celadmissionpolyfill.k8s.io/v0alpha1"
	"github.com/alexzielenski/cel_polyfill/pkg/validator"
	"github.com/alexzielenski/cel_polyfill/pkg/webhook"
	apiextensionsclientset "k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset"
	apiextensionsclientsetscheme "k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset/scheme"
	apiextensionsinformers "k8s.io/apiextensions-apiserver/pkg/client/informers/externalversions"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/util/wait"
	aggregatorclientsetscheme "k8s.io/kube-aggregator/pkg/client/clientset_generated/clientset/scheme"

	"k8s.io/apiserver/pkg/admission"
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
	kubeClient := v1alpha1.NewWrappedClient(unwrappedKubeClient, customClient)

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

	cel_polyfill.DEBUG_InstallCRDs(mainContext, apiextensionsClient)

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

	type runnable interface {
		Run(context.Context) error
	}

	validators := []admission.ValidationInterface{
		// StartV0Alpha1(serverContext, cleanupWorker, structuralschemaController, customFactory.Celadmissionpolyfill().V0alpha1().ValidationRuleSets()),
		// StartV0Alpha2(serverContext, cleanupWorker, dynamicClient, apiextensionsClient, structuralschemaController, customFactory.Celadmissionpolyfill().V0alpha2().PolicyTemplates().Informer()),
		v1alpha1.NewPlugin(factory, kubeClient, restmapper, schemaresolver.New(apiextensionsFactory.Apiextensions().V1().CustomResourceDefinitions(), kubeClient.Discovery()), dynamicClient, nil),
	}

	for _, v := range validators {
		if r, ok := v.(runnable); ok {
			waitGroup.Add(1)
			go func() {
				err := r.Run(serverContext)
				if err != nil {
					klog.Errorf("worker stopped due to error: %v", err)
				}
				serverCancel()
				waitGroup.Done()
			}()
		}
	}

	webhook := webhook.New(9091, locateCertificates(), clientsetscheme.Scheme, validator.NewMulti(validators...))

	// Start HTTP REST server for webhook
	waitGroup.Add(1)
	go func() {
		defer func() {
			// Cancel the server context to stop other workers
			serverCancel()
			waitGroup.Done()
		}()

		cancellationReason := webhook.Run(serverContext)
		klog.Infof("webhook server closure reason: %v", cancellationReason)
	}()

	// Set up bindings with cluster automatically if debugging
	if DEBUG {
		err = wait.Poll(250*time.Millisecond, 1*time.Second, func() (done bool, err error) {
			// When debugging, expect server to be running already. Treat
			// as non-fatal error if it isn't.
			// Install webhook configuration
			if err := webhook.Install(kubeClient); err != nil {
				klog.Errorf("failed to install webhook: %v", err.Error())
				return false, nil
			}

			klog.Info("successfully updated webhook configuration")
			return true, nil
		})
		if err != nil {
			serverCancel()
		}
	}

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
		// return webhook.CertInfo{
		// 	CertFile: "/Users/alex/go/src/github.com/alexzielenski/cel_polyfill/certs/leaf.pem",
		// 	KeyFile:  "/Users/alex/go/src/github.com/alexzielenski/cel_polyfill/certs/leaf.key",
		// 	RootFile: "/Users/alex/go/src/github.com/alexzielenski/cel_polyfill/certs/root.pem",
		// }
		res, err := webhook.GenerateLocalCertificates()
		if err != nil {
			panic(err)
		}
		return res
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
