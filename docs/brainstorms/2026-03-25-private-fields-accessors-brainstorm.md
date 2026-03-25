---
date: 2026-03-25
topic: private-fields-accessors
---

# Private Fields with Generated Getters/Setters

## What We're Building

When entity struct fields are lowercase (private/unexported), the code generator
should produce getters, setters, slice helpers, and serialization methods for them.

For edge fields annotated with `validate:"max=1"` or `validate:"len=1"`, the
getter and setter operate on `*Type` (pointer to the edge entity) rather than
`[]Type`, flattening the slice into a singular optional value.

The generated code also handles Dgraph serialization for private fields via
generated `toMap()` (writes) and `UnmarshalJSON()` (reads), since Go's
`encoding/json` and dgman cannot access unexported fields natively.

## Serialization Strategy (Approach 2D)

### The Problem

Go's `encoding/json` ignores unexported fields. dgman (the Dgraph ORM layer)
uses `reflect` to walk struct fields and `CanInterface()` to read values — both
skip unexported fields. This means private fields would not persist to or load
from Dgraph without intervention.

### Approaches Considered

| Approach | Description | Verdict |
|----------|-------------|---------|
| 1: Keep fields exported | Generate accessors on exported fields | Confusing dual API surface |
| 2A: Fork dgman | Patch `filterStruct` to read unexported fields | Maintenance burden, uses `unsafe` |
| 2B: Custom marshal layer | Pre-mutation conversion in modusgraph | Duplicates dgman logic |
| 2C: Generated MarshalJSON | Custom `MarshalJSON()` on entities | dgman bypasses `MarshalJSON` on mutation path |
| **2D: Generated toMap() + UnmarshalJSON** | **`toMap()` for writes, `UnmarshalJSON()` for reads** | **Chosen — clean, no dgman fork, no unsafe** |

### Why 2D

- **Write path:** dgman's `filterStruct` uses reflect and skips private fields,
  but `MutateBasic` also accepts maps. Generated `toMap()` converts the struct
  (with private fields) to `map[string]interface{}` — the generated code is in
  the same package and can access private fields. A thin engine hook detects
  structs that implement `DgraphMapper` and routes them through the map path.

- **Read path:** dgman queries return JSON which is decoded via `json.Unmarshal`.
  A generated `UnmarshalJSON()` method is automatically called by the standard
  library, populating private fields from the JSON response.

- **UID writeback:** After mutation, UIDs must be written back to the struct.
  Since UID is always exported, a simple `reflect.FieldByName("UID").SetString()`
  works after the map-based mutation.

- **Backward compatibility:** Existing structs with all-exported fields don't
  implement `DgraphMapper`, so they take the existing dgman code path. Zero
  regression.

- **Schema auto-generation:** Verified that dgman's `CreateSchema` →
  `TypeSchema.Marshal` uses `reflect.Type.Field(i)` which sees ALL fields
  including unexported ones. Struct tags are type metadata, accessible regardless
  of export status. The `CanInterface()` check only blocks private fields in the
  mutation data path, not the schema path. And `process()` passes the original
  struct to `UpdateSchema` before our `DgraphMapper` intercept. No schema changes
  needed — private fields are correctly included in Dgraph schema generation.

- **Performance:** Generated `toMap()` is ~3-5x faster than dgman's reflect-based
  `filterStruct` for typical entities (5-10 fields). Reflect operations cost
  ~100-300ns each (field access, tag parsing, `CanInterface()`, `isNull()` via
  `reflect.DeepEqual`); generated code uses direct field access (~1-5ns) with
  compile-time tag resolution. For a struct with 8 fields: `filterStruct` ~2-4μs
  vs `toMap()` ~200-500ns. Negligible vs Dgraph network round-trip for single
  mutations, but meaningful for batch inserts (thousands of entities). The
  performance gain is a free bonus of generated code, not the primary motivation.

### Serialization Flow

```
WRITE PATH:
  User struct (private fields)
      ↓
  toMap() — generated, same-package access to private fields
      ↓
  dgman.MutateBasic(map) — map is natively supported
      ↓
  json.Marshal(map) → Dgraph
      ↓
  UID writeback via reflect on exported UID field

READ PATH:
  Dgraph response (JSON)
      ↓
  json.Unmarshal → calls generated UnmarshalJSON()
      ↓
  User struct (private fields populated)
```

## Accessor Design Decisions

### Singular Edge Signatures

We explored three options for `max=1`/`len=1` edge signatures:

- **Option A: `*Type` everywhere** — both `max=1` and `len=1` use pointer getter/setter
- **Option B: Value type for `len=1`** — express "required" in the signature
- **Option C: Hybrid** — value setter, pointer getter for `len=1`

We chose **Option A** because:
- Matches protobuf codegen convention (the most widely-used Go codegen)
- Consistent API — callers don't need to remember which edges are optional vs required
- The `validate` tag enforces cardinality at runtime; no need to duplicate in type system
- Go lacks non-nullable reference types, so encoding "required" fights the language

### Scalar Signatures

Value types (no pointers) because:
- No behavior change from direct field access today
- `validate:"required"` catches empty values at runtime

## Key Decisions

- **Getter naming**: Go convention — `Director()` not `GetDirector()`. Setters use `Set` prefix: `SetDirector()`
- **Singular edge naming**: Use `EdgeEntity` name, not field name. `directors []Director` with `max=1` → `Director()` / `SetDirector()`
- **UID and DType**: Always remain exported regardless of other field visibility
- **Model field**: Simple `IsSingularEdge bool` — YAGNI on richer cardinality tracking
- **Scalar getters/setters**: Value types matching the field's Go type
- **Edge getters/setters**: `*Type` for singular edges; standard `[]Type` for multi-edges
- **Serialization**: Generated `toMap()` + `UnmarshalJSON()` (Approach 2D)

### Field Mapping Rules

1. No `json` tag → **skip** (no accessors, no functional option, no serialization)
2. `dgraph:"-"` → **skip** (explicit opt-out, even if field has a json tag)
3. UID and DType fields → **always exported**, never generate accessors
4. Everything else → **mapped** (getters, setters, serialization, and where applicable append/remove)

### Slice Helper Methods

All slice fields (primitive and edge, but NOT singular edges) get:

- **`Append[Field](v ...Type)`** — variadic append
- **`Remove[Field](v)`** — remove by value (primitives) or by UID string (edges)
- **`Remove[Field]Func(fn func(Type) bool)`** — remove by predicate, using `slices.DeleteFunc` internally (Go 1.21+ stdlib convention)

Singular edge fields (`max=1`/`len=1`) do NOT get append/remove — only `Get`/`Set` with `*Type`.

## Design Summary

### User writes:
```go
type Film struct {
    UID        string     `json:"uid,omitempty"`
    DType      []string   `json:"dgraph.type,omitempty"`
    name       string     `json:"name,omitempty" dgraph:"index=hash,term"`
    director   *Director  `json:"director,omitempty" validate:"max=1"`
    genres     []Genre    `json:"genres,omitempty"`
    tags       []string   `json:"tags,omitempty"`
    tempCache  string                                                     // skipped: no json tag
    Internal   string     `json:"internal,omitempty" dgraph:"-"`         // skipped: explicit opt-out
}
```

### Generator produces:

```go
// --- Scalar getter/setter (value types) ---
func (f *Film) Name() string     { return f.name }
func (f *Film) SetName(v string) { f.name = v }

// --- Singular edge getter/setter (*Type) ---
func (f *Film) Director() *Director { return f.director }
func (f *Film) SetDirector(v *Director) { f.director = v }

// --- Multi-edge getter/setter + helpers ---
func (f *Film) Genres() []Genre              { return f.genres }
func (f *Film) SetGenres(v []Genre)          { f.genres = v }
func (f *Film) AppendGenres(v ...Genre)      { f.genres = append(f.genres, v...) }
func (f *Film) RemoveGenres(uid string)      { /* filter by UID */ }
func (f *Film) RemoveGenresFunc(fn func(Genre) bool) {
    f.genres = slices.DeleteFunc(f.genres, fn)
}

// --- Primitive slice getter/setter + helpers ---
func (f *Film) Tags() []string              { return f.tags }
func (f *Film) SetTags(v []string)           { f.tags = v }
func (f *Film) AppendTags(v ...string)       { f.tags = append(f.tags, v...) }
func (f *Film) RemoveTags(v string)          { /* filter out first match */ }
func (f *Film) RemoveTagsFunc(fn func(string) bool) {
    f.tags = slices.DeleteFunc(f.tags, fn)
}

// --- Serialization (generated) ---
func (f *Film) toMap() map[string]interface{} {
    m := make(map[string]interface{})
    if f.UID != "" { m["uid"] = f.UID }
    if len(f.DType) > 0 { m["dgraph.type"] = f.DType }
    if f.name != "" { m["name"] = f.name }
    if f.director != nil { m["director"] = f.director.toMap() }
    if len(f.genres) > 0 { /* marshal each genre */ }
    if len(f.tags) > 0 { m["tags"] = f.tags }
    return m
}

func (f *Film) UnmarshalJSON(data []byte) error {
    // Uses exported alias struct to decode, then assigns to private fields
}

// tempCache — no json tag, SKIPPED
// Internal  — dgraph:"-", SKIPPED
```

## Resolved Questions

- **Functional options**: Yes, still generated for private fields. Call setters instead of direct assignment.
- **CLI template**: Yes, use setters for private fields.
- **Slice helpers**: Append (variadic), Remove (by value or UID), RemoveFunc (predicate). `Func` suffix convention.
- **Opt-out**: No json tag → skip. `dgraph:"-"` → explicit skip. Both work on public and private fields.
- **Serialization**: Approach 2D — generated `toMap()` for writes, `UnmarshalJSON()` for reads. Thin engine hook routes private-field structs through map path. No dgman fork. No `unsafe`.

## Change Map

| Component | File | Change |
|-----------|------|--------|
| **Model** | `model.go` | Add `IsPrivate bool`, `IsSingularEdge bool`, `IsSkipped bool` |
| **Parser** | `parser.go` | Stop skipping unexported fields, parse `validate` tag, detect `dgraph:"-"` and missing json tag |
| **Generator** | `generator.go` | Add `privateFields`, `sliceFields` helpers, register new templates |
| **Template (new)** | `accessors.go.tmpl` | Getter/setter/append/remove generation for private fields |
| **Template (new)** | `marshal.go.tmpl` | `toMap()` and `UnmarshalJSON()` generation |
| **Template (update)** | `options.go.tmpl` | Use setters for private fields |
| **Template (update)** | `cli.go.tmpl` | Use setters for private fields |
| **Engine** | `client.go` | Add `DgraphMapper` interface, route Insert/Update/Upsert through map path |
| **Engine** | `mutate.go` or new file | UID writeback helper for post-mutation struct update |

## Next Steps

-> `/workflows:plan` for implementation details
