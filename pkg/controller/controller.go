package controller

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/alexzielenski/cel_polyfill/pkg/client/clientset/versioned"
	informers "github.com/alexzielenski/cel_polyfill/pkg/client/informers/externalversions/celadmissionpolyfill.k8s.io/v1"
	apiextensions "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	apiextensionsclient "k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset"
	kerrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/util/workqueue"
	"k8s.io/klog/v2"
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

func NewAdmissionRulesController(client kubernetes.Interface, ruleSetsInformer informers.ValidationRuleSetInformer) Controller {
	name := "admissionRulesController"
	result := &admissionRulesController{
		name:             name,
		client:           client,
		customClient:     versioned.New(client.Discovery().RESTClient()),
		queue:            workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(), name),
		ruleSetsInformer: ruleSetsInformer,
	}

	return result
}

type admissionRulesController struct {
	name         string
	client       kubernetes.Interface
	customClient versioned.Interface
	queue        workqueue.RateLimitingInterface

	ruleSetsInformer informers.ValidationRuleSetInformer
}

func (c *admissionRulesController) Install() error {
	//!TODO: install CRDs for our thingy
	client := apiextensionsclient.New(c.client.Discovery().RESTClient())
	client.ApiextensionsV1().CustomResourceDefinitions().Update(context.Background(), &apiextensions.CustomResourceDefinition{}, metav1.UpdateOptions{})
	return nil
}

func (c *admissionRulesController) Run(ctx context.Context) error {
	fmt.Println("starting admission rules controller")
	defer fmt.Println("stopping admission rules controller")

	// Start informer for admission rules
	// c.client.

	enqueue := func(obj interface{}) {
		var key string
		var err error
		if key, err = cache.MetaNamespaceKeyFunc(obj); err != nil {
			utilruntime.HandleError(err)
			return
		}
		c.queue.Add(key)
	}

	//TODO: Remove this event handler when we are finished with the informer
	// support removal of event handlers from SharedIndexInformers
	// PR: https://github.com/kubernetes/kubernetes/pull/111122
	c.ruleSetsInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {
			enqueue(obj)
		},
		UpdateFunc: func(oldObj, newObj interface{}) {
			oldMeta, err1 := meta.Accessor(oldObj)
			newMeta, err2 := meta.Accessor(newObj)

			if err1 != nil || err2 != nil {
				if err1 != nil {
					utilruntime.HandleError(err1)
				}

				if err2 != nil {
					utilruntime.HandleError(err2)
				}
				return
			} else if oldMeta.GetResourceVersion() == newMeta.GetResourceVersion() {
				return
			}

			enqueue(newObj)
		},
		DeleteFunc: func(obj interface{}) {
			// Enqueue
			enqueue(obj)
		},
	})

	//!TODO: check if informer is even valid?
	// e.g. if crd isnt even installed yet this just waits forever here
	if !cache.WaitForNamedCacheSync(c.name, ctx.Done(), c.ruleSetsInformer.Informer().HasSynced) {
		// ctx cancelled during cache sync. return early
		err := ctx.Err()
		if err == nil {
			// if context wasnt cancelled then the sync failed for another reason
			err = errors.New("cache sync failed")
		}
		return err
	}

	workers := 2
	waitGroup := sync.WaitGroup{}
	waitGroup.Add(workers)
	for i := 0; i < workers; i++ {
		go func() {
			wait.Until(c.runWorker, time.Second, ctx.Done())
			waitGroup.Done()
		}()
	}

	// Wait for context cancel.
	<-ctx.Done()

	// Gracefully shutdown workqueue. Finish processing any enqueued items.
	//!TODO: May want to put deadline on this and forcefully shutdown?
	c.queue.ShutDownWithDrain()

	// Workqueue shutdown signals for workers to stop. Wait for all workers to
	// clean up
	waitGroup.Wait()

	// Only way for workers to ever stop is for caller to cancel the context
	return ctx.Err()
}

func (c *admissionRulesController) runWorker() {
	for {
		obj, shutdown := c.queue.Get()
		if shutdown {
			return
		}

		// We wrap this block in a func so we can defer c.workqueue.Done.
		err := func(obj interface{}) error {
			// We call Done here so the workqueue knows we have finished
			// processing this item. We also must remember to call Forget if we
			// do not want this work item being re-queued. For example, we do
			// not call Forget if a transient error occurs, instead the item is
			// put back on the workqueue and attempted again after a back-off
			// period.
			defer c.queue.Done(obj)
			var key string
			var ok bool
			// We expect strings to come off the workqueue. These are of the
			// form namespace/name. We do this as the delayed nature of the
			// workqueue means the items in the informer cache may actually be
			// more up to date that when the item was initially put onto the
			// workqueue.
			if key, ok = obj.(string); !ok {
				// As the item in the workqueue is actually invalid, we call
				// Forget here else we'd go into a loop of attempting to
				// process a work item that is invalid.
				c.queue.Forget(obj)
				return fmt.Errorf("expected string in workqueue but got %#v", obj)
			}

			if err := c.reconcile(key); err != nil {
				// Put the item back on the workqueue to handle any transient errors.
				c.queue.AddRateLimited(key)
				return fmt.Errorf("error syncing '%s': %s, requeuing", key, err.Error())
			}
			// Finally, if no error occurs we Forget this item so it does not
			// get queued again until another change happens.
			c.queue.Forget(obj)
			klog.Infof("Successfully synced '%s'", key)
			return nil
		}(obj)

		if err != nil {
			utilruntime.HandleError(err)
		}
	}
}

func (c *admissionRulesController) reconcile(key string) error {
	// Convert the namespace/name string into a distinct namespace and name
	namespace, name, err := cache.SplitMetaNamespaceKey(key)
	if err != nil {
		utilruntime.HandleError(fmt.Errorf("invalid resource key: %s", key))
		return nil
	}

	ruleSet, err := c.ruleSetsInformer.Lister().ValidationRuleSets(namespace).Get(name)
	if err != nil {
		// The ruleSet resource may no longer exist, in which case we stop
		// processing.
		if kerrors.IsNotFound(err) {
			utilruntime.HandleError(fmt.Errorf("foo '%s' in work queue no longer exists", key))
			return nil
		}

		return err
	}

	// Commit the ruleSet to our local data structure for enforcing validation
	// rules
	_ = ruleSet
	return nil
}
