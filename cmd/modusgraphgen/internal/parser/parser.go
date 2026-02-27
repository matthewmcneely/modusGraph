// Package parser extracts entity and field metadata from Go source files by
// inspecting struct declarations and their struct tags. It uses go/ast and
// go/parser to walk the AST, then builds a model.Package for the generator.
package parser

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"reflect"
	"strings"

	"github.com/matthewmcneely/modusgraph/cmd/modusgraphgen/internal/model"
)

// Parse loads all Go source files in the directory at pkgDir, extracts exported
// structs, and returns a model.Package with fully resolved entities and fields.
func Parse(pkgDir string) (*model.Package, error) {
	fset := token.NewFileSet()
	pkgs, err := parser.ParseDir(fset, pkgDir, nil, parser.ParseComments)
	if err != nil {
		return nil, fmt.Errorf("parsing package at %s: %w", pkgDir, err)
	}

	if len(pkgs) == 0 {
		return nil, fmt.Errorf("no Go packages found in %s", pkgDir)
	}

	// Take the first (and typically only) non-test package.
	var pkgName string
	var pkgAST *ast.Package
	for name, pkg := range pkgs {
		if strings.HasSuffix(name, "_test") {
			continue
		}
		pkgName = name
		pkgAST = pkg
		break
	}
	if pkgAST == nil {
		return nil, fmt.Errorf("no non-test package found in %s", pkgDir)
	}

	// First pass: collect all struct names so we can identify edges.
	structNames := collectStructNames(pkgAST)

	// Second pass: parse each struct into an Entity.
	var entities []model.Entity
	for _, file := range pkgAST.Files {
		for _, decl := range file.Decls {
			genDecl, ok := decl.(*ast.GenDecl)
			if !ok || genDecl.Tok != token.TYPE {
				continue
			}
			for _, spec := range genDecl.Specs {
				typeSpec, ok := spec.(*ast.TypeSpec)
				if !ok {
					continue
				}
				structType, ok := typeSpec.Type.(*ast.StructType)
				if !ok {
					continue
				}
				if !typeSpec.Name.IsExported() {
					continue
				}

				entity, isEntity := parseStruct(typeSpec.Name.Name, structType, structNames)
				if isEntity {
					entities = append(entities, entity)
				}
			}
		}
	}

	// Collect import mappings: package alias → full import path.
	imports := collectImports(pkgAST)

	// Read the module path from go.mod.
	modulePath := readModulePath(pkgDir)

	return &model.Package{
		Name:       pkgName,
		ModulePath: modulePath,
		Imports:    imports,
		Entities:   entities,
	}, nil
}

// collectStructNames returns a set of all exported struct type names in the package.
func collectStructNames(pkg *ast.Package) map[string]bool {
	names := make(map[string]bool)
	for _, file := range pkg.Files {
		for _, decl := range file.Decls {
			genDecl, ok := decl.(*ast.GenDecl)
			if !ok || genDecl.Tok != token.TYPE {
				continue
			}
			for _, spec := range genDecl.Specs {
				typeSpec, ok := spec.(*ast.TypeSpec)
				if !ok {
					continue
				}
				if _, ok := typeSpec.Type.(*ast.StructType); ok {
					if typeSpec.Name.IsExported() {
						names[typeSpec.Name.Name] = true
					}
				}
			}
		}
	}
	return names
}

// parseStruct parses a single struct into a model.Entity. Returns the entity and
// true if the struct qualifies as an entity (has both UID and DType fields),
// or a zero Entity and false otherwise.
func parseStruct(name string, st *ast.StructType, structNames map[string]bool) (model.Entity, bool) {
	var fields []model.Field
	hasUID := false
	hasDType := false

	for _, f := range st.Fields.List {
		if len(f.Names) == 0 {
			continue // embedded field, skip
		}
		fieldName := f.Names[0].Name
		if !ast.IsExported(fieldName) {
			continue
		}

		goType := typeString(f.Type)
		field := model.Field{
			Name:   fieldName,
			GoType: goType,
		}

		// Parse struct tags.
		if f.Tag != nil {
			tagValue := strings.Trim(f.Tag.Value, "`")
			tag := reflect.StructTag(tagValue)

			// Parse json tag.
			jsonTag := tag.Get("json")
			if jsonTag != "" {
				parts := strings.SplitN(jsonTag, ",", 2)
				field.JSONTag = parts[0]
				if len(parts) > 1 && strings.Contains(parts[1], "omitempty") {
					field.OmitEmpty = true
				}
			}

			// Parse dgraph tag.
			dgraphTag := tag.Get("dgraph")
			if dgraphTag != "" {
				parseDgraphTag(dgraphTag, &field)
			}
		}

		// Detect UID and DType fields.
		if fieldName == "UID" && goType == "string" {
			field.IsUID = true
			hasUID = true
		}
		if fieldName == "DType" && goType == "[]string" {
			field.IsDType = true
			hasDType = true
		}

		// Resolve predicate: use explicit predicate if set, else fall back to json tag.
		if field.Predicate == "" {
			field.Predicate = field.JSONTag
		}

		// Detect edges: field type is []SomeEntity where SomeEntity is a known struct.
		if strings.HasPrefix(goType, "[]") {
			elemType := goType[2:]
			if structNames[elemType] {
				field.IsEdge = true
				field.EdgeEntity = elemType
			}
		}

		// Detect reverse edges from predicate.
		if strings.HasPrefix(field.Predicate, "~") {
			field.IsReverse = true
		}

		fields = append(fields, field)
	}

	if !hasUID || !hasDType {
		return model.Entity{}, false
	}

	entity := model.Entity{
		Name:   name,
		Fields: fields,
	}

	// Apply inference rules.
	applyInference(&entity)

	return entity, true
}

// typeString converts an ast.Expr representing a type into a human-readable Go
// type string, e.g. "string", "time.Time", "[]Genre", "[]float64".
func typeString(expr ast.Expr) string {
	switch t := expr.(type) {
	case *ast.Ident:
		return t.Name
	case *ast.SelectorExpr:
		// e.g., time.Time
		if x, ok := t.X.(*ast.Ident); ok {
			return x.Name + "." + t.Sel.Name
		}
		return t.Sel.Name
	case *ast.ArrayType:
		if t.Len == nil {
			// slice type
			return "[]" + typeString(t.Elt)
		}
		// array type (unlikely in our structs but handle it)
		return "[...]" + typeString(t.Elt)
	case *ast.StarExpr:
		return "*" + typeString(t.X)
	case *ast.MapType:
		return "map[" + typeString(t.Key) + "]" + typeString(t.Value)
	default:
		return fmt.Sprintf("%T", expr)
	}
}

// collectImports scans all files in the package and builds a map from package
// alias (the local name used in qualified types like enums.ResourceType) to the
// full import path (e.g., "github.com/Istari-digital/.../enums").
func collectImports(pkg *ast.Package) map[string]string {
	imports := make(map[string]string)
	for _, file := range pkg.Files {
		for _, imp := range file.Imports {
			path := strings.Trim(imp.Path.Value, `"`)
			var alias string
			if imp.Name != nil {
				alias = imp.Name.Name
			} else {
				// Default alias is the last path segment.
				parts := strings.Split(path, "/")
				alias = parts[len(parts)-1]
			}
			imports[alias] = path
		}
	}
	return imports
}

// readModulePath reads the go.mod file in or above pkgDir and extracts the
// module path. It walks up from pkgDir looking for go.mod. Returns empty
// string if no go.mod is found.
func readModulePath(pkgDir string) string {
	dir := pkgDir
	for {
		goModPath := filepath.Join(dir, "go.mod")
		data, err := os.ReadFile(goModPath)
		if err == nil {
			for _, line := range strings.Split(string(data), "\n") {
				line = strings.TrimSpace(line)
				if strings.HasPrefix(line, "module ") {
					return strings.TrimSpace(strings.TrimPrefix(line, "module "))
				}
			}
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return ""
}

// parseDgraphTag parses a dgraph struct tag value into its component parts and
// populates the corresponding fields on the model.Field.
//
// The dgraph tag uses a mixed format where space separates independent
// directives and commas separate values within a directive:
//
//	dgraph:"predicate=initial_release_date index=year"
//	dgraph:"predicate=genre,reverse,count"
//	dgraph:"index=hash,term,trigram,fulltext"
//	dgraph:"index=geo,type=geo"
//	dgraph:"index=exact,upsert"
//	dgraph:"count"
//
// Parsing rules:
//  1. Split on spaces first to get independent directives.
//  2. For each directive, split on commas to get tokens.
//  3. Each token is either "key=value" or a bare flag.
//  4. Special handling: "predicate=" sets the predicate, "index=" starts an index
//     list, "type=" sets the type hint, "reverse"/"count"/"upsert" are boolean flags.
//  5. Bare tokens after "index=" that don't contain "=" are additional index values.
func parseDgraphTag(tag string, field *model.Field) {
	// Split on spaces for independent directives.
	directives := strings.Fields(tag)

	for _, directive := range directives {
		tokens := strings.Split(directive, ",")
		inIndex := false

		for _, tok := range tokens {
			tok = strings.TrimSpace(tok)
			if tok == "" {
				continue
			}

			if strings.HasPrefix(tok, "predicate=") {
				field.Predicate = tok[len("predicate="):]
				inIndex = false
				continue
			}
			if strings.HasPrefix(tok, "index=") {
				indexVal := tok[len("index="):]
				field.Indexes = append(field.Indexes, indexVal)
				inIndex = true
				continue
			}
			if strings.HasPrefix(tok, "type=") {
				field.TypeHint = tok[len("type="):]
				inIndex = false
				continue
			}

			switch tok {
			case "reverse":
				field.IsReverse = true
				inIndex = false
			case "count":
				field.HasCount = true
				inIndex = false
			case "upsert":
				field.Upsert = true
				inIndex = false
			default:
				// Bare token: if we were in an index= list, treat as additional index value.
				if inIndex {
					field.Indexes = append(field.Indexes, tok)
				}
			}
		}
	}
}
