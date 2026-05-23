---
date: 2026-05-22
topic: merge-generated-files
status: draft
---

# One Generated File Per Entity

## Goal

Reduce the number of `_gen.go` files `modusgraph-gen` writes into a package.
Today the generator emits **five files per entity** — `<entity>_gen.go` (the
wrapper struct), `<entity>_accessors_gen.go`, `<entity>_options_gen.go`,
`<entity>_client_gen.go`, and `<entity>_query_gen.go` — plus three
package-level files. For the 10-entity `movies` fixture that is **53 files**,
and the per-entity directory is hard to scan.

Collapse the five per-entity templates into a single `<entity>_gen.go` that
tells the whole story of one entity: struct, accessors, options, client, and
query, under one `package` declaration and one `import` block. The `movies`
fixture drops from **53 files to 13** (10 × `<entity>_gen.go`, plus
`client_gen.go`, `schema/client_gen.go`, `schema/marker_gen.go`).

## Non-Goals

- **An opt-in/opt-out flag.** The merged layout is the only layout. Generated
  files carry a `DO NOT EDIT` header and are fully overwritten on every run, so
  the layout is a low-risk part of the generator's contract. Supporting two
  layouts indefinitely is real, permanent complexity for a cosmetic feature.
- **Merging the package-level files.** `client_gen.go` (the wrapper-side
  aggregate `Client`) is package-level — it lists every entity and belongs to
  no single one — so it stays its own file. `schema/client_gen.go` and
  `schema/marker_gen.go` live in a different directory and package; they cannot
  merge with entity files and are left untouched.
- **A generator-side cleanup of stale files.** Proactively deleting `*_gen.go`
  files before emitting is a useful feature but a separate, riskier one. The
  one-time orphan deletion this change requires (see Migration) is done by
  hand, once.
- **Any change to generated code's behavior or API.** The merged file contains
  byte-identical declarations to today's five files; only the file boundaries
  and the import block change.

## Why This Approach

Two decisions were settled during brainstorming.

**Granularity: one file per entity.** The unit a developer navigates an
ORM-style codebase by is "the entity." One file per concern (all accessors
together, all queries together) would be fewer files still, but unrelated
entities would churn together on every regeneration. One file per package
(~2800 lines, one massive diff per run) is too coarse. One file per entity
keeps each regeneration diff scoped to the entity that changed.

**Mechanism: header-less template fragments, not AST merging.** The rejected
alternative rendered all five templates as today (each a complete file) and
merged the results with `go/parser`/`go/ast`. That keeps templates untouched
but leaves a permanent AST-merge layer — a magnet for subtle breakage
(comment attachment, build constraints, blank-line handling) every time a
template grows. Instead, the five per-entity templates become **header-less
fragments**: each emits only declarations. The generator owns the `package`
line and the unified `import` block. More template editing now; nothing extra
to maintain afterward.

## Design

### The template contract

Templates split into two kinds:

- **Fragment templates** — `entity.go.tmpl`, `accessors.go.tmpl`,
  `options.go.tmpl`, `wrapper_entity_client.go.tmpl`, `wrapper_query.go.tmpl`.
  Each loses its leading `package {{ .EntityPackageName }}` line and its
  `import ( ... )` block, emitting only declarations. The conditional
  body logic (`{{- range $multiEdges }}`, etc.) is unchanged.
- **Standalone templates** — `wrapper_client.go.tmpl`, `schema_client.go.tmpl`,
  `schema_marker.go.tmpl`, `cli.go.tmpl`. Each produces one whole file and
  keeps its own `package`/`import` header. They have nothing to compose with;
  editing them would be refactor for its own sake.

The contract: *templates that compose are fragments; templates that stand
alone own their file.*

### The merge rule

Per-entity fragment outputs are grouped by **output directory**. Each
`(directory, entity)` group is concatenated into one `<entity>_gen.go` in that
directory. Fragment bodies are emitted in a fixed order: entity, accessors,
options, client, query.

In the **default layout** (`entity-client-dir` defaults to `entity-dir`) all
five fragments share one directory, so each entity yields exactly one file. In
a **split layout** (`--entity-client-dir` set to a separate directory) the
entity/accessors/options fragments form `<entity>_gen.go` in `entity-dir` and
the client/query fragments form `<entity>_gen.go` in `entity-client-dir` — two
files per entity, still far fewer than five. Grouping by directory makes the
rule correct for every flag combination without special-casing.

### Generator changes (`internal/generator/generator.go`)

`executeAndWrite` splits into two reusable pieces:

- `render(tmpl, name, data) (string, error)` — executes a template and returns
  the body, writing nothing.
- `writeGoFile(path, content) error` — prepends the `DO NOT EDIT` header, runs
  `format.Source`, writes the file (today's gofmt-plus-`.broken`-on-error
  logic, factored out).

The per-entity loop renders the fragments it needs for each directory group,
assembles one buffer — `package <name>` + the unified import block + the
ordered fragment bodies — and passes it to `writeGoFile` as `<entity>_gen.go`.
Standalone templates keep going through `render` → `writeGoFile` directly,
emitting their own headers.

### Import unification

Each fragment template has a Go-side **import contribution** — the set of
imports its body needs for a given entity. A merged file's import block is the
**union** of the contributions of the fragments that went into it. Computing
the union per group (rather than one fixed "entity-file imports" set) keeps
both the default and split layouts correct.

The contributions, lifted verbatim from the templates' existing
`$needsTime`/`$needsSlices`/`$extImports` guards:

| Fragment | Imports | Condition |
|----------|---------|-----------|
| `entity` | `typed`, schema path | always |
| `accessors` | `iter` | `len(allMultiEdgeFields) > 0` |
| | `slices` | `len(allMultiEdgeFields) > 0 \|\| len(allSliceFields) > 0` |
| | `time` | any non-edge field with `time.` in `GoType` |
| | `typed`, schema path | any edge field present |
| | external (`enums`, `scalars`, …) | `externalImports` over scalar fields |
| `options` | `typed` | any non-UID/DType scalar field present |
| | `time` | any non-slice scalar field with `time.` in `GoType` |
| | external | `externalImports` over non-slice scalar fields |
| `client` | `context`, `modusgraph`, `typed`, schema path | always |
| `query` | `iter`, `typed`, schema path | always |

In the default layout the union is therefore always `context`, `iter`,
`modusgraph`, `typed`, and the schema path — because `client` and `query` are
emitted for every entity and unconditionally reference all five. `slices`,
`time`, and external imports are the only genuinely conditional members. The
import block is emitted as gofmt-style groups (stdlib / `modusgraph` / schema +
external); the final `format.Source` pass sorts within each group.

**Correctness note.** `format.Source` sorts and groups imports but never
removes unused ones, and an unused import is a Go compile error. The
contributions above must be exact. They are a faithful port of guards the
templates already encode, and the regenerated `movies` fixture is compiled by
`go test ./...`, so any drift fails the build immediately.

## File-Count Outcome

For the 10-entity `movies` fixture, default layout:

| | Before | After |
|--|--------|-------|
| Per-entity files | 50 (5 × 10) | 10 (1 × 10) |
| `client_gen.go` (wrapper) | 1 | 1 |
| `schema/client_gen.go` | 1 | 1 |
| `schema/marker_gen.go` | 1 | 1 |
| **Total** | **53** | **13** |

## Migration

When an entity goes from five files to one, the generator does not overwrite
the four now-orphaned files (`<entity>_accessors_gen.go`,
`<entity>_options_gen.go`, `<entity>_client_gen.go`,
`<entity>_query_gen.go`) — it simply stops emitting them. This change must
therefore, as a one-time step:

1. Regenerate every in-repo generated package (the `movies` fixture, and any
   `examples/` that run the generator).
2. `git rm` the orphaned per-entity files.

Subsequent regenerations are stable: the generator only ever emits
`<entity>_gen.go` and `client_gen.go`, so nothing is left stale.

## Testing

- **Build + behavioral.** The `movies` fixture under
  `internal/parser/testdata/movies/` is compiled and exercised by
  `go test ./...`. After regeneration, the existing e2e tests
  (`unwrap_e2e_test.go`, `wrapper_query_e2e_test.go`) pass unchanged — proof
  the merged file is byte-equivalent in behavior.
- **Import-contribution unit tests.** Each fragment's import-contribution
  function gets direct table tests: an entity with multi-edges pulls `iter` +
  `slices`; a `time.Time` field pulls `time`; an entity with only `UID`/`DType`
  pulls only the always-set. This locks the conditions that, if wrong, would
  produce an uncompilable unused/missing import.
- **Merge unit test.** `TestGenerate` (generator package) asserts the merged
  output: one `<entity>_gen.go` per entity, exactly one `package` declaration
  and one `import` block, and all expected declarations present.
- **Split-layout test.** A generator test with `EntityDir != EntityClientDir`
  asserts two files per entity with correctly partitioned import unions.

## Risks

- **Unused/missing import in the merged file.** Mitigated by the contribution
  unit tests and the fixture compile; the failure is loud and immediate.
- **One-time fixture churn.** Regenerating the `movies` fixture is a large,
  reviewable, one-time diff — expected and harmless for `DO NOT EDIT` files.
- **External filename references.** If any CI check, `.gitignore` glob, or doc
  links a specific generated filename, it breaks. A repo grep for
  `_accessors_gen`, `_options_gen`, `_client_gen`, `_query_gen` during
  implementation confirms there are none beyond the files themselves.
