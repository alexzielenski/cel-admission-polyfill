//go:build DEBUG

package cel_polyfill

import (
	"context"
	_ "embed"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/dynamic"
)

//go:embed crds/celadmissionpolyfill.k8s.io_validationrulesets.yaml
var validationRuleSetsCRD string

//go:embed crds/celadmissionpolyfill.k8s.io_policytemplates.yaml
var policyTempaltesCRD string

func DEBUG_InstallCRDs(ctx context.Context, client dynamic.Interface) {
	_, err := client.Resource(schema.GroupVersionResource{
		Group:    "apiextensions.k8s.io",
		Version:  "v1",
		Resource: "customresourcedefinitions",
	}).
		Patch(
			ctx,
			"validationrulesets.celadmissionpolyfill.k8s.io",
			types.ApplyPatchType,
			[]byte(validationRuleSetsCRD),
			metav1.PatchOptions{FieldManager: "cel-polyfill-controller"},
		)

	if err != nil {
		panic(err)
	}

	_, err = client.Resource(schema.GroupVersionResource{
		Group:    "apiextensions.k8s.io",
		Version:  "v1",
		Resource: "customresourcedefinitions",
	}).
		Patch(
			ctx,
			"policytemplates.celadmissionpolyfill.k8s.io",
			types.ApplyPatchType,
			[]byte(policyTempaltesCRD),
			metav1.PatchOptions{FieldManager: "cel-polyfill-controller"},
		)

	if err != nil {
		panic(err)
	}
}
