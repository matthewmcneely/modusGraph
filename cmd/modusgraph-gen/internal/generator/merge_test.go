package generator

import (
	"strings"
	"testing"

	"github.com/matthewmcneely/modusgraph/cmd/modusgraph-gen/internal/model"
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
