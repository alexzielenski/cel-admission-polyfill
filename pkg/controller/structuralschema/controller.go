package structuralschema

import (
	"errors"
	"fmt"
	"sync"

	"github.com/alexzielenski/cel_polyfill/pkg/controller"
	apiextensionshelpers "k8s.io/apiextensions-apiserver/pkg/apihelpers"
	apiextensionsinternal "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	apiserverschema "k8s.io/apiextensions-apiserver/pkg/apiserver/schema"
	structuraldefaulting "k8s.io/apiextensions-apiserver/pkg/apiserver/schema/defaulting"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/tools/cache"
)

type Controller interface {
	controller.Interface

	Get(gvr metav1.GroupVersionResource) (*apiserverschema.Structural, error)
}

func NewController(crdInformer cache.SharedIndexInformer) Controller {
	wrappedInformer := controller.NewInformer[*apiextensionsv1.CustomResourceDefinition](crdInformer)
	result := &structuralSchemaController{
		lister:            wrappedInformer.Lister(),
		structuralSchemas: make(map[string]map[string]*apiserverschema.Structural),
	}

	result.Interface = controller.New(
		wrappedInformer,
		result.reconcileCRD,
		controller.ControllerOptions{},
	)
	return result
}

// Controller which caches and indexes structural schemas for CRDs
type structuralSchemaController struct {
	controller.Interface
	lister controller.Lister[*apiextensionsv1.CustomResourceDefinition]
	lock   sync.RWMutex

	// Map CRD Name to CRD version to structural schema
	structuralSchemas map[string]map[string]*apiserverschema.Structural
}

func (c *structuralSchemaController) reconcileCRD(
	namespace, name string,
	template *apiextensionsv1.CustomResourceDefinition,
) error {
	c.lock.Lock()
	defer c.lock.Unlock()

	// Whenever there is update/deletoin of a CRD, we want to wipe cache of
	// structural schemas so they are recreated next time they are requested
	delete(c.structuralSchemas, name)
	return nil
}

// Gets a structural schema for a given crd GVR
// !TODO: (does this support namespace-local CRDs)
func (sc *structuralSchemaController) Get(gvr metav1.GroupVersionResource) (*apiserverschema.Structural, error) {
	name := gvr.Resource + "." + gvr.Group

	res := func() *apiserverschema.Structural {
		sc.lock.RLock()
		defer sc.lock.RUnlock()

		if parsed, exists := sc.structuralSchemas[name]; exists {
			if version, exists := parsed[gvr.Version]; exists {
				return version
			}
		}

		return nil
	}()

	if res != nil {
		return res, nil
	}

	// Attempt to create the gvr by finding CRD with name and creating
	crd, err := sc.lister.Get(name)
	if err != nil {
		return nil, errors.New("crd not found")
	}

	sc.lock.Lock()
	defer sc.lock.Unlock()

	// copied from https://github.com/kubernetes/kubernetes/blob/01cf641ffbb3c876c4fc6c3e53a0613356f883e5/staging/src/k8s.io/apiextensions-apiserver/pkg/apiserver/customresource_handler.go#L650-L651
	for _, v := range crd.Spec.Versions {
		sch, err := apiextensionshelpers.GetSchemaForVersion(crd, v.Name)
		if err != nil {
			utilruntime.HandleError(err)
			return nil, fmt.Errorf("the server could not properly serve the CR schema")
		}
		if sch == nil {
			continue
		}
		internalValidation := &apiextensionsinternal.CustomResourceValidation{}
		if err := apiextensionsv1.Convert_v1_CustomResourceValidation_To_apiextensions_CustomResourceValidation(sch, internalValidation, nil); err != nil {
			return nil, fmt.Errorf("failed converting CRD validation to internal version: %v", err)
		}
		s, err := apiserverschema.NewStructural(internalValidation.OpenAPIV3Schema)
		if !crd.Spec.PreserveUnknownFields && err != nil {
			// This should never happen. If it does, it is a programming error.
			utilruntime.HandleError(fmt.Errorf("failed to convert schema to structural: %v", err))
			return nil, fmt.Errorf("the server could not properly serve the CR schema") // validation should avoid this
		}

		if !crd.Spec.PreserveUnknownFields {
			// we don't own s completely, e.g. defaults are not deep-copied. So better make a copy here.
			s = s.DeepCopy()

			if err := structuraldefaulting.PruneDefaults(s); err != nil {
				// This should never happen. If it does, it is a programming error.
				utilruntime.HandleError(fmt.Errorf("failed to prune defaults: %v", err))
				return nil, fmt.Errorf("the server could not properly serve the CR schema") // validation should avoid this
			}
		}

		if _, exists := sc.structuralSchemas[name]; !exists {
			sc.structuralSchemas[name] = make(map[string]*apiserverschema.Structural)
		}

		sc.structuralSchemas[name][v.Name] = s
	}
	if parsed, exists := sc.structuralSchemas[name]; exists {
		if version, exists := parsed[gvr.Version]; exists {
			return version, nil
		}
	}

	// If we reached this point the CRD exists but the requested version is
	// not valid
	return nil, errors.New("version not found")
}
