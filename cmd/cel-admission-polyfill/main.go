package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"sync"
	"time"

	"github.com/alexzielenski/cel_polyfill/pkg/controller"
	"github.com/alexzielenski/cel_polyfill/pkg/generated/clientset/versioned"
	"github.com/alexzielenski/cel_polyfill/pkg/generated/clientset/versioned/scheme"
	"github.com/alexzielenski/cel_polyfill/pkg/generated/informers/externalversions"
	"github.com/alexzielenski/cel_polyfill/pkg/validator"
	"github.com/alexzielenski/cel_polyfill/pkg/webhook"
	apiextensionsclientset "k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset"
	apiextensionsclientsetscheme "k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset/scheme"
	apiextensionsinformers "k8s.io/apiextensions-apiserver/pkg/client/informers/externalversions"
	"k8s.io/client-go/kubernetes"
	clientsetscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/restmapper"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/klog/v2"
)

// Global K8s webhook which respects policy rules

var DEBUG = true

func main() {
	// Create an overarching context which is cancelled if there is ever an
	// OS interrupt (eg. Ctrl-C)
	mainContext, _ := signal.NotifyContext(context.Background(), os.Interrupt)
	restConfig, err := loadClientConfig()

	if err != nil {
		fmt.Printf("Failed to load Client Configuration: %v", err)
		return
	}

	// TBH not sure the full extent of what this does
	_ = scheme.AddToScheme(clientsetscheme.Scheme)
	_ = apiextensionsclientsetscheme.AddToScheme(clientsetscheme.Scheme)

	kubeClient, err := kubernetes.NewForConfig(restConfig)
	// customClient := versioned.New(kubeClient.Discovery().RESTClient())
	if err != nil {
		fmt.Printf("Failed to create kubernetes client: %v", err)
		return
	}

	customClient, err := versioned.NewForConfig(restConfig)
	if err != nil {
		klog.Errorf("Failed to create crd client: %v", err)
		return
	}

	apiextensionsClient, err := apiextensionsclientset.NewForConfig(restConfig)
	if err != nil {
		klog.Errorf("Failed to create apiextensions client: %v", err)
		return
	}

	// Create RESTMapper
	//!TODO: Get updates
	discoveryDocument, err := restmapper.GetAPIGroupResources(kubeClient.DiscoveryClient)
	if err != nil {
		klog.Error(err)
		return
	}
	mapper := restmapper.NewDiscoveryRESTMapper(discoveryDocument)

	// used to keep process alive until all workers are finished
	waitGroup := sync.WaitGroup{}
	serverContext, serverCancel := context.WithCancel(mainContext)

	// Start any informers
	// What is appropriate resync perriod?
	customFactory := externalversions.NewSharedInformerFactory(customClient, 30*time.Second)
	apiextensionsFactory := apiextensionsinformers.NewSharedInformerFactory(apiextensionsClient, 30*time.Second)
	validator := validator.New(apiextensionsFactory.Apiextensions().V1().CustomResourceDefinitions().Lister(), mapper)

	// Start HTTP REST server for webhook
	waitGroup.Add(1)
	go func() {
		webhook := webhook.New(locateCertificates(), validator)

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
		serverCancel()
		waitGroup.Done()
	}()

	waitGroup.Add(1)

	// call outside of goroutine so that informer is requested before we start
	// factory. (for some reason factory doesn't start informers requested
	// after it was already started?)
	admissionRulesController := controller.NewAdmissionRulesController(
		kubeClient,
		apiextensionsClient,
		customFactory.Celadmissionpolyfill().V1().ValidationRuleSets(),
		validator,
	)
	go func() {
		// Start k8s operator for digesting validation configuration

		if DEBUG {
			// Install CR Definiitons for our types
			klog.Info("updated CR definition")
			admissionRulesController.Install()
		}

		cancellationReason := admissionRulesController.Run(serverContext)
		klog.Infof("controller closure reason: %v", cancellationReason)

		serverCancel()
		waitGroup.Done()
	}()

	// Start after informers have been requested from factory
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
