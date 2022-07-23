package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"sync"

	"github.com/alexzielenski/cel_polyfill/pkg/controller"
	"github.com/alexzielenski/cel_polyfill/pkg/webhook"

	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

// Global K8s webhook which respects policy rules

var DEBUG = true

func main() {
	// Create an overarching context which is cancelled if there is ever an
	// OS interrupt (eg. Ctrl-C)
	mainContext, _ := signal.NotifyContext(context.Background(), os.Interrupt)

	kubeclient, err := loadClientSet()
	if err != nil {
		fmt.Printf("Failed to load Kubernetes Client Configuration: %v", err)
		return
	}

	// used to keep process alive until all workers are finished
	waitGroup := sync.WaitGroup{}
	waitGroup.Add(2)

	serverContext, serverCancel := context.WithCancel(mainContext)
	controllerContext, controllerCancel := context.WithCancel(mainContext)

	// Start HTTP REST server for webhook
	go func() {
		webhook := webhook.New(locateCertificates())

		// Set up bindings with cluster automatically if debugging
		if DEBUG {
			// When debugging, expect server to be running already. Treat
			// as non-fatal error if it isn't.
			if err := webhook.Install(kubeclient); err != nil {
				fmt.Println(err)
			} else {
				// Install webhook configuration
				fmt.Println("updated webhook configuration")
			}
		}

		cancellationReason := webhook.Run(serverContext)
		fmt.Printf("server closure reason: %v", cancellationReason)

		controllerCancel()
		waitGroup.Done()
	}()

	go func() {
		// Start k8s operator for digesting validation configuration
		admissionRulesController := controller.NewAdmissionRulesController(kubeclient)
		if DEBUG {
			// Install CR Definiitons for our types
			fmt.Println("updated CR definition")
		}

		cancellationReason := admissionRulesController.Run(controllerContext)
		fmt.Printf("controller closure reason: %v", cancellationReason)

		serverCancel()
		waitGroup.Done()
	}()

	// Wait for controller and HTTP server to stop. They both signal to the other's
	// context that it is time to wrap up
	waitGroup.Wait()
}

func loadClientSet() (kubernetes.Interface, error) {
	// To be honest have no idea how to do this
	var config *rest.Config
	var err error

	// Use KubeConfig to find cluser if debugging, otherwise use the in cluser
	// configuration
	if DEBUG {
		// Connect to k8s
		loadingRules := clientcmd.NewDefaultClientConfigLoadingRules()
		// if you want to change the loading rules (which files in which order), you can do so here

		configOverrides := &clientcmd.ConfigOverrides{}
		// if you want to change override values or bind them to flags, there are methods to help you

		kubeConfig := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(loadingRules, configOverrides)

		config, err = kubeConfig.ClientConfig()
	} else {
		// untested. assuming this is how it might work when run from inside clsuter
		config, err = rest.InClusterConfig()
	}

	if err != nil {
		return nil, err
	}

	return kubernetes.NewForConfig(config)
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
