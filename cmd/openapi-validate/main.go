// Command openapi-validate checks that an OpenAPI document is valid.
// It exists to keep contract validation reproducible and dependency-
// light (a single pinned Go module, no Node/Spectral toolchain).
package main

import (
	"context"
	"fmt"
	"os"

	"github.com/getkin/kin-openapi/openapi3"
)

func main() {
	if len(os.Args) != 2 {
		_, err := fmt.Fprintf(os.Stderr, "usage: %s <openapi-spec.yaml>\n", os.Args[0])
		if err != nil {
			return
		}
		os.Exit(2)
	}

	path := os.Args[1]

	loader := openapi3.NewLoader()
	loader.IsExternalRefsAllowed = false

	doc, err := loader.LoadFromFile(path)
	if err != nil {
		fmt.Fprintf(os.Stderr, "load %s: %v\n", path, err)
		os.Exit(1)
	}

	if err := doc.Validate(context.Background()); err != nil {
		fmt.Fprintf(os.Stderr, "%s is not a valid OpenAPI document: %v\n", path, err)
		os.Exit(1)
	}

	fmt.Printf("%s is a valid OpenAPI document\n", path)
}
