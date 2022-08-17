package controller

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	kerrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"

	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/util/workqueue"
	"k8s.io/klog/v2"
)

type Controller[T runtime.Object] interface {
	// Meant to be run inside a goroutine
	// Waits for and reacts to changes in whatever type the controller
	// is concerned with.
	//
	// Returns an error always non-nil explaining why the worker stopped
	Run(ctx context.Context) error
}

type Informer[T runtime.Object] interface {
	Informer() cache.SharedIndexInformer
	Lister() Lister[T]
}

// TLister helps list Ts.
// All objects returned here must be treated as read-only.
type Lister[T runtime.Object] interface {
	// List lists all ValidationRuleSets in the indexer for a given namespace.
	// Objects returned here must be treated as read-only.
	List(namespace string, selector labels.Selector) (ret []*T, err error)

	// Get retrieves the ValidationRuleSet from the indexer for a given namespace and name.
	// Objects returned here must be treated as read-only.
	Get(namespace string, name string) (*T, error)
}

type NamespacedLister[T runtime.Object] interface {
	// List lists all ValidationRuleSets in the indexer for a given namespace.
	// Objects returned here must be treated as read-only.
	List(selector labels.Selector) (ret []*T, err error)
	// Get retrieves the ValidationRuleSet from the indexer for a given namespace and name.
	// Objects returned here must be treated as read-only.
	Get(name string) (*T, error)
}

type controller[T runtime.Object] struct {
	lister     Lister[T]
	informer   cache.SharedIndexInformer
	queue      workqueue.RateLimitingInterface
	reconciler func(newObj *T) error

	options ControllerOptions
}

type ControllerOptions struct {
	Name    string
	Workers uint
}

func NewController[T runtime.Object](
	informer Informer[T],
	reconciler func(newObj *T) error,
	options ControllerOptions,
) Controller[T] {
	if options.Workers == 0 {
		options.Workers = 2
	}

	if len(options.Name) == 0 {
		options.Name = fmt.Sprintf("%T-controller", *new(*T))
	}

	return &controller[T]{
		options:    options,
		lister:     informer.Lister(),
		informer:   informer.Informer(),
		reconciler: reconciler,
	}
}

func (c *controller[T]) Run(ctx context.Context) error {
	klog.Info("starting admission rules controller")
	defer klog.Info("stopping admission rules controller")

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
	c.informer.AddEventHandler(cache.ResourceEventHandlerFuncs{
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
	if !cache.WaitForNamedCacheSync(c.options.Name, ctx.Done(), c.informer.HasSynced) {
		// ctx cancelled during cache sync. return early
		err := ctx.Err()
		if err == nil {
			// if context wasnt cancelled then the sync failed for another reason
			err = errors.New("cache sync failed")
		}
		return err
	}

	waitGroup := sync.WaitGroup{}

	for i := uint(0); i < c.options.Workers; i++ {
		waitGroup.Add(1)
		go func() {
			wait.Until(c.runWorker, time.Second, ctx.Done())
			waitGroup.Done()
		}()
	}

	klog.Infof("Started %v workers for %v", c.options.Workers, c.options.Name)

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

func (c *controller[T]) runWorker() {
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

func (c *controller[T]) reconcile(key string) error {
	var newObj *T
	var err error

	// Convert the namespace/name string into a distinct namespace and name
	namespace, name, err := cache.SplitMetaNamespaceKey(key)
	if err != nil {
		utilruntime.HandleError(fmt.Errorf("invalid resource key: %s", key))
		return nil
	}

	newObj, err = c.lister.Get(namespace, name)
	if err != nil {
		if kerrors.IsNotFound(err) {
			// Rule was deleted. Remove it from our database of enforced rules
			newObj = nil
		} else {
			return err
		}
	}

	return c.reconciler(newObj)
}
