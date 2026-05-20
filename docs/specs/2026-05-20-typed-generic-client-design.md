---
date: 2026-05-20
topic: typed-generic-client
status: draft
---

# A Handwritten Generic `typed` Substrate Replacing Per-Entity Generated Clients

## Goal

Collapse the per-entity generated client and query types into a single
handwritten generic package, `github.com/matthewmcneely/modusgraph/typed`.

Today `modusgraph-gen` emits, for every entity, a `<E>Client` and an `<E>Query`
that are near-identical pass-throughs over the `any`-typed `modusgraph.Client`
interface — they differ only by the type name. That is duplication a Go generic
expresses once. After this change:

- `typed.Client[T]` and `typed.Query[T]` are written by hand, once, and bind a
  type to the otherwise untyped `modusgraph.Client`.
- The generator stops emitting per-entity schema clients/queries. It still emits
  a thin per-schema aggregate (`Client` with named entity fields) whose fields
  are `*typed.Client[T]`.
- The wrapper/entity layer is kept and rewired to compose over `typed` instead
  of over the deleted schema clients.
- Four templates whose output nothing consumes, or that a generic supersedes,
  are deleted.

## Non-Goals

- A deprecation window or downstream migration shim. The repo has no stable
  release of the per-entity-client approach; the swap is atomic.
- Changing the hand-edited schema structs, their `dgraph`/`json` tags, or the
  `marker_gen.go` output. `SchemaTypeName`/`SchemaPredicates`/
  `SchemaSearchPredicate` are unaffected.
- Adding bulk/batched APIs to `modusgraph` itself.
- Cursor-based query pagination. `typed.Client[T].Iter` uses offset paging;
  cursor paging via `After(uid)` is a possible later refinement.
- Removing the wrapper/entity layer. It is explicitly retained.

## Why This Approach

A critical look at the generated schema-side client code found it does not earn
its keep:

- **`<E>Client`** is six one-line forwards to `modusgraph.Client`
  (`Insert`/`Update`/`Upsert`/`Get`/`Delete`/`Query`). Across all six methods
  there are exactly two lines of real logic: `Get` allocates a value and returns
  its pointer; `Delete` wraps a `uid` into `[]string{uid}`. Everything else is
  byte-identical forwarding. The generator emits N copies of this, one per
  entity, differing only in the type name.
- **`<E>Query`** is worse: a "query builder" with only `Nodes` and `First`. It
  has no `Filter`/`Order`/`Limit`, and it constructs a fresh `*dg.Query` at
  terminal time and immediately ends it — so it cannot build queries at all. It
  also hides the capable builder (`modusgraph.Client.Query` returns dgman's
  `*dg.Query`, which has `Filter`, `OrderAsc/Desc`, `First(n)`, `Offset`,
  `After`, `Cascade`). `<E>Query.First` claims an "implicit Limit=1" but applies
  no limit — it fetches every row and returns `[0]`.

`modusgraph.Client` is fully `any`-typed. Putting a static type on an untyped
interface is the textbook job of a **generic**, not a code-generation template.
A single `typed.Client[T]` / `typed.Query[T]` expresses the whole thing once,
with no per-entity files.

The per-entity schema clients only ever existed as the typed substrate the
wrapper layer composed over. `typed` *is* that substrate, handwritten — so the
wrapper layer composes over it directly, and the generator no longer emits the
intermediate per-entity layer.

The schema-side aggregate `Client` (named per-entity fields) is kept generated:
named-field autocomplete genuinely requires per-schema generation, and it
collapses to a struct-plus-constructor with no logic to get wrong.

## Architecture

### Layering

```
github.com/dolan-in/dgman/v2     (*dg.Query — untyped builder)
modusgraph.Client                (interface — all methods take `any`)
        │
        ▼
modusgraph/typed                 (HANDWRITTEN GENERICS, this change)
  Client[T]   Query[T]   Wrapper[S]   Option[T]/Apply[T]
        │
        ├──────────────────────────────┐
        ▼                              ▼
schema package (generated)      entity package (generated)
  Client aggregate:               Actor struct embeds typed.Wrapper[schema.Actor]
    Actor *typed.Client[Actor]    ActorClient wraps *typed.Client[schema.Actor]
    Film  *typed.Client[Film]     ActorQuery  wraps *typed.Query[schema.Actor]
    ...                           accessors / options / New / Wrap
        │                              │
        └──────────┬───────────────────┘
                   ▼
            generated CLI (Kong)
```

The schema aggregate and the wrapper layer are **siblings** over `typed`, not a
stack — neither depends on the other. (Previously the wrapper layer depended on
the schema clients.)

### What the generator emits, before and after

For the 10-entity `movies` fixture generated with `-entities`:

| Artifact | Before | After |
|---|---|---|
| `schema/<entity>_client_gen.go` | 10 files | **deleted** |
| `schema/<entity>_query_gen.go` | 10 files | **deleted** |
| `schema/client_gen.go` (aggregate) | 1 file | 1 file, fields are `*typed.Client[T]` |
| `schema/marker_gen.go` | 1 file | unchanged |
| `entity/<entity>_client_gen.go` | 10 files | 10 files, wrap `typed.Client` |
| `entity/<entity>_query_gen.go` | 10 files | 10 files, wrap `typed.Query`, fluent |
| `entity/<entity>_gen.go` | 10 files | 10 files, embed `typed.Wrapper` |
| `entity/<entity>_accessors_gen.go` | 10 files | 10 files, reach struct via `Unwrap()` |
| `entity/<entity>_options_gen.go` | 10 files | 10 files, `typed.Option[T]` |
| `entity/client_gen.go` (wrapper aggregate) | 1 file | 1 file, decoupled from `schema.Client` |
| `entity/iter_gen.go` | 1 file | **deleted** (dead; replaced by `typed.Client.Iter`) |
| `entity/page_options_gen.go` | 1 file | **deleted** (dead, static) |
| CLI `main.go` | 1 file | 1 file, `List` one-liner tweak |

## The `typed` package

New package, `github.com/matthewmcneely/modusgraph/typed`, four source files
plus tests. All handwritten, all generic.

### `typed/client.go` — `Client[T]`

```go
type Client[T any] struct{ conn modusgraph.Client }

func NewClient[T any](conn modusgraph.Client) *Client[T] {
    return &Client[T]{conn: conn}
}

func (c *Client[T]) Get(ctx context.Context, uid string) (*T, error) {
    var rec T
    if err := c.conn.Get(ctx, &rec, uid); err != nil {
        return nil, err
    }
    return &rec, nil
}

func (c *Client[T]) Add(ctx context.Context, rec *T) error    { return c.conn.Insert(ctx, rec) }
func (c *Client[T]) Update(ctx context.Context, rec *T) error { return c.conn.Update(ctx, rec) }

func (c *Client[T]) Upsert(ctx context.Context, rec *T, predicates ...string) error {
    return c.conn.Upsert(ctx, rec, predicates...)
}

func (c *Client[T]) Delete(ctx context.Context, uid string) error {
    return c.conn.Delete(ctx, []string{uid})
}

func (c *Client[T]) Query(ctx context.Context) *Query[T] {
    var z T
    return &Query[T]{q: c.conn.Query(ctx, &z)}
}

func (c *Client[T]) Iter(ctx context.Context) iter.Seq2[*T, error] { /* see below */ }
```

`T` is unconstrained (`any`). modusgraph CRUD works off the schema struct's
`dgraph`/`json` tags via reflection, not off the marker interface — so `typed`
needs no constraint and stays decoupled from `marker_gen.go`.

`Iter` is a **real** paged iterator (replacing the deleted `iter.go.tmpl`, whose
`ListIter` claimed to page but did not):

```go
const defaultPageSize = 50

func (c *Client[T]) Iter(ctx context.Context) iter.Seq2[*T, error] {
    return func(yield func(*T, error) bool) {
        for offset := 0; ; offset += defaultPageSize {
            var page []T
            err := c.conn.Query(ctx, new(T)).
                Offset(offset).First(defaultPageSize).Nodes(&page)
            if err != nil {
                yield(nil, err)
                return
            }
            for i := range page {
                if !yield(&page[i], nil) {
                    return
                }
            }
            if len(page) < defaultPageSize {
                return
            }
        }
    }
}
```

Offset paging can skip or repeat rows if the data set mutates mid-iteration;
that is an accepted limitation for this iterator.

### `typed/query.go` — `Query[T]`

A fluent, typed builder wrapping dgman's `*dg.Query`. Chainable methods return
`*Query[T]`; terminals return typed values.

```go
type Query[T any] struct{ q *dg.Query }

// Chainable — thin pass-throughs that keep the chain typed.
func (qb *Query[T]) Filter(f string, params ...any) *Query[T] { qb.q.Filter(f, params...); return qb }
func (qb *Query[T]) OrderAsc(clause string) *Query[T]         { qb.q.OrderAsc(clause); return qb }
func (qb *Query[T]) OrderDesc(clause string) *Query[T]        { qb.q.OrderDesc(clause); return qb }
func (qb *Query[T]) Limit(n int) *Query[T]                    { qb.q.First(n); return qb }   // dgman names it First(n)
func (qb *Query[T]) Offset(n int) *Query[T]                   { qb.q.Offset(n); return qb }
func (qb *Query[T]) After(uid string) *Query[T]               { qb.q.After(uid); return qb }
func (qb *Query[T]) Cascade(predicates ...string) *Query[T]   { qb.q.Cascade(predicates...); return qb }

// Terminals — typed returns.
func (qb *Query[T]) Nodes() ([]T, error) {
    var out []T
    err := qb.q.Nodes(&out)
    return out, err
}

func (qb *Query[T]) First() (*T, error) {
    var out []T
    if err := qb.q.First(1).Nodes(&out); err != nil {
        return nil, err
    }
    if len(out) == 0 {
        return nil, nil // not-found is not an error
    }
    return &out[0], nil
}

// Escape hatch for dgman query-block methods we do not mirror
// (Var, As, Name, RootFunc, GroupBy, Vars).
func (qb *Query[T]) Raw() *dg.Query { return qb.q }
```

Naming note: dgman's limit-setter is `First(n int)`; the typed wrapper renames
it `Limit(n)` so the `First() (*T, error)` terminal does not collide.

### `typed/wrapper.go` — `Wrapper[S]`

The generic base the wrapper/entity types embed. Holds the schema struct in an
**unexported** field, so the only access is the exported `Unwrap`.

```go
type Wrapper[S any] struct{ s *S }

func WrapValue[S any](s *S) Wrapper[S] { return Wrapper[S]{s: s} }

func (w Wrapper[S]) Unwrap() *S { return w.s }

func (w *Wrapper[S]) MarshalJSON() ([]byte, error) { return json.Marshal(w.s) }

func (w *Wrapper[S]) UnmarshalJSON(data []byte) error {
    if w.s == nil {
        w.s = new(S)
    }
    return json.Unmarshal(data, w.s)
}

func (w *Wrapper[S]) Validate(ctx context.Context, v modusgraph.StructValidator) error {
    return v.StructCtx(ctx, w.s)
}
```

`Validate` takes `modusgraph.StructValidator` (the interface modusgraph already
defines, satisfied by `*validator.Validate`) so `typed` needs no direct
dependency on `go-playground/validator`.

### `typed/option.go` — `Option[T]` / `Apply[T]`

```go
type Option[T any] func(*T)

func Apply[T any](target *T, opts ...Option[T]) {
    for _, opt := range opts {
        opt(target)
    }
}
```

Replaces the per-entity `<E>Option` type and `Apply<E>Options` loop. Generated
`WithXField` constructors are typed as `typed.Option[<E>]`.

## Generated output after the change

### Schema aggregate — `schema/client_gen.go`

```go
type Client struct {
    GraphClient modusgraph.Client
    Actor       *typed.Client[Actor]
    Film        *typed.Client[Film]
    // ... one field per entity
}

func NewClient(conn modusgraph.Client) *Client {
    return &Client{
        GraphClient: conn,
        Actor:       typed.NewClient[Actor](conn),
        Film:        typed.NewClient[Film](conn),
        // ...
    }
}
```

### Wrapper entity client — `entity/<entity>_client_gen.go`

```go
type ActorClient struct{ typed *typed.Client[schema.Actor] }

func NewActorClient(conn modusgraph.Client) *ActorClient {
    return &ActorClient{typed: typed.NewClient[schema.Actor](conn)}
}

func (c *ActorClient) Get(ctx context.Context, uid string) (*Actor, error) {
    s, err := c.typed.Get(ctx, uid)
    if err != nil {
        return nil, err
    }
    return WrapActor(s), nil
}

func (c *ActorClient) Add(ctx context.Context, w *Actor) error    { return c.typed.Add(ctx, w.Unwrap()) }
func (c *ActorClient) Update(ctx context.Context, w *Actor) error { return c.typed.Update(ctx, w.Unwrap()) }
func (c *ActorClient) Upsert(ctx context.Context, w *Actor, predicates ...string) error {
    return c.typed.Upsert(ctx, w.Unwrap(), predicates...)
}
func (c *ActorClient) Delete(ctx context.Context, uid string) error { return c.typed.Delete(ctx, uid) }

func (c *ActorClient) Query(ctx context.Context) *ActorQuery {
    return &ActorQuery{typed: c.typed.Query(ctx)}
}
```

### Wrapper query — `entity/<entity>_query_gen.go`

Fluent and typed (decision B1): builder methods are mirrored so the wrapper-side
chain stays typed; terminals wrap results.

```go
type ActorQuery struct{ typed *typed.Query[schema.Actor] }

func (q *ActorQuery) Filter(f string, params ...any) *ActorQuery { q.typed.Filter(f, params...); return q }
func (q *ActorQuery) OrderAsc(clause string) *ActorQuery         { q.typed.OrderAsc(clause); return q }
func (q *ActorQuery) OrderDesc(clause string) *ActorQuery        { q.typed.OrderDesc(clause); return q }
func (q *ActorQuery) Limit(n int) *ActorQuery                    { q.typed.Limit(n); return q }
func (q *ActorQuery) Offset(n int) *ActorQuery                   { q.typed.Offset(n); return q }
func (q *ActorQuery) After(uid string) *ActorQuery               { q.typed.After(uid); return q }
func (q *ActorQuery) Cascade(predicates ...string) *ActorQuery   { q.typed.Cascade(predicates...); return q }

func (q *ActorQuery) Nodes() ([]*Actor, error) {
    recs, err := q.typed.Nodes()
    if err != nil {
        return nil, err
    }
    out := make([]*Actor, len(recs))
    for i := range recs {
        out[i] = WrapActor(&recs[i])
    }
    return out, nil
}

func (q *ActorQuery) First() (*Actor, error) {
    s, err := q.typed.First()
    if err != nil || s == nil {
        return nil, err
    }
    return WrapActor(s), nil
}
```

The builder methods are mirrored at two layers — `typed.Query[T]` (handwritten,
once) and `<E>Query` (generated, per entity). The per-entity copy is
*generated*, so adding a builder method means editing one template, not N files.

### Wrapper entity — `entity/<entity>_gen.go`

```go
type Actor struct {
    typed.Wrapper[schema.Actor]
}

func NewActor(opts ...typed.Option[Actor]) *Actor {
    e := &Actor{Wrapper: typed.WrapValue(&schema.Actor{})}
    typed.Apply(e, opts...)
    return e
}

func WrapActor(s *schema.Actor, opts ...typed.Option[Actor]) *Actor {
    e := &Actor{Wrapper: typed.WrapValue(s)}
    typed.Apply(e, opts...)
    return e
}

func (e *Actor) UID() string       { return e.Unwrap().UID }
func (e *Actor) SetUID(v string)   { e.Unwrap().UID = v }
func (e *Actor) DType() []string   { return e.Unwrap().DType }
func (e *Actor) SetDType(v []string) { e.Unwrap().DType = v }
```

`Unwrap`, `MarshalJSON`, `UnmarshalJSON`, and `Validate` are inherited from the
embedded `typed.Wrapper`. `UID`/`SetUID`/`DType`/`SetDType` remain generated per
entity — they name fields on the schema struct, and Go type parameters cannot
access struct fields without a constraint; they are four trivial lines.

### Accessors — `entity/<entity>_accessors_gen.go`

Logically unchanged, but every reference to the schema struct goes through the
exported `Unwrap()` instead of the (now cross-package, unexported) `s` field:
`e.s.Name` becomes `e.Unwrap().Name`. Edge accessors construct wrappers via
`typed.WrapValue`: `&Performance{Wrapper: typed.WrapValue(x)}`.

### Wrapper aggregate — `entity/client_gen.go`

Decoupled from `schema.Client`: holds `conn` directly and constructs per-entity
wrapper clients with `NewActorClient(conn)`. `GraphClient()` returns the stored
`conn`.

## Generator changes

### Template inventory

| Template | Action |
|---|---|
| `schema_entity_client.go.tmpl` | **delete** |
| `schema_query.go.tmpl` | **delete** |
| `iter.go.tmpl` | **delete** (dead output; superseded by `typed.Client.Iter`) |
| `page_options.go.tmpl` | **delete** (dead, fully static output) |
| `schema_client.go.tmpl` | rewrite — aggregate fields become `*typed.Client[T]` |
| `wrapper_entity_client.go.tmpl` | rewrite — wrap `*typed.Client[schema.<E>]` |
| `wrapper_query.go.tmpl` | rewrite — wrap `*typed.Query[schema.<E>]`, fluent |
| `entity.go.tmpl` | rewrite — embed `typed.Wrapper`; drop `Unwrap`/`Marshal`/`Unmarshal`/`Validate` |
| `options.go.tmpl` | rewrite — `typed.Option[T]`; drop per-entity option type + apply loop |
| `accessors.go.tmpl` | rewrite — `e.s.X` → `e.Unwrap().X`; edges via `typed.WrapValue` |
| `wrapper_client.go.tmpl` | tweak — decouple from `schema.Client` |
| `cli.go.tmpl` | tweak — `List` becomes `recs, err := client.<E>.Query(ctx).Nodes()` |
| `schema_marker.go.tmpl` | unchanged |

### `generator.go`

- Remove the per-entity emit calls for `schema_entity_client.go.tmpl` and
  `schema_query.go.tmpl` from the entity loop.
- Remove the emit calls for `iter.go.tmpl` and `page_options.go.tmpl`.
- `schema_client.go.tmpl` (aggregate) is still emitted.

### `main.go` flags

- `NoSchemaClients` is retained but narrows: it now gates only the aggregate
  `Client`, since per-entity schema client/query files no longer exist.
- The `-no-schema-clients` ⇒ `-no-entity-clients` implication is **removed**.
  It modeled the old dependency of the wrapper layer on the schema clients; the
  wrapper layer now composes over `typed`, so the implication is obsolete.
- `-entities`, `-no-entity-clients`, `-no-cli`, `-with-validator`, and all path
  flags are unchanged.

## Generated-code audit

The four deletions/collapses below were found by auditing every template for
output that nothing consumes or that a generic supersedes.

| Finding | Disposition |
|---|---|
| `page_options.go.tmpl` output (`PageOption`, `pageConfig`, `First`, `Offset`, `defaultPageSize`) has **zero consumers** anywhere in the repo, and contains no schema-derived content — it is static text wrongly shipped as a template. `typed.Query[T]` provides `Limit`/`Offset` directly. | Delete the template. |
| `iter.go.tmpl` output (`<E>Client.ListIter`) has **zero consumers**, is byte-identical per entity, and its doc comment claims paging it does not perform (one `Query().Nodes()`, then ranges the materialized slice). | Delete the template; add a genuinely paged `Iter` to `typed.Client[T]`. |
| `entity.go.tmpl` emits `Unwrap`/`MarshalJSON`/`UnmarshalJSON`/`Validate` byte-identically for every entity; they touch only the whole schema struct, never a named field. | Move to the generic `typed.Wrapper[S]`; emit only the concrete per-entity payload. |
| `options.go.tmpl` emits a per-entity `<E>Option` type and `Apply<E>Options` loop, identical in shape across entities. | Replace with generic `typed.Option[T]`/`typed.Apply[T]`; emit only the per-field `WithXField` constructors. |

Templates confirmed to be legitimately generated and kept: `accessors.go.tmpl`
(per-field field access, uncollapsible by generics), `cli.go.tmpl` (Kong needs
concrete struct types with field tags), `schema_marker.go.tmpl` (per-entity data
extracted by the parser).

## Testing

- `typed/client_test.go`, `typed/query_test.go` — unit tests against a real
  local `file://` client (`modusgraph.NewClient("file://"+t.TempDir(), …)`,
  the established pattern). A small in-test struct with `dgraph`/`json` tags
  keeps `typed`'s tests independent of the generator fixture. Coverage:
  - `Add`/`Get`/`Update`/`Upsert`/`Delete` round-trips.
  - `Query` with `Filter`, `OrderAsc/Desc`, `Limit`, `Offset`.
  - `First` returns `(nil, nil)` for no match and applies a real `Limit(1)`.
  - `Iter` over a data set larger than `defaultPageSize` — assert every record
    streams through and that more than one page is fetched.
  - Error pass-through from `modusgraph.Client`.
- `typed/wrapper_test.go` — `Unwrap`, JSON round-trip, `Validate`.
- `generator_test.go` — golden expectations regenerated;
  `TestGenerate_CLIImportsSchemaByFullPath` re-verified.
- Regenerate the `movies` fixture (`go generate ./...` under
  `cmd/modusgraph-gen/internal/parser/testdata/movies`).
- `unwrap_e2e_test.go` (imports the `movies` wrapper package, calls
  `movies.WrapStudio` etc.) must still pass — the wrapper public API is
  preserved. This is an explicit verification gate.
- `go build ./...`, `go vet ./...`, `go test ./...` all green.

## Migration / blast radius

- **New files:** `typed/client.go`, `typed/query.go`, `typed/wrapper.go`,
  `typed/option.go`, plus `typed/*_test.go`.
- **Deleted templates:** `schema_entity_client.go.tmpl`, `schema_query.go.tmpl`,
  `iter.go.tmpl`, `page_options.go.tmpl`.
- **Rewritten templates:** `schema_client`, `wrapper_entity_client`,
  `wrapper_query`, `entity`, `options`, `accessors`.
- **Tweaked templates:** `wrapper_client`, `cli`.
- **`generator.go`:** drop four emit paths.
- **`main.go`:** drop the `-no-schema-clients` ⇒ `-no-entity-clients`
  implication; narrow `NoSchemaClients`.
- **Regenerated:** the `movies` fixture — 20 schema per-entity files, plus
  `iter_gen.go` and `page_options_gen.go`, are deleted; the rest are rewritten.
- The prior `docs/specs/2026-05-18-public-wrapper-types-design.md` is partially
  superseded: its "Typed clients (two layers, same names)" section described the
  per-entity schema clients this change removes. Its wrapper-type, accessor,
  schema-constraint, and flag/path-resolution content remains in force.

## Open decisions

None outstanding. All design choices (package placement, aggregate retention,
fluent typed query, wrapper-layer retention and rewiring, the four audit
deletions) were settled during brainstorming.
