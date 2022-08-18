package v1alpha2_test

import (
	"bytes"
	"context"
	"io/ioutil"
	"testing"
	"time"

	"github.com/alexzielenski/cel_polyfill/pkg/apis/celadmissionpolyfill.k8s.io/v1alpha2"
	"github.com/alexzielenski/cel_polyfill/pkg/controller/structuralschema"
	controllerv1alpha2 "github.com/alexzielenski/cel_polyfill/pkg/controller/v1alpha2"
	"github.com/alexzielenski/cel_polyfill/pkg/generated/clientset/versioned/fake"
	"github.com/alexzielenski/cel_polyfill/pkg/generated/informers/externalversions"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	yamlutil "k8s.io/apimachinery/pkg/util/yaml"

	apiextensionsfake "k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset/fake"
	apiextensionsinformers "k8s.io/apiextensions-apiserver/pkg/client/informers/externalversions"
)

func TestBasic(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	client := fake.NewSimpleClientset()
	fakeext := apiextensionsfake.NewSimpleClientset()

	factory := externalversions.NewSharedInformerFactory(client, 30*time.Second)
	apiextensionsFactory := apiextensionsinformers.NewSharedInformerFactory(fakeext, 30*time.Second)

	structuralschemaController := structuralschema.NewController(
		apiextensionsFactory.Apiextensions().V1().CustomResourceDefinitions().Informer(),
	)

	controller := controllerv1alpha2.NewPolicyTemplateController(
		factory.Celadmissionpolyfill().V1alpha2().PolicyTemplates().Informer(),
		structuralschemaController,
		fakeext,
	)

	factory.Start(ctx.Done())
	apiextensionsFactory.Start(ctx.Done())

	go func() {
		err := controller.Run(ctx)

		if ctx.Err() == nil {
			t.Error(err)
		}
	}()

	// Install policy
	file, err := ioutil.ReadFile("testdata/required_labels/policy.yaml")
	if err != nil {
		t.Fatalf(err.Error())
	}
	policy := &v1alpha2.PolicyTemplate{}
	decoder := yamlutil.NewYAMLOrJSONDecoder(bytes.NewReader(file), 24)
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

	// Check that CRD is created
	<-ctx.Done()
	// err = wait.PollWithContext(ctx, 30*time.Millisecond, 1*time.Hour, func(ctx context.Context) (done bool, err error) {
	// 	// Wait until CRD pops up
	// 	obj, err := fakeext.ApiextensionsV1().CustomResourceDefinitions().Get(
	// 		ctx,
	// 		"required_labels.celadmissionpolyfill.k8s.io",
	// 		metav1.GetOptions{},
	// 	)

	// 	if err != nil {
	// 		if errors.IsNotFound(err) {
	// 			return false, nil
	// 		}
	// 		return false, err
	// 	}

	// 	return obj != nil, nil
	// })

	// if err != nil {
	// 	t.Fatalf(err.Error())
	// }

	// Create instance of policy
	// Check rule is enforced
}
