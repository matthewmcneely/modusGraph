---
date: 2026-05-18
topic: public-wrapper-types
status: draft
---

# Generated Wrapper Types Over Public-Fielded Schema Structs

## Goal

Support interfaceable (method-based) generated types in modusGraph and
modusgraph-gen *without* serializing, querying, or marshalling private struct
fields. The hand-edited "schema" structs go back to plain Go: public fields,
ordinary `json`/`dgraph` tags. The generator emits wrapper types that hold a
schema struct in a private field and expose its data through methods.

This replaces the current approach, in which modusGraph and a forked dgman
were modified to serialize private fields via generated `ToReflectable` /
`FromReflectable` / mirror structs / custom `MarshalJSON` / `UnmarshalJSON` /
`ValidateWith` machinery.

## Non-Goals

- A deprecation window. The repo is on `feature/add-modusgraphgen` with no
  stable release of the private-field approach; the swap is atomic.
- A migration shim for downstream consumers (there are none).
- Performance benchmarking of wrap/unwrap allocations. We assume one
  wrapper allocation per result struct is acceptable.
- Bulk-read or batched-API additions to modusGraph.
- Replacing the prebuilt `query` and `modusgraphgen` binaries checked into
  the repo root.

## Why This Approach

Public-fielded schemas remove every reason for the private-field machinery to
exist. Vanilla `go-playground/validator`, vanilla `encoding/json`, and
upstream `dolan-in/dgman/v2` all process them natively. The wrapper layer
provides encapsulation at the *consumer-facing* surface, where it actually
matters, instead of at the persistence layer, where it forced a fork of
dgman and a substantial amount of generated glue.

## Architecture

### Layout

Two-package model per generated entity collection:

```
movies/                              parent package — generated, consumer-facing
  generate.go                        //go:generate directive (hand-edited, ~3 lines)
  client_gen.go                      typed Client
  iter_gen.go                        iter.Seq2 helpers
  page_options_gen.go                pagination types
  studio_gen.go                      wrapper: type Studio struct { s *schema.Studio }
  studio_accessors_gen.go            field/edge accessors delegating to e.s
  studio_options_gen.go              StudioOption + WithStudioRecord + WithStudio<Field>
  studio_query_gen.go                typed query builder
  film_gen.go
  film_*_gen.go
  ...
  studio_ext.go                      (optional) hand-written wrapper extensions
  schema/                            user-edited source of truth
    studio.go                        type Studio struct { Name string ...; Films []*Film ... }
    film.go
    doc.go                           (optional) package doc
```

Layout follows the `ent` ORM precedent: schemas in a `schema/` subpackage,
the consumer-facing API in the parent. Consumers `import "<path>/movies"`
and use `movies.Studio`; the `schema` package is reached only when callers
want raw struct access via `Record()`.

The `schema/` package is plain (not `internal/`) so the `*schema.Studio`
returned by `Record()` is nameable by external callers.

### Vocabulary

| Concept | Name | Notes |
|---|---|---|
| User-edited source-of-truth struct | `schema.Studio` | public fields, `json`/`dgraph` tags |
| Generated wrapper exposing methods | `movies.Studio` | the consumer-facing type |
| Wrapper's private backing field | `s *schema.Studio` | terse, never accessed outside generated methods |
| Installer option | `WithStudioRecord(s *schema.Studio) StudioOption` | installs an existing schema struct |
| Escape-hatch accessor (typed) | `(e *Studio) Record() *schema.Studio` | for native struct access |
| Hook accessor (untyped) | `(e *Studio) SchemaRecord() any` | satisfies `modusgraph.SchemaCarrier` |
| Field setter option family | `WithStudio<Field>(v) StudioOption` | matches existing generator pattern |
| Constructor | `NewStudio(opts ...StudioOption) *Studio` | option-pattern only |
| Validation shim | `(e *Studio) Validate(ctx, v) error` | delegates to `v.StructCtx(ctx, e.s)` |

The word "schema" denotes type *definitions* (the `schema` package, the
`schema.Studio` type, the `WithStudioRecord` parameter type). The word
"record" denotes a value of that type — the wrapper carries one
`*schema.Studio` "record". "Wrap"/"Record" describes the lifecycle relation;
"schema" describes the typed shape. The two words don't overlap in meaning.

### Data Flow

```
Hand-edited schema.Studio  (public fields, validate/dgman tags)
        │
        │ parse phase reads source
        ▼
modusgraph-gen
        │
        │ emit phase writes wrapper + accessors + options + client + query
        ▼
movies.Studio { s *schema.Studio }    consumer code talks to this
        │
        │ typed StudioClient.Add/Update/Delete pass wrapper through;
        │ read-path StudioClient methods allocate wrappers from schema results
        ▼
modusgraph.Client.Insert(ctx, w)      w is *movies.Studio (a SchemaCarrier)
        │
        │ SchemaCarrier hook detects wrapper, substitutes w.SchemaRecord()
        ▼
modusgraph.Client internal pipeline operates on *schema.Studio
        │
        ▼
upstream github.com/dolan-in/dgman/v2   no fork, no HasReflectable, no mirror structs
```

## Wrapper API

The generator emits the same shape for every entity. Below uses `Studio` as
the canonical example.

### Wrapper struct, constructor, options

```go
package movies

// Studio wraps a schema.Studio and exposes its data through methods.
type Studio struct {
    s *schema.Studio
}

// StudioOption configures a *Studio at construction time, or in bulk later
// via ApplyStudioOptions.
type StudioOption func(*Studio)

// WithStudioRecord installs an existing schema.Studio as this wrapper's
// backing record. If unset, NewStudio allocates a fresh empty schema.Studio.
func WithStudioRecord(s *schema.Studio) StudioOption {
    return func(e *Studio) { e.s = s }
}

// WithStudioName, WithStudioYearFounded, ... — one option per public field
// on schema.Studio, generated mechanically. Each calls the corresponding
// Set<Field> method on the wrapper.

// NewStudio constructs a *Studio. With no options it has an empty backing
// record; with options it applies them in order.
func NewStudio(opts ...StudioOption) *Studio {
    e := &Studio{s: &schema.Studio{}}
    for _, opt := range opts { opt(e) }
    return e
}

func ApplyStudioOptions(e *Studio, opts ...StudioOption) {
    for _, opt := range opts { opt(e) }
}
```

### Escape hatches

```go
// Record returns the backing schema.Studio for direct field access.
func (e *Studio) Record() *schema.Studio { return e.s }

// SchemaRecord returns the backing schema.Studio as any. It satisfies
// modusgraph.SchemaCarrier so wrappers can be passed to any modusgraph.Client
// method directly.
func (e *Studio) SchemaRecord() any { return e.s }
```

Two accessors with the same return value: `Record()` for consumers (typed
pointer, no assertion needed) and `SchemaRecord()` for the modusgraph hook
(untyped, satisfies the interface). The redundancy is two trivial lines per
entity.

### UID and DType

The schema's public bookkeeping fields get direct method pairs on the wrapper:

```go
func (e *Studio) UID() string         { return e.s.UID }
func (e *Studio) SetUID(v string)     { e.s.UID = v }
func (e *Studio) DType() []string     { return e.s.DType }
func (e *Studio) SetDType(v []string) { e.s.DType = v }
```

### Scalar field accessors

One Get/Set pair per public scalar field on the schema, delegating to `e.s`:

```go
func (e *Studio) Name() string         { return e.s.Name }
func (e *Studio) SetName(v string)     { e.s.Name = v }
func (e *Studio) YearFounded() int     { return e.s.YearFounded }
func (e *Studio) SetYearFounded(v int) { e.s.YearFounded = v }
// ...one pair per scalar field
```

### Singular edge accessors

Three sub-shapes depending on how the schema field is declared:

```go
// (1) Pointer-typed singular edge: schema.Studio has  Founder *schema.Director
func (e *Studio) Founder() *Director {
    if e.s.Founder == nil { return nil }
    return &Director{s: e.s.Founder}
}
func (e *Studio) SetFounder(v *Director) {
    if v == nil { e.s.Founder = nil; return }
    e.s.Founder = v.s
}

// (2) Value-typed singular edge: schema.Studio has  Headquarters schema.Country
//     The wrapper takes the address of the schema field, which is stable
//     (field-of-struct addresses don't move when the parent moves, modulo GC).
func (e *Studio) Headquarters() *Country {
    return &Country{s: &e.s.Headquarters}
}
func (e *Studio) SetHeadquarters(v *Country) {
    if v != nil { e.s.Headquarters = *v.s }
}

// (3) Singular-via-list edge: schema has  CurrentHead []schema.Director with validate:"max=1"
//     Treated as singular at the wrapper layer; slice is just storage shape.
func (e *Studio) CurrentHead() *Director {
    if len(e.s.CurrentHead) == 0 { return nil }
    return &Director{s: &e.s.CurrentHead[0]}
}
func (e *Studio) SetCurrentHead(v *Director) {
    if v == nil { e.s.CurrentHead = nil; return }
    e.s.CurrentHead = []schema.Director{*v.s}
}
```

### Multi-edge accessors

True multi-edges in the schema **must** be declared as `[]*schema.X` (pointer
slice). The generator rejects `[]schema.X` for multi-edges at parse time
with a clear error; see [Schema constraints](#schema-constraints).

```go
// schema: Films []*schema.Film
func (e *Studio) Films() []*Film {
    out := make([]*Film, len(e.s.Films))
    for i, f := range e.s.Films {
        out[i] = &Film{s: f}
    }
    return out
}

func (e *Studio) FilmSeq() iter.Seq[*Film] {
    return func(yield func(*Film) bool) {
        for _, f := range e.s.Films {
            if !yield(&Film{s: f}) { return }
        }
    }
}

func (e *Studio) SetFilms(films ...*Film) {
    e.s.Films = make([]*schema.Film, len(films))
    for i, f := range films {
        e.s.Films[i] = f.s
    }
}

func (e *Studio) AppendFilms(films ...*Film) {
    for _, f := range films {
        e.s.Films = append(e.s.Films, f.s)
    }
}

func (e *Studio) RemoveFilms(uids ...string) {
    e.s.Films = slices.DeleteFunc(e.s.Films, func(f *schema.Film) bool {
        return f != nil && slices.Contains(uids, f.UID)
    })
}
```

Predicate-based removal (`RemoveFilmsFunc(fn func(Film) bool)` in the
current generator) is dropped for multi-edges. The natural typing would be
`func(*movies.Film) bool`, which forces a wrapper allocation per element to
evaluate the predicate. Callers wanting custom predicates reach `Record()`
and filter on `[]*schema.Film` directly.

### Scalar slice accessors

Unchanged in shape since they don't involve wrappers:

```go
// schema: Tags []string
func (e *Studio) Tags() []string                      { return e.s.Tags }
func (e *Studio) SetTags(v []string)                  { e.s.Tags = v }
func (e *Studio) AppendTags(v ...string)              { e.s.Tags = append(e.s.Tags, v...) }
func (e *Studio) RemoveTags(v string)                 { /* first-occurrence delete */ }
func (e *Studio) RemoveTagsFunc(fn func(string) bool) { e.s.Tags = slices.DeleteFunc(e.s.Tags, fn) }
```

Predicate-based removal (`RemoveTagsFunc`) is fine for scalar slices: the
element type is primitive, no wrapping involved.

### Validation shim

```go
import "github.com/go-playground/validator/v10"

// Validate runs v against the backing schema.Studio. Validate tags on
// schema.Studio's exported fields are evaluated natively by the validator.
func (e *Studio) Validate(ctx context.Context, v *validator.Validate) error {
    return v.StructCtx(ctx, e.s)
}
```

### JSON marshaling

The wrapper has only a private field, so default `encoding/json` would emit
`{}`. The generator emits trivial delegating methods:

```go
func (e *Studio) MarshalJSON() ([]byte, error) {
    return json.Marshal(e.s)
}

func (e *Studio) UnmarshalJSON(data []byte) error {
    if e.s == nil { e.s = &schema.Studio{} }
    return json.Unmarshal(data, e.s)
}
```

`schema.Studio` itself needs no custom marshaling — its fields are public,
so `encoding/json` handles it directly.

### Typed client

```go
type StudioClient struct { conn modusgraph.Client }

func (c *StudioClient) Get(ctx context.Context, uid string) (*Studio, error) {
    var rec schema.Studio
    if err := c.conn.Get(ctx, &rec, uid); err != nil { return nil, err }
    return &Studio{s: &rec}, nil
}

func (c *StudioClient) Add(ctx context.Context, w *Studio) error {
    return c.conn.Insert(ctx, w)   // SchemaCarrier hook unwraps internally
}

func (c *StudioClient) Update(ctx context.Context, w *Studio) error {
    return c.conn.Update(ctx, w)   // hook unwraps
}

func (c *StudioClient) Delete(ctx context.Context, uid string) error {
    return c.conn.Delete(ctx, []string{uid})
}

func (c *StudioClient) List(ctx context.Context, opts ...PageOption) ([]*Studio, error) {
    var recs []schema.Studio
    // ...existing query construction unchanged
    if err := q.Nodes(&recs); err != nil { return nil, err }
    out := make([]*Studio, len(recs))
    for i := range recs {
        out[i] = &Studio{s: &recs[i]}
    }
    return out, nil
}
```

Write paths pass the wrapper through; the `SchemaCarrier` hook in
`modusgraph.Client` unwraps before doing any work. Read paths still wrap
explicitly because `conn.Get` and `q.Nodes` fill structs they own.

### Consumer code (the goal)

```go
package main

import (
    "context"
    "fmt"

    "example.com/proj/movies"
    "example.com/proj/movies/schema"
    "github.com/matthewmcneely/modusgraph"
)

func main() {
    ctx := context.Background()
    conn, _ := modusgraph.NewClient(...)
    studios := movies.NewStudioClient(conn)

    pixar := movies.NewStudio(
        movies.WithStudioName("Pixar"),
        movies.WithStudioYearFounded(1986),
    )
    _ = studios.Add(ctx, pixar)

    s, _ := studios.Get(ctx, pixar.UID())
    for _, f := range s.Films() {
        fmt.Println(f.Title())
    }

    // Drop into schema-land for native field access.
    rec := s.Record()
    rec.Name = "Pixar Animation Studios"
    _ = studios.Update(ctx, s)

    // Raw modusgraph.Client also works — SchemaCarrier hook unwraps.
    studio := movies.NewStudio(movies.WithStudioName("Another"))
    _ = conn.Insert(ctx, studio)
}
```

## Schema Constraints

Constraints enforced by the generator at parse time. Violations produce a
clear error that names the field and the rule.

### Multi-edges must use pointer slices

A field is a **multi-edge** when it has a slice element type that is itself
an entity, with no `validate:"max=1"` or `validate:"len=1"` constraint.

- Allowed: `Films []*schema.Film`
- Rejected: `Films []schema.Film`
  - Error: `multi-edge "Films" must use []*schema.Film (pointer slice); value-slice multi-edges are unsupported because wrapper pointer captures into the slice can be silently invalidated by AppendFilms or SetFilms`.

A slice with `validate:"max=1"` or `validate:"len=1"` is a *singular-via-list*
edge and is allowed in both value and pointer form. The wrapper accessor
treats these as singular (returning `*Director`, not `[]*Director`).

### Schema field exports

All fields the generator cares about must be exported (public). The
generator skips:

- Unexported fields (no synthesis, no error — they're just not part of the
  entity surface).
- Fields tagged `dgraph:"-"`.
- Fields with no `json` tag (matches today's behavior).

### `UID` and `DType` bookkeeping

Each schema entity must declare `UID string` and `DType []string` with the
standard tags:

```go
UID   string   `json:"uid,omitempty"`
DType []string `json:"dgraph.type,omitempty"`
```

These are required by dgman; the generator emits methods on top of them.

## modusGraph Package Changes

### Additions

```go
// SchemaCarrier is implemented by types that wrap a schema struct. When
// modusgraph.Client receives a SchemaCarrier as its target value, it
// operates on SchemaRecord() instead. Used by modusgraph-gen-generated
// wrapper types; useful for any user-written wrapper type as well.
type SchemaCarrier interface {
    SchemaRecord() any
}
```

Detection is a one-line check at the top of each `Client` method that takes
an entity value (`Insert`, `InsertRaw`, `Update`, `Upsert`, `Get`, `Query`,
and any other method that ultimately reflects over a struct):

```go
func unwrapSchemaCarrier(obj any) any {
    if sc, ok := obj.(SchemaCarrier); ok {
        return sc.SchemaRecord()
    }
    return obj
}
```

Implementation note: for methods that take `any` as a value parameter, the
unwrap is straightforward. For methods like `Get(ctx, obj, uid)` where the
caller passes a destination pointer to be filled, the hook can unwrap
similarly: if `obj` is a `*Studio` (wrapper) implementing `SchemaCarrier`,
the hook substitutes the inner `*schema.Studio` for population.

### Removals

| Symbol / file | Location | Reason |
|---|---|---|
| `SelfValidator` interface | `client.go:25-33` | Was the entity-side validation hook for private fields. Public-fielded schemas validate natively. |
| `validateOne` SelfValidator branch | `client.go:388-396` | The interface check goes away; the function simplifies to a one-liner: `return c.options.validator.StructCtx(ctx, iface)`. |
| Comment references to `SelfValidator` | `client.go` and elsewhere | Stale once the interface is gone. |

`StructValidator` (interface, `client.go:125-127`) stays. It's the type
constraint on the validator implementation passed to `WithValidator(v)`;
`*validator.Validate` satisfies it. This interface predates and is
independent of the private-field machinery.

### go.mod

Remove:

```
replace github.com/dolan-in/dgman/v2 => github.com/mlwelles/dgman/v2 v2.2.0-preview2.0.20260415160033-bc0b95f26417
```

Pin the `require` line to an upstream `dolan-in/dgman/v2` version that has
the API surface modusGraph relies on (everything outside the
`HasReflectable` hook, which modusGraph itself does not call). If upstream
lacks something we still need, surface that as a separate finding during
implementation.

### Test cleanup

In `internal_test.go`:

- Delete the `privateFieldEntity`, `customTagEntity`, and `privateFieldEntityReflectable` fixture types and their `ToReflectable` / `FromReflectable` / `ValidateWith` methods.
- Delete `TestHasReflectable` (line ~432).
- Delete `TestSelfValidatorDispatch` (line ~287), `TestSelfValidatorWithCustomValidator` (line ~352), and `TestValidateOneDispatchesSelfValidator` (line ~402).
- Keep any validator tests that exercise *public-fielded* structs against `WithValidator`. If none exist, add minimal coverage for the new flow: vanilla struct with validate tags, `WithValidator(*validator.Validate)` configured on the client, validation fires on Insert/Update.

### VALIDATOR.md

Rewrite to describe the new flow:

- Schema structs declare validate tags on their public fields.
- Callers pass `*validator.Validate` to the client via `modusgraph.WithValidator`.
- Validation fires automatically on Insert/Update; no entity-side hook required.

Remove all references to `SelfValidator` and `ValidateWith`.

## Generator Changes

### Template inventory

| Template | New role | Status |
|---|---|---|
| `entity.go.tmpl` → `<snake>_gen.go` | Emits wrapper struct, `NewStudio`, `Record()`, `SchemaRecord()`, `Validate()`, `MarshalJSON`/`UnmarshalJSON`, `UID`/`SetUID`/`DType`/`SetDType`, typed `StudioClient`. | rewritten |
| `accessors.go.tmpl` → `<snake>_accessors_gen.go` | Per-field and per-edge methods on the wrapper that delegate to `e.s`, wrapping/unwrapping at edges. Always emitted (no longer gated on private fields). | rewritten |
| `options.go.tmpl` → `<snake>_options_gen.go` | `StudioOption`, `WithStudioRecord`, `WithStudio<Field>`, `ApplyStudioOptions`. `WithStudioRecord` is new; per-field options largely unchanged. | minor update |
| `client.go.tmpl` → `client_gen.go` | Top-level `Client` factory and shared infrastructure. Per-entity client types now live in `entity.go.tmpl`. | minor update |
| `query.go.tmpl` → `<snake>_query_gen.go` | Query builder. Public signatures take/return wrappers; internals run against `schema.X`. Results wrapped on return. | rewritten |
| `iter.go.tmpl` → `iter_gen.go` | `iter.Seq2` helpers used by `<Edge>Seq` accessors. No change. | no change |
| `page_options.go.tmpl` → `page_options_gen.go` | Pagination types. No change. | no change |
| `cli.go.tmpl` → `cmd/<pkg>/main.go` | Builds `schema.X` directly from Kong flags; calls `conn.Insert(ctx, &record)`. Simpler than today. | minor update |
| `marshal.go.tmpl` | Deleted. The private-field marshal mirror is no longer needed. | removed |

### Generator flags

All optional, all with defaults that match an unflagged
`//go:generate go run github.com/matthewmcneely/modusgraph/cmd/modusgraph-gen`:

| Flag | Default | Meaning |
|---|---|---|
| `-schema-dir` | `./schema` | Path (relative to CWD) where schema source lives. |
| `-out` | `.` | Path (relative to CWD) where generated files are written. |
| `-schema-alias` | dir basename of `-schema-dir` (`schema`) | Import alias for the schema package in generated code. |
| `-pkg-name` | resolved from existing files in `-out`, then basename of `-out` | Package name to use in generated files (the `package X` declaration). |
| `-cli-dir` | `./cmd/<pkg>` | Output directory for the Kong CLI. Unchanged. |
| `-cli-name` | parent package name | Name for the CLI binary. Unchanged. |
| `-with-validator` | `false` | Enable validation in the generated CLI. Unchanged. |

The `//go:generate` directive lives in `<parent>/generate.go`. CWD when the
directive runs is the directory containing the file (the parent package),
so `./schema` resolves to `<parent>/schema`.

### Parser changes

`internal/parser/parser.go`:

- Accept a source directory (`-schema-dir`) distinct from the output directory.
- Resolve the schema package's import path so it can be referenced in
  generated code (`import "<modpath>/<parent>/schema"`).
- Determine *two* package names:
  - **Schema package name** — from parsing the source dir (e.g., `schema`).
    Used as the import alias in generated code (overridable via
    `-schema-alias`).
  - **Parent package name** — for the `package` declaration in generated
    files. Resolved in this order: (1) explicit `-pkg-name` flag if given,
    (2) the `package` declaration in any existing `.go` file in `-out` if
    one exists, (3) the basename of the absolute `-out` directory path as
    a final fallback.
- Reject value-slice multi-edges (the [Schema constraints](#schema-constraints)
  rule). Error message must name the field, name the entity, and tell the
  user exactly what to change.
- All other parsing logic (field types, edge detection, validate tag
  extraction, dgman tag handling, `dgraph:"-"` skipping) stays.

### Generator changes

`internal/generator/generator.go`:

- Remove the `hasPrivateFields` gating on `accessors.go.tmpl`. Always emit.
- Remove the `marshal.go.tmpl` emit step entirely.
- Add the schema package's import path to the template data so templates
  can emit correct imports.
- Adjust the per-entity emit loop to produce the new file set (no marshal
  file; accessors always emitted).

### Golden files

All `*_gen.go` and `*_marshal_gen.go` files under
`cmd/modusgraph-gen/internal/generator/testdata/golden/` are regenerated
from scratch by running the new generator against the migrated
`testdata/movies/` (see [Testdata migration](#testdata-migration)). The
marshal golden files are deleted permanently.

### Generator tests

In `internal/generator/generator_test.go`:

- Delete `TestGeneratedValidateWithMethod`, `TestReflectable*` sub-tests,
  and any test asserting on `ToReflectable`/`FromReflectable`/`Reflectable`
  struct presence.
- Replace with tests asserting on the new wrapper shape: presence of
  `func NewStudio(opts ...StudioOption)`, `func (e *Studio) Record()`,
  `func (e *Studio) SchemaRecord() any`, edge accessor wrapping behavior,
  `MarshalJSON` delegation, etc.

## Testdata Migration

Source files in `cmd/modusgraph-gen/internal/parser/testdata/movies/`
move into `testdata/movies/schema/` and convert to public fields.

Concrete migrations for `studio.go`:

| Before (private) | After (public, in `schema/`) |
|---|---|
| `name string` | `Name string` |
| `yearFounded int` | `YearFounded int` |
| `films []Film` (value slice, true multi-edge) | `Films []*Film` (pointer slice, Q8 rule) |
| `currentHead []Director` with `validate:"max=1"` | unchanged shape (singular-via-list, allowed) |
| `homeBase []Country` with `validate:"len=1"` | unchanged shape (singular-via-list, allowed) |
| `advisors []*Director` | unchanged (already pointer slice) |
| `tempCache string` (no json tag) | deleted (test fixture for skip behavior; no longer needed) |
| `Internal string ... dgraph:"-"` | kept (test fixture for `dgraph:"-"` skip behavior) |

Other entity files in `testdata/movies/` (`film.go`, `director.go`,
`country.go`, `actor.go`, `genre.go`, `rating.go`, `performance.go`,
`location.go`, `content_rating.go`): same treatment. Move to `schema/`,
uppercase fields, convert value-slice multi-edges to pointer slices, drop
test-only private-field fixtures that no longer exercise meaningful
generator behavior.

Add `testdata/movies/generate.go`:

```go
package movies

//go:generate go run github.com/matthewmcneely/modusgraph/cmd/modusgraph-gen
```

## Examples

Inspect `examples/basic`, `examples/load`, and `examples/readme`. Convert
each to public-fielded schemas only if it currently uses private fields.
If you want a new example explicitly demonstrating the wrapper API, add
`examples/wrapper/` (out of scope for this design unless requested).

## Breaking Change Posture

Single PR (or stacked PRs at preference) that swaps the model atomically.
No deprecation window, no side-by-side dual emission. The branch
`feature/add-modusgraphgen` has not shipped a stable release of the
private-field approach.

## Open Questions

None. All decisions are locked in.

## Implementation

Next step: invoke the `writing-plans` skill to produce a step-by-step
implementation plan against this spec.
