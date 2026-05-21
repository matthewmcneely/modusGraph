---
date: 2026-05-21
topic: query-untyped-operations
status: draft
---

# Wrapping the Untyped DQL Operations on `typed.Query[T]`

## Goal

`typed.Query[T]` (`typed/query.go`) wraps a subset of dgman's `*dg.Query`
builder: `Filter`, `OrderAsc`/`OrderDesc`, `Limit`, `Offset`, `After`,
`Cascade`. Six dgman builder operations are not wrapped — `Var`, `As`, `Name`,
`RootFunc`, `GroupBy`, `Vars` — and `Raw()`'s doc comment names exactly those
six as the gap it exists to bridge.

Promote those six from the `Raw()` escape-hatch list into first-class methods
on `Query[T]`, so advanced DQL composition — custom root functions, query
variables, parameterized queries, grouped aggregation — is reachable without
dropping to the raw dgman query.

## Non-Goals

- The generated per-entity `<E>Query` wrapper
  (`cmd/modusgraph-gen/.../wrapper_query.go.tmpl`). This change is scoped to the
  handwritten `typed.Query[T]` builder only, matching how the recent
  `IterNodes` work was scoped (the template gained no `IterNodes`).
- A typed groupby-aggregation terminal (a `Groups() ([]Group, error)`
  decoder). `@groupby` can group by multiple predicates with multiple
  aggregations; a general decoder is its own feature. `GroupBy` here yields a
  `*RawQuery`, and the caller decodes via `Raw()`.
- Multi-block query composition. `typed.Query[T]` wraps a single `*dg.Query`,
  which emits one query block; `As`/`Var`/`Name` produce valid DQL but their
  cross-block referencing purpose stays out of reach.
- Removing `Raw()`. It remains the escape hatch for operations still unwrapped
  (`UID`, `Query`, `NodesAndCount`, `Model`).

## Why This Approach

The six operations split cleanly in two, and dgman's decode behavior — not
taste — decides the split.

dgman's `nodes()` decoder strips a `{"<q.name>":` prefix from the JSON
response, where `q.name` defaults to `"data"` (set by `txn.Get`). The block
name drives both query generation and response stripping, so it is used
**symmetrically**.

**Safe — the query still yields `[]T`.** `RootFunc`, `As`, `Name`, and `Vars`
leave the `{"data":[...]}` response shape intact:

- `RootFunc` changes only `(func: ...)`; the response key is unchanged.
- `As` prefixes the block with `x as`; the response key is still the block
  name.
- `Name` renames the block, and dgman strips `{"<name>":` symmetrically, so the
  result still decodes.
- `Vars` adds a `query <funcDef>` prefix and routes execution through
  `tx.QueryWithVars`; the response key is unchanged.

These four are thin pass-throughs returning `*Query[T]`, byte-for-byte in the
style of the existing `Filter`/`OrderAsc` methods.

**Shape-changing — the query no longer yields `[]T`.** `Var` and `GroupBy`
change what the query returns:

- `Var()` makes dgman emit `var` in place of the block name; a `var` block
  returns **no data** at all.
- `GroupBy` adds `@groupby(...)`; the result is `{"data":[{"@groupby":[...]}]}`
  — aggregation groups, not nodes.

For both, `Nodes()`/`First()`/`IterNodes()` would decode nonsense. So `Var()`
and `GroupBy()` return a new type, `*RawQuery`, that exposes no typed node
terminal. `qb.Var().Nodes()` becomes a **compile error** rather than a silent
empty result — the central value of this design.

## Design

### Safe builders on `Query[T]`

Four new methods, each a thin pass-through returning `*Query[T]`:

```go
// RootFunc overrides the query root function. dgman's default is
// type(<NodeType>); RootFunc replaces it with an expression such as
// eq(name, "Alice") or has(email).
func (qb *Query[T]) RootFunc(rootFunc string) *Query[T] {
	qb.q.RootFunc(rootFunc)
	return qb
}

// As assigns a dgraph query-variable name to the query block.
func (qb *Query[T]) As(varName string) *Query[T] {
	qb.q.As(varName)
	return qb
}

// Name sets the query block name (the result key). It defaults to "data";
// dgman uses the name symmetrically to generate and decode the query, so a
// renamed block still decodes into []T.
func (qb *Query[T]) Name(queryName string) *Query[T] {
	qb.q.Name(queryName)
	return qb
}

// Vars supplies GraphQL variables for a parameterized query: funcDef is the
// query function definition (e.g. "getByName($n: string)") and vars binds
// each variable. The query then executes via dgraph's QueryWithVars path.
func (qb *Query[T]) Vars(funcDef string, vars map[string]string) *Query[T] {
	qb.q.Vars(funcDef, vars)
	return qb
}
```

Method names match dgman exactly; none collide with an existing `Query[T]`
method, so — unlike `First`→`Limit` — no rename is needed.

### Shape-changing transitions and `RawQuery`

`Var` and `GroupBy` return `*RawQuery`:

```go
// Var marks the query block as a dgraph var block. A var block computes query
// variables and returns no data of its own, so Var transitions out of the
// typed query: it returns a *RawQuery, which exposes no node terminal.
func (qb *Query[T]) Var() *RawQuery {
	qb.q.Var()
	return &RawQuery{q: qb.q}
}

// GroupBy adds an @groupby(predicate) aggregation. A grouped query returns
// aggregation groups rather than a slice of T, so GroupBy transitions out of
// the typed query: it returns a *RawQuery, which exposes no node terminal.
func (qb *Query[T]) GroupBy(predicate string) *RawQuery {
	qb.q.GroupBy(predicate)
	return &RawQuery{q: qb.q}
}
```

`RawQuery` is a new, non-generic type in package `typed`. Once a query has left
the typed-results world, `T` is meaningless, so carrying it would be an unused
type parameter.

```go
// RawQuery is a query whose result is not a slice of T — produced by the
// shape-changing builders Query.Var and Query.GroupBy. A RawQuery deliberately
// exposes no typed node terminal: its result must be decoded by the caller
// through the underlying dgman query, obtained via Raw.
type RawQuery struct {
	q *dg.Query
}

// Raw returns the underlying dgman query, for the caller to execute and decode.
func (r *RawQuery) Raw() *dg.Query { return r.q }

// String returns the generated DQL.
func (r *RawQuery) String() string { return r.q.String() }

// Var marks the block as a var block. See Query.Var.
func (r *RawQuery) Var() *RawQuery {
	r.q.Var()
	return r
}

// GroupBy adds an @groupby(predicate) aggregation. See Query.GroupBy.
func (r *RawQuery) GroupBy(predicate string) *RawQuery {
	r.q.GroupBy(predicate)
	return r
}
```

`RawQuery` re-exposes only `Var` and `GroupBy` — so the canonical
`.GroupBy(...).Var()` combination still chains — plus `Raw` and `String`. It
does not re-expose `Filter`/`Order`/`Limit`/etc.: those are set on `*Query[T]`
before the transition, or applied via `Raw()`.

The natural call order is: safe builders on `*Query[T]`, then `Var()`/
`GroupBy()` as the transition into `*RawQuery`. For example:

```go
raw := client.Query(ctx).Filter(`ge(year, 2000)`).As("genres").GroupBy("genre")
// raw is *RawQuery; decode via raw.Raw()
```

### Doc comment updates

`Raw()`'s comment names the six now-wrapped operations; it is replaced:

```go
// Raw returns the underlying dgman query for operations Query does not wrap
// (for example UID, Query, NodesAndCount).
func (qb *Query[T]) Raw() *dg.Query {
	return qb.q
}
```

The `Query[T]` type doc comment changes in two places:

- The opening line "Builder methods return `*Query[T]` for chaining" gains a
  trailing clause: "...except `Var` and `GroupBy`, which transition to
  `*RawQuery`."
- The "repeated builder calls" paragraph adds `As`, `Name`, `RootFunc`, and
  `Vars` to the overwrite list (last call wins), and a sentence noting that
  `Var` and `GroupBy` change the result shape and so return `*RawQuery`.

## Error handling

The four safe builders set a single field on the dgman query and cannot fail;
they have no error path, exactly like the existing builder methods. `Vars`
changes the *execution* path (dgman uses `QueryWithVars` when variables are
set) — any resulting error surfaces at the terminal (`Nodes`/`First`/
`IterNodes`), unchanged from how query-execution errors already surface.

`Var()` and `GroupBy()` cannot fail and have no error path. A `*RawQuery` has
no terminal, so it produces no error itself; execution and error handling
belong to whoever runs `RawQuery.Raw()`.

## Testing

New tests in `typed/query_test.go`, following the file's conventions —
behavioral tests against `newConn(t)`, string assertions via
`.Raw().String()`.

**Behavioral tests** (operation is safe to execute and decode):

- `RootFunc` — a query with `RootFunc` set to an `eq(name, ...)` expression,
  run through `Nodes()`, returns exactly the matching widget.
- `Name` — a query with `Name("widgets")` set, run through `Nodes()`, still
  returns all records. This is the executable proof of the decode-symmetry
  argument: a renamed block round-trips through dgman's prefix stripping.
- `Vars` — a parameterized query (`Vars("getByName($n: string)", {"$n": "b"})`
  with `RootFunc("eq(name, $n)")`) executed via `Nodes()` returns the `b`
  widget, exercising dgman's `QueryWithVars` path. Implementation-time check: if
  the embedded file engine rejects `QueryWithVars`, this test falls back to a
  `String()` assertion.

**String-assertion tests** (`.Raw().String()` / `RawQuery.String()`):

- `As` — output contains `x as data(`; plus an overwrite test (second `As`
  wins).
- `Name` — output contains `widgets(func:`; plus an overwrite test.
- `RootFunc` — an overwrite test (second `RootFunc` wins).
- `Var` — `RawQuery.String()` contains `var(func:`.
- `GroupBy` — `RawQuery.String()` contains `@groupby(name)`.

**`RawQuery` structural tests:**

- `Var()` and `GroupBy()` return a non-nil `*RawQuery`; `Raw()` returns the
  underlying `*dg.Query`; `String()` equals `Raw().String()`.
- The `.GroupBy("name").Var()` combination chains and emits both `@groupby` and
  `var`.
- That `*RawQuery` exposes no `Nodes`/`First`/`IterNodes` is a compile-time
  guarantee of the type, noted here rather than asserted at runtime.

## Migration / blast radius

- **Modified:** `typed/query.go` — four safe builder methods (`RootFunc`, `As`,
  `Name`, `Vars`), two transition methods (`Var`, `GroupBy`), the new
  `RawQuery` type, the `Raw()` doc comment, and the `Query[T]` type doc
  comment.
- **New tests** in `typed/query_test.go`.
- No change to `Nodes()`, `First()`, `IterNodes()`, `Limit`/`Offset`, CRUD, the
  generated `<E>Query` wrapper, or any generated artifact. `Raw()`'s signature
  and behavior are unchanged; only its doc comment changes.

## Open decisions

None. The layer scope (`typed.Query[T]` only), the safe/shape-changing split
(decided by dgman's decode behavior), the `RawQuery` transition type
(non-generic, `Var`/`GroupBy`/`Raw`/`String` only), and the decision not to
build a groupby decoder were all settled during brainstorming.
