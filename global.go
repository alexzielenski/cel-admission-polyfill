//go:build !DEBUG

package cel_polyfill

import (
	"context"

	"k8s.io/client-go/dynamic"
)

func DEBUG_InstallCRDs(ctx context.Context, client dynamic.Interface) {
	// Do nothing on production
}
