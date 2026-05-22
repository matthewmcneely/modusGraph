package generator

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/matthewmcneely/modusgraph/cmd/modusgraph-gen/internal/model"
	"github.com/matthewmcneely/modusgraph/cmd/modusgraph-gen/internal/parser"
)

// scalarField builds a plain scalar model.Field.
func scalarField(name, goType string) model.Field {
	return model.Field{Name: name, GoType: goType}
}

func TestGenImports_EntitySide(t *testing.T) {
	const schemaPath = "example.com/proj/schema"

	t.Run("scalar only", func(t *testing.T) {
		g := newGenImports()
		g.addEntitySideImports(model.Entity{Fields: []model.Field{scalarField("Name", "string")}}, nil, schemaPath)
		block := g.block()
		for _, want := range []string{
			`"github.com/matthewmcneely/modusgraph/typed"`,
			`"example.com/proj/schema"`,
		} {
			if !strings.Contains(block, want) {
				t.Errorf("block missing %q\n%s", want, block)
			}
		}
		for _, absent := range []string{`"iter"`, `"slices"`, `"time"`, `"context"`} {
			if strings.Contains(block, absent) {
				t.Errorf("block must not contain %q\n%s", absent, block)
			}
		}
	})

	t.Run("multi-edge pulls iter and slices", func(t *testing.T) {
		g := newGenImports()
		fields := []model.Field{{Name: "Films", GoType: "[]*schema.Film", IsEdge: true, IsSingularEdge: false}}
		g.addEntitySideImports(model.Entity{Fields: fields}, nil, schemaPath)
		block := g.block()
		for _, want := range []string{`"iter"`, `"slices"`} {
			if !strings.Contains(block, want) {
				t.Errorf("block missing %q\n%s", want, block)
			}
		}
	})

	t.Run("scalar slice pulls slices", func(t *testing.T) {
		g := newGenImports()
		g.addEntitySideImports(model.Entity{Fields: []model.Field{scalarField("Tags", "[]string")}}, nil, schemaPath)
		if !strings.Contains(g.block(), `"slices"`) {
			t.Errorf("scalar slice should pull slices\n%s", g.block())
		}
	})

	t.Run("time field pulls time", func(t *testing.T) {
		g := newGenImports()
		g.addEntitySideImports(model.Entity{Fields: []model.Field{scalarField("Created", "time.Time")}}, nil, schemaPath)
		if !strings.Contains(g.block(), `"time"`) {
			t.Errorf("time.Time field should pull time\n%s", g.block())
		}
	})

	t.Run("external type pulls aliased import", func(t *testing.T) {
		g := newGenImports()
		fields := []model.Field{scalarField("Status", "enums.Status")}
		imports := map[string]string{"enums": "example.com/proj/enums"}
		g.addEntitySideImports(model.Entity{Fields: fields}, imports, schemaPath)
		if !strings.Contains(g.block(), `"example.com/proj/enums"`) {
			t.Errorf("external type should pull its import\n%s", g.block())
		}
	})
}

func TestGenImports_ClientSide(t *testing.T) {
	g := newGenImports()
	g.addClientSideImports("example.com/proj/schema")
	block := g.block()
	for _, want := range []string{
		`"context"`,
		`"iter"`,
		`"github.com/matthewmcneely/modusgraph"`,
		`"github.com/matthewmcneely/modusgraph/typed"`,
		`"example.com/proj/schema"`,
	} {
		if !strings.Contains(block, want) {
			t.Errorf("client-side block missing %q\n%s", want, block)
		}
	}
}

func TestGenImports_EmptyBlock(t *testing.T) {
	if got := newGenImports().block(); got != "" {
		t.Errorf("empty genImports should render empty block, got %q", got)
	}
}

func TestAssembleGenFile(t *testing.T) {
	out := assembleGenFile("entity", "import (\n\t\"context\"\n)", "type A struct{}", "func F() {}")
	if !strings.HasPrefix(out, "package entity\n") {
		t.Errorf("missing package decl:\n%s", out)
	}
	for _, want := range []string{"import (", "type A struct{}", "func F() {}"} {
		if !strings.Contains(out, want) {
			t.Errorf("assembled file missing %q:\n%s", want, out)
		}
	}
	if strings.Count(out, "package entity") != 1 {
		t.Errorf("expected exactly one package decl:\n%s", out)
	}
}

// TestGenerate_MergedSingleFilePerEntity verifies the default layout emits
// exactly one <entity>_gen.go per entity and no split per-entity files.
func TestGenerate_MergedSingleFilePerEntity(t *testing.T) {
	_, _, entityDir := generateFromMinimalSchema(t)
	if _, err := os.Stat(filepath.Join(entityDir, "studio_gen.go")); err != nil {
		t.Fatalf("studio_gen.go must exist: %v", err)
	}
	for _, stale := range []string{
		"studio_accessors_gen.go",
		"studio_options_gen.go",
		"studio_client_gen.go",
		"studio_query_gen.go",
	} {
		if _, err := os.Stat(filepath.Join(entityDir, stale)); !os.IsNotExist(err) {
			t.Errorf("%s must NOT be emitted in the merged layout; stat err = %v", stale, err)
		}
	}
}

// TestGenerate_SplitLayout verifies that when EntityDir != EntityClientDir the
// entity-side fragments and client-side fragments land in separate
// <entity>_gen.go files, each with a correctly partitioned import block.
func TestGenerate_SplitLayout(t *testing.T) {
	srcDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(srcDir, "go.mod"),
		[]byte("module example.com/test\n\ngo 1.25\n"), 0o644); err != nil {
		t.Fatalf("writing go.mod: %v", err)
	}
	src := `package schema

type Studio struct {
	UID   string   ` + "`json:\"uid,omitempty\"`" + `
	DType []string ` + "`json:\"dgraph.type,omitempty\"`" + `
	Name  string   ` + "`json:\"name\"`" + `
}
`
	if err := os.WriteFile(filepath.Join(srcDir, "studio.go"), []byte(src), 0o644); err != nil {
		t.Fatalf("writing studio.go: %v", err)
	}
	pkg, err := parser.Parse(srcDir)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	entityDir := filepath.Join(t.TempDir(), "entity")
	clientDir := filepath.Join(t.TempDir(), "client")
	for _, d := range []string{entityDir, clientDir} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", d, err)
		}
	}
	cfg := Config{
		SchemaDir:               srcDir,
		SchemaClientDir:         srcDir,
		EntityDir:               entityDir,
		EntityClientDir:         clientDir,
		EntityPackageName:       "entity",
		EntityClientPackageName: "client",
		SchemaClientPackageName: "schema",
		SchemaAlias:             "schema",
		SchemaImportPath:        "example.com/test",
		NoCLI:                   true,
	}
	if err := Generate(pkg, cfg); err != nil {
		t.Fatalf("generate: %v", err)
	}

	entitySide := mustReadGen(t, entityDir, "studio_gen.go")
	for _, want := range []string{`package entity`, `type Studio struct {`, `func WithStudioName(`} {
		if !strings.Contains(entitySide, want) {
			t.Errorf("entity-side studio_gen.go missing %q:\n%s", want, entitySide)
		}
	}
	if strings.Contains(entitySide, `type StudioClient struct {`) {
		t.Errorf("entity-side file must not contain the client type")
	}

	clientSide := mustReadGen(t, clientDir, "studio_gen.go")
	for _, want := range []string{`package client`, `type StudioClient struct {`, `type StudioQuery struct {`} {
		if !strings.Contains(clientSide, want) {
			t.Errorf("client-side studio_gen.go missing %q:\n%s", want, clientSide)
		}
	}
}
