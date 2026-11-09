// Command apitypes writes the TypeScript types of the API: `go run ./cmd/apitypes > docs/api-types.ts`.
package main

import (
	"flag"
	"fmt"
	"os"

	"rabbit-hole-server/internal/apitypes"
)

func main() {
	spec := flag.String("spec", "docs/openapi.yaml", "OpenAPI document")
	out := flag.String("out", "docs/api-types.ts", "TypeScript file to write")
	flag.Parse()
	source, err := apitypes.Generate(*spec)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if err := os.WriteFile(*out, []byte(source), 0o600); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
