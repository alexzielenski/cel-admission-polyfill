//go:build tools

package celadmissionpolyfill

// Used to force direct dependency on code-generator so that it stays in vendor
// directory.
import (
	_ "k8s.io/code-generator"
	_ "sigs.k8s.io/controller-tools/cmd/controller-gen"
)
