---
date: 2026-05-23
topic: typed-filter-helpers
status: draft
---

# Typed Scalar-Predicate Filters for Generated Query Builders

## Goal

Let a generated `<Entity>Query` filter records by a root scalar predicate using
typed values instead of raw DQL strings:

```go
client.Comment.Query(ctx).
    ByID(ids...).         // []filter.UUID
    ByName(names...).     // []filter.String
    Nodes()
```

Today, callers must hand-author DQL fragments:

```go
client.Comment.Query(ctx).Filter("eq(id, $1) OR eq(id, $2)", id1, id2).Nodes()
```

…or build their own typed wrapper as `go-registry-poc` did — ~200 lines of
`UUIDFilter`/`StringFilter`/`dqlBuilder`/`<Entity>Filters`-with-`toDQL()`
hand-rolled in `internal/service/`. The substrate (a parameterised, OR-within-
group / AND-between-group DQL expression builder) is mechanical and worth
generating once.

## Non-Goals

- **Closed `<Entity>Filters` structs.** Consumers keep their own HTTP-shaped
  filter struct. The generator emits per-predicate methods on Query, not a
  whole struct that consumers must adopt as-is.
- **Custom domain rules.** Things like "archive_status defaults to active" or
  "ResourceType maps wire-form to repo-form" stay hand-written on the consumer
  side. The generator handles predicate-name binding, not semantics.
- **Non-equality operators.** v1 emits `By<Field>(filter.X...)` that translates
  to `eq(<predicate>, $N)` terms joined by `OR` within the field, `AND` across
  fields. Future work can add `By<Field>Like`, `By<Field>Between`, etc.
- **Non-string/UUID scalar types.** v1 covers fields typed as `string`,
  `scalars.UUID`, or known string-aliased types. `int64`, `time.Time`, `bool`,
  custom enums — defer; consumers can still use raw `.Filter()` for those.
- **Multi-hop or edge-target filters.** `WhereEdge` already covers those.

## Why Now

The `go-registry-poc` migration to spec-faithful wire types (#8, #9, #10) made
the hand-rolled `toDQL()` layer visible: every entity has its own
`<X>_filter.go`, every endpoint has a `.Filter(expr, fparams...)` boilerplate
chain after `filters.toDQL()`. The shape of the boilerplate is identical
across entities, and the only reason it isn't generated is that nobody pushed
it down into modusgraph.

## Architecture

### Part A: `typed/filter` runtime package

A new sub-package of `typed` holding the runtime substrate. Consumer-facing:

```go
package filter

// UUID is one UUID-valued filter term, optionally negated.
type UUID struct {
    Negated bool
    Value   string
}

// String is one string-valued filter term, optionally negated.
type String struct {
    Negated bool
    Value   string
}

// ParseUUID parses "value" or "!value" into a UUID. A leading "!" negates.
func ParseUUID(s string) UUID
func ParseString(s string) String

// Builder composes parameterised DQL @filter expressions. Terms within a group
// join with OR; groups join with AND.
type Builder struct { /* unexported */ }

func (b *Builder) EqGroupUUID(predicate string, terms []UUID)
func (b *Builder) EqGroupString(predicate string, terms []String)
func (b *Builder) RequiredEq(predicate, value string)
func (b *Builder) Build() (expr string, params []any)
```

Internal-only term abstraction:

```go
// term is a predicate-agnostic filter value, used by Builder.
type term struct {
    value   string
    negated bool
}
```

The Builder is exported so consumers who need bespoke combinations
(e.g., `ResourceFilters.ArchiveStatus`'s default-to-active rule) can use it
directly without going through a generated method.

### Part B: Generator extension — per-predicate `By<Field>` methods

For each entity processed by `modusgraph-gen`, the wrapper-query template
inspects each scalar field. For each field whose Go type maps onto a known
filter type AND whose dgraph tag declares some non-edge index, emit:

```go
// In <entity>_gen.go alongside existing Query methods:
func (q *CommentQuery) ByID(filters ...filter.UUID) *CommentQuery {
    var b filter.Builder
    b.EqGroupUUID("id", filters)
    if expr, params := b.Build(); expr != "" {
        q.typed.Filter(expr, params...)
    }
    return q
}
```

Field-name → Go-method mapping is the field's exported name with `By` prefix
(`ID` → `ByID`, `Name` → `ByName`, `ExternalIdentifier` → `ByExternalIdentifier`).

Predicate-name lookup uses the existing dgraph-tag parser: explicit
`predicate=foo` if present, otherwise the camelCase field name (existing
modusgraph-gen behaviour).

### Type-mapping table (v1)

| Go field type                | Generated method signature                                |
|------------------------------|-----------------------------------------------------------|
| `scalars.UUID`               | `By<Field>(filters ...filter.UUID) *<Entity>Query`        |
| any other named string alias | `By<Field>(filters ...filter.String) *<Entity>Query`      |
| `string`                     | `By<Field>(filters ...filter.String) *<Entity>Query`      |
| anything else                | *skipped*; consumer uses raw `.Filter()`                  |

Detection: the existing parser already records the Go type name as it appears
in the schema source. The generator adds a small classifier that maps that
type name (e.g. `"string"`, `"scalars.UUID"`, `"enums.ResourceType"`) to a
filter-type spec. Unknown types are silently skipped (logged at DEBUG).

### Indexed-predicate gate

Only emit `By<Field>` for predicates with a dgraph index — `index=hash`,
`index=exact`, `index=term`, etc. Querying on an unindexed predicate would
work at the DQL level but force a full scan; better to skip and let the
consumer drop `index=...` into the schema if they really want the API.

## What stays in the consumer

The `go-registry-poc` simplification after this lands:

| Today (~206 lines)                    | After (~70 lines)                              |
|----------------------------------------|------------------------------------------------|
| `filter.go` runtime primitives         | *deleted* (now in modusgraph)                  |
| `filter.go`: `ResourceTypeFilter`      | stays — wire-enum mapping is domain            |
| `filter.go`: `ArchiveStatusFilter`     | stays — default-to-active is domain            |
| `comment_filter.go` + `toDQL`          | struct stays; `toDQL` deleted                  |
| `revision_filter.go` + `toDQL`         | struct stays; `toDQL` deleted                  |
| `resource_filter.go` + `toDQL`         | struct stays; `toDQL` partially deleted —      |
|                                        | `ResourceType`/`ArchiveStatus` handling stays  |

Service-layer call sites change from:

```go
expr, fparams := filters.toDQL()
q := s.client.Comment.Query(ctx).WhereCommentOn("eq(id, $1)", resourceID)
if expr != "" {
    q = q.Filter(expr, fparams...)
}
```

to:

```go
q := s.client.Comment.Query(ctx).
    WhereCommentOn("eq(id, $1)", resourceID).
    ByID(filters.CommentID...)
```

`ResourceService.List` is the most complex case (typed `ByID` + `ByName` + raw
`.Filter()` for `ResourceType` and `ArchiveStatus` defaulting). The raw escape
hatch is still there for the domain bits.

## Risks & mitigations

**Risk:** Field-name collision with existing methods. If an entity has a field
named `Filter`, `Where`, or `ID` (well, `ID` is fine — it becomes `ByID`),
the generated `By<Field>` could collide with an existing query method.
*Mitigation:* the existing methods are `Filter`, `Where<Edge>`, `OrderAsc/Desc`,
`Limit`, `Offset`, `Nodes`, `First`, `IterNodes`. None starts with `By`. Reserve
`By` prefix in the generator's reserved-words list; assert in unit tests.

**Risk:** Stutter at the import site. `filter.String` reads close to Go's
builtin `string`. *Mitigation:* the package qualifier makes intent obvious in
context (`filter.ParseString`, `[]filter.String`); the brevity is worth more
than the avoid-stutter rule for a sub-package this small. If reviewers push
back during the modusgraph PR, rename to `filter.StringTerm` / `filter.UUIDTerm`.

**Risk:** Generator complexity creep. A type classifier and a new template
section add surface area. *Mitigation:* test against the existing `movies/`
fixture: add a movie field with `dgraph:"index=hash"` and assert that
`ByTitle(filters ...filter.String)` is emitted. Test that unindexed fields
do NOT get methods.

**Risk:** Two-PR coordination (modusgraph release, then registry-poc bump).
*Mitigation:* same dance as Phase 2 of the wire migration. Spec/plan covers
both. Modusgraph ships first as a dev release; registry-poc bumps and
deletes its hand-rolled primitives in the second PR.

## Out of scope

- Operators beyond `eq`. `Like(...)`, `Between(...)`, `In(...)`, `Gt(...)`,
  etc. are future extensions; the runtime `Builder` will need new methods
  but the template pattern is reusable.
- Filters for `int64`, `time.Time`, `bool`, named-enum types. Future work
  once the v1 string/UUID cases prove out.
- HTTP query-string parsing helpers beyond `ParseUUID` / `ParseString`. The
  full HTTP wiring belongs in the consumer.
- Removing `Query.Filter(string, ...any)`. Stays as the escape hatch.

## Deliverables

1. **Modusgraph PR**
   - `typed/filter/filter.go`: `UUID`, `String`, `Builder`, parsers
   - `typed/filter/filter_test.go`: builder behaviour
   - `cmd/modusgraph-gen/internal/generator/templates/wrapper_query.go.tmpl`:
     `By<Field>` emission block
   - `cmd/modusgraph-gen/internal/parser/...`: scalar-type → filter-type
     classifier (small addition)
   - Regenerate `cmd/modusgraph-gen/internal/parser/testdata/movies/...` to
     include `By<Field>` methods (and update goldens)
   - Tag: `v0.5.0-dev-mlwelles-<YYYYMMDD>a`
   - GitHub Release on `mlwelles/modusGraph`

2. **Registry-poc PR (depends on the modusgraph release)**
   - Bump go.mod pin to the new dev release
   - Delete `internal/service/filter.go`'s shared primitives; keep
     `ResourceTypeFilter` and `ArchiveStatusFilter` only
   - Delete `toDQL()` methods from `comment_filter.go`, `revision_filter.go`
   - Trim `resource_filter.go`'s `toDQL` to just the domain bits
     (`ResourceType` enum mapping + `ArchiveStatus` default rule)
   - Update service call sites to chain `By<Field>` calls
   - Update any tests that referenced the deleted primitives

## Verification

- Modusgraph: `go test ./...` (movies fixture + new filter tests pass)
- Registry-poc: `make test` (full integration suite against live Dgraph)
- Manual: `q := client.Comment.Query(ctx).ByID(id1, id2).Nodes()` produces a
  query whose DQL @filter is `(eq(id, $1) OR eq(id, $2))` with `$1=id1, $2=id2`
