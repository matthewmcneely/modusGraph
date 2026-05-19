// Package movies_test provides end-to-end tests that verify the generated
// wrapper types (movies.*) integrate correctly with modusgraph.UnwrapSchema.
// These tests live here rather than in the root record_test.go because the
// generated wrapper packages import modusgraph, which would cause an import
// cycle from the root test package. Placing the tests inside the testdata
// tree satisfies Go's internal-package visibility rule.
package movies_test

import (
	"testing"

	modusgraph "github.com/matthewmcneely/modusgraph"
	movies "github.com/matthewmcneely/modusgraph/cmd/modusgraph-gen/internal/parser/testdata/movies"
	moviesSchema "github.com/matthewmcneely/modusgraph/cmd/modusgraph-gen/internal/parser/testdata/movies/schema"
)

// TestUnwrapSchema_RealWrapperRoutesToSchema verifies the UnwrapSchema
// reflection probe correctly substitutes a generated wrapper (movies.Studio)
// with its inner schema struct (*moviesSchema.Studio).
func TestUnwrapSchema_RealWrapperRoutesToSchema(t *testing.T) {
	s := &moviesSchema.Studio{Name: "Pixar"}
	w := movies.WrapStudio(s)

	out := modusgraph.UnwrapSchema(w)
	got, ok := out.(*moviesSchema.Studio)
	if !ok {
		t.Fatalf("expected *moviesSchema.Studio after unwrap, got %T", out)
	}
	if got != s {
		t.Fatalf("expected unwrap to return the SAME inner pointer; got a different *moviesSchema.Studio")
	}
}

// TestUnwrapSchema_RealSchemaPassthrough verifies that a plain
// *moviesSchema.Studio (already implementing the Schema interface via its
// generated SchemaTypeName method) passes through UnwrapSchema unchanged.
func TestUnwrapSchema_RealSchemaPassthrough(t *testing.T) {
	s := &moviesSchema.Studio{Name: "Pixar"}
	out := modusgraph.UnwrapSchema(s)
	if out != any(s) {
		t.Fatalf("expected schema struct to pass through unchanged; got %T", out)
	}
}

// TestSchemaInterface_RealSchemaSatisfies verifies that the generated
// schema struct satisfies the modusgraph.Schema interface via its
// generated SchemaTypeName method.
func TestSchemaInterface_RealSchemaSatisfies(t *testing.T) {
	var _ modusgraph.Schema = (*moviesSchema.Studio)(nil)
}

// TestSchemaTypeName_RealSchemaReturnsCanonical verifies the generated
// SchemaTypeName returns the canonical entity name.
func TestSchemaTypeName_RealSchemaReturnsCanonical(t *testing.T) {
	s := &moviesSchema.Studio{}
	if got := s.SchemaTypeName(); got != "Studio" {
		t.Fatalf("expected SchemaTypeName() == %q, got %q", "Studio", got)
	}
}
