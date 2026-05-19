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
	schemaDir := flag.String("schema-dir", "", "schema source directory (default: ./schema if exists, else CWD)")
	schemaAlias := flag.String("schema-alias", "", "import alias for schema pkg (default: basename of -schema-dir)")
	schemaClientDir := flag.String("schema-client-dir", "", "output dir for schema-level clients (default: -schema-dir)")
	entityDir := flag.String("entity-dir", "", "output dir for wrapper types (default: ./entity if schema-dir==CWD, else CWD)")
	entityClientDir := flag.String("entity-client-dir", "", "output dir for wrapper-level clients (default: -entity-dir)")
	cliDir := flag.String("cli-dir", "", "output dir for the CLI (default: ./cmd/<pkg>)")
	pkgName := flag.String("pkg-name", "", "package name for wrapper files (default: basename of -entity-dir)")
	cliName := flag.String("cli-name", "", "name for CLI binary (default: wrapper pkg name)")
	out := flag.String("out", "", "[deprecated] alias for -entity-dir")

	noSchemaClients := flag.Bool("no-schema-clients", false, "skip schema-level clients/queries (implies -no-entity-clients)")
	entities := flag.Bool("entities", false, "generate the wrapper/entity layer (types, accessors, options, clients); off by default")
	noEntityClients := flag.Bool("no-entity-clients", false, "skip wrapper-level clients/queries only")
	noCLI := flag.Bool("no-cli", false, "skip CLI generation")
	withValidator := flag.Bool("with-validator", false, "enable validation in the generated CLI")
	flag.Parse()

	// Apply the deprecated -out alias.
	if *out != "" && *entityDir == "" {
		fmt.Fprintln(os.Stderr, "warning: -out is deprecated; use -entity-dir instead")
		*entityDir = *out
	}

	// Resolve the current working directory.
	cwd, err := os.Getwd()
	if err != nil {
		log.Fatalf("getwd: %v", err)
	}

	// Resolve path defaults via the Task 6 helper.
	resolved := resolveDefaults(cwd, defaults{
		schemaDirExplicit:       *schemaDir,
		schemaClientDirExplicit: *schemaClientDir,
		entityDirExplicit:       *entityDir,
		entityClientDirExplicit: *entityClientDir,
	})

	// Apply toggle implication rules.
	if *noSchemaClients {
		*noEntityClients = true
	}
	if !*entities {
		*noEntityClients = true
	}

	// Parse phase: extract the model from Go source files.
	// When the wrapper layer is NOT being generated, relax the pointer-slice
	// rule so schemas with []Entity value slices work in raw-only mode.
	var parseOpts []parser.ParseOption
	if !*entities {
		parseOpts = append(parseOpts, parser.WithAllowValueElementEntitySlices())
	}
	pkg, err := parser.Parse(resolved.SchemaDir, parseOpts...)
	if err != nil {
		log.Fatalf("parse error: %v", err)
	}

	// Derive package name defaults.
	entityPackageName := *pkgName
	if entityPackageName == "" {
		entityPackageName = filepath.Base(resolved.EntityDir)
	}
	schemaAliasResolved := *schemaAlias
	if schemaAliasResolved == "" {
		schemaAliasResolved = filepath.Base(resolved.SchemaDir)
	}

	// Resolve CLI dir default.
	cliDirResolved := *cliDir
	if cliDirResolved == "" {
		cliDirResolved = filepath.Join(cwd, "cmd", entityPackageName)
	}

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
	fmt.Printf("\nGenerating code ...\n")
	cfg := generator.Config{
		SchemaDir:               resolved.SchemaDir,
		SchemaClientDir:         resolved.SchemaClientDir,
		EntityDir:               resolved.EntityDir,
		EntityClientDir:         resolved.EntityClientDir,
		CLIDir:                  cliDirResolved,
		EntityPackageName:       entityPackageName,
		EntityClientPackageName: entityPackageName, // same as EntityDir basename for default layout
		SchemaClientPackageName: pkg.Name,          // schema package name from parse
		SchemaAlias:             schemaAliasResolved,
		SchemaImportPath:        pkg.SchemaImportPath,
		NoSchemaClients:         *noSchemaClients,
		NoEntities:              !*entities,
		NoEntityClients:         *noEntityClients,
		NoCLI:                   *noCLI,
		CLIName:                 *cliName,
		WithValidator:           *withValidator,
	}

	if err := generator.Generate(pkg, cfg); err != nil {
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
