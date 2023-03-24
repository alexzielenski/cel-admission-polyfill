package cel_polyfill

import (
	"context"
	"embed"
	"fmt"
	"path/filepath"
	"strings"

	apiextensionsclientset "k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

//go:embed crds
var crds embed.FS

func InstallCRDs(ctx context.Context, a apiextensionsclientset.Interface) error {
	names, err := crds.ReadDir("crds")
	if err != nil {
		return fmt.Errorf("failed to list crds dir: %w", err)
	}

	for _, f := range names {
		byts, err := crds.ReadFile("crds/" + f.Name())
		if err != nil {
			return fmt.Errorf("failed to read file crds/%s: %w", f.Name(), err)
		}

		filename := strings.TrimSuffix(f.Name(), filepath.Ext(f.Name()))
		group, name, found := strings.Cut(filename, "_")
		if !found {
			return fmt.Errorf("filename in wrong format: %v", name)
		}

		_, err = a.ApiextensionsV1().CustomResourceDefinitions().Patch(ctx, name+"."+group, types.ApplyPatchType, byts, v1.PatchOptions{FieldManager: "cel-polyfill-controller"})
		if err != nil {
			return err
		}
	}

	return nil
}
