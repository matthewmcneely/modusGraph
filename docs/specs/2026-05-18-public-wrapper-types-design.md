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

Both packages host a full set of generated CRUD/query types. The schema
package exposes a *raw* client that operates on `*schema.Studio`; the
parent package exposes a *wrapper* client that operates on `*movies.Studio`.
Consumers pick the level of abstraction they want — same method names,
different return types.

```
movies/                              parent package — generated, wrapper-facing
  generate.go                        //go:generate directive (hand-edited, ~3 lines)
  client_gen.go                      top-level movies.Client factory
  iter_gen.go                        iter.Seq2 helpers
  page_options_gen.go                pagination types
  studio_gen.go                      wrapper: type Studio struct { s *schema.Studio }
                                     + movies.StudioClient (composes over schema.StudioClient)
  studio_accessors_gen.go            field/edge accessors delegating to e.s
  studio_options_gen.go              StudioOption + WithStudio<Field>
  studio_query_gen.go                wrapper-level query builder (wraps schema query results)
  film_gen.go
  film_*_gen.go
  ...
  studio_ext.go                      (optional) hand-written wrapper extensions
  schema/                            user-edited types + generated raw CRUD
    studio.go                        type Studio struct { Name string ...; Films []*Film ... }
    film.go
    doc.go                           (optional) package doc
    marker_gen.go                    SchemaTypeName/SchemaPredicates/SchemaSearchPredicate
                                     for every entity in this package
    client_gen.go                    top-level schema.Client factory + per-entity clients
    studio_client_gen.go             schema.StudioClient — operates on *schema.Studio directly
    studio_query_gen.go              schema-level query builder
    film_client_gen.go
    film_query_gen.go
    ...
```

The example above shows the **wrapper-parent layout**: schemas in a
`schema/` subpackage, wrappers in the parent. This follows the `ent` ORM
precedent and is the default when the `//go:generate` stub lives in the
parent of a `./schema/` directory. A **schema-local layout** is also
supported (and is the default when the stub lives alongside the schema
files themselves): schemas in CWD, wrappers in a sibling `./entity/`
subpackage. See [Generator flags](#generator-flags) for the detection
rule and an example. The architecture and vocabulary that follow apply
to both layouts — only the file locations and import paths differ.

Two same-named generated clients in two packages give consumers a choice:

- `schema.StudioClient` — raw client; methods take and return `*schema.Studio`.
  No wrapper allocation. Best for bulk processing, ETL, callers who never
  need the method-based wrapper API.
- `movies.StudioClient` — wrapper client; methods take and return
  `*movies.Studio`. Holds a `*schema.StudioClient` internally and adds a
  wrap/unwrap layer at the boundary. Best for code that wants the method
  API (validation, encapsulation, ergonomic field access).

The wrapper client's CRUD methods are tiny — a `WrapStudio(...)` on read
returns, a `w.s` deref on write. Single source of truth for actual logic
lives in the schema-level client.

**The schema package imports modusgraph.** This isn't ideal in a pure-types
sense, but the trade-off doesn't pay for itself here: schema types are
Dgraph-specific (json/dgraph tags, validation predicates), nobody is
porting them across stores, and any project using these types already has
modusgraph in its dependency tree from the parent package. Carrying a
dependency on modusgraph in `schema/` costs nothing realistic and lets the
schema package host its own first-class CRUD client.

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

// (3) Singular-via-list edge: schema has  CurrentHead []*schema.Director with validate:"max=1"
//     Treated as singular at the wrapper layer; slice is just storage shape.
//     Must use a pointer slice (same rule as true multi-edges): a wrapper
//     returned by CurrentHead() must remain valid across SetCurrentHead calls,
//     which a value-element slice cannot guarantee because reassignment moves
//     the backing array.
func (e *Studio) CurrentHead() *Director {
    if len(e.s.CurrentHead) == 0 || e.s.CurrentHead[0] == nil { return nil }
    return &Director{s: e.s.CurrentHead[0]}
}
func (e *Studio) SetCurrentHead(v *Director) {
    if v == nil { e.s.CurrentHead = nil; return }
    e.s.CurrentHead = []*schema.Director{v.s}
}
```

**Set semantics — value vs pointer fields.** Shape (1) (pointer-typed
singular) and shape (3) (singular-via-list with pointer-element slice) both
store a pointer in the schema field, so the wrapper passed to `Set<Edge>`
and the wrapper returned by `<Edge>()` afterward share the same
`*schema.Director` — mutations through either are visible everywhere.
Shape (2) (value-typed singular) copies the value into the schema field:
`SetHeadquarters(v *Country)` runs `e.s.Headquarters = *v.s`, so `v` and
`e.Headquarters()` no longer share state after the call. Mutations to `v`
afterwards do not propagate to the studio. This asymmetry is inherent to
how Go handles value vs pointer fields; callers using value-typed singular
edges should `Set` once with their final value or use the returned wrapper
from `e.Headquarters()` for further mutations.

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

### Typed clients (two layers, same names)

Two same-named `StudioClient` types are generated, one per package:

- **`schema.StudioClient`** — operates on `*schema.Studio` directly. No
  wrappers, no allocations beyond what dgman performs internally. The
  single source of truth for CRUD logic.
- **`movies.StudioClient`** — operates on `*movies.Studio`. Composes over
  `schema.StudioClient`; adds a wrap on the way out of reads and an
  unwrap on the way into writes.

Consumers import the package they want and call `NewStudioClient(conn)`.
The method names and signatures match across both clients except for the
entity argument/return types.

```go
// schema/studio_client_gen.go
package schema

type StudioClient struct { conn modusgraph.Client }

func NewStudioClient(conn modusgraph.Client) *StudioClient {
    return &StudioClient{conn: conn}
}

func (c *StudioClient) Get(ctx context.Context, uid string) (*Studio, error) {
    var rec Studio
    if err := c.conn.Get(ctx, &rec, uid); err != nil { return nil, err }
    return &rec, nil
}

func (c *StudioClient) Add(ctx context.Context, s *Studio) error {
    return c.conn.Insert(ctx, s)
}

func (c *StudioClient) Update(ctx context.Context, s *Studio) error {
    return c.conn.Update(ctx, s)
}

func (c *StudioClient) Upsert(ctx context.Context, s *Studio, predicates ...string) error {
    return c.conn.Upsert(ctx, s, predicates...)
}

func (c *StudioClient) Delete(ctx context.Context, uid string) error {
    return c.conn.Delete(ctx, []string{uid})
}

func (c *StudioClient) List(ctx context.Context, opts ...PageOption) ([]Studio, error) {
    var recs []Studio
    // ...query construction...
    if err := q.Nodes(&recs); err != nil { return nil, err }
    return recs, nil
}

func (c *StudioClient) Query(ctx context.Context) *StudioQuery {
    return &StudioQuery{ /* ... */ }
}
```

```go
// movies/studio_client_gen.go
package movies

type StudioClient struct { schemaClient *schema.StudioClient }

func NewStudioClient(conn modusgraph.Client) *StudioClient {
    return &StudioClient{schemaClient: schema.NewStudioClient(conn)}
}

func (c *StudioClient) Get(ctx context.Context, uid string) (*Studio, error) {
    s, err := c.schemaClient.Get(ctx, uid)
    if err != nil { return nil, err }
    return WrapStudio(s), nil
}

func (c *StudioClient) Add(ctx context.Context, w *Studio) error {
    return c.schemaClient.Add(ctx, w.s)
}

func (c *StudioClient) Update(ctx context.Context, w *Studio) error {
    return c.schemaClient.Update(ctx, w.s)
}

func (c *StudioClient) Upsert(ctx context.Context, w *Studio, predicates ...string) error {
    return c.schemaClient.Upsert(ctx, w.s, predicates...)
}

func (c *StudioClient) Delete(ctx context.Context, uid string) error {
    return c.schemaClient.Delete(ctx, uid)
}

func (c *StudioClient) List(ctx context.Context, opts ...PageOption) ([]*Studio, error) {
    recs, err := c.schemaClient.List(ctx, opts...)
    if err != nil { return nil, err }
    out := make([]*Studio, len(recs))
    for i := range recs {
        out[i] = WrapStudio(&recs[i])
    }
    return out, nil
}

func (c *StudioClient) Query(ctx context.Context) *StudioQuery {
    return &StudioQuery{schemaQuery: c.schemaClient.Query(ctx)}
}
```

**Input/output by client:**

| Method | `schema.StudioClient` | `movies.StudioClient` |
|---|---|---|
| `Get(ctx, uid)` | `(*schema.Studio, error)` | `(*movies.Studio, error)` |
| `Add(ctx, x)` | takes `*schema.Studio` | takes `*movies.Studio` |
| `Update(ctx, x)` | takes `*schema.Studio` | takes `*movies.Studio` |
| `Upsert(ctx, x, preds...)` | takes `*schema.Studio` | takes `*movies.Studio` |
| `Delete(ctx, uid)` | (uid only) | (uid only) |
| `List(ctx, opts...)` | `([]schema.Studio, error)` | `([]*movies.Studio, error)` |
| `Query(ctx)` | `*schema.StudioQuery` | `*movies.StudioQuery` |

`schema.StudioClient.List` returns a value slice (`[]schema.Studio`) since
the schema struct is a value type and dgman fills a slice it owns;
`movies.StudioClient.List` allocates wrappers around each element and
returns `[]*movies.Studio`.

**Why composition over duplication:**

The wrapper-level client is a thin wrap/unwrap shim. The schema-level
client holds all the actual CRUD logic. This keeps the two clients in
lockstep automatically — change a method body in the schema client and
the wrapper client follows for free.

**The directionality at a glance:**

```
*schema.Studio  ──── WrapStudio(s) ────►  *movies.Studio  ──── Unwrap() ────►  *schema.Studio
                                              ▲     │
                                              │     │  movies.StudioClient
                                              │     │      composes over
                                              │     ▼  schema.StudioClient
```

The query builder types `schema.StudioQuery` and `movies.StudioQuery`
follow the same discipline: terminal methods (`Nodes`, `First`, `Single`)
return raw schemas or wrappers respectively. The wrapper-level query
composes over the schema-level query and wraps results on the way out.

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

### Slice-of-entity fields must use pointer slices

Any field whose element type is an entity (`[]X` where `X` is another
schema struct) must use a pointer slice: `[]*X`, not `[]X`. The rule
applies uniformly to both shapes:

- **True multi-edge** (no `validate:"max=1"`/`"len=1"` constraint):
  - Allowed: `Films []*schema.Film`
  - Rejected: `Films []schema.Film`
- **Singular-via-list** (`validate:"max=1"` or `validate:"len=1"`):
  - Allowed: `CurrentHead []*schema.Director` with `validate:"max=1"`
  - Rejected: `CurrentHead []schema.Director` with `validate:"max=1"`

Error format: `slice-of-entity field "<Field>" on <Entity> must use []*<schema.Type> (pointer slice); value-element slices are unsupported because wrapper pointer captures into the slice can be silently invalidated by setter calls that reassign or grow the slice`.

The hazard is identical in both shapes: a wrapper returned by `<Edge>()`
holds a pointer into the slice's backing array. A subsequent
`Set<Edge>`/`Append<Edge>`/`Remove<Edge>` call can reassign or grow the
slice, leaving the previously returned wrapper pointing into a detached
backing array. Requiring pointer-element slices closes the hazard for
every entity-slice field with a single rule.

(Scalar slices like `Tags []string` are unaffected — there are no wrappers
to invalidate.)

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

`unwrapSchema` is invoked at the top of each `Client` method that accepts a
struct-shaped `any` parameter. Concretely, the wired entry points are:

- `Insert(ctx, any)` — `obj = unwrapSchema(obj)` before validation and dgman dispatch.
- `InsertRaw(ctx, any)` — same treatment.
- `Update(ctx, any)` — same treatment.
- `Upsert(ctx, any, ...string)` — same treatment.
- `Get(ctx, any, string)` — the destination pointer is unwrapped so dgman writes into the inner `*schema.Studio` that the wrapper already references; the wrapper sees the populated data via its `Unwrap()` accessor afterwards.
- `Query(ctx, any)` — the template argument is unwrapped so the returned `*dg.Query` reflects over the schema struct shape (predicates, types) rather than the wrapper.
- `UpdateSchema(ctx, ...any)` — each variadic template argument is unwrapped, so a caller that passes `*movies.Studio` gets the same schema derivation as if they had passed `*schema.Studio` directly.

For `any`-valued parameters the substitution is straightforward
(`obj = unwrapSchema(obj)`). For variadic `...any` (UpdateSchema only), the
substitution loops over the slice and replaces each element.

Methods that do *not* reflect over an entity value — `Delete(ctx, []string)`,
`Close()`, `QueryRaw(ctx, string, map[string]string)`, `DgraphClient()`,
`WithRetry(ctx, RetryPolicy, func() error)`, `LoadData(ctx, string, ...Option)`,
`GetSchema(ctx)`, `DropAll(ctx)`, `DropData(ctx)` — are unaffected.

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

Templates split into two groups by which package their output lives in.

**Schema-side templates** (always emitted unless gated by toggle flags):

| Template | Output | Gate flag | Role |
|---|---|---|---|
| `schema_marker.go.tmpl` *(new)* | `<schema-dir>/marker_gen.go` | always emitted | `SchemaTypeName/SchemaPredicates/SchemaSearchPredicate` for every entity in the schema package. Single file. |
| `schema_client.go.tmpl` *(new)* | `<schema-client-dir>/client_gen.go` | `-no-schema-clients` | Top-level `schema.Client` factory (returns conn-bound per-entity sub-clients). |
| `schema_entity_client.go.tmpl` *(new)* | `<schema-client-dir>/<snake>_client_gen.go` | `-no-schema-clients` | Per-entity `schema.StudioClient` operating on `*schema.Studio` directly. Holds all CRUD logic. |
| `schema_query.go.tmpl` *(new)* | `<schema-client-dir>/<snake>_query_gen.go` | `-no-schema-clients` | Per-entity `schema.StudioQuery` — returns raw schema slices. |

**Wrapper-side templates** (gated by `-no-entities`):

| Template | Output | Gate flag | Role |
|---|---|---|---|
| `entity.go.tmpl` | `<entity-dir>/<snake>_gen.go` | `-no-entities` | Wrapper struct, `NewStudio`, `WrapStudio`, `Unwrap`, `Validate`, `MarshalJSON`/`UnmarshalJSON`, `UID/DType` methods. |
| `accessors.go.tmpl` | `<entity-dir>/<snake>_accessors_gen.go` | `-no-entities` | Per-field/edge methods delegating to `e.s`. Always emitted (no longer gated on private fields). |
| `options.go.tmpl` | `<entity-dir>/<snake>_options_gen.go` | `-no-entities` | `StudioOption`, `WithStudio<Field>` family, `ApplyStudioOptions`. (No installer option — `WrapStudio` constructor handles that case.) |
| `iter.go.tmpl` | `<entity-dir>/iter_gen.go` | `-no-entities` | `iter.Seq2` helpers used by `<Edge>Seq` accessors. No change. |
| `page_options.go.tmpl` | `<entity-dir>/page_options_gen.go` | `-no-entities` | Pagination types. No change. |
| `wrapper_client.go.tmpl` *(new)* | `<entity-client-dir>/client_gen.go` | `-no-entities` or `-no-entity-clients` | Top-level `movies.Client` factory. |
| `wrapper_entity_client.go.tmpl` *(new)* | `<entity-client-dir>/<snake>_client_gen.go` | `-no-entities` or `-no-entity-clients` | Per-entity `movies.StudioClient`. Composes over `schema.StudioClient`. |
| `wrapper_query.go.tmpl` *(new)* | `<entity-client-dir>/<snake>_query_gen.go` | `-no-entities` or `-no-entity-clients` | Per-entity `movies.StudioQuery`. Composes over `schema.StudioQuery`; wraps results. |

**CLI:**

| Template | Output | Gate flag | Role |
|---|---|---|---|
| `cli.go.tmpl` | `<cli-dir>/main.go` | `-no-cli` | Kong CLI. Builds `schema.X` directly from flags; calls `schema.<Entity>Client.Add(...)`. |

**Removed:**

| Template | Status |
|---|---|
| `marshal.go.tmpl` | Deleted. The private-field marshal mirror is no longer needed. |
| `client.go.tmpl` (current, single-tier) | Split into `schema_client.go.tmpl` + `wrapper_client.go.tmpl`. |
| `query.go.tmpl` (current, single-tier) | Split into `schema_query.go.tmpl` + `wrapper_query.go.tmpl`. |

### Generator flags

All optional, all with defaults that match an unflagged
`//go:generate go run github.com/matthewmcneely/modusgraph/cmd/modusgraph-gen`.
Three categories: inputs, outputs, toggles.

**Inputs:**

| Flag | Default | Meaning |
|---|---|---|
| `-schema-dir` | `./schema` if a `./schema/` subdir exists in CWD; otherwise `.` (CWD itself) | Path (relative to CWD) where schema source lives. The "is `./schema/` a directory?" probe runs once at flag-parse time. Also the default output location for the schema-level client (overridable via `-schema-client-dir`). |
| `-schema-alias` | dir basename of `-schema-dir` (`schema` for the default-subdir layout, basename of CWD for the schema-local layout) | Import alias for the schema package in generated code. |

**Outputs (one flag per generated artifact group):**

| Flag | Default | What it controls |
|---|---|---|
| `-schema-client-dir` | same as `-schema-dir` | Where the schema-level CRUD clients (`schema.StudioClient`, etc.) and the schema-level query builders are written. Default puts them alongside the schema types they operate on. |
| `-entity-dir` | `./entity/` when the resolved `-schema-dir` equals CWD (i.e., the schema structs live in the same directory `//go:generate` was invoked from); `.` (CWD) otherwise | Where the wrapper types and accessors are written. The condition is checked against the resolved schema-dir (explicit or default), so explicit `-schema-dir .` triggers `./entity/` exactly the same way the unflagged schema-local case does. |
| `-entity-client-dir` | same as `-entity-dir` | Where the wrapper-level CRUD clients (`movies.StudioClient`, etc.) and the wrapper-level query builders are written. Default puts them alongside the wrapper types they operate on. |
| `-cli-dir` | `./cmd/<pkg>` | Output directory for the Kong CLI. Unchanged. |
| `-out` | matches `-entity-dir` default | *Deprecated alias* for `-entity-dir`. Accepted for backward compatibility with the unflagged default invocation. |

**Toggles (turn off entire artifact groups):**

| Flag | Default | Meaning |
|---|---|---|
| `-no-schema-clients` | `false` | Skip generating schema-level clients and query builders. Schema types and markers are still generated. **Implies `-no-entity-clients`** — the wrapper client composes over the schema client, so it cannot exist without one. |
| `-no-entities` | `false` | Skip generating wrapper types, wrapper accessors, wrapper options, *and* wrapper clients. Implies `-no-entity-clients`. Raw-only mode. |
| `-no-entity-clients` | `false` | Skip wrapper-level clients and query builders, but still emit wrapper types and accessors. Useful when you want the wrapper data model but compose your own client over it. |
| `-no-cli` | `false` | Skip CLI generation entirely. (Was implicit; making it a flag.) |

**Other (unchanged):**

| Flag | Default | Meaning |
|---|---|---|
| `-pkg-name` | resolved from existing `.go` files in `-entity-dir`, then basename of the absolute `-entity-dir` path | Package name to use in generated wrapper files (the `package X` declaration). |
| `-cli-name` | wrapper package name | Name for the CLI binary. |
| `-with-validator` | `false` | Enable validation in the generated CLI. |

The `//go:generate` directive can live in either of two natural places, and
the unflagged defaults adapt to which one you pick:

- **Wrapper-parent layout** — `generate.go` in the parent of a schema
  subdir. CWD has a `./schema/` subdir; that subdir holds schema files.
  Unflagged defaults resolve to `-schema-dir ./schema` and `-entity-dir .`
  so wrappers land next to the generate stub.
- **Schema-local layout** — `generate.go` alongside schema files in the
  schema directory itself. CWD has no `./schema/` subdir, so the unflagged
  default for `-schema-dir` falls back to `.` (CWD). Since the resolved
  schema-dir now equals CWD, `-entity-dir` defaults to `./entity/`
  so wrappers land in a sibling subpackage.

Two separate defaulting steps, applied in order:

1. **`-schema-dir` default.** If `./schema/` exists in CWD, default
   `-schema-dir` to `./schema`. Otherwise default it to `.`. This step
   exists only so the unflagged case finds the schema files in either
   layout; an explicit `-schema-dir` skips this step entirely.
2. **`-entity-dir` default.** Whatever `-schema-dir` resolved to
   (explicit or defaulted), compare its absolute path to CWD. If they
   match (schema files live in CWD), default `-entity-dir` to
   `./entity/`. Otherwise default it to `.`. This is the rule that
   actually controls where wrappers land, and it works the same way
   regardless of how schema-dir was supplied.

The `-entity-dir` condition is therefore "are the schema structs in the
same directory `//go:generate` was invoked from?" — measured directly on
the resolved schema-dir, not by inspecting subdirectory names. Explicit
`-schema-dir .` triggers `./entity/` exactly the same way the unflagged
schema-local case does. Users who want different paths can override any
flag explicitly.

**Common layouts and the flags that produce them:**

```
# Wrapper-parent layout (default A) — generate.go lives in the parent dir
movies/
  generate.go                 //go:generate go run ...
  schema/                     types + markers + schema.StudioClient
  *_gen.go                    wrappers + accessors + movies.StudioClient

# CWD when generate runs: movies/
# Resolved -schema-dir: movies/schema/ (not equal to CWD)
# → wrapper default: . (CWD)
# Flags: (none — defaults are sufficient)
```

```
# Schema-local layout (default B) — generate.go lives alongside schema files
movies/
  schema/
    generate.go               //go:generate go run ...
    studio.go                 schema struct
    marker_gen.go             schema markers
    studio_client_gen.go      schema.StudioClient
    entity/
      studio_gen.go           wrappers + accessors + entity.StudioClient

# CWD when generate runs: movies/schema/
# Resolved -schema-dir: . → movies/schema/ (equal to CWD)
# → wrapper default: ./entity/
# Flags: (none — defaults are sufficient)
# Wrapper package name defaults to "entity" — consumers do
#   import "example.com/proj/movies/schema/entity"
# and reference entity.Studio, entity.NewStudio, entity.StudioClient.
```

```
# Pure schema package (types only), clients elsewhere
movies/
  schema/                     types + markers
  schemaclient/               schema.StudioClient
  *_gen.go                    wrappers + accessors + movies.StudioClient

# Flags: -schema-client-dir ./schemaclient
```

```
# Raw-only — no wrapper layer at all
movies/
  schema/                     types + markers + schema.StudioClient

# Flags: -no-entities
```

```
# Types-only — schemas + wrappers as data model, no clients on either side
# (caller composes their own CRUD layer over modusgraph.Client)
movies/
  schema/                     types + markers (no client)
  *_gen.go                    wrappers + accessors (no client)

# Flags: -no-schema-clients
# (Implies -no-entity-clients: wrapper clients compose over schema clients,
# so disabling schema clients disables wrapper clients automatically.)
```

```
# Fully split — output dirs for everything
project/
  api/                        wrappers
  api/clients/                wrapper clients
  internal/types/             schemas + markers
  internal/clients/           raw clients

# Flags:
#   -schema-dir ./internal/types
#   -schema-client-dir ./internal/clients
#   -entity-dir ./api
#   -entity-client-dir ./api/clients
```

### Parser changes

`internal/parser/parser.go`:

- Accept a source directory (`-schema-dir`) distinct from the output directories.
- Resolve the schema package's import path so it can be referenced in
  generated code (`import "<modpath>/<schema-pkg-path>"`).
- Determine the relevant package names per output directory:
  - **Schema package name** — from parsing `-schema-dir` (e.g., `schema`).
    Used as the import alias in generated wrapper code (overridable via
    `-schema-alias`).
  - **Schema-client package name** — needed only when `-schema-client-dir`
    differs from `-schema-dir`. Resolved by reading the destination
    directory if any `.go` file already exists there, otherwise basename
    of the absolute path. Errors out if the destination directory's
    existing package name doesn't match what would be emitted.
  - **Wrapper package name** — for the `package X` declaration in generated
    wrapper files. Resolved in this order: (1) explicit `-pkg-name` flag,
    (2) `package` declaration in any existing `.go` file in `-entity-dir`,
    (3) basename of the absolute `-entity-dir` path.
  - **Wrapper-client package name** — needed only when
    `-entity-client-dir` differs from `-entity-dir`. Resolved the same
    way as schema-client package name.
- Reject value-element slices for any field whose element type is an entity
  (the [Schema constraints](#schema-constraints) rule). Applies uniformly
  to true multi-edges and singular-via-list edges. Error message must name
  the field, name the entity, and tell the user exactly what to change.
- Apply the reserved-name collision guard against generated field accessors.
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
| `films []Film` (value slice, true multi-edge) | `Films []*Film` (pointer slice — pointer-slice rule) |
| `currentHead []Director` with `validate:"max=1"` | `CurrentHead []*Director` with `validate:"max=1"` (pointer-slice rule applies to singular-via-list too) |
| `homeBase []Country` with `validate:"len=1"` | `HomeBase []*Country` with `validate:"len=1"` (pointer-slice rule applies to singular-via-list too) |
| `advisors []*Director` | unchanged (already pointer slice) |
| `tempCache string` (no json tag) | deleted (test fixture for skip behavior; no longer needed) |
| `Internal string ... dgraph:"-"` | kept (test fixture for `dgraph:"-"` skip behavior) |

Other entity files in `testdata/movies/` (`film.go`, `director.go`,
`country.go`, `actor.go`, `genre.go`, `rating.go`, `performance.go`,
`location.go`, `content_rating.go`): same treatment. Move to `schema/`,
uppercase fields, convert any value-element slice-of-entity field (both
true multi-edges and singular-via-list) to pointer slices per the
[pointer-slice rule](#slice-of-entity-fields-must-use-pointer-slices),
drop test-only private-field fixtures that no longer exercise meaningful
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
