# Design: Merge modusGraphGen into modusGraph

**Date:** 2026-02-27
**Status:** Approved

## Summary

Merge the standalone modusGraphGen code generator into the modusGraph repository as `cmd/modusgraphgen/` with internal packages. This consolidates the toolchain into a single repo, simplifying installation and maintenance.

## Background

modusGraphGen is a Go code generator that parses structs with `json`/`dgraph` tags and produces typed CRUD clients, query builders, iterators, functional options, and a CLI. It uses only the Go standard library (go/ast, go/parser, text/template, embed) and has zero external dependencies. The generated code imports `github.com/matthewmcneely/modusgraph`.

Currently modusGraphGen lives in a separate repository (`github.com/mlwelles/modusGraphGen`). Merging it into modusGraph means consumers install one module and get both the library and the generator.

## Directory Layout

```
cmd/modusgraphgen/
  main.go                           # CLI entry point
  internal/
    model/
      model.go                      # IR types (Package, Entity, Field)
    parser/
      parser.go                     # AST-based Go source parser
      inference.go                  # Post-parse inference rules
      parser_test.go                # Tests using local testdata
      testdata/
        movies/                     # Fixtures copied from modusGraphMoviesProject
          actor.go, film.go, director.go, genre.go, country.go,
          rating.go, content_rating.go, performance.go, location.go,
          generate.go
    generator/
      generator.go                  # Template execution engine
      generator_test.go             # Golden file tests
      templates/                    # Embedded via go:embed
        client.go.tmpl, entity.go.tmpl, query.go.tmpl, cli.go.tmpl,
        iter.go.tmpl, options.go.tmpl, page_options.go.tmpl
      testdata/golden/              # Golden test files
        *.go
```

## Import Path Rewriting

Internal imports change from:
- `github.com/mlwelles/modusGraphGen/{model,parser,generator}`

To:
- `github.com/matthewmcneely/modusgraph/cmd/modusgraphgen/internal/{model,parser,generator}`

Template output imports (`github.com/matthewmcneely/modusgraph`) remain unchanged.

Consumer go:generate directives change to:
```go
//go:generate go run github.com/matthewmcneely/modusgraph/cmd/modusgraphgen
```

## Git Workflow

1. Fetch upstream
2. Create `fork-main-pre-sync` branch from current `origin/main` to preserve divergence
3. Push preservation branch to origin
4. Reset local main to `upstream/main`
5. Force push main to origin (sync fork with upstream)
6. Create `feature/add-modusgraphgen` from clean main
7. Perform the merge (single squashed commit)
8. Push feature branch
9. Open PR to upstream (`matthewmcneely/modusgraph:main`)
10. Open PR to fork (`mlwelles/modusGraph:main`)

## Test Strategy

- Parser tests: rewrite to reference `testdata/movies/` instead of external sibling project
- Generator tests: golden file tests preserved in `testdata/golden/`
- Test fixtures include the 9 movies struct files plus `generate.go`
- No new dependencies required (gen tool uses only stdlib)

## README Update

Add a "Code Generation" section to the main README covering:
- Overview of what the gen tool does
- Installation via `go install`
- Usage via `go:generate`
- CLI flags reference
- Generated output file table
- Struct tag reference

## Decisions

| Decision | Choice | Rationale |
|----------|--------|-----------|
| Placement | `cmd/modusgraphgen/` | Follows Go convention for CLI tools in a library repo |
| Package visibility | `internal/` | Gen internals are not public API |
| Import paths | Rewrite to new module paths | Clean integration, no multi-module complexity |
| Template import paths | Keep as-is | Templates correctly reference the upstream module path |
| Test data | Copy fixtures into testdata/ | Self-contained tests, no external dependencies |
| Git history | Single squashed commit | Clean PR, original repo preserves full history |
| Stub templates | Skip | Dead code, only real templates from generator/templates/ |
| Documentation | Section in main README | Single source of truth |
