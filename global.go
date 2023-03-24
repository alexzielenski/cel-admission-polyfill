//go:build !DEBUG

//go:generate ./hack/update-codegen.sh

// Workaround for kubebuilder bug which does not respect empty value for defaults
//go:generate go run github.com/mikefarah/yq/v4 eval ".spec.versions[0].schema.openAPIV3Schema.properties.spec.properties.matchConstraints.properties.namespaceSelector.default = {}" ./crds/admissionregistration.polyfill.sigs.k8s.io_validatingadmissionpolicies.yaml -i
//go:generate go run github.com/mikefarah/yq/v4 eval ".spec.versions[0].schema.openAPIV3Schema.properties.spec.properties.matchConstraints.properties.objectSelector.default = {}" ./crds/admissionregistration.polyfill.sigs.k8s.io_validatingadmissionpolicies.yaml -i
//go:generate go run github.com/mikefarah/yq/v4 eval ".spec.versions[0].schema.openAPIV3Schema.properties.spec.properties.matchResources.properties.namespaceSelector.default = {}" ./crds/admissionregistration.polyfill.sigs.k8s.io_validatingadmissionpolicybindings.yaml -i
//go:generate go run github.com/mikefarah/yq/v4 eval ".spec.versions[0].schema.openAPIV3Schema.properties.spec.properties.matchResources.properties.objectSelector.default = {}" ./crds/admissionregistration.polyfill.sigs.k8s.io_validatingadmissionpolicybindings.yaml -i

package cel_polyfill

import (
	"context"

	"k8s.io/client-go/dynamic"
)

func DEBUG_InstallCRDs(ctx context.Context, client apiextensionsclientset.Interface) {
	// Do nothing on production
}
