package controller

import (
	"context"
	"fmt"

	"k8s.io/client-go/kubernetes"
)

type Controller interface {
	// Meant to be run inside a goroutine
	// Waits for and reacts to changes in whatever type the controller
	// is concerned with.
	//
	// Returns an error always non-nil explaining why the worker stopped
	Run(ctx context.Context) error

	// Installs latest custom resource definitions used by this controller
	Install() error
}

func NewAdmissionRulesController(client kubernetes.Interface) Controller {
	return &admissionRulesController{}
}

type admissionRulesController struct {
	client kubernetes.Interface
}

func (c *admissionRulesController) Install() error {
	return nil
}

func (c *admissionRulesController) Run(ctx context.Context) error {
	fmt.Println("starting admission rules controller")
	defer fmt.Println("stopping admission rules controller")

	// Start informer for admission rules
	// c.client.

	<-ctx.Done()
	return ctx.Err()
}
