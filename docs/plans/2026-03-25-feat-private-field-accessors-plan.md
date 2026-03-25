---
title: "feat: Private field accessors with generated serialization"
type: feat
date: 2026-03-25
---

# feat: Private Field Accessors with Generated Serialization

## Overview

Add support for private (unexported) struct fields in modusGraph entities. The
code generator produces getters, setters, slice helpers, and serialization
methods (`toMap()` / `UnmarshalJSON()`) so that private fields persist correctly
to Dgraph without requiring engine forks or `unsafe` code.

Brainstorm: `docs/brainstorms/2026-03-25-private-fields-accessors-brainstorm.md`

## Problem Statement

Currently, all entity fields must be exported for Dgraph serialization to work
(dgman + `encoding/json` skip unexported fields). This means consumers can
bypass any intended API surface and directly mutate struct fields, making it
impossible to enforce invariants or provide a clean accessor API.

## Proposed Solution

**Approach 2D:** Generate `toMap()` for the write path and `UnmarshalJSON()` for
the read path. A thin engine hook (`DgraphMapper` interface) routes private-field
entities through the map-based mutation path. No dgman fork, no `unsafe`.

## Implementation Phases

### Phase 1: Model & Parser (Foundation)

**Goal:** The parser recognizes private fields, validate tags, and opt-out markers.

#### 1.1 Update `model.Field` — `model.go`

Add three fields:

```go
type Field struct {
    // ... existing fields ...
    IsPrivate      bool // True if the Go field name is lowercase (unexported)
    IsSingularEdge bool // True if edge field has validate:"max=1" or validate:"len=1"
    IsSkipped      bool // True if field has no json tag or dgraph:"-"
}
```

Update the `Entity.Fields` doc comment (line 19) from "All exported fields" to
"All mapped fields from the struct."

- [x] Add `IsPrivate`, `IsSingularEdge`, `IsSkipped` to `model.Field` in `cmd/modusgraph-gen/internal/model/model.go`
- [x] Update `Fields` doc comment

#### 1.2 Update parser — `parser.go`

**A. Stop skipping unexported fields** (line 131):

```go
// Before:
if !ast.IsExported(fieldName) {
    continue
}
// After:
field.IsPrivate = !ast.IsExported(fieldName)
```

**B. Parse `validate` tag** — add after dgraph tag parsing (after line 160):

```go
validateTag := tag.Get("validate")
if validateTag != "" {
    parseValidateTag(validateTag, &field)
}
```

New function `parseValidateTag`:

```go
func parseValidateTag(tag string, field *model.Field) {
    for _, rule := range strings.Split(tag, ",") {
        rule = strings.TrimSpace(rule)
        if rule == "max=1" || rule == "len=1" {
            field.IsSingularEdge = true
        }
    }
}
```

**C. Detect `dgraph:"-"`** — add to `parseDgraphTag` (line 314, before directive parsing):

```go
if tag == "-" {
    field.IsSkipped = true
    return
}
```

**D. Detect missing json tag** — set `IsSkipped` when no json tag present (after
tag parsing, for non-UID/non-DType fields):

```go
if field.JSONTag == "" && !field.IsUID && !field.IsDType {
    field.IsSkipped = true
}
```

**E. Detect singular edges from type** — for private fields, `*Director` (pointer
to entity) should also be detected as a singular edge:

```go
// After existing []Entity edge detection (line 179-185):
if strings.HasPrefix(goType, "*") {
    elemType := goType[1:]
    if structNames[elemType] {
        field.IsEdge = true
        field.EdgeEntity = elemType
        field.IsSingularEdge = true
    }
}
```

- [x] Remove exported-only guard in `parseStruct`, set `IsPrivate` — `parser.go:131`
- [x] Add `parseValidateTag` function — `parser.go`
- [x] Add `dgraph:"-"` detection in `parseDgraphTag` — `parser.go:305`
- [x] Add no-json-tag skip logic in `parseStruct` — `parser.go`
- [x] Add `*Entity` singular edge detection — `parser.go`

#### 1.3 Update inference — `inference.go`

No changes needed. `IsSingularEdge` is set during parsing. Existing inference
(searchability) works on field names, which are preserved.

#### 1.4 Tests for Phase 1

- [x] Add test cases for `parseValidateTag` in `parser_test.go`:
  - `validate:"max=1"` → `IsSingularEdge=true`
  - `validate:"required,max=1"` → `IsSingularEdge=true`
  - `validate:"len=1"` → `IsSingularEdge=true`
  - `validate:"max=10"` → `IsSingularEdge=false`
  - `validate:"required"` → `IsSingularEdge=false`
- [x] Add test cases for `dgraph:"-"` → `IsSkipped=true`
- [x] Add test struct with private fields to `testdata/movies/` (Studio entity)
- [x] Test that private fields are parsed (not skipped)
- [x] Test that fields without json tag are excluded from Fields
- [x] Test that `*Director` is detected as singular edge

### Phase 2: Generator Helpers & Accessor Template

**Goal:** Generate getters, setters, and slice helpers for private fields.

#### 2.1 Generator helpers — `generator.go`

Add new template functions:

```go
// privateFields returns fields where IsPrivate=true and IsSkipped=false
func privateFields(fields []model.Field) []model.Field { ... }

// privateScalarFields returns private non-edge, non-UID, non-DType, non-skipped fields
func privateScalarFields(fields []model.Field) []model.Field { ... }

// privateSingularEdgeFields returns private edge fields with IsSingularEdge=true
func privateSingularEdgeFields(fields []model.Field) []model.Field { ... }

// privateMultiEdgeFields returns private edge fields with IsSingularEdge=false
func privateMultiEdgeFields(fields []model.Field) []model.Field { ... }

// privateSliceFields returns private non-edge slice fields ([]string, []float64, etc.)
func privateSliceFields(fields []model.Field) []model.Field { ... }

// hasPrivateFields returns true if entity has any private, non-skipped fields
func hasPrivateFields(fields []model.Field) bool { ... }
```

Register in `funcMap`:

```go
"privateFields":            privateFields,
"privateScalarFields":      privateScalarFields,
"privateSingularEdgeFields": privateSingularEdgeFields,
"privateMultiEdgeFields":   privateMultiEdgeFields,
"privateSliceFields":       privateSliceFields,
"hasPrivateFields":         hasPrivateFields,
```

- [x] Add helper functions — `generator.go`
- [x] Register in template `funcMap` — `generator.go:57`
- [x] Update `scalarFields` to also exclude `IsSkipped` fields — `generator.go:213`

#### 2.2 New template: `accessors.go.tmpl`

Generates per-entity `<snake>_accessors_gen.go` — only emitted when entity has
private fields.

```
{{- if hasPrivateFields .Entity.Fields}}
package {{.PackageName}}

import "slices"

// --- Scalar getters/setters ---
{{range privateScalarFields .Entity.Fields}}
func (e *{{$name}}) {{.Name | toCamelCase}}() {{.GoType}} { return e.{{.Name}} }
func (e *{{$name}}) Set{{.Name | toCamelCase}}(v {{.GoType}}) { e.{{.Name}} = v }
{{end}}

// --- Singular edge getters/setters ---
{{range privateSingularEdgeFields .Entity.Fields}}
func (e *{{$name}}) {{.EdgeEntity}}() *{{.EdgeEntity}} { return e.{{.Name}} }
func (e *{{$name}}) Set{{.EdgeEntity}}(v *{{.EdgeEntity}}) { e.{{.Name}} = v }
{{end}}

// --- Multi-edge getters/setters + helpers ---
{{range privateMultiEdgeFields .Entity.Fields}}
func (e *{{$name}}) {{.Name | toCamelCase}}() []{{.EdgeEntity}} { return e.{{.Name}} }
func (e *{{$name}}) Set{{.Name | toCamelCase}}(v []{{.EdgeEntity}}) { e.{{.Name}} = v }
func (e *{{$name}}) Append{{.Name | toCamelCase}}(v ...{{.EdgeEntity}}) { e.{{.Name}} = append(e.{{.Name}}, v...) }
func (e *{{$name}}) Remove{{.Name | toCamelCase}}(uid string) {
    e.{{.Name}} = slices.DeleteFunc(e.{{.Name}}, func(v {{.EdgeEntity}}) bool { return v.UID == uid })
}
func (e *{{$name}}) Remove{{.Name | toCamelCase}}Func(fn func({{.EdgeEntity}}) bool) {
    e.{{.Name}} = slices.DeleteFunc(e.{{.Name}}, fn)
}
{{end}}

// --- Primitive slice getters/setters + helpers ---
{{range privateSliceFields .Entity.Fields}}
func (e *{{$name}}) {{.Name | toCamelCase}}() {{.GoType}} { return e.{{.Name}} }
func (e *{{$name}}) Set{{.Name | toCamelCase}}(v {{.GoType}}) { e.{{.Name}} = v }
// ... Append, Remove, RemoveFunc ...
{{end}}
{{- end}}
```

- [x] Create `accessors.go.tmpl` — `templates/accessors.go.tmpl`
- [x] Register in `Generate()` — emit `<snake>_accessors_gen.go` only when `hasPrivateFields` — `generator.go`
- [x] Update `TestGenerateOutputFiles` to expect accessor files when private fields exist — `generator_test.go`

#### 2.3 Tests for Phase 2

- [x] Add test entity with private fields to `testdata/movies/` (e.g., add private fields to existing entity or create new one)
- [x] Verify accessor file is generated with correct method signatures
- [x] Verify accessor file is NOT generated for entities with only exported fields
- [x] Run `go test -update` to regenerate golden files

### Phase 3: Serialization Templates

**Goal:** Generate `toMap()` and `UnmarshalJSON()` for entities with private fields.

#### 3.1 New template: `marshal.go.tmpl`

Generates per-entity `<snake>_marshal_gen.go` — only emitted when entity has
private fields.

**`toMap()` — write path:**
```go
func (e *{{$name}}) toMap() map[string]interface{} {
    m := make(map[string]interface{})
    // UID (always exported)
    if e.UID != "" { m["uid"] = e.UID }
    // DType (always exported)
    if len(e.DType) > 0 { m["dgraph.type"] = e.DType }
    // Each mapped field (private or exported, using json tag as key):
    {{range scalarFields .Entity.Fields}}{{if not .IsSkipped}}
    // scalar: check zero value, use json tag as key
    {{end}}{{end}}
    {{range privateSingularEdgeFields .Entity.Fields}}
    // singular edge: call nested toMap() if non-nil
    {{end}}
    {{range privateMultiEdgeFields .Entity.Fields}}
    // multi-edge: iterate and call toMap() on each
    {{end}}
    return m
}
```

**`UnmarshalJSON()` — read path:**
```go
func (e *{{$name}}) UnmarshalJSON(data []byte) error {
    // Define an alias struct with exported fields matching json tags
    type alias struct {
        UID   string   `json:"uid,omitempty"`
        DType []string `json:"dgraph.type,omitempty"`
        // ... all mapped fields as exported, with json tags ...
    }
    var a alias
    if err := json.Unmarshal(data, &a); err != nil {
        return err
    }
    // Assign from alias to private fields
    e.UID = a.UID
    e.DType = a.DType
    e.name = a.Name
    // ... etc ...
    return nil
}
```

- [x] Create `marshal.go.tmpl` — `templates/marshal.go.tmpl`
- [x] Register in `Generate()` — emit `<snake>_marshal_gen.go` only when `hasPrivateFields` — `generator.go`
- [x] Handle all field types in `toMap()`: scalars, singular edges, multi-edges, primitive slices
- [x] Handle `omitempty` logic in `toMap()` (skip zero values)
- [x] Handle nested entity serialization (edge entities that also have private fields)

#### 3.2 Tests for Phase 3

- [x] Unit test `toMap()` output for a struct with mixed field types
- [x] Unit test `UnmarshalJSON()` round-trip: marshal → unmarshal → verify fields
- [x] Verify golden files include marshal/unmarshal code
- [x] Test that entities WITHOUT private fields do NOT get marshal files

### Phase 4: Engine Integration

**Goal:** modusgraph routes private-field entities through the map-based mutation path.

#### 4.1 `DgraphMapper` interface — `client.go` or new file

```go
// DgraphMapper is implemented by entities with private fields that need
// custom serialization for Dgraph mutations.
type DgraphMapper interface {
    toMap() map[string]interface{}
}
```

Note: `toMap()` is unexported (lowercase), so this interface is only satisfiable
within the modusgraph module and generated packages that import it. This is
intentional — it's an internal contract.

Wait — actually, since the generated code is in the user's package (not in the
modusgraph package), the interface needs to use an exported method:

```go
type DgraphMapper interface {
    DgraphMap() map[string]interface{}
}
```

And the generated method becomes `DgraphMap()` instead of `toMap()`.

- [x] Define `DgraphMapper` interface with `DgraphMap()` method — `client.go`
- [x] Update template to generate `DgraphMap()` instead of `toMap()`

#### 4.2 Update mutation path — `client.go`

Modify `Insert`, `Update`, `Upsert` to check for `DgraphMapper`:

```go
func (c client) Insert(ctx context.Context, obj any) error {
    if err := c.validateStruct(ctx, obj); err != nil {
        return err
    }
    return c.process(ctx, obj, "Insert", func(tx *dg.TxnContext, obj any) ([]string, error) {
        if mapper, ok := obj.(DgraphMapper); ok {
            return c.mutateWithMap(tx, obj, mapper)
        }
        return tx.MutateBasic(obj)
    })
}
```

New helper `mutateWithMap`:

```go
func (c client) mutateWithMap(tx *dg.TxnContext, original any, mapper DgraphMapper) ([]string, error) {
    mapped := mapper.DgraphMap()
    uids, err := tx.MutateBasic(mapped)
    if err != nil {
        return nil, err
    }
    // Write UID back to original struct
    if len(uids) > 0 {
        writeUIDBack(original, uids)
    }
    return uids, err
}

func writeUIDBack(obj any, uids map[string]string) {
    v := reflect.ValueOf(obj)
    if v.Kind() == reflect.Ptr {
        v = v.Elem()
    }
    uidField := v.FieldByName("UID")
    if uidField.IsValid() && uidField.CanSet() {
        // Use the first UID from the map (matching dgman convention)
        for _, uid := range uids {
            uidField.SetString(uid)
            break
        }
    }
}
```

Also handle slices of DgraphMapper objects.

- [x] Add `DgraphMapper` interface — `client.go`
- [x] Add `mutateWithMap` helper — `client.go` or `mutate.go`
- [x] Add `writeUIDBack` helper — `mutate.go`
- [x] Update `Insert` to check `DgraphMapper` — `client.go:359`
- [x] Update `InsertRaw` to check `DgraphMapper` — `client.go:376`
- [x] Update `Update` to check `DgraphMapper` — `client.go`
- [x] Update `Upsert` to check `DgraphMapper` — `client.go`
- [x] Handle slice inputs (e.g., `[]*Film` where Film implements DgraphMapper)

#### 4.3 Tests for Phase 4

- [x] Test Insert with a struct implementing `DgraphMapper` — verify it persists
- [x] Test Insert with a regular struct (no `DgraphMapper`) — verify existing path still works
- [x] Test UID writeback after Insert with `DgraphMapper`
- [x] Test Update with `DgraphMapper`
- [x] Test with slice of `DgraphMapper` objects
- [x] Integration test: Insert with private fields → Get → verify all fields round-trip

### Phase 5: Update Existing Templates

**Goal:** Options and CLI templates use setters for private fields.

#### 5.1 Update `options.go.tmpl`

For private fields, use setter instead of direct assignment:

```go
{{range $fields}}
{{- if .IsPrivate}}
func With{{$name}}{{.Name | toCamelCase}}(v {{.GoType}}) {{$name}}Option {
    return func(e *{{$name}}) {
        e.Set{{.Name | toCamelCase}}(v)
    }
}
{{- else}}
func With{{$name}}{{.Name}}(v {{.GoType}}) {{$name}}Option {
    return func(e *{{$name}}) {
        e.{{.Name}} = v
    }
}
{{- end}}
{{end}}
```

Also need to filter out `IsSkipped` fields.

- [x] Update `options.go.tmpl` — conditional setter vs direct assignment
- [x] Filter `IsSkipped` fields from options generation

#### 5.2 Update `cli.go.tmpl`

For private fields, use setter instead of struct literal assignment:

```go
// Current (exported):
v := &{{$.Name}}.{{.Name}}{
    {{.Name}}: c.{{.Name}},
}

// New (private): build then set
v := &{{$.Name}}.{{.Name}}{}
v.Set{{.Name | toCamelCase}}(c.{{.Name}})
```

This requires restructuring the AddCmd to use setters instead of struct literal
syntax for private fields.

- [x] Update `cli.go.tmpl` AddCmd — use setters for private fields
- [x] Filter `IsSkipped` fields from CLI flag generation

#### 5.3 Tests for Phase 5

- [x] Regenerate golden files with `go test -update`
- [x] Verify options use setters for private fields
- [x] Verify CLI uses setters for private fields
- [x] Verify backward compatibility: entities with only exported fields unchanged

### Phase 6: End-to-End Testing

- [x] Create a test entity with mixed public/private fields, singular edges, multi-edges, primitive slices, and opt-out fields
- [x] Run full codegen pipeline: parse → generate → compile generated code
- [x] Integration test: Insert entity with private fields → Get → verify all fields present
- [x] Integration test: Update entity with private fields → Get → verify changes
- [x] Integration test: Verify `dgraph:"-"` fields are NOT persisted
- [x] Integration test: Verify fields without json tags are NOT persisted
- [x] Run existing test suite to confirm no regressions
- [x] Run `go vet` and `staticcheck` on generated code

## Acceptance Criteria

- [x] Private (lowercase) struct fields generate getters and setters
- [x] Singular edge fields (`validate:"max=1"` or `validate:"len=1"`) generate `*Type` accessor pair
- [x] Multi-edge slice fields generate Get/Set/Append/Remove/RemoveFunc helpers
- [x] Primitive slice fields generate Get/Set/Append/Remove/RemoveFunc helpers
- [x] Fields without `json` tag are skipped
- [x] Fields with `dgraph:"-"` are skipped
- [x] UID and DType always remain exported, no accessors generated
- [x] Generated `DgraphMap()` serializes private fields for mutations
- [x] Generated `UnmarshalJSON()` deserializes private fields from queries
- [x] Existing entities with exported fields work unchanged (backward compatible)
- [x] Functional options use setters for private fields
- [x] CLI template uses setters for private fields
- [x] All existing tests pass
- [x] Golden files updated and passing

## Dependencies & Risks

- **dgman compatibility:** We rely on `MutateBasic` accepting `map[string]interface{}`. Confirmed this works from dgman source.
- **UID writeback:** The `writeUIDBack` helper uses reflect on the exported UID field. Low risk since UID is always `string` and exported.
- **UnmarshalJSON recursion:** If entity A has an edge to entity B, and both have `UnmarshalJSON`, the standard library handles nested unmarshaling correctly.
- **~~Schema generation~~** — **RESOLVED, NOT A RISK.** Investigated dgman's
  schema path thoroughly. `CreateSchema` → `TypeSchema.Marshal` uses
  `reflect.Type.Field(i)` which returns **all** fields including unexported ones.
  Struct tags (`json`, `dgraph`) are type metadata and are accessible regardless
  of export status. The `CanInterface()` check that blocks private fields only
  exists in the **mutation data path** (`filterStruct`, `generateSchemaHook`),
  NOT in the schema definition path. Furthermore, `process()` passes the original
  struct to `UpdateSchema` **before** the `txFunc` where we intercept with
  `DgraphMapper`, so schema generation always sees the full struct with all type
  metadata. No schema changes needed.

  Key code paths verified:
  - `schema.go:168` — `TypeSchema.Marshal` uses `reflect.Type.Field(i)` ✅
  - `schema.go:207` — `parseDgraphTag` reads tags from `reflect.StructField` ✅
  - `schema.go:570` — `CreateSchema` iterates fields via `reflect.Type` ✅
  - `mutate.go:67-68` — `process()` calls `UpdateSchema(ctx, schemaObj)` with original struct ✅

## References

- Brainstorm: `docs/brainstorms/2026-03-25-private-fields-accessors-brainstorm.md`
- Parser: `cmd/modusgraph-gen/internal/parser/parser.go`
- Model: `cmd/modusgraph-gen/internal/model/model.go`
- Generator: `cmd/modusgraph-gen/internal/generator/generator.go`
- Engine mutations: `client.go:357-411`, `mutate.go`
- dgman filterStruct: `dolan-in/dgman/v2@v2.2.0-preview2/mutate.go:231-282`
- dgman MutateBasic: `dolan-in/dgman/v2@v2.2.0-preview2/txn.go:83`
- Golden test files: `cmd/modusgraph-gen/internal/generator/testdata/golden/`
- Test data: `cmd/modusgraph-gen/internal/parser/testdata/movies/`
