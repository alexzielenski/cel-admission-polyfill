package v0alpha1_test

import (
	"bytes"
	"context"
	"errors"
	"io/ioutil"
	"testing"
	"time"

	"github.com/alexzielenski/cel_polyfill/pkg/apis/celadmissionpolyfill.k8s.io/v0alpha1"
	"github.com/alexzielenski/cel_polyfill/pkg/controller/structuralschema"
	controllerv0alpha1 "github.com/alexzielenski/cel_polyfill/pkg/controller/v0alpha1"
	"github.com/alexzielenski/cel_polyfill/pkg/generated/clientset/versioned/fake"
	"github.com/alexzielenski/cel_polyfill/pkg/generated/informers/externalversions"
	"github.com/alexzielenski/cel_polyfill/pkg/validator"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	apiextensionsfake "k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset/fake"
	apiextensionsinformers "k8s.io/apiextensions-apiserver/pkg/client/informers/externalversions"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	utilerrors "k8s.io/apimachinery/pkg/util/errors"
	"k8s.io/apimachinery/pkg/util/validation/field"
	yamlutil "k8s.io/apimachinery/pkg/util/yaml"
)

func TestBasic(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	client := fake.NewSimpleClientset()
	fakeext := apiextensionsfake.NewSimpleClientset()

	factory := externalversions.NewSharedInformerFactory(client, 30*time.Second)
	apiextensionsFactory := apiextensionsinformers.NewSharedInformerFactory(fakeext, 30*time.Second)

	// Two main pieces of functionaltiy: Validate Objects, and Store Rules
	// Starts empty
	structuralschemaController := structuralschema.NewController(
		apiextensionsFactory.Apiextensions().V1().CustomResourceDefinitions().Informer(),
	)
	vald := controllerv0alpha1.NewValidator(structuralschemaController)

	// Populates the validator with rule sets depending upon the CRD definition
	controller := controllerv0alpha1.NewAdmissionRulesController(
		factory.Celadmissionpolyfill().V0alpha1().ValidationRuleSets(),
		vald,
	)

	factory.Start(ctx.Done())
	apiextensionsFactory.Start(ctx.Done())

	// Add CRD definitions accessible to validator
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
	_, err = fakeext.ApiextensionsV1().CustomResourceDefinitions().Create(ctx, crd, metav1.CreateOptions{})
	if err != nil {
		t.Fatalf(err.Error())
	}

	go func() {
		err := controller.Run(ctx)

		if ctx.Err() == nil {
			t.Error(err)
		}
	}()

	// Install rules
	file, err = ioutil.ReadFile("testdata/admission_rules.yaml")
	if err != nil {
		t.Fatalf(err.Error())
	}
	rules := &v0alpha1.ValidationRuleSet{}
	decoder = yamlutil.NewYAMLOrJSONDecoder(bytes.NewReader(file), 24)
	err = decoder.Decode(rules)
	if err != nil {
		t.Fatalf(err.Error())
	}

	_, err = client.CeladmissionpolyfillV0alpha1().ValidationRuleSets("default").Create(ctx, rules, metav1.CreateOptions{})
	if err != nil {
		t.Fatalf(err.Error())
	}

	// Run test cases
	type testCase struct {
		filename      string
		errorExpected []error
	}

	cases := []testCase{
		{
			filename: "cr_1_create",
			errorExpected: []error{
				field.Invalid(nil, nil, "if discriminator is mode1, mode1 and value must be equal"),
			},
		},
		{
			filename:      "cr_2_update",
			errorExpected: nil,
		},
		{
			filename: "cr_3_name_rejected",
			errorExpected: []error{
				field.Invalid(nil, nil, "name should be a test"),
			},
		},
	}

	var prev *BasicUnion
	for _, cs := range cases {
		file, err = ioutil.ReadFile("testdata/" + cs.filename + ".yaml")
		if err != nil {
			t.Fatalf(err.Error())
		}

		obj := &BasicUnion{}
		decoder = yamlutil.NewYAMLOrJSONDecoder(bytes.NewReader(file), 24)
		err = decoder.Decode(obj)

		if err != nil {
			t.Fatalf(err.Error())
		}

		err := vald.Validate(metav1.GroupVersionResource{
			Group:    "stable.example.com",
			Version:  "v1",
			Resource: "basicunions",
		}, prev, obj)

		var returnedErrs []error

		if err.Status != validator.ValidationOK {
			if list, ok := err.Error.(utilerrors.Aggregate); ok {
				returnedErrs = list.Errors()
			} else if err.Error != nil {
				returnedErrs = append(returnedErrs, err.Error)
			} else {
				panic("status not OK but error nil?")
			}
		}

		for _, e := range cs.errorExpected {
			if fieldError, ok := e.(*field.Error); ok {
				// Check that returned errors has one with the same field
				found := false
				for idx, re := range returnedErrs {
					f := &field.Error{}
					if errors.As(re, &f) {
						if f.Detail == fieldError.Detail && f.Type == fieldError.Type && f.Field == fieldError.Field {
							found = true
							// remove matched error
							returnedErrs = append(returnedErrs[:idx], returnedErrs[idx+1:]...)
							break
						}
					}
				}
				if !found {
					t.Fatalf("error does not match expectation:\n\terror: %v\n\texpectation: %v", err, cs.errorExpected)
				}
			} else {
				// Check that returned errors has one and remove matching error
				panic("unimplemented")
			}
		}

		if len(returnedErrs) > 0 {
			t.Fatalf("unexpected errors: %v", returnedErrs)
		}

		prev = obj
	}
}

type BasicUnion struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   BasicUnionSpec   `json:"spec,omitempty"`
	Status BasicUnionStatus `json:"status,omitempty"`
}

type BasicUnionStatus struct{}
type BasicUnionSpec struct {
	Discriminator string `json:"discriminator"`

	// +optional
	Mode1 string `json:"mode1"`

	// +optional
	Mode2 string `json:"mode2"`

	// +optional
	Value string `json:"value,omitempty"`
}
