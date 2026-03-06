# Merge modusGraphGen into modusGraph Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Merge the standalone modusGraphGen code generator into the modusGraph repository as `cmd/modusgraphgen/` with internal packages, then open PRs to both upstream and fork.

**Architecture:** Copy modusGraphGen source files into `cmd/modusgraphgen/internal/{model,parser,generator}`, rewrite internal import paths to the modusgraph module, copy test fixtures from modusGraphMoviesProject, update the README, and deliver via two PRs.

**Tech Stack:** Go (stdlib only for gen tool: go/ast, go/parser, text/template, embed), git, gh CLI

---

### Task 1: Git Setup -- Preserve Fork Divergence and Sync Main

**Files:**
- No file changes -- git operations only.

**Step 1: Fetch latest from both remotes**

Run: `git fetch upstream && git fetch origin`

**Step 2: Switch to main branch**

Run: `git checkout main`

**Step 3: Create preservation branch from current origin/main**

This saves the 3 fork-only commits (slice tests, predicate tests, go.mod change).

Run: `git checkout -b fork-main-pre-sync origin/main`
Run: `git push -u origin fork-main-pre-sync`

**Step 4: Reset local main to upstream/main**

Run: `git checkout main`
Run: `git reset --hard upstream/main`
Run: `git push --force origin main`

Expected: Fork's main now matches upstream's main exactly.

**Step 5: Create feature branch from clean main**

Run: `git checkout -b feature/add-modusgraphgen`

Expected: New branch at the same commit as upstream/main.

---

### Task 2: Create Directory Structure

**Files:**
- Create: `cmd/modusgraphgen/main.go` (placeholder)
- Create: `cmd/modusgraphgen/internal/model/` (directory)
- Create: `cmd/modusgraphgen/internal/parser/` (directory)
- Create: `cmd/modusgraphgen/internal/parser/testdata/movies/` (directory)
- Create: `cmd/modusgraphgen/internal/generator/` (directory)
- Create: `cmd/modusgraphgen/internal/generator/templates/` (directory)
- Create: `cmd/modusgraphgen/internal/generator/testdata/golden/` (directory)

**Step 1: Create all directories**

Run:
```bash
mkdir -p cmd/modusgraphgen/internal/model
mkdir -p cmd/modusgraphgen/internal/parser/testdata/movies
mkdir -p cmd/modusgraphgen/internal/generator/templates
mkdir -p cmd/modusgraphgen/internal/generator/testdata/golden
```

---

### Task 3: Copy and Adapt model Package

**Files:**
- Create: `cmd/modusgraphgen/internal/model/model.go`

**Step 1: Copy model.go**

Copy from `../modusGraphGen/model/model.go`. The model package has no imports to rewrite -- it's pure Go types with no external dependencies. The file is identical to the source.

Run: `cp ../modusGraphGen/model/model.go cmd/modusgraphgen/internal/model/model.go`

**Step 2: Verify no import changes needed**

The model package only declares types (`Package`, `Entity`, `Field`). It has no imports at all. No changes needed.

---

### Task 4: Copy and Adapt parser Package

**Files:**
- Create: `cmd/modusgraphgen/internal/parser/parser.go`
- Create: `cmd/modusgraphgen/internal/parser/inference.go`
- Create: `cmd/modusgraphgen/internal/parser/parser_test.go`

**Step 1: Copy parser.go and rewrite imports**

Copy from `../modusGraphGen/parser/parser.go`. Change the import:
- FROM: `"github.com/mlwelles/modusGraphGen/model"`
- TO: `"github.com/matthewmcneely/modusgraph/cmd/modusgraphgen/internal/model"`

Run: `cp ../modusGraphGen/parser/parser.go cmd/modusgraphgen/internal/parser/parser.go`

Then edit the import path in the copied file.

**Step 2: Copy inference.go and rewrite imports**

Copy from `../modusGraphGen/parser/inference.go`. Same import change.

Run: `cp ../modusGraphGen/parser/inference.go cmd/modusgraphgen/internal/parser/inference.go`

Then edit the import path.

**Step 3: Copy parser_test.go and rewrite imports + test paths**

Copy from `../modusGraphGen/parser/parser_test.go`. Changes needed:
- Import: `"github.com/mlwelles/modusGraphGen/model"` → `"github.com/matthewmcneely/modusgraph/cmd/modusgraphgen/internal/model"`
- Replace `moviesDir()` function to point to local testdata instead of sibling project:

The `moviesDir` function currently uses `runtime.Caller(0)` to navigate to `../../modusGraphMoviesProject/movies/`. Replace it with:

```go
func moviesDir(t *testing.T) string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	return filepath.Join(filepath.Dir(thisFile), "testdata", "movies")
}
```

- Update `TestReadModulePath/FromModusGraphGen` to expect `"github.com/matthewmcneely/modusgraph"` (the host module) instead of `"github.com/mlwelles/modusGraphGen"`. Note: `readModulePath` walks up from the test file directory to find go.mod, so it will now find the modusGraph go.mod.

- Update `TestReadModulePath/FromMoviesProject` -- the movies testdata directory won't have its own go.mod, so we need to add a minimal go.mod to the testdata directory. Create `cmd/modusgraphgen/internal/parser/testdata/movies/go.mod` with:
```
module github.com/mlwelles/modusGraphMoviesProject

go 1.25.6
```

- Update `TestModulePathPopulated` to expect `"github.com/mlwelles/modusGraphMoviesProject"` (from the testdata go.mod).

- Update `TestCollectImports` -- no change needed (still tests for "time" import).

---

### Task 5: Copy Test Fixtures (Movies Struct Files)

**Files:**
- Create: `cmd/modusgraphgen/internal/parser/testdata/movies/actor.go`
- Create: `cmd/modusgraphgen/internal/parser/testdata/movies/film.go`
- Create: `cmd/modusgraphgen/internal/parser/testdata/movies/director.go`
- Create: `cmd/modusgraphgen/internal/parser/testdata/movies/genre.go`
- Create: `cmd/modusgraphgen/internal/parser/testdata/movies/country.go`
- Create: `cmd/modusgraphgen/internal/parser/testdata/movies/rating.go`
- Create: `cmd/modusgraphgen/internal/parser/testdata/movies/content_rating.go`
- Create: `cmd/modusgraphgen/internal/parser/testdata/movies/performance.go`
- Create: `cmd/modusgraphgen/internal/parser/testdata/movies/location.go`
- Create: `cmd/modusgraphgen/internal/parser/testdata/movies/generate.go`
- Create: `cmd/modusgraphgen/internal/parser/testdata/movies/go.mod`

**Step 1: Copy all struct files from modusGraphMoviesProject**

Run:
```bash
for f in actor.go film.go director.go genre.go country.go rating.go content_rating.go performance.go location.go generate.go; do
  cp ../modusGraphMoviesProject/movies/$f cmd/modusgraphgen/internal/parser/testdata/movies/$f
done
```

**Step 2: Update generate.go directive**

Change the `go:generate` line in `cmd/modusgraphgen/internal/parser/testdata/movies/generate.go` from:
```go
//go:generate go run github.com/mlwelles/modusGraphGen
```
to:
```go
//go:generate go run github.com/matthewmcneely/modusgraph/cmd/modusgraphgen
```

**Step 3: Create minimal go.mod for testdata**

Create `cmd/modusgraphgen/internal/parser/testdata/movies/go.mod`:
```
module github.com/mlwelles/modusGraphMoviesProject

go 1.25.6
```

This allows `readModulePath()` to resolve the module path in tests.

---

### Task 6: Copy and Adapt generator Package

**Files:**
- Create: `cmd/modusgraphgen/internal/generator/generator.go`
- Create: `cmd/modusgraphgen/internal/generator/generator_test.go`
- Create: `cmd/modusgraphgen/internal/generator/templates/*.tmpl` (7 files)
- Create: `cmd/modusgraphgen/internal/generator/testdata/golden/*.go` (30 files)

**Step 1: Copy generator.go and rewrite imports**

Copy from `../modusGraphGen/generator/generator.go`. Change:
- `"github.com/mlwelles/modusGraphGen/model"` → `"github.com/matthewmcneely/modusgraph/cmd/modusgraphgen/internal/model"`

Run: `cp ../modusGraphGen/generator/generator.go cmd/modusgraphgen/internal/generator/generator.go`

Then edit the import.

**Step 2: Copy generator_test.go and rewrite imports + paths**

Copy from `../modusGraphGen/generator/generator_test.go`. Changes:
- `"github.com/mlwelles/modusGraphGen/model"` → `"github.com/matthewmcneely/modusgraph/cmd/modusgraphgen/internal/model"`
- `"github.com/mlwelles/modusGraphGen/parser"` → `"github.com/matthewmcneely/modusgraph/cmd/modusgraphgen/internal/parser"`

Replace `moviesDir()` to point to the parser's testdata (shared fixtures):

```go
func moviesDir(t *testing.T) string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	// thisFile = .../generator/generator_test.go
	// testdata is at .../parser/testdata/movies/
	genDir := filepath.Dir(thisFile)
	return filepath.Join(filepath.Dir(genDir), "parser", "testdata", "movies")
}
```

Run: `cp ../modusGraphGen/generator/generator_test.go cmd/modusgraphgen/internal/generator/generator_test.go`

Then edit imports and moviesDir.

**Step 3: Copy all template files**

Run:
```bash
cp ../modusGraphGen/generator/templates/*.tmpl cmd/modusgraphgen/internal/generator/templates/
```

Templates reference `github.com/matthewmcneely/modusgraph` in generated output -- no changes needed.

**Step 4: Copy all golden files**

Run:
```bash
cp ../modusGraphGen/generator/testdata/golden/*.go cmd/modusgraphgen/internal/generator/testdata/golden/
```

Golden files are generated output snapshots -- no changes needed.

---

### Task 7: Copy and Adapt main.go Entry Point

**Files:**
- Create: `cmd/modusgraphgen/main.go`

**Step 1: Copy main.go and rewrite imports**

Copy from `../modusGraphGen/main.go`. Changes:
- `"github.com/mlwelles/modusGraphGen/generator"` → `"github.com/matthewmcneely/modusgraph/cmd/modusgraphgen/internal/generator"`
- `"github.com/mlwelles/modusGraphGen/parser"` → `"github.com/matthewmcneely/modusgraph/cmd/modusgraphgen/internal/parser"`
- Update the doc comment: `go run github.com/mlwelles/modusGraphGen` → `go run github.com/matthewmcneely/modusgraph/cmd/modusgraphgen`

Run: `cp ../modusGraphGen/main.go cmd/modusgraphgen/main.go`

Then edit imports and doc comment.

---

### Task 8: Run Tests

**Step 1: Run parser tests**

Run: `go test -v ./cmd/modusgraphgen/internal/parser/...`

Expected: All tests pass including entity detection, field parsing, tag parsing, module path resolution.

**Step 2: Run generator tests**

Run: `go test -v ./cmd/modusgraphgen/internal/generator/...`

Expected: All golden file comparisons pass, output file checks pass, snake_case conversion tests pass.

**Step 3: Run the full modusGraph test suite to check for regressions**

Run: `go test -short -race -v .`

Expected: All existing tests still pass.

**Step 4: Verify the gen tool builds**

Run: `go build ./cmd/modusgraphgen/`

Expected: Clean build, no errors.

---

### Task 9: Update README

**Files:**
- Modify: `README.md` (add Code Generation section before Limitations)

**Step 1: Add Code Generation section**

Insert a new section before the "Limitations" section (around line 630). The section should cover:

```markdown
## Code Generation

modusGraph includes a code generation tool that reads your Go structs and produces a fully typed
client library with CRUD operations, query builders, auto-paging iterators, functional options, and
an optional CLI.

### Installation

```sh
go install github.com/matthewmcneely/modusgraph/cmd/modusgraphgen@latest
```

### Usage

Add a `go:generate` directive to your package:

```go
//go:generate go run github.com/matthewmcneely/modusgraph/cmd/modusgraphgen
```

Then run:

```sh
go generate ./...
```

### What Gets Generated

| Template | Output | Scope |
|----------|--------|-------|
| client | `client_gen.go` | Once -- typed `Client` with sub-clients per entity |
| page_options | `page_options_gen.go` | Once -- `First(n)` and `Offset(n)` pagination |
| iter | `iter_gen.go` | Once -- auto-paging `SearchIter` and `ListIter` |
| entity | `<entity>_gen.go` | Per entity -- `Get`, `Add`, `Update`, `Delete`, `Search`, `List` |
| options | `<entity>_options_gen.go` | Per entity -- functional options for each scalar field |
| query | `<entity>_query_gen.go` | Per entity -- fluent query builder |
| cli | `cmd/<pkg>/main.go` | Once -- Kong CLI with subcommands per entity |

### Flags

| Flag | Default | Description |
|------|---------|-------------|
| `-pkg` | `.` | Path to the target Go package directory |
| `-output` | same as `-pkg` | Output directory for generated files |
| `-cli-dir` | `{output}/cmd/{package}` | Output directory for CLI main.go |
| `-cli-name` | package name | Name for CLI binary |
| `-with-validator` | `false` | Enable struct validation in generated CLI |

### Entity Detection

A struct is recognized as an entity when it has both of these fields:

```go
UID   string   `json:"uid,omitempty"`
DType []string `json:"dgraph.type,omitempty"`
```

All other exported fields with `json` and optional `dgraph` struct tags are parsed as entity fields.
Edge relationships are detected when a field type is `[]OtherEntity` where `OtherEntity` is another
struct in the same package. See the [Defining Your Graph with Structs](#defining-your-graph-with-structs)
section above for the full struct tag reference.
```

---

### Task 10: Commit and Push

**Step 1: Stage all new and modified files**

Run: `git add cmd/modusgraphgen/ README.md docs/plans/`

**Step 2: Commit**

Run:
```bash
git commit -m "feat: add modusgraphgen code generator

Merge the standalone modusGraphGen code generator into the modusGraph
repository as cmd/modusgraphgen/. The tool parses Go structs with
json/dgraph tags and generates typed CRUD clients, query builders,
auto-paging iterators, functional options, and a Kong CLI.

Code is organized under cmd/modusgraphgen/internal/ with model, parser,
and generator packages. Test fixtures are self-contained in testdata/.

The generated code imports github.com/matthewmcneely/modusgraph and
requires no additional dependencies beyond the Go standard library."
```

**Step 3: Push feature branch**

Run: `git push -u origin feature/add-modusgraphgen`

---

### Task 11: Open PR to Upstream

**Step 1: Create PR to upstream**

Run:
```bash
gh pr create \
  --repo matthewmcneely/modusgraph \
  --base main \
  --head mlwelles:feature/add-modusgraphgen \
  --title "feat: add modusgraphgen code generator" \
  --body "$(cat <<'EOF'
## Summary

- Adds `cmd/modusgraphgen/`, a code generation tool that reads Go structs with `json`/`dgraph` tags and produces typed CRUD clients, query builders, auto-paging iterators, functional options, and a Kong CLI
- Zero new dependencies -- the generator uses only the Go standard library (`go/ast`, `go/parser`, `text/template`, `embed`)
- Code organized under `cmd/modusgraphgen/internal/` with `model`, `parser`, and `generator` packages
- Self-contained test fixtures in `testdata/` with golden file regression tests
- Adds "Code Generation" section to README

## Usage

```go
//go:generate go run github.com/matthewmcneely/modusgraph/cmd/modusgraphgen
```

Or install directly:

```sh
go install github.com/matthewmcneely/modusgraph/cmd/modusgraphgen@latest
```
EOF
)"
```

---

### Task 12: Open PR to Fork

**Step 1: Create PR to fork's main**

Run:
```bash
gh pr create \
  --repo mlwelles/modusGraph \
  --base main \
  --head feature/add-modusgraphgen \
  --title "feat: add modusgraphgen code generator" \
  --body "$(cat <<'EOF'
## Summary

- Adds `cmd/modusgraphgen/`, a code generation tool that reads Go structs with `json`/`dgraph` tags and produces typed CRUD clients, query builders, auto-paging iterators, functional options, and a Kong CLI
- Zero new dependencies -- the generator uses only the Go standard library
- Self-contained tests with golden file regression testing
- Adds "Code Generation" section to README

Mirror of PR opened to upstream (matthewmcneely/modusgraph). Merging here to use while awaiting upstream review.
EOF
)"
```
