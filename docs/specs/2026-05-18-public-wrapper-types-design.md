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
  studio_options_gen.go              StudioOption + WithStudio<Field>
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
want raw struct access via `Unwrap()`.

The `schema/` package is plain (not `internal/`) so the `*schema.Studio`
returned by `Unwrap()` is nameable by external callers.

### Vocabulary

| Concept | Name | Notes |
|---|---|---|
| User-edited source-of-truth struct | `schema.Studio` | public fields, `json`/`dgraph` tags |
| Generated wrapper exposing methods | `movies.Studio` | the consumer-facing type |
| Wrapper's private backing field | `s *schema.Studio` | terse, never accessed outside generated methods |
| Empty-wrapper constructor | `NewStudio(opts ...StudioOption) *Studio` | allocates a fresh empty `schema.Studio` inside, then applies options |
| Wrap-existing constructor | `WrapStudio(s *schema.Studio, opts ...StudioOption) *Studio` | wraps an existing `*schema.Studio`, then applies options |
| Wrapper escape hatch | `(e *Studio) Unwrap() *schema.Studio` | returns the backing schema struct; idiomatic match for `errors.Unwrap` |
| Type-name introspection (generated) | `(*schema.Studio) SchemaTypeName() string` | returns canonical entity name (e.g. `"Studio"`); satisfies `modusgraph.Schema` |
| Predicates introspection (generated) | `(*schema.Studio) SchemaPredicates() []string` | returns all predicate names (json tag values), excluding `uid`/`dgraph.type` |
| Search predicate introspection (generated) | `(*schema.Studio) SchemaSearchPredicate() string` | returns the predicate marked searchable, or `""` if none |
| modusgraph marker interface | `modusgraph.Schema` | identifies a value as a generated schema record; minimal — just `SchemaTypeName() string` |
| Field setter option family | `WithStudio<Field>(v) StudioOption` | one per public schema field, mechanically derived |
| Validation shim | `(e *Studio) Validate(ctx, v) error` | delegates to `v.StructCtx(ctx, e.s)` |

**Three vocabularies, three namespaces, no collisions:**

- **Lifecycle (Wrap / Unwrap)** — top-level constructor functions
  `NewStudio` and `WrapStudio`, and the wrapper's single escape-hatch
  method `Unwrap()`. These name the relationship between wrapper and
  inner schema struct, not any field. None of them use the `WithStudio<X>`
  prefix, so they're in a separate namespace from per-field setters.
  `Unwrap` matches the established Go idiom (`errors.Unwrap`).
- **Per-field setters (`WithStudio<Field>` option family)** —
  mechanically derived from each public schema field. Collisions within
  the family are impossible by construction (each field has a unique name);
  collisions with lifecycle functions are impossible because lifecycle
  functions don't use the `WithStudio*` prefix.
- **Schema introspection (`Schema*` methods on schema structs)** —
  `SchemaTypeName`, `SchemaPredicates`, `SchemaSearchPredicate`. Live on
  the schema struct, not the wrapper. The `Schema` prefix signals "metadata
  about the type itself", not "metadata about an instance". The set is
  designed to grow over time (candidates: `SchemaUniquePredicates()`,
  `SchemaIndexedPredicates()`) without changing the marker contract.
  The modusgraph marker interface `modusgraph.Schema` requires only
  `SchemaTypeName()`; the other methods are conventional additions that
  callers probe via ad-hoc anonymous interfaces.

Earlier drafts proposed `Record()` for the wrapper accessor and
`WithStudioRecord(s *schema.Studio)` for an installer option. That design
carried a real collision risk: a schema struct with a literal `Record`
field (a common noun) would generate `Record() string`, `SetRecord(v
string)`, and `WithStudioRecord(v string)`, all colliding with the
lifecycle method and option. Moving to two top-level constructors and the
`Unwrap` method eliminates the collision class entirely — no `With*` option
takes `*schema.Studio`, so no field name can ever shadow a lifecycle name.

**Reserved method names — generator collision guard.** The generator's
parse phase rejects schema fields whose generated accessor or setter name
would collide with any of these reserved methods:

- On wrappers: `Unwrap`, `MarshalJSON`, `UnmarshalJSON`, `Validate`, `UID`, `SetUID`, `DType`, `SetDType`.
- On schema structs: `SchemaTypeName`, `SchemaPredicates`, `SchemaSearchPredicate`.

The error names the offending field and tells the author to rename it. Better
loud at generate time than a silent shadow at compile time.

### Data Flow

```
Hand-edited schema.Studio  (public fields, validate/dgman tags)
        │
        │ parse phase reads source
        ▼
modusgraph-gen
        │
        │ emit phase writes wrapper + accessors + options + client + query
        │ + schema marker methods (SchemaTypeName, SchemaPredicates, etc.)
        ▼
movies.Studio { s *schema.Studio }    consumer code talks to this
        │
        │ typed StudioClient.Add/Update/Delete pass wrapper through;
        │ read-path StudioClient methods allocate wrappers from schema results
        ▼
modusgraph.Client.Insert(ctx, w)      w is *movies.Studio
        │
        │ modusgraph.Client unwraps: reflects on w to find Unwrap() method,
        │ confirms its return satisfies modusgraph.Schema marker, substitutes
        │ the inner *schema.Studio. Falls through transparently for callers
        │ that pass *schema.Studio (or any other type) directly.
        ▼
modusgraph.Client internal pipeline operates on *schema.Studio
        │
        ▼
upstream github.com/dolan-in/dgman/v2   no fork, no HasReflectable, no mirror structs
```

The unwrap is *additive*: existing modusgraph users who pass plain structs
hit the same code path they hit today. The reflection probe only fires when
the argument exposes an `Unwrap()` method whose return implements
`modusgraph.Schema`. See [modusGraph Package Changes](#modusgraph-package-changes).

## Wrapper API

The generator emits the same shape for every entity. Below uses `Studio` as
the canonical example.

### Wrapper struct, constructors, options

```go
package movies

// Studio wraps a schema.Studio and exposes its data through methods.
type Studio struct {
    s *schema.Studio
}

// StudioOption configures a *Studio. Used at construction time via NewStudio
// or WrapStudio, or in bulk later via ApplyStudioOptions.
type StudioOption func(*Studio)

// WithStudioName, WithStudioYearFounded, ... — one option per public field
// on schema.Studio, generated mechanically. Each calls the corresponding
// Set<Field> method on the wrapper.

// NewStudio constructs a *Studio with a fresh, empty schema.Studio inside,
// then applies the given options.
func NewStudio(opts ...StudioOption) *Studio {
    e := &Studio{s: &schema.Studio{}}
    for _, opt := range opts { opt(e) }
    return e
}

// WrapStudio constructs a *Studio backed by the given schema.Studio, then
// applies the given options. The wrapper holds s directly — no defensive
// copy. Mutations through Studio's setters write to s.
func WrapStudio(s *schema.Studio, opts ...StudioOption) *Studio {
    e := &Studio{s: s}
    for _, opt := range opts { opt(e) }
    return e
}

func ApplyStudioOptions(e *Studio, opts ...StudioOption) {
    for _, opt := range opts { opt(e) }
}
```

Two constructors, no installer option. `NewStudio` is for the empty case;
`WrapStudio` is for the wrap-existing case. Splitting these means there is
no `With*` option that takes `*schema.Studio` and therefore no risk of
colliding with a per-field setter for any field a schema author might name.

### Escape hatch

```go
// Unwrap returns the backing schema.Studio for direct field access. This is
// the only schema-touching method on the wrapper; modusgraph.Client uses
// reflection to call it when a wrapper is passed across the boundary.
func (e *Studio) Unwrap() *schema.Studio { return e.s }
```

One method, typed. The name `Unwrap` matches the established Go idiom
(`errors.Unwrap`) and avoids collision with any field name a schema author
might pick. No untyped counterpart: `modusgraph.Client` finds the method
via reflection and verifies its return via the `modusgraph.Schema` marker
interface, which the schema struct satisfies through its generated
`SchemaTypeName()` method.

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
evaluate the predicate. Callers wanting custom predicates reach `Unwrap()`
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

**Input/output discipline:** the typed client deals exclusively in
**wrappers** (`*movies.Studio`, `[]*movies.Studio`) for entity arguments
and returns. UIDs pass as `string`. Schema structs never appear in the
signature — callers who hold a `*schema.Studio` wrap it with
`WrapStudio(s)` first; callers who want the raw struct out call
`result.Unwrap()`.

```go
type StudioClient struct { conn modusgraph.Client }

// Get reads a single Studio by UID. Returns the wrapper.
func (c *StudioClient) Get(ctx context.Context, uid string) (*Studio, error) {
    var rec schema.Studio
    if err := c.conn.Get(ctx, &rec, uid); err != nil { return nil, err }
    return &Studio{s: &rec}, nil
}

// Add inserts a new Studio. The wrapper passes through to modusgraph.Client,
// which reflects on Unwrap() to substitute the inner *schema.Studio before
// invoking dgman.
func (c *StudioClient) Add(ctx context.Context, w *Studio) error {
    return c.conn.Insert(ctx, w)
}

// Update modifies an existing Studio identified by w.UID().
func (c *StudioClient) Update(ctx context.Context, w *Studio) error {
    return c.conn.Update(ctx, w)
}

// Upsert inserts the Studio if no node with a matching upsert predicate
// exists, otherwise updates it. predicates names the upsert predicate(s);
// if empty, modusgraph picks the first predicate tagged `dgraph:"upsert"`.
func (c *StudioClient) Upsert(ctx context.Context, w *Studio, predicates ...string) error {
    return c.conn.Upsert(ctx, w, predicates...)
}

// Delete removes the Studio with the given UID.
func (c *StudioClient) Delete(ctx context.Context, uid string) error {
    return c.conn.Delete(ctx, []string{uid})
}

// List returns Studios with optional pagination.
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

// Query returns a query builder for richer filtering, sorting, and pagination.
// The builder's terminal methods (Nodes, First, Single) return wrappers.
func (c *StudioClient) Query(ctx context.Context) *StudioQuery {
    return &StudioQuery{ /* ...existing query construction... */ }
}
```

The query builder type `StudioQuery` (existing today) follows the same
discipline: its terminal methods (`Nodes() ([]*Studio, error)`,
`First() (*Studio, error)`, etc.) return wrappers. Internally it
queries against `[]schema.Studio` and wraps each result on return.

Write paths (`Add`, `Update`, `Upsert`) pass the wrapper through directly;
modusgraph.Client's reflection-based unwrap probes for the `Unwrap()`
method and substitutes the inner `*schema.Studio` before processing — see
[modusGraph Package Changes](#modusgraph-package-changes). Read paths
(`Get`, `List`, `Query.Nodes`) still wrap explicitly because `conn.Get`
and `q.Nodes` fill structs they own.

**The directionality at a glance:**

```
*schema.Studio  ──── WrapStudio(s) ────►  *movies.Studio  ──── Unwrap() ────►  *schema.Studio
                                              ▲     │
                                              │     │  the typed client lives here:
                                              │     │  StudioClient.Add/Update/Upsert/Get/List
                                              │     ▼  StudioQuery.Nodes/First/Single
```

Schema-struct paths exist for native interop (loading from elsewhere,
passing to raw `modusgraph.Client`, JSON unmarshalling). The moment a
caller wants the method-driven API, they wrap.

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

    // Empty wrapper + per-field options:
    pixar := movies.NewStudio(
        movies.WithStudioName("Pixar"),
        movies.WithStudioYearFounded(1986),
    )
    _ = studios.Add(ctx, pixar)

    s, _ := studios.Get(ctx, pixar.UID())
    for _, f := range s.Films() {
        fmt.Println(f.Title())
    }

    // Drop into schema-land for native field access via Unwrap.
    rec := s.Unwrap()
    rec.Name = "Pixar Animation Studios"
    _ = studios.Update(ctx, s)

    // Wrap an existing schema struct (e.g. one you unmarshalled, loaded from
    // a file, or constructed elsewhere) and use the wrapper's method API:
    raw := &schema.Studio{Name: "Disney", YearFounded: 1923}
    disney := movies.WrapStudio(raw)
    disney.SetActive(true)
    _ = studios.Add(ctx, disney)

    // Raw modusgraph.Client also works — reflection on Unwrap() substitutes
    // the inner *schema.Studio automatically.
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

All modusGraph-side changes are **additive**. Existing users — including
users who have written their own structs and pass them to `modusgraph.Client`
without using `modusgraph-gen` — see no signature changes, no removed
interfaces, and no behavior changes for the inputs they pass today. New
behavior fires only when an input opts into it via the new `Schema`
interface or the conventional `Unwrap()` method.

### Additions

#### `Schema` interface

```go
// Schema identifies a value as a record of a generated schema-defining type.
// It is implemented by every modusgraph-gen-emitted schema struct, via the
// generated SchemaTypeName method that lives in <schema-pkg>/marker_gen.go.
//
// Plain user structs (not emitted by modusgraph-gen) do not implement Schema
// and are unaffected — modusgraph.Client falls through to its existing
// handling for them.
type Schema interface {
    SchemaTypeName() string
}
```

The interface is intentionally minimal — a single method, returning a useful
piece of metadata (the canonical entity name, e.g. `"Studio"`). The schema
struct may also expose additional conventional methods (`SchemaPredicates()
[]string`, `SchemaSearchPredicate() string`) — see [Generator Changes](#generator-changes).
Those aren't part of the `Schema` contract; callers that want them probe
for them via ad-hoc anonymous interfaces.

#### `unwrapSchema` helper, called at the top of mutation/query methods

```go
// unwrapSchema returns the schema-defining record contained in obj. If obj
// is already a Schema, it is returned as-is. If obj exposes an Unwrap()
// method whose return value satisfies Schema, that return is substituted.
// Otherwise obj is returned unchanged.
//
// This is the bridge between modusgraph-gen-emitted wrapper types and the
// rest of modusgraph.Client. It is purely additive: types that don't
// implement Schema and don't have an Unwrap() method (i.e. existing
// modusgraph users' plain structs) pass through untouched.
func unwrapSchema(obj any) any {
    if obj == nil {
        return obj
    }
    if _, ok := obj.(Schema); ok {
        return obj
    }
    v := reflect.ValueOf(obj)
    if !v.IsValid() {
        return obj
    }
    m := v.MethodByName("Unwrap")
    if !m.IsValid() || m.Type().NumIn() != 0 || m.Type().NumOut() != 1 {
        return obj
    }
    inner := m.Call(nil)[0].Interface()
    if _, ok := inner.(Schema); ok {
        return inner
    }
    return obj
}
```

`unwrapSchema` is invoked at the top of each `Client` method that takes an
entity value (`Insert`, `InsertRaw`, `Update`, `Upsert`, `Get`, `Query`, and
any other method that ultimately reflects over a struct).

For methods that take `any` as a value parameter (Insert/Update/Upsert),
the substitution is straightforward: `obj = unwrapSchema(obj)`. For methods
like `Get(ctx, obj, uid)` where the caller passes a destination pointer to
be filled, `unwrapSchema` returns the inner `*schema.Studio` so dgman writes
the result into the schema struct that the wrapper already references. The
wrapper sees the populated data through its `Unwrap()` accessor afterwards.

Reflection cost: one `reflect.ValueOf` and one `MethodByName` lookup per
modusgraph.Client call site that uses `unwrapSchema`. Negligible compared
to the network/disk I/O these methods perform. Not on any tight inner loop.

Note on the `errors.Unwrap` overlap: Go's `errors` package uses `Unwrap()
error` as the standard "give me the wrapped thing" method. The `unwrapSchema`
helper does an additional check — the returned value must implement `Schema`
— so an `error` value with an `Unwrap()` method is not mistaken for a
modusgraph wrapper. The reflection probe simply falls through.

### Removals

| Symbol / file | Location | Reason |
|---|---|---|
| `replace github.com/dolan-in/dgman/v2 => github.com/mlwelles/dgman/v2 ...` | `go.mod` | Forked dgman only existed to host the `HasReflectable` hook for private-field machinery. modusGraph itself does not call into that hook; with codegen no longer producing `HasReflectable` implementations, upstream dgman is sufficient. |

That's the entire removal list inside the modusgraph package. Everything else
stays:

- `SelfValidator` interface stays. Even though codegen no longer emits
  `ValidateWith` methods, the interface remains a documented extension point.
  Any user who has implemented `ValidateWith` on their own type continues
  to dispatch through it.
- `StructValidator` interface stays (unchanged from today).
- `validateOne` SelfValidator dispatch branch stays.
- All Client/Engine/Embedded interfaces stay verbatim.
- All `*_test.go` tests for `SelfValidator` dispatch stay.

### Test cleanup

In `internal_test.go`, the only tests that go away are the ones bound to
the dropped dgman fork:

- Delete `privateFieldEntityReflectable` type and `ToReflectable` / `FromReflectable` methods on `privateFieldEntity`.
- Delete `TestHasReflectable` (line ~432).

The `privateFieldEntity` struct itself **stays**, with only its
`Reflectable`-related methods removed. Its `ValidateWith` method and the
tests that exercise SelfValidator dispatch (`TestSelfValidatorDispatch`,
`TestSelfValidatorWithCustomValidator`, `TestValidateOneDispatchesSelfValidator`)
remain in place — they continue to verify that user-implemented
`SelfValidator` types are dispatched correctly. Without them we'd have no
test coverage for the `SelfValidator` path that we're keeping.

Add new tests covering the additive `Record`-aware path:

- `*schema.Studio` (with `SchemaTypeName()` generated) implements `modusgraph.Schema` and is processed normally.
- `*movies.Studio` (wrapper, with `Unwrap() *schema.Studio`) is detected by `unwrapSchema` and the inner record is substituted.
- Plain user struct with no `Unwrap()` method and no `SchemaTypeName()` method is passed through unchanged — proves backward compatibility for non-codegen users.

### VALIDATOR.md

Update — don't rewrite. The doc currently describes `SelfValidator` as
*the* validation extension point. After this work it describes two equally
valid patterns:

1. **Tag your struct fields and pass `WithValidator(v)`** — the default,
   recommended for any new code. Works for hand-written structs and for
   modusgraph-gen-emitted schema structs alike.
2. **Implement `SelfValidator`** — the extension point for cases where you
   need custom validation logic the validator can't express via tags
   (cross-field rules, asynchronous checks, etc.). Unchanged from today.

The `ValidateWith` generation in codegen disappears (no longer needed since
public-fielded schemas validate natively), but the runtime interface and
dispatch stay available.

## Generator Changes

### Template inventory

| Template | Output | New role | Status |
|---|---|---|---|
| `entity.go.tmpl` | `<out>/<snake>_gen.go` | Wrapper struct, `NewStudio`, `WrapStudio`, `Unwrap()`, `Validate()`, `MarshalJSON`/`UnmarshalJSON`, `UID`/`SetUID`/`DType`/`SetDType`, typed `StudioClient`. | rewritten |
| `accessors.go.tmpl` | `<out>/<snake>_accessors_gen.go` | Per-field and per-edge methods on the wrapper that delegate to `e.s`, wrapping/unwrapping at edges. Always emitted (no longer gated on private fields). | rewritten |
| `options.go.tmpl` | `<out>/<snake>_options_gen.go` | `StudioOption`, `WithStudio<Field>` family, `ApplyStudioOptions`. The schema-installation case is handled by the `WrapStudio` constructor in `entity.go.tmpl`, not by an option, to avoid any chance of collision with a `WithStudio<X>` field setter. | minor update |
| `client.go.tmpl` | `<out>/client_gen.go` | Top-level `Client` factory and shared infrastructure. Per-entity client types now live in `entity.go.tmpl`. | minor update |
| `query.go.tmpl` | `<out>/<snake>_query_gen.go` | Query builder. Public signatures take/return wrappers; internals run against `schema.X`. Results wrapped on return. | rewritten |
| `iter.go.tmpl` | `<out>/iter_gen.go` | `iter.Seq2` helpers used by `<Edge>Seq` accessors. No change. | no change |
| `page_options.go.tmpl` | `<out>/page_options_gen.go` | Pagination types. No change. | no change |
| `cli.go.tmpl` | `<cli-dir>/main.go` | Builds `schema.X` directly from Kong flags; calls `conn.Insert(ctx, &record)`. Simpler than today. | minor update |
| **`schema_marker.go.tmpl`** (new) | `<schema-dir>/marker_gen.go` | Emits `SchemaTypeName() string`, `SchemaPredicates() []string`, `SchemaSearchPredicate() string` on each schema struct. Single file per schema package containing all entities' marker methods. | added |
| `marshal.go.tmpl` | — | Deleted. The private-field marshal mirror is no longer needed. | removed |

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
  `func NewStudio(opts ...StudioOption)`, `func WrapStudio(s *schema.Studio, opts ...StudioOption)`,
  `func (e *Studio) Unwrap() *schema.Studio`, `func (e *Studio) Validate(...)`,
  `func (e *Studio) MarshalJSON()`, edge accessor wrapping behavior,
  schema-side `SchemaTypeName/SchemaPredicates/SchemaSearchPredicate`
  presence, etc.

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
