// gen-kustomize generates the kustomize patch for CRD config schema.
// It imports all registered providers and generates a patch file containing
// the oneOf schema for provider-specific config validation.
//
// Usage: go run ./cmd/gen-kustomize -out config/crd/patches/config-schema.yaml
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/lukasngl/secret-manager/internal/adapter"
)

func main() {
	var outFile string
	flag.StringVar(&outFile, "out", "", "Output file path (required)")
	flag.Parse()

	if outFile == "" {
		fmt.Fprintln(os.Stderr, "error: -out flag is required")
		flag.Usage()
		os.Exit(1)
	}

	patch, err := adapter.GenerateKustomizePatch()
	if err != nil {
		fmt.Fprintf(os.Stderr, "error generating patch: %v\n", err)
		os.Exit(1)
	}

	// Ensure directory exists
	if err := os.MkdirAll(filepath.Dir(outFile), 0o755); err != nil {
		fmt.Fprintf(os.Stderr, "error creating directory: %v\n", err)
		os.Exit(1)
	}

	if err := os.WriteFile(outFile, patch, 0o644); err != nil {
		fmt.Fprintf(os.Stderr, "error writing file: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("Generated %s\n", outFile)
}
