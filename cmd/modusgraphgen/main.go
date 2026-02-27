// modusGraphGen is a code generation tool that reads Go structs with dgraph
// struct tags and produces a typed client library, functional options, query
// builders, and a Kong CLI.
//
// Usage:
//
//	go run github.com/matthewmcneely/modusgraph/cmd/modusgraphgen [flags]
//
// When invoked via go:generate (the typical case), it uses the current working
// directory as the target package.
package main

import (
	"flag"
	"fmt"
	"log"
	"os"

	"github.com/matthewmcneely/modusgraph/cmd/modusgraphgen/internal/generator"
	"github.com/matthewmcneely/modusgraph/cmd/modusgraphgen/internal/parser"
)

func main() {
	pkgDir := flag.String("pkg", ".", "path to the target Go package directory")
	outputDir := flag.String("output", "", "output directory (default: same as -pkg)")
	cliDir := flag.String("cli-dir", "", "output directory for CLI main.go (default: {output}/cmd/{package})")
	cliName := flag.String("cli-name", "", "name for CLI binary and kong.Name (default: package name)")
	withValidator := flag.Bool("with-validator", false, "enable struct validation via modusgraph.WithValidator in the generated CLI")
	flag.Parse()

	// Resolve the package directory.
	dir := *pkgDir
	if dir == "." {
		var err error
		dir, err = os.Getwd()
		if err != nil {
			log.Fatalf("failed to get working directory: %v", err)
		}
	}

	// Resolve the output directory.
	outDir := *outputDir
	if outDir == "" {
		outDir = dir
	}

	// Parse phase: extract the model from Go source files.
	pkg, err := parser.Parse(dir)
	if err != nil {
		log.Fatalf("parse error: %v", err)
	}

	// Apply CLI name override if provided.
	if *cliName != "" {
		pkg.CLIName = *cliName
	}

	// Apply validator flag.
	pkg.WithValidator = *withValidator

	fmt.Printf("Package: %s\n", pkg.Name)
	fmt.Printf("Entities: %d\n", len(pkg.Entities))
	for _, e := range pkg.Entities {
		searchInfo := ""
		if e.Searchable {
			searchInfo = fmt.Sprintf(" (searchable on %s)", e.SearchField)
		}
		fmt.Printf("  - %s: %d fields%s\n", e.Name, len(e.Fields), searchInfo)
	}

	// Generate phase: execute templates and write output files.
	fmt.Printf("\nGenerating code into %s ...\n", outDir)
	var genOpts []generator.GenerateOption
	if *cliDir != "" {
		genOpts = append(genOpts, generator.WithCLIDir(*cliDir))
	}
	if err := generator.Generate(pkg, outDir, genOpts...); err != nil {
		log.Fatalf("generation error: %v", err)
	}
	fmt.Println("Done.")
}
