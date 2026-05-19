// modusGraphGen is a code generation tool that reads Go structs with dgraph
// struct tags and produces a typed client library, functional options, query
// builders, and a Kong CLI.
//
// Usage:
//
//	go run github.com/matthewmcneely/modusgraph/cmd/modusgraph-gen [flags]
//
// When invoked via go:generate (the typical case), it uses the current working
// directory as the target package.
package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"

	"github.com/matthewmcneely/modusgraph/cmd/modusgraph-gen/internal/generator"
	"github.com/matthewmcneely/modusgraph/cmd/modusgraph-gen/internal/parser"
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

// defaults captures any flag values explicitly provided by the user. An empty
// string means "not provided"; the corresponding field is auto-defaulted by
// resolveDefaults. Fields use the same names as the public flag names (minus
// the leading dash) for direct correspondence.
type defaults struct {
	schemaDirExplicit       string
	schemaClientDirExplicit string
	entityDirExplicit       string
	entityClientDirExplicit string
}

// resolvedConfig holds the final resolved path settings. All paths are
// absolute (a path supplied as a relative explicit value is resolved against
// cwd; a path supplied as an absolute explicit value is used directly).
type resolvedConfig struct {
	SchemaDir       string
	SchemaClientDir string
	EntityDir       string
	EntityClientDir string
}

// resolveDefaults applies the spec's two-step defaulting rules to produce a
// final resolvedConfig. cwd is the absolute current working directory; d
// carries any explicit flag values.
//
// Rules (in order):
//
//  1. -schema-dir defaults to <cwd>/schema if that subdir exists, otherwise
//     <cwd> itself. An explicit value wins, made absolute against cwd.
//  2. -schema-client-dir defaults to the resolved -schema-dir. Explicit wins.
//  3. -entity-dir defaults to <cwd>/entity if the resolved -schema-dir == cwd,
//     otherwise <cwd>. Explicit wins. The condition is checked against the
//     RESOLVED schema-dir so an explicit -schema-dir . triggers ./entity/
//     identically to the unflagged schema-local case.
//  4. -entity-client-dir defaults to the resolved -entity-dir. Explicit wins.
func resolveDefaults(cwd string, d defaults) resolvedConfig {
	cfg := resolvedConfig{}

	// 1) -schema-dir
	switch {
	case d.schemaDirExplicit != "":
		cfg.SchemaDir = absJoin(cwd, d.schemaDirExplicit)
	case dirExists(filepath.Join(cwd, "schema")):
		cfg.SchemaDir = filepath.Join(cwd, "schema")
	default:
		cfg.SchemaDir = cwd
	}

	// 2) -schema-client-dir
	if d.schemaClientDirExplicit != "" {
		cfg.SchemaClientDir = absJoin(cwd, d.schemaClientDirExplicit)
	} else {
		cfg.SchemaClientDir = cfg.SchemaDir
	}

	// 3) -entity-dir — keyed on whether resolved schema-dir == cwd
	switch {
	case d.entityDirExplicit != "":
		cfg.EntityDir = absJoin(cwd, d.entityDirExplicit)
	case cfg.SchemaDir == cwd:
		cfg.EntityDir = filepath.Join(cwd, "entity")
	default:
		cfg.EntityDir = cwd
	}

	// 4) -entity-client-dir
	if d.entityClientDirExplicit != "" {
		cfg.EntityClientDir = absJoin(cwd, d.entityClientDirExplicit)
	} else {
		cfg.EntityClientDir = cfg.EntityDir
	}

	return cfg
}

// absJoin returns an absolute, cleaned path. If p is already absolute, it is
// cleaned and returned. Otherwise it is joined under cwd and cleaned.
func absJoin(cwd, p string) string {
	if filepath.IsAbs(p) {
		return filepath.Clean(p)
	}
	return filepath.Clean(filepath.Join(cwd, p))
}

// dirExists reports whether the given path exists AND is a directory.
func dirExists(p string) bool {
	info, err := os.Stat(p)
	return err == nil && info.IsDir()
}
