---
date: 2026-05-21
topic: query-edge-filter
status: draft
---

# Edge-Predicate Filtering for Generated Query Builders

## Goal

Let a generated `<Entity>Query` filter root records by a scalar predicate of a
*neighbouring* node reached over an edge — "people who have a dog named Fido" —
as a first-class, generated method:

```go
client.Person.Query(ctx).WhereDogs(`eq(name, "Fido")`).Nodes()
```

Today `<Entity>Query.Filter` (and the `typed.Query[T].Filter` it delegates to)
only constrains the root node's *own* predicates: the filter string lands in
dgraph's root `@filter`, which has no syntax for an edge target's scalar value.
There is no way, short of hand-written DQL through `Client.QueryRaw`, to express
"root has an edge whose target matches X."

## Non-Goals

- **A typed predicate DSL.** `WhereDogs(filter string, params ...any)` takes a
  dgraph `@filter` string, exactly like the existing `Filter`. A type-safe
  `WhereDogs(func(c *DogCriteria){ c.NameEq("Fido") })` face is future work; it
  would layer over the same `WhereEdge` substrate this spec introduces.
- **Multi-hop filters** (root → edge → edge). The filter string constrains the
  *immediate* edge target's own predicates.
- **Changing `Filter`, `Nodes`, `First`, `IterNodes`, or CRUD.**

## Why This Approach

**dgman emits one query block.** A `typed.Query[T]` wraps a single
`*dg.Query`, which dgman renders as one root `@filter` over an `expand(_all_)`
body (`query.go:generateQuery`). dgman exposes no way to attach a `@filter` to
an edge sub-block. So edge filtering cannot be a new dgman builder call — it
needs a genuinely separate execution path.

**Two-step semi-join, executed by the substrate.** A query carrying edge
constraints runs as:

1. **Pre-pass** — an `@cascade` query over `type(T)` whose body is `uid` plus
   one filtered block per edge constraint. `@cascade` drops any node with an
   empty block, so a survivor satisfies every constraint. The pre-pass returns
   the surviving UIDs.
2. **Main query** — the existing `*dg.Query`, with its root function rewritten
   to `uid(<matched>)`.

The alternative — a single hand-written two-block DQL query via `QueryRaw` —
was rejected: it would force re-implementing the result projection, and
`expand(_all_)` drops managed reverse edges (`reverse_test.go`), so edge-filtered
results would silently differ from normal ones. The two-step keeps step 2 on
the dgman path, so ordering, `Limit`/`Offset`/`After`, `IterNodes` paging,
`NodesAndCount`, and reverse-edge-aware projection all keep working untouched.

**The cost** is a second read: the pre-pass and the main query run in separate
read-only transactions, so a writer committing between them is observable. This
is the same consistency class the package already tolerates, and is negligible
on the embedded file engine. It is documented, not eliminated.

## Design

### `typed.Query[T]` — the `WhereEdge` substrate

`Query[T]` gains `conn`/`ctx` (to run the pre-pass) and an `edges` slice:

```go
type Query[T any] struct {
	q      *dg.Query
	conn   modusgraph.Client
	ctx    context.Context
	limit  int
	offset int
	edges  []edgeFilter
}

type edgeFilter struct {
	predicate string
	filter    string
	params    []any
}
```

New builder, accumulating (each call ANDs another constraint):

```go
func (qb *Query[T]) WhereEdge(predicate, filter string, params ...any) *Query[T]
```

The three terminals (`Nodes`, `First`, `IterNodes`) call `resolveRoots()`
first. With no edge constraints it is a no-op. Otherwise it runs the pre-pass;
if zero roots match it reports so and the terminal returns an empty result
without running the main query; otherwise it rewrites the main query's root
function to `uid(<matched>)`.

### Pre-pass DQL

For `WhereEdge("pets", `eq(name, $1)`, "Fido")` over `Owner`:

```dql
{
  data(func: type(Owner)) @filter(has(dgraph.type)) @cascade {
    uid
    mg_e0 : pets @filter(eq(name, "Fido")) { uid }
  }
}
```

Built by reconfiguring a fresh `conn.Query(ctx, &T{})` with `Cascade()` (bare
`@cascade`) and `Query(body, params...)` (dgman substitutes `$N`). Every block
is aliased `mg_e0`, `mg_e1`, … so two constraints on the same predicate do not
collide as duplicate fields. Each edge filter is written numbering its params
from `$1`; `shiftPlaceholders` renumbers them against the concatenated params
slice before they are joined into one body.

### Generated face — `<Entity>Query.Where<Edge>`

`wrapper_query.go.tmpl` emits one thin method per edge field, delegating to the
substrate — the same pattern `Filter`/`Cascade` already use:

```go
func (q *OwnerQuery) WherePets(filter string, params ...any) *OwnerQuery {
	q.typed.WhereEdge("pets", filter, params...)
	return q
}
```

The method name is `Where` + the field's accessor name; the predicate string is
the field's resolved dgraph predicate. Generated for every edge field (multi,
singular, and reverse). No parser changes — `model.Field` already carries
`IsEdge`/`Predicate`.

## Error handling

`WhereEdge` never executes — it only appends. The pre-pass error (malformed
filter, transport failure) surfaces from the terminal: `Nodes`/`First` return
it; `IterNodes` yields one `(nil, err)` and stops. A pre-pass matching zero
roots is not an error — the terminal returns an empty result.

## Testing

- **`typed/query_test.go`** — new `owner`/`pet` test types (an edge pair).
  Behavioral tests against the file engine: `WhereEdge` filters by edge target;
  no match yields empty; `$N` params bind; `WhereEdge` composes with a root
  `Filter`; two `WhereEdge` calls AND; `First` and `IterNodes` honor edge
  constraints.
- **`generator_test.go`** — a two-type edge schema asserts `Where<Edge>` is
  generated and delegates to `typed.WhereEdge`, and that an edgeless type gets
  no `Where*` method.
- **`wrapper_query_e2e_test.go`** — `client.Director.Query(ctx).WhereFilms(...)`
  end-to-end against the file-backed client.

## Migration / blast radius

- **Modified:** `typed/query.go` (3 struct fields, `WhereEdge`, the
  `resolveRoots`/`matchedUIDs`/`edgeMatchBody`/`shiftPlaceholders` helpers,
  edge-aware terminals, doc comments); `typed/client.go` (`Query` passes
  `conn`/`ctx`); `wrapper_query.go.tmpl` (generated `Where<Edge>`).
- **Regenerated:** the `movies` fixture — every `*_query_gen.go` for an entity
  with edges gains `Where<Edge>` methods.
- **New tests** in `typed/query_test.go`, `generator_test.go`,
  `wrapper_query_e2e_test.go`.
- No change to `Filter`, `Nodes`, `First`, `IterNodes`, CRUD, or any other
  generated artifact. The pre-pass is inert unless `WhereEdge` is called.

## Open decisions

None. The string-filter API (over a typed DSL), the two-step semi-join (over a
two-block `QueryRaw`), and one-hop depth were settled before implementation.
The typed predicate DSL is recorded above as future work.
