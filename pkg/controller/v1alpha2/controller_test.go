package v1alpha2_test

import (
	"bytes"
	"context"
	"io/ioutil"
	"reflect"
	"testing"
	"time"

	"github.com/alexzielenski/cel_polyfill/pkg/apis/celadmissionpolyfill.k8s.io/v1alpha2"
	"github.com/alexzielenski/cel_polyfill/pkg/controller"
	"github.com/alexzielenski/cel_polyfill/pkg/controller/structuralschema"
	controllerv1alpha2 "github.com/alexzielenski/cel_polyfill/pkg/controller/v1alpha2"
	"github.com/alexzielenski/cel_polyfill/pkg/controller/v1alpha2/testdata"
	"github.com/alexzielenski/cel_polyfill/pkg/generated/clientset/versioned/fake"
	"github.com/alexzielenski/cel_polyfill/pkg/generated/informers/externalversions"
	"github.com/alexzielenski/cel_polyfill/pkg/validator"
	"github.com/google/go-cmp/cmp"

	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/wait"
	yamlutil "k8s.io/apimachinery/pkg/util/yaml"

	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	apiextensionsfake "k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset/fake"
	apiextensionsinformers "k8s.io/apiextensions-apiserver/pkg/client/informers/externalversions"
	dynamicfake "k8s.io/client-go/dynamic/fake"
)

type IntrospectableController interface {
	controller.Interface
	validator.Interface
	GetNumberInstances(templateName string) int
}

func TestBasic(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	scheme := runtime.NewScheme()
	scheme.AddKnownTypeWithName(schema.GroupVersionKind{Group: "celadmissionpolyfill.k8s.io", Version: "v1alpha2", Kind: "required_labels"}, &unstructured.Unstructured{})
	scheme.AddKnownTypeWithName(schema.GroupVersionKind{Group: "celadmissionpolyfill.k8s.io", Version: "v1alpha2", Kind: "required_labelsList"}, &unstructured.UnstructuredList{})

	crd := &apiextensionsv1.CustomResourceDefinition{}
	file, err := ioutil.ReadFile("testdata/stable.example.com_basicunions.yaml")
	if err != nil {
		t.Fatalf(err.Error())
	}
	decoder := yamlutil.NewYAMLOrJSONDecoder(bytes.NewReader(file), 24)
	err = decoder.Decode(crd)
	if err != nil {
		t.Fatalf(err.Error())
	}

	dynamicClient := dynamicfake.NewSimpleDynamicClientWithCustomListKinds(scheme, map[schema.GroupVersionResource]string{
		{
			Group:    "celadmissionpolyfill.k8s.io",
			Version:  "v1alpha2",
			Resource: "required_labels",
		}: "required_labelsList",
	})
	client := fake.NewSimpleClientset()
	fakeext := apiextensionsfake.NewSimpleClientset(crd)

	factory := externalversions.NewSharedInformerFactory(client, 30*time.Second)
	apiextensionsFactory := apiextensionsinformers.NewSharedInformerFactory(fakeext, 30*time.Second)

	structuralschemaController := structuralschema.NewController(
		apiextensionsFactory.Apiextensions().V1().CustomResourceDefinitions().Informer(),
	)

	controller := controllerv1alpha2.NewPolicyTemplateController(
		dynamicClient,
		factory.Celadmissionpolyfill().V1alpha2().PolicyTemplates().Informer(),
		structuralschemaController,
		fakeext,
	).(IntrospectableController)

	factory.Start(ctx.Done())
	apiextensionsFactory.Start(ctx.Done())

	go func() {
		err := controller.Run(ctx)

		if ctx.Err() == nil {
			t.Error(err)
		}
	}()

	// Install policy
	file, err = ioutil.ReadFile("testdata/required_labels/policy.yaml")
	if err != nil {
		t.Fatalf(err.Error())
	}
	policy := &v1alpha2.PolicyTemplate{}
	decoder = yamlutil.NewYAMLOrJSONDecoder(bytes.NewReader(file), 24)
	err = decoder.Decode(policy)
	if err != nil {
		t.Fatalf(err.Error())
	}

	_, err = client.CeladmissionpolyfillV1alpha2().
		PolicyTemplates(policy.Namespace).
		Create(ctx, policy, metav1.CreateOptions{})

	if err != nil {
		t.Fatalf(err.Error())
	}

	err = wait.PollWithContext(ctx, 30*time.Millisecond, 1*time.Second, func(ctx context.Context) (done bool, err error) {
		// Wait until CRD pops up
		obj, err := fakeext.ApiextensionsV1().CustomResourceDefinitions().Get(
			ctx,
			"required_labels.celadmissionpolyfill.k8s.io",
			metav1.GetOptions{},
		)

		if err != nil {
			if errors.IsNotFound(err) {
				return false, nil
			}
			return false, err
		}

		return obj != nil, nil
	})

	// Check that CRD is created
	if err != nil {
		t.Fatalf(err.Error())
	}

	// Create instance of policy
	file, err = ioutil.ReadFile("testdata/required_labels/instance.yaml")
	if err != nil {
		t.Fatalf(err.Error())
	}

	instance := &unstructured.Unstructured{}
	decoder = yamlutil.NewYAMLOrJSONDecoder(bytes.NewReader(file), 24)
	err = decoder.Decode(instance)
	if err != nil {
		t.Fatalf(err.Error())
	}

	_, err = dynamicClient.Resource(schema.GroupVersionResource{
		Group:    instance.GroupVersionKind().Group,
		Version:  instance.GroupVersionKind().Version,
		Resource: "required_labels",
	}).Namespace(instance.GetNamespace()).Create(ctx, instance, metav1.CreateOptions{})
	if err != nil {
		t.Fatalf(err.Error())
	}

	// wait until policy is instantiated
	err = wait.PollWithContext(ctx, 30*time.Millisecond, 1*time.Second, func(ctx context.Context) (done bool, err error) {
		return controller.GetNumberInstances("required_labels") > 0, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	// Check rule is enforced
	gvr := metav1.GroupVersionResource{
		Group:    "stable.example.com",
		Version:  "v1",
		Resource: "basicunions",
	}

	err = controller.Validate(gvr, nil, &testdata.BasicUnion{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "stable.example.com/v1",
			Kind:       "BasicUnion",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "testobject",
			Namespace: "default",
			Labels: map[string]string{
				"ssh":      "enabled",
				"env":      "prod",
				"verified": "true",
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	err = controller.Validate(gvr, nil, &testdata.BasicUnion{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "stable.example.com/v1",
			Kind:       "BasicUnion",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "testobject",
			Namespace: "default",
			Labels: map[string]string{
				"ssh":      "enabled",
				"env":      "invalid_value", // policy instance expects 'prod'
				"verified": "true",
			},
		},
	})

	// Check output
	expected := []any{
		map[string]any{
			"details": map[string]any{
				"data": []any{
					"env",
				},
			},
			"message": "invalid values provided on one or more labels",
		},
	}
	actual := err.(controllerv1alpha2.DecisionError).ErrorJSON()
	if !reflect.DeepEqual(expected, actual) {
		t.Fatalf("%s", cmp.Diff(expected, actual))
	}
	err = controller.Validate(gvr, nil, &testdata.BasicUnion{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "stable.example.com/v1",
			Kind:       "BasicUnion",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "testobject",
			Namespace: "default",
			Labels: map[string]string{
				"verified": "true",
				"env":      "prod",
				// policy instance expects 'env': 'prod' and 'ssh': enabled
			},
		},
	})
	expected = []any{
		map[string]any{
			"details": map[string]any{
				"data": []any{
					"ssh",
				},
			},
			"message": "missing one or more required labels",
		},
	}
	actual = err.(controllerv1alpha2.DecisionError).ErrorJSON()
	if !reflect.DeepEqual(expected, actual) {
		t.Fatalf("%s", cmp.Diff(expected, actual))
	}

	err = controller.Validate(gvr, nil, &testdata.BasicUnion{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "stable.example.com/v1",
			Kind:       "BasicUnion",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "testobject",
			Namespace: "default",
			Labels: map[string]string{
				"verified": "true",
				"env":      "incorrect",
				// policy instance expects 'env': 'prod' and 'ssh': enabled
			},
		},
	})
	expected = []any{
		map[string]any{
			"details": map[string]any{
				"data": []any{
					"ssh",
				},
			},
			"message": "missing one or more required labels",
		},
		map[string]any{
			"details": map[string]any{
				"data": []any{
					"env",
				},
			},
			"message": "invalid values provided on one or more labels",
		},
	}
	actual = err.(controllerv1alpha2.DecisionError).ErrorJSON()
	if !reflect.DeepEqual(expected, actual) {
		t.Fatalf("%s", cmp.Diff(expected, actual))
	}
}
