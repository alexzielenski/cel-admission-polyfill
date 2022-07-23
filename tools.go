//go:build tools

package celadmissionpolyfill

// Used to force direct dependency on code-generator so that it stays in vendor
// directory.
import _ "k8s.io/code-generator"

// go:generate hack/update-codegen.sh
