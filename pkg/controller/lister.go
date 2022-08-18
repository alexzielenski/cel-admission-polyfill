package controller

import (
	"github.com/alexzielenski/cel_polyfill/pkg/apis/celadmissionpolyfill.k8s.io/v1alpha1"
	kerrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/cache"
)

var _ Lister[runtime.Object] = lister[runtime.Object]{}

type lister[T runtime.Object] struct {
	indexer cache.Indexer
}

func (w lister[T]) List(namespace string, selector labels.Selector) (ret []T, err error) {
	err = cache.ListAllByNamespace(w.indexer, namespace, selector, func(m interface{}) {
		ret = append(ret, m.(T))
	})
	return ret, err
}

func (w lister[T]) Get(namespace string, name string) (T, error) {
	var result T

	obj, exists, err := w.indexer.GetByKey(namespace + "/" + name)
	if err != nil {
		return result, err
	}
	if !exists {
		return result, kerrors.NewNotFound(v1alpha1.Resource("validationruleset"), name)
	}
	result = obj.(T)
	return result, nil
}

func NewLister[T runtime.Object](indexer cache.Indexer) Lister[T] {
	return lister[T]{indexer: indexer}
}
