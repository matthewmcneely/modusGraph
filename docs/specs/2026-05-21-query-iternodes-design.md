---
date: 2026-05-21
topic: query-iternodes
status: draft
---

# A Streaming `IterNodes()` Terminal for `typed.Query[T]`

## Goal

Give a built query a way to **stream** its results instead of only
materializing them. `typed.Query[T]` today has one collecting terminal,
`Nodes() ([]T, error)`, which loads the entire result set into a slice. Add a
second terminal, `IterNodes() iter.Seq2[*T, error]`, that pages through the
results transparently so a large or unbounded result set is never held in
memory at once.

The generated wrapper `<E>Query` gains a matching `IterNodes()` yielding
wrapped `*<E>` values, so the streaming terminal is reachable from the same
call site as `Nodes()`: `client.Foo.Query(ctx).Filter(...).IterNodes()`.

`typed.Client[T].Iter` — which already streams *all* records of a type — is
re-expressed in terms of the new terminal, removing a duplicated paging loop.

## Non-Goals

- A configurable page size. `IterNodes()` uses the existing fixed
  `defaultPageSize = 50`, matching `Client.Iter`.
- A top-level `<E>Client.Iter` (iterate-all from the generated wrapper client).
  That is a separate, parallel gap; this change is scoped to the query
  terminal.
- Cursor-based (`After`) iteration. `IterNodes()` is offset-paged.
- Changing `Nodes()` or `First()`.

## Why This Approach

Two design decisions, settled during brainstorming:

**Respect caller-set `Limit`/`Offset` (not override them).** A built query may
already carry `.Limit(n)`/`.Offset(n)`. `IterNodes()` must itself drive
`Offset`/`Limit` to page, so it collides with any caller-set bounds. The chosen
behavior: a caller `Limit` caps the total rows streamed; a caller `Offset` is
the starting point. This is the intuitive reading of
`.Offset(100).Limit(500).IterNodes()` and avoids a silent-discard footgun. The
cost is that `Query[T]` must track `limit`/`offset` in its own struct fields —
it currently delegates straight to dgman, whose fields are unexported and
unreadable. This is a small, contained departure from "pure pass-through."

**Unify `Client.Iter` onto the new terminal.** `Client[T].Iter` is structurally
"`IterNodes()` over an unfiltered query." Collapsing it removes a duplicated
paging loop — and the loop encodes subtle logic (offset advance, the
short-page stop condition, error-as-final-yield) that should have one tested
source of truth. The unification is also a correctness upgrade: see below.

**The one-transaction snapshot.** `IterNodes()` pages by re-executing a single
`*dg.Query`. modusgraph creates that query's read-only transaction once and
reuses it across executions, so every page reads from one server snapshot — a
writer committing mid-iteration cannot make the stream skip or duplicate rows.
The current `Client.Iter` builds a fresh query (and fresh transaction) per
page and *does* have that hazard; its doc comment admits it. Unification fixes
that. The mirror-image caveat — one pinned snapshot has a server-side lifetime,
so an extremely long-paused iteration could outlive it on remote Dgraph — is a
doc note, irrelevant to the embedded file engine.

## Design

### `typed.Query[T]` — tracked bounds + `IterNodes()`

`Query[T]` gains two fields. `0` means "unset" for both, consistent with
dgman, which emits the `first:`/`offset:` clauses only for non-zero values.

```go
type Query[T any] struct {
	q      *dg.Query
	limit  int // caller-set row cap; 0 = unbounded
	offset int // caller-set starting offset; 0 = none
}
```

`Limit` and `Offset` record the value locally as well as forwarding to dgman:

```go
func (qb *Query[T]) Limit(n int) *Query[T] {
	qb.limit = n
	qb.q.First(n)
	return qb
}

func (qb *Query[T]) Offset(n int) *Query[T] {
	qb.offset = n
	qb.q.Offset(n)
	return qb
}
```

`Nodes()`, `First()`, `Raw()`, and all other builder methods are unchanged —
they already execute `qb.q`, which carries the bounds via the dgman calls.

New terminal:

```go
// IterNodes executes the query and returns an iterator over matching records,
// paging transparently so a large result set is never materialized at once.
//
// IterNodes is a terminal operation: it drives Offset/Limit internally as it
// pages and leaves the builder spent — do not call another terminal on the
// same Query afterward. A Limit set on the query caps the total number of
// rows streamed; an Offset is the starting point.
//
// All pages execute against one read-only transaction, so the iteration reads
// a single consistent snapshot: a concurrent writer cannot make it skip or
// repeat rows. On error it yields a final (nil, err) and stops.
func (qb *Query[T]) IterNodes() iter.Seq2[*T, error] {
	return func(yield func(*T, error) bool) {
		remaining := qb.limit // 0 = unbounded
		for off := qb.offset; ; off += defaultPageSize {
			size := defaultPageSize
			if remaining > 0 && remaining < size {
				size = remaining // shrink the last page so it can't overshoot the cap
			}
			var page []T
			if err := qb.q.Offset(off).First(size).Nodes(&page); err != nil {
				yield(nil, err)
				return
			}
			for i := range page {
				if !yield(&page[i], nil) {
					return // consumer broke out
				}
			}
			if remaining > 0 {
				if remaining -= len(page); remaining <= 0 {
					return // hit the caller's Limit
				}
			}
			if len(page) < size {
				return // result set exhausted
			}
		}
	}
}
```

The `Query[T]` type doc comment's terminal list (`Nodes, First`) gains
`IterNodes`.

Behavior across the cases:

- `q.IterNodes()` — `offset 0, limit 0` — streams every matching record,
  50 at a time, until a short page.
- `q.Offset(100).Limit(120).IterNodes()` — pages `100‑149`, `150‑199`, then a
  final `size=20` page `200‑219`; stops at exactly 120 rows.
- `q.Filter(...).IterNodes()` — streams all matches.

### `Client.Iter` unification

`Client[T].Iter` collapses to a delegation:

```go
// Iter returns an iterator over every T, paging transparently. All pages read
// one consistent read-only snapshot. On error it yields a final (nil, err).
func (c *Client[T]) Iter(ctx context.Context) iter.Seq2[*T, error] {
	return c.Query(ctx).IterNodes()
}
```

A fresh `c.Query(ctx)` carries no bounds, so `IterNodes()` streams everything —
identical observable behavior to the prior loop. The `defaultPageSize` const
stays (now consumed by `IterNodes`).

The current `Client.Iter` doc comment warns "a data set mutated mid-iteration
may skip or repeat rows." After unification that is false — the iteration
pages one snapshot. The doc comment is corrected to describe the
snapshot-consistent behavior (shown above).

### Wrapper layer — generated `<E>Query.IterNodes()`

`cmd/modusgraph-gen/internal/generator/templates/wrapper_query.go.tmpl` gains a
generated `IterNodes()` — the streaming analogue of the existing generated
`Nodes()`:

```go
// IterNodes streams the query's results as wrapped {{ $E }} values, paging
// transparently. Terminal operation; see typed.Query.IterNodes.
func (q *{{ $E }}Query) IterNodes() iter.Seq2[*{{ $E }}, error] {
	return func(yield func(*{{ $E }}, error) bool) {
		for s, err := range q.typed.IterNodes() {
			if err != nil {
				yield(nil, err)
				return
			}
			if !yield(Wrap{{ $E }}(s), nil) {
				return
			}
		}
	}
}
```

The template adds `"iter"` to its import block. The inner `q.typed.IterNodes()`
yields `*schema.<E>`, so `Wrap<E>(s)` applies directly. The `movies` fixture is
regenerated.

## Error handling

`IterNodes()` is a pure pass-through of errors from the underlying
`modusgraph.Client` query execution. On the first page that errors, it yields
exactly one `(nil, err)` pair and stops — the established `Client.Iter`
contract. Builder methods never execute, so no error can arise before the
first `yield`. The wrapper `<E>Query.IterNodes()` forwards the error pair
unchanged.

## Testing

- **`typed/query_test.go`** — behavioral tests against the local file-backed
  client: unbounded `IterNodes()` streams all records; a caller `Limit` caps
  the total; a caller `Offset` is the start; `Offset`+`Limit` yields the exact
  window; a >50-record set forces multiple pages; a consumer `break` stops
  iteration early; the error path; an empty result yields nothing. A regression
  test that `Limit`/`Offset` still drive `Nodes()` correctly now that they also
  set struct fields. A counting-logger test (`newCountingConn`) asserting an
  N-record stream executes ⌈N/50⌉ queries — builder methods execute none.
- **`typed/client_test.go`** — the existing `Client.Iter` tests
  (`TestClient_IterPagesThroughAllRecords`, `TestClient_IterStopsOnConsumerBreak`)
  must still pass unchanged after the unification; they assert record counts,
  which are unaffected.
- **`generator_test.go`** — `TestGenerate_WrapperQuery` gains an assertion that
  the generated `<E>Query` includes the `IterNodes` method.
- **`wrapper_query_e2e_test.go`** — a behavioral test that
  `client.Film.Query(ctx)...IterNodes()` streams correctly wrapped `*Film`
  values.

## Migration / blast radius

- **Modified:** `typed/query.go` (two struct fields, `Limit`/`Offset` bodies,
  the `IterNodes` terminal, the type doc comment); `typed/client.go`
  (`Client.Iter` collapses to a delegation, doc comment corrected);
  `wrapper_query.go.tmpl` (generated `IterNodes` + `iter` import);
  `generator_test.go` (one added assertion).
- **Regenerated:** the `movies` fixture — every `*_query_gen.go` gains an
  `IterNodes` method.
- **New tests** in `typed/query_test.go` and `wrapper_query_e2e_test.go`.
- No change to `Nodes()`, `First()`, CRUD, or any other generated artifact.
  `Client.Iter`'s signature and observable behavior are unchanged (its internal
  paging and doc comment change).

## Open decisions

None. Naming (`IterNodes`), the caller-bounds policy (respect them), the
page size (fixed `defaultPageSize`), and the `Client.Iter` unification were all
settled during brainstorming.
