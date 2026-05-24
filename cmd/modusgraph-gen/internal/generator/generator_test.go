package generator

import (
	"flag"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/matthewmcneely/modusgraph/cmd/modusgraph-gen/internal/model"
	"github.com/matthewmcneely/modusgraph/cmd/modusgraph-gen/internal/parser"
)

var update = flag.Bool("update", false, "update golden files")

func moviesDir(t *testing.T) string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	// thisFile = .../generator/generator_test.go
	// testdata is at .../parser/testdata/movies/schema/
	genDir := filepath.Dir(thisFile)
	return filepath.Join(filepath.Dir(genDir), "parser", "testdata", "movies", "schema")
}

// goldenDir returns the path to the legacy flat golden test data directory.
// As of Task 18, this directory contains only a .gitkeep — the old flat goldens
// were deleted and replaced by the split layout under parser/testdata/movies/.
// The directory is kept for the -update flag workflow; Task 20 will remove the
// TestGenerate golden-comparison test entirely.
func goldenDir(t *testing.T) string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	return filepath.Join(filepath.Dir(thisFile), "testdata", "golden")
}

// flatConfig returns a Config that routes all emits into a single dir.
// Used by legacy tests that don't care about dir separation.
// CLIDir is set to dir/cmd/<pkg.Name> so the CLI stub ends up in its
// expected location.
func flatConfig(pkg *model.Package, dir string) Config {
	return Config{
		SchemaDir:               dir,
		SchemaClientDir:         dir,
		EntityDir:               dir,
		EntityClientDir:         dir,
		CLIDir:                  filepath.Join(dir, "cmd", pkg.Name),
		EntityPackageName:       "entity",
		EntityClientPackageName: "entity",
		SchemaClientPackageName: pkg.Name,
		SchemaAlias:             pkg.Name,
		SchemaImportPath:        pkg.SchemaImportPath,
		CLIName:                 pkg.CLIName,
		WithValidator:           pkg.WithValidator,
	}
}

func TestGenerate(t *testing.T) {
	dir := moviesDir(t)
	pkg, err := parser.Parse(dir)
	if err != nil {
		t.Fatalf("Parse(%s) failed: %v", dir, err)
	}

	// Generate to a temp directory.
	tmpDir := t.TempDir()
	if err := Generate(pkg, flatConfig(pkg, tmpDir)); err != nil {
		t.Fatalf("Generate failed: %v", err)
	}

	golden := goldenDir(t)

	if *update {
		// Copy all generated files to golden directory.
		t.Log("Updating golden files...")
		entries, err := os.ReadDir(tmpDir)
		if err != nil {
			t.Fatal(err)
		}
		// Clean golden dir first.
		_ = os.RemoveAll(golden)
		if err := os.MkdirAll(golden, 0o755); err != nil {
			t.Fatal(err)
		}
		for _, entry := range entries {
			if entry.IsDir() {
				continue // skip cmd/ directory for golden tests
			}
			src := filepath.Join(tmpDir, entry.Name())
			dst := filepath.Join(golden, entry.Name())
			data, err := os.ReadFile(src)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(dst, data, 0o644); err != nil {
				t.Fatal(err)
			}
		}
		t.Log("Golden files updated.")
		return
	}

	// Compare generated files against golden files.
	// As of Task 18, the flat golden/ dir contains only a .gitkeep (the old
	// flat goldens were replaced by the split layout under
	// parser/testdata/movies/). When no .go golden files are present, skip
	// gracefully — Task 20 will remove this comparison block entirely.
	goldenEntries, err := os.ReadDir(golden)
	if err != nil {
		t.Fatalf("Reading golden dir %s: %v\nRun with -update to create golden files.", golden, err)
	}

	// Count .go files only; ignore .gitkeep and other non-Go files.
	var goldenGoFiles []os.DirEntry
	for _, entry := range goldenEntries {
		if !entry.IsDir() && filepath.Ext(entry.Name()) == ".go" {
			goldenGoFiles = append(goldenGoFiles, entry)
		}
	}
	if len(goldenGoFiles) == 0 {
		t.Skip("No .go golden files found in testdata/golden/ (replaced by split layout in parser/testdata/movies/); Task 20 will remove this comparison.")
	}

	for _, entry := range goldenGoFiles {
		name := entry.Name()
		t.Run(name, func(t *testing.T) {
			goldenPath := filepath.Join(golden, name)
			generatedPath := filepath.Join(tmpDir, name)

			goldenData, err := os.ReadFile(goldenPath)
			if err != nil {
				t.Fatalf("reading golden file: %v", err)
			}

			generatedData, err := os.ReadFile(generatedPath)
			if err != nil {
				t.Fatalf("reading generated file: %v", err)
			}

			if string(goldenData) != string(generatedData) {
				t.Errorf("generated output differs from golden file %s", name)
				// Show a diff summary.
				goldenLines := strings.Split(string(goldenData), "\n")
				generatedLines := strings.Split(string(generatedData), "\n")
				maxLines := max(len(goldenLines), len(generatedLines))
				diffCount := 0
				for i := range maxLines {
					var gl, genl string
					if i < len(goldenLines) {
						gl = goldenLines[i]
					}
					if i < len(generatedLines) {
						genl = generatedLines[i]
					}
					if gl != genl {
						if diffCount < 10 {
							t.Errorf("  line %d:\n    golden:    %q\n    generated: %q", i+1, gl, genl)
						}
						diffCount++
					}
				}
				if diffCount > 10 {
					t.Errorf("  ... and %d more differences", diffCount-10)
				}
			}
		})
	}
}

func TestGenerateHeader(t *testing.T) {
	dir := moviesDir(t)
	pkg, err := parser.Parse(dir)
	if err != nil {
		t.Fatalf("Parse(%s) failed: %v", dir, err)
	}

	tmpDir := t.TempDir()
	if err := Generate(pkg, flatConfig(pkg, tmpDir)); err != nil {
		t.Fatalf("Generate failed: %v", err)
	}

	// Check that all generated files start with the expected header.
	entries, err := os.ReadDir(tmpDir)
	if err != nil {
		t.Fatal(err)
	}

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		t.Run(entry.Name(), func(t *testing.T) {
			data, err := os.ReadFile(filepath.Join(tmpDir, entry.Name()))
			if err != nil {
				t.Fatal(err)
			}
			if !strings.HasPrefix(string(data), "// Code generated by modusGraphGen. DO NOT EDIT.") {
				t.Errorf("file %s does not start with expected header", entry.Name())
			}
		})
	}
}

func TestExternalImports(t *testing.T) {
	t.Run("NoExternalTypes", func(t *testing.T) {
		fields := []model.Field{
			{Name: "Name", GoType: "string"},
			{Name: "Created", GoType: "time.Time"},
			{Name: "Size", GoType: "int64"},
		}
		imports := map[string]string{"time": "time"}
		got := externalImports(fields, imports)
		if len(got) != 0 {
			t.Errorf("expected no external imports, got %v", got)
		}
	})

	t.Run("WithExternalPackage", func(t *testing.T) {
		fields := []model.Field{
			{Name: "Name", GoType: "string"},
			{Name: "TypeName", GoType: "enums.ResourceType"},
			{Name: "Status", GoType: "enums.ArchiveStatus"},
			{Name: "Created", GoType: "time.Time"},
		}
		imports := map[string]string{
			"time":  "time",
			"enums": "github.com/example/project/enums",
		}
		got := externalImports(fields, imports)
		if len(got) != 1 {
			t.Fatalf("expected 1 external import, got %v", got)
		}
		if got[0].Path != "github.com/example/project/enums" {
			t.Errorf("got path %q, want %q", got[0].Path, "github.com/example/project/enums")
		}
		if got[0].Alias != "" {
			t.Errorf("got alias %q, want empty (package name matches last path segment)", got[0].Alias)
		}
	})

	t.Run("MultipleExternalPackages", func(t *testing.T) {
		fields := []model.Field{
			{Name: "TypeName", GoType: "enums.ResourceType"},
			{Name: "PageInfo", GoType: "pagination.PageInfo"},
		}
		imports := map[string]string{
			"enums":      "github.com/example/project/enums",
			"pagination": "github.com/example/project/pagination",
		}
		got := externalImports(fields, imports)
		if len(got) != 2 {
			t.Fatalf("expected 2 external imports, got %v", got)
		}
		// Should be sorted by path.
		if got[0].Path != "github.com/example/project/enums" {
			t.Errorf("got[0].Path = %q, want enums path", got[0].Path)
		}
		if got[1].Path != "github.com/example/project/pagination" {
			t.Errorf("got[1].Path = %q, want pagination path", got[1].Path)
		}
	})

	t.Run("AliasedImport", func(t *testing.T) {
		fields := []model.Field{
			{Name: "Embedding", GoType: "*dg.VectorFloat32"},
		}
		imports := map[string]string{
			"dg": "github.com/dolan-in/dgman/v2",
		}
		got := externalImports(fields, imports)
		if len(got) != 1 {
			t.Fatalf("expected 1 external import, got %v", got)
		}
		if got[0].Path != "github.com/dolan-in/dgman/v2" {
			t.Errorf("got path %q, want %q", got[0].Path, "github.com/dolan-in/dgman/v2")
		}
		if got[0].Alias != "dg" {
			t.Errorf("got alias %q, want %q", got[0].Alias, "dg")
		}
	})

	t.Run("PointerPrefixStripped", func(t *testing.T) {
		fields := []model.Field{
			{Name: "ID", GoType: "scalars.UUID"},
			{Name: "Ptr", GoType: "*scalars.UUID"},
			{Name: "Slice", GoType: "[]scalars.UUID"},
		}
		imports := map[string]string{
			"scalars": "github.com/example/project/scalars",
		}
		got := externalImports(fields, imports)
		if len(got) != 1 {
			t.Fatalf("expected 1 external import (deduplicated), got %v", got)
		}
		if got[0].Path != "github.com/example/project/scalars" {
			t.Errorf("got path %q, want scalars path", got[0].Path)
		}
	})

	t.Run("UnknownPackageSkipped", func(t *testing.T) {
		fields := []model.Field{
			{Name: "TypeName", GoType: "unknown.SomeType"},
		}
		imports := map[string]string{}
		got := externalImports(fields, imports)
		if len(got) != 0 {
			t.Errorf("expected no imports for unknown package, got %v", got)
		}
	})
}

func TestToSnakeCase(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"Film", "film"},
		{"ContentRating", "content_rating"},
		{"UID", "uid"},
		{"HTTPServer", "http_server"},
		{"Actor", "actor"},
		{"Performance", "performance"},
		{"Location", "location"},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := toSnakeCase(tt.input)
			if got != tt.want {
				t.Errorf("toSnakeCase(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestToCamelCaseInitialisms(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		// Standard initialisms
		{"id", "ID"},
		{"url", "URL"},
		{"http", "HTTP"},
		{"api", "API"},
		{"json", "JSON"},
		{"xml", "XML"},
		{"sql", "SQL"},
		{"ssh", "SSH"},
		{"uid", "UID"},
		{"uuid", "UUID"},
		{"uri", "URI"},
		{"html", "HTML"},
		{"css", "CSS"},
		{"ip", "IP"},
		{"tcp", "TCP"},
		{"tls", "TLS"},
		{"ttl", "TTL"},
		{"cpu", "CPU"},
		{"ram", "RAM"},
		{"ui", "UI"},

		// Compound names with initialisms
		{"http_endpoint", "HTTPEndpoint"},
		{"api_key", "APIKey"},
		{"user_id", "UserID"},
		{"json_data", "JSONData"},
		{"sql_query", "SQLQuery"},
		{"tcp_port", "TCPPort"},

		// Non-initialisms unchanged
		{"name", "Name"},
		{"yearFounded", "YearFounded"},
		{"active", "Active"},
		{"createdAt", "CreatedAt"},
		{"revenue", "Revenue"},

		// Edge cases
		{"", ""},
		{"a", "A"},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := toCamelCase(tt.input)
			if got != tt.want {
				t.Errorf("toCamelCase(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestAccessorName(t *testing.T) {
	// accessorName should use AccessorName override when set.
	t.Run("WithOverride", func(t *testing.T) {
		f := model.Field{Name: "id", AccessorName: "ID"}
		got := accessorName(f)
		if got != "ID" {
			t.Errorf("accessorName(id with override ID) = %q, want %q", got, "ID")
		}
	})

	t.Run("OverrideTakesPrecedence", func(t *testing.T) {
		f := model.Field{Name: "yearFounded", AccessorName: "Founded"}
		got := accessorName(f)
		if got != "Founded" {
			t.Errorf("accessorName(yearFounded with override Founded) = %q, want %q", got, "Founded")
		}
	})

	// accessorName should fall back to toCamelCase when no override is set.
	t.Run("FallbackInitialism", func(t *testing.T) {
		f := model.Field{Name: "id"}
		got := accessorName(f)
		if got != "ID" {
			t.Errorf("accessorName(id no override) = %q, want %q", got, "ID")
		}
	})

	t.Run("FallbackRegular", func(t *testing.T) {
		f := model.Field{Name: "name"}
		got := accessorName(f)
		if got != "Name" {
			t.Errorf("accessorName(name no override) = %q, want %q", got, "Name")
		}
	})
}

func TestSearchPredicate(t *testing.T) {
	dir := moviesDir(t)
	pkg, err := parser.Parse(dir)
	if err != nil {
		t.Fatalf("Parse(%s) failed: %v", dir, err)
	}

	for _, entity := range pkg.Entities {
		if entity.Searchable {
			pred := searchPredicate(entity)
			if pred == "" {
				t.Errorf("entity %s is searchable but searchPredicate returned empty", entity.Name)
			}
			t.Logf("%s: search predicate = %q", entity.Name, pred)
		}
	}
}

func TestWithCLIDir(t *testing.T) {
	dir := moviesDir(t)
	pkg, err := parser.Parse(dir)
	if err != nil {
		t.Fatalf("Parse(%s) failed: %v", dir, err)
	}

	tmpDir := t.TempDir()
	customCLIDir := filepath.Join(tmpDir, "custom", "cli")

	cfg := flatConfig(pkg, tmpDir)
	cfg.CLIDir = customCLIDir
	if err := Generate(pkg, cfg); err != nil {
		t.Fatalf("Generate failed: %v", err)
	}

	// CLI should be at the custom path.
	customCLIPath := filepath.Join(customCLIDir, "main.go")
	if _, err := os.Stat(customCLIPath); err != nil {
		t.Fatalf("CLI not found at custom path %s: %v", customCLIPath, err)
	}

	// CLI should NOT be at the default path.
	defaultCLIPath := filepath.Join(tmpDir, "cmd", "movies", "main.go")
	if _, err := os.Stat(defaultCLIPath); !os.IsNotExist(err) {
		t.Errorf("CLI should not exist at default path %s when custom dir is set", defaultCLIPath)
	}
}

func TestGenerate_EmitsSchemaMarkerFile(t *testing.T) {
	// Lay out a temp schema package and run the generator against it.
	srcDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(srcDir, "go.mod"), []byte("module example.com/test\n\ngo 1.25\n"), 0o644); err != nil {
		t.Fatalf("writing go.mod: %v", err)
	}
	studio := `package schema

type Studio struct {
	UID   string   ` + "`json:\"uid,omitempty\"`" + `
	DType []string ` + "`json:\"dgraph.type,omitempty\"`" + `
	Name  string   ` + "`json:\"name\" dgraph:\"index=fulltext\"`" + `
}
`
	if err := os.WriteFile(filepath.Join(srcDir, "studio.go"), []byte(studio), 0o644); err != nil {
		t.Fatalf("writing studio.go: %v", err)
	}

	pkg, err := parser.Parse(srcDir)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	outDir := t.TempDir()
	cfg := Config{
		SchemaDir:               outDir,
		SchemaClientDir:         outDir,
		EntityDir:               outDir,
		EntityClientDir:         outDir,
		SchemaClientPackageName: pkg.Name,
		SchemaAlias:             pkg.Name,
		SchemaImportPath:        pkg.SchemaImportPath,
		NoCLI:                   true,
	}
	if err := Generate(pkg, cfg); err != nil {
		t.Fatalf("generate: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(outDir, "marker_gen.go"))
	if err != nil {
		t.Fatalf("reading marker_gen.go: %v", err)
	}
	out := string(data)

	for _, want := range []string{
		`package schema`,
		`func (*Studio) SchemaTypeName() string { return "Studio" }`,
		`func (*Studio) SchemaPredicates() []string`,
		`"name"`,
		`func (*Studio) SchemaSearchPredicate() string { return "name" }`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("marker_gen.go missing expected content: %q\n---file---\n%s", want, out)
		}
	}

	// Negative: uid and dgraph.type must NOT appear inside the predicates list.
	for _, notWant := range []string{`"uid"`, `"dgraph.type"`} {
		if strings.Contains(out, notWant) {
			t.Errorf("marker_gen.go must not include bookkeeping predicate %q in SchemaPredicates", notWant)
		}
	}
}

func TestGenerate_SchemaMarkerNoSearchPredicate(t *testing.T) {
	// An entity with no fulltext-indexed field should emit `return ""`.
	srcDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(srcDir, "go.mod"), []byte("module example.com/test\n\ngo 1.25\n"), 0o644); err != nil {
		t.Fatalf("writing go.mod: %v", err)
	}
	src := `package schema

type Plain struct {
	UID   string   ` + "`json:\"uid,omitempty\"`" + `
	DType []string ` + "`json:\"dgraph.type,omitempty\"`" + `
	Label string   ` + "`json:\"label\"`" + `
}
`
	if err := os.WriteFile(filepath.Join(srcDir, "plain.go"), []byte(src), 0o644); err != nil {
		t.Fatalf("writing plain.go: %v", err)
	}

	pkg, err := parser.Parse(srcDir)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	outDir := t.TempDir()
	cfg := Config{
		SchemaDir:               outDir,
		SchemaClientDir:         outDir,
		EntityDir:               outDir,
		EntityClientDir:         outDir,
		SchemaClientPackageName: pkg.Name,
		SchemaAlias:             pkg.Name,
		SchemaImportPath:        pkg.SchemaImportPath,
		NoCLI:                   true,
	}
	if err := Generate(pkg, cfg); err != nil {
		t.Fatalf("generate: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(outDir, "marker_gen.go"))
	if err != nil {
		t.Fatalf("reading marker_gen.go: %v", err)
	}
	if !strings.Contains(string(data), `func (*Plain) SchemaSearchPredicate() string { return "" }`) {
		t.Errorf("expected SchemaSearchPredicate to return \"\" for entity with no search field; got:\n%s", string(data))
	}
}

func TestGenerate_EmitsSchemaClientFactory(t *testing.T) {
	srcDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(srcDir, "go.mod"), []byte("module example.com/test\n\ngo 1.25\n"), 0o644); err != nil {
		t.Fatalf("writing go.mod: %v", err)
	}
	src := `package schema

type Studio struct {
	UID   string   ` + "`json:\"uid,omitempty\"`" + `
	DType []string ` + "`json:\"dgraph.type,omitempty\"`" + `
	Name  string   ` + "`json:\"name\"`" + `
}

type Film struct {
	UID   string   ` + "`json:\"uid,omitempty\"`" + `
	DType []string ` + "`json:\"dgraph.type,omitempty\"`" + `
	Title string   ` + "`json:\"title\"`" + `
}
`
	if err := os.WriteFile(filepath.Join(srcDir, "studio.go"), []byte(src), 0o644); err != nil {
		t.Fatalf("writing studio.go: %v", err)
	}
	pkg, err := parser.Parse(srcDir)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	schemaClientDir := t.TempDir()
	entityDir := t.TempDir()
	cfg := Config{
		SchemaDir:               srcDir,
		SchemaClientDir:         schemaClientDir,
		EntityDir:               entityDir,
		EntityClientDir:         entityDir,
		SchemaClientPackageName: pkg.Name,
		SchemaAlias:             pkg.Name,
		SchemaImportPath:        pkg.SchemaImportPath,
		NoCLI:                   true,
	}
	if err := Generate(pkg, cfg); err != nil {
		t.Fatalf("generate: %v", err)
	}

	// schema-side client factory lands in SchemaClientDir as client_gen.go.
	data := mustReadGen(t, schemaClientDir, "client_gen.go")
	for _, want := range []string{
		`package schema`,
		`"github.com/matthewmcneely/modusgraph"`,
		`"github.com/matthewmcneely/modusgraph/typed"`,
		`type Client struct {`,
		`GraphClient modusgraph.Client`,
		`*typed.Client[Studio]`,
		`*typed.Client[Film]`,
		`func NewClient(conn modusgraph.Client) *Client {`,
		`GraphClient: conn,`,
		`typed.NewClient[Studio](conn)`,
		`typed.NewClient[Film](conn)`,
	} {
		if !strings.Contains(data, want) {
			t.Errorf("client_gen.go missing: %q\n---file---\n%s", want, data)
		}
	}
}

// generateFromMinimalSchema creates a temp schema with a single Studio entity
// and runs Generate against it, returning the temp source dir, schemaDir, and entityDir.
// Used by multiple per-entity emit tests.
func generateFromMinimalSchema(t *testing.T) (srcDir, schemaDir, entityDir string) {
	t.Helper()
	srcDir = t.TempDir()
	if err := os.WriteFile(filepath.Join(srcDir, "go.mod"), []byte("module example.com/test\n\ngo 1.25\n"), 0o644); err != nil {
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
	schemaDir = srcDir // schema files live in srcDir; emit schema-side artifacts here too
	entityDir = filepath.Join(t.TempDir(), "entity")
	if err := os.MkdirAll(entityDir, 0o755); err != nil {
		t.Fatalf("mkdir entityDir: %v", err)
	}
	cfg := Config{
		SchemaDir:               schemaDir,
		SchemaClientDir:         schemaDir,
		EntityDir:               entityDir,
		EntityClientDir:         entityDir,
		EntityPackageName:       "entity",
		EntityClientPackageName: "entity",
		SchemaClientPackageName: "schema",
		SchemaAlias:             "schema",
		SchemaImportPath:        "example.com/test",
		CLIName:                 "test",
	}
	if err := Generate(pkg, cfg); err != nil {
		t.Fatalf("generate: %v", err)
	}
	return srcDir, schemaDir, entityDir
}

// mustReadGen reads a generated file from outDir, failing the test on error.
func mustReadGen(t *testing.T, outDir, name string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(outDir, name))
	if err != nil {
		t.Fatalf("reading %s: %v", name, err)
	}
	return string(data)
}

func TestGenerate_AccessorsAllShapes(t *testing.T) {
	// Build a synthetic schema exercising the six shapes.
	srcDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(srcDir, "go.mod"), []byte("module example.com/test\n\ngo 1.25\n"), 0o644); err != nil {
		t.Fatalf("writing go.mod: %v", err)
	}
	src := `package schema

type Director struct {
	UID   string   ` + "`json:\"uid,omitempty\"`" + `
	DType []string ` + "`json:\"dgraph.type,omitempty\"`" + `
	Name  string   ` + "`json:\"name\"`" + `
}

type Country struct {
	UID   string   ` + "`json:\"uid,omitempty\"`" + `
	DType []string ` + "`json:\"dgraph.type,omitempty\"`" + `
	Code  string   ` + "`json:\"code\"`" + `
}

type Film struct {
	UID   string   ` + "`json:\"uid,omitempty\"`" + `
	DType []string ` + "`json:\"dgraph.type,omitempty\"`" + `
	Title string   ` + "`json:\"title\"`" + `
}

type Studio struct {
	UID          string      ` + "`json:\"uid,omitempty\"`" + `
	DType        []string    ` + "`json:\"dgraph.type,omitempty\"`" + `
	Name         string      ` + "`json:\"name\"`" + `
	Founder      *Director   ` + "`json:\"founder,omitempty\"`" + `
	Headquarters Country     ` + "`json:\"headquarters,omitempty\"`" + `
	CurrentHead  []*Director ` + "`json:\"current_head,omitempty\" validate:\"max=1\"`" + `
	Films        []*Film     ` + "`json:\"films,omitempty\"`" + `
	Tags         []string    ` + "`json:\"tags,omitempty\"`" + `
}
`
	if err := os.WriteFile(filepath.Join(srcDir, "studio.go"), []byte(src), 0o644); err != nil {
		t.Fatalf("writing studio.go: %v", err)
	}
	pkg, err := parser.Parse(srcDir)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	outDir := t.TempDir()
	cfg := Config{
		SchemaDir:               outDir,
		SchemaClientDir:         outDir,
		EntityDir:               outDir,
		EntityClientDir:         outDir,
		EntityPackageName:       "entity",
		EntityClientPackageName: "entity",
		SchemaClientPackageName: pkg.Name,
		SchemaAlias:             pkg.Name,
		SchemaImportPath:        pkg.SchemaImportPath,
		NoCLI:                   true,
	}
	if err := Generate(pkg, cfg); err != nil {
		t.Fatalf("generate: %v", err)
	}
	data := mustReadGen(t, outDir, "studio_gen.go")

	// Scalar
	for _, want := range []string{
		`func (e *Studio) Name() string { return e.Unwrap().Name }`,
		`func (e *Studio) SetName(v string)`,
	} {
		if !strings.Contains(data, want) {
			t.Errorf("scalar accessor missing: %q", want)
		}
	}
	// Pointer-singular edge
	for _, want := range []string{
		`func (e *Studio) Founder() *Director {`,
		`if e.Unwrap().Founder == nil {`,
		`return &Director{Wrapper: typed.WrapValue(e.Unwrap().Founder)}`,
		`func (e *Studio) SetFounder(v *Director)`,
	} {
		if !strings.Contains(data, want) {
			t.Errorf("pointer-singular accessor missing: %q", want)
		}
	}
	// Value-singular edge
	for _, want := range []string{
		`func (e *Studio) Headquarters() *Country {`,
		`return &Country{Wrapper: typed.WrapValue(&e.Unwrap().Headquarters)}`,
		`func (e *Studio) SetHeadquarters(v *Country)`,
		`e.Unwrap().Headquarters = *v.Unwrap()`,
	} {
		if !strings.Contains(data, want) {
			t.Errorf("value-singular accessor missing: %q", want)
		}
	}
	// Singular-via-list
	for _, want := range []string{
		`func (e *Studio) CurrentHead() *Director {`,
		`if len(e.Unwrap().CurrentHead) == 0 || e.Unwrap().CurrentHead[0] == nil {`,
		`func (e *Studio) SetCurrentHead(v *Director)`,
		`e.Unwrap().CurrentHead = []*schema.Director{v.Unwrap()}`,
	} {
		if !strings.Contains(data, want) {
			t.Errorf("singular-via-list accessor missing: %q", want)
		}
	}
	// Multi-edge
	for _, want := range []string{
		`func (e *Studio) Films() []*Film {`,
		`func (e *Studio) FilmsSeq() iter.Seq[*Film]`,
		`func (e *Studio) SetFilms(items ...*Film)`,
		`func (e *Studio) AppendFilms(items ...*Film)`,
		`func (e *Studio) RemoveFilms(uids ...string)`,
	} {
		if !strings.Contains(data, want) {
			t.Errorf("multi-edge accessor missing: %q", want)
		}
	}
	// Scalar slice
	for _, want := range []string{
		`func (e *Studio) Tags() []string`,
		`func (e *Studio) SetTags(v []string)`,
		`func (e *Studio) AppendTags(v ...string)`,
		`func (e *Studio) RemoveTagsFunc(fn func(string) bool)`,
	} {
		if !strings.Contains(data, want) {
			t.Errorf("scalar-slice accessor missing: %q", want)
		}
	}

	// Scalar slice must not also appear in the scalar block — that would
	// emit duplicate Get/Set declarations and break the build.
	if n := strings.Count(data, "func (e *Studio) Tags() []string"); n != 1 {
		t.Errorf("Tags() must appear exactly once; got %d duplicates", n)
	}
	if n := strings.Count(data, "func (e *Studio) SetTags(v []string)"); n != 1 {
		t.Errorf("SetTags() must appear exactly once; got %d duplicates", n)
	}

	// UID/DType getters/setters are provided by the entity fragment of the
	// merged file, so they SHOULD appear exactly once. (Earlier layouts kept
	// accessors in a separate file and forbade those methods there.)
	for _, want := range []string{
		`func (e *Studio) UID() string`,
		`func (e *Studio) DType() []string`,
	} {
		if !strings.Contains(data, want) {
			t.Errorf("merged studio_gen.go should include UID/DType getter %q", want)
		}
	}
}

func TestGenerate_EntityWrapperStruct(t *testing.T) {
	_, _, entityDir := generateFromMinimalSchema(t)
	data := mustReadGen(t, entityDir, "studio_gen.go")
	for _, want := range []string{
		`package entity`,
		`"example.com/test"`,
		`"github.com/matthewmcneely/modusgraph/typed"`,
		`type Studio struct {`,
		`typed.Wrapper[schema.Studio]`,
		`func NewStudio(opts ...typed.Option[Studio]) *Studio {`,
		`func WrapStudio(s *schema.Studio, opts ...typed.Option[Studio]) *Studio {`,
		`typed.WrapValue(&schema.Studio{})`,
		`typed.WrapValue(s)`,
		`typed.Apply(e, opts...)`,
		`func (e *Studio) UID() string { return e.Unwrap().UID }`,
		`func (e *Studio) SetUID(v string)`,
		`func (e *Studio) DType() []string { return e.Unwrap().DType }`,
		`func (e *Studio) SetDType(v []string)`,
	} {
		if !strings.Contains(data, want) {
			t.Errorf("studio_gen.go missing expected content: %q\n---file---\n%s", want, data)
		}
	}

	// Negative: Unwrap/Marshal/Unmarshal/Validate are inherited from
	// typed.Wrapper and MUST NOT be re-emitted by any fragment. (The client
	// type IS legitimately present in the merged file via the client fragment;
	// it's covered by TestGenerate_WrapperEntityClient.)
	for _, notWant := range []string{
		`func (e *Studio) Unwrap()`,
		`func (e *Studio) MarshalJSON(`,
		`func (e *Studio) UnmarshalJSON(`,
		`func (e *Studio) Validate(`,
	} {
		if strings.Contains(data, notWant) {
			t.Errorf("studio_gen.go must NOT include %q (provided by typed.Wrapper)", notWant)
		}
	}
}

func TestGenerate_OptionsScalarOnly(t *testing.T) {
	_, _, entityDir := generateFromMinimalSchema(t)
	data := mustReadGen(t, entityDir, "studio_gen.go")

	for _, want := range []string{
		`package entity`,
		`"github.com/matthewmcneely/modusgraph/typed"`,
		`func WithStudioName(v string) typed.Option[Studio] {`,
		`return func(e *Studio) { e.SetName(v) }`,
	} {
		if !strings.Contains(data, want) {
			t.Errorf("studio_gen.go (options content) missing: %q\n---file---\n%s", want, data)
		}
	}

	// Negative: the per-entity option type and apply loop are replaced by the
	// generic typed.Option / typed.Apply; UID/DType get no With option.
	for _, notWant := range []string{
		`type StudioOption func(*Studio)`,
		`func ApplyStudioOptions(`,
		`func WithStudioUID(`,
		`func WithStudioDType(`,
	} {
		if strings.Contains(data, notWant) {
			t.Errorf("studio_gen.go (options content) must NOT emit %q", notWant)
		}
	}
}

func TestGenerate_NoMarshalFileEmitted(t *testing.T) {
	_, _, entityDir := generateFromMinimalSchema(t)
	// The marshal template was deleted; no <snake>_marshal_gen.go should exist
	// in the output regardless of which entities are present.
	if _, err := os.Stat(filepath.Join(entityDir, "studio_marshal_gen.go")); !os.IsNotExist(err) {
		t.Errorf("studio_marshal_gen.go must NOT be emitted; stat err = %v", err)
	}
}

func TestGenerate_WrapperClientFactory(t *testing.T) {
	srcDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(srcDir, "go.mod"), []byte("module example.com/test\n\ngo 1.25\n"), 0o644); err != nil {
		t.Fatalf("writing go.mod: %v", err)
	}
	src := `package schema

type Studio struct {
	UID   string   ` + "`json:\"uid,omitempty\"`" + `
	DType []string ` + "`json:\"dgraph.type,omitempty\"`" + `
	Name  string   ` + "`json:\"name\"`" + `
}

type Film struct {
	UID   string   ` + "`json:\"uid,omitempty\"`" + `
	DType []string ` + "`json:\"dgraph.type,omitempty\"`" + `
	Title string   ` + "`json:\"title\"`" + `
}
`
	if err := os.WriteFile(filepath.Join(srcDir, "studio.go"), []byte(src), 0o644); err != nil {
		t.Fatalf("writing studio.go: %v", err)
	}
	pkg, err := parser.Parse(srcDir)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	outDir := t.TempDir()
	cfg := Config{
		SchemaDir:               outDir,
		SchemaClientDir:         outDir,
		EntityDir:               outDir,
		EntityClientDir:         outDir,
		EntityPackageName:       "entity",
		EntityClientPackageName: "entity",
		SchemaClientPackageName: pkg.Name,
		SchemaAlias:             pkg.Name,
		SchemaImportPath:        "example.com/test",
		NoCLI:                   true,
	}
	if err := Generate(pkg, cfg); err != nil {
		t.Fatalf("generate: %v", err)
	}

	data := mustReadGen(t, outDir, "client_gen.go")
	// The wrapper-side client_gen.go and schema-side client_gen.go go to the
	// SAME dir here (flat config). The last write wins — which is the entity
	// client (wrapper_client.go.tmpl). Check for wrapper-specific content.
	for _, want := range []string{
		`package entity`,
		`type Client struct {`,
		`conn modusgraph.Client`,
		`func NewClient(conn modusgraph.Client) *Client`,
		`c.Studio = NewStudioClient(conn)`,
		`c.Film = NewFilmClient(conn)`,
		`func (c *Client) GraphClient() modusgraph.Client`,
		`return c.conn`,
	} {
		if !strings.Contains(data, want) {
			t.Errorf("client_gen.go (wrapper side) missing: %q\n---file---\n%s", want, data)
		}
	}
	for _, notWant := range []string{
		`schemaClient *schema.Client`,
		`sc := schema.NewClient(conn)`,
	} {
		if strings.Contains(data, notWant) {
			t.Errorf("wrapper client_gen.go must NOT depend on the schema aggregate: %q", notWant)
		}
	}
}

func TestGenerate_WrapperEntityClient(t *testing.T) {
	_, _, entityDir := generateFromMinimalSchema(t)
	data := mustReadGen(t, entityDir, "studio_gen.go")
	for _, want := range []string{
		`package entity`,
		`"github.com/matthewmcneely/modusgraph/typed"`,
		`type StudioClient struct {`,
		`typed *typed.Client[schema.Studio]`,
		`func NewStudioClient(conn modusgraph.Client) *StudioClient`,
		`typed.NewClient[schema.Studio](conn)`,
		`func (c *StudioClient) Get(ctx context.Context, uid string) (*Studio, error)`,
		`return WrapStudio(s), nil`,
		`func (c *StudioClient) Add(ctx context.Context, w *Studio) error`,
		`w.Unwrap()`,
		`func (c *StudioClient) Update(ctx context.Context, w *Studio) error`,
		`func (c *StudioClient) Upsert(ctx context.Context, w *Studio, predicates ...string) error`,
		`func (c *StudioClient) Delete(ctx context.Context, uid string) error`,
		`func (c *StudioClient) Query(ctx context.Context) *StudioQuery`,
	} {
		if !strings.Contains(data, want) {
			t.Errorf("studio_gen.go (client content) missing: %q", want)
		}
	}
	for _, notWant := range []string{
		`schema.NewStudioClient(`,
		`*schema.StudioClient`,
	} {
		if strings.Contains(data, notWant) {
			t.Errorf("studio_gen.go (client content) must NOT reference the deleted schema client: %q", notWant)
		}
	}
}

func TestGenerate_WrapperQuery(t *testing.T) {
	_, _, entityDir := generateFromMinimalSchema(t)
	data := mustReadGen(t, entityDir, "studio_gen.go")
	for _, want := range []string{
		`package entity`,
		`"github.com/matthewmcneely/modusgraph/typed"`,
		`"iter"`,
		`type StudioQuery struct {`,
		`typed *typed.Query[schema.Studio]`,
		`func (q *StudioQuery) Filter(filter string, params ...any) *StudioQuery`,
		`func (q *StudioQuery) OrderAsc(clause string) *StudioQuery`,
		`func (q *StudioQuery) OrderDesc(clause string) *StudioQuery`,
		`func (q *StudioQuery) Limit(n int) *StudioQuery`,
		`func (q *StudioQuery) Offset(n int) *StudioQuery`,
		`func (q *StudioQuery) After(uid string) *StudioQuery`,
		`func (q *StudioQuery) Cascade(predicates ...string) *StudioQuery`,
		`func (q *StudioQuery) Nodes() ([]*Studio, error)`,
		`func (q *StudioQuery) First() (*Studio, error)`,
		`func (q *StudioQuery) IterNodes() iter.Seq2[*Studio, error]`,
		`return WrapStudio(s), nil`,
	} {
		if !strings.Contains(data, want) {
			t.Errorf("studio_gen.go (query content) missing: %q", want)
		}
	}
	if strings.Contains(data, `*schema.StudioQuery`) {
		t.Errorf("studio_gen.go (query content) must NOT reference the deleted schema query type")
	}
}

// TestGenerate_WrapperQueryEdgeFilter checks that <Entity>Query gains a
// Where<Edge> method for each edge field — delegating to typed.WhereEdge — and
// that an entity with no edges gains none.
func TestGenerate_WrapperQueryEdgeFilter(t *testing.T) {
	srcDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(srcDir, "go.mod"),
		[]byte("module example.com/test\n\ngo 1.25\n"), 0o644); err != nil {
		t.Fatalf("writing go.mod: %v", err)
	}
	src := "package schema\n\n" +
		"type Owner struct {\n" +
		"\tUID   string   `json:\"uid,omitempty\"`\n" +
		"\tDType []string `json:\"dgraph.type,omitempty\"`\n" +
		"\tName  string   `json:\"name\"`\n" +
		"\tPets  []*Pet   `json:\"pets,omitempty\"`\n" +
		"}\n\n" +
		"type Pet struct {\n" +
		"\tUID   string   `json:\"uid,omitempty\"`\n" +
		"\tDType []string `json:\"dgraph.type,omitempty\"`\n" +
		"\tName  string   `json:\"name\"`\n" +
		"}\n"
	if err := os.WriteFile(filepath.Join(srcDir, "schema.go"), []byte(src), 0o644); err != nil {
		t.Fatalf("writing schema.go: %v", err)
	}
	pkg, err := parser.Parse(srcDir)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	entityDir := filepath.Join(t.TempDir(), "entity")
	if err := os.MkdirAll(entityDir, 0o755); err != nil {
		t.Fatalf("mkdir entityDir: %v", err)
	}
	cfg := Config{
		SchemaDir:               srcDir,
		SchemaClientDir:         srcDir,
		EntityDir:               entityDir,
		EntityClientDir:         entityDir,
		EntityPackageName:       "entity",
		EntityClientPackageName: "entity",
		SchemaClientPackageName: "schema",
		SchemaAlias:             "schema",
		SchemaImportPath:        "example.com/test",
		CLIName:                 "test",
	}
	if err := Generate(pkg, cfg); err != nil {
		t.Fatalf("generate: %v", err)
	}

	// Owner has a Pets edge → OwnerQuery gains WherePets, delegating to the
	// typed substrate with the resolved predicate.
	ownerQuery := mustReadGen(t, entityDir, "owner_gen.go")
	for _, want := range []string{
		`func (q *OwnerQuery) WherePets(filter string, params ...any) *OwnerQuery`,
		`q.typed.WhereEdge("pets", filter, params...)`,
	} {
		if !strings.Contains(ownerQuery, want) {
			t.Errorf("owner_gen.go missing %q; got:\n%s", want, ownerQuery)
		}
	}

	// Pet has no edges → PetQuery must carry no Where* method.
	petQuery := mustReadGen(t, entityDir, "pet_gen.go")
	if strings.Contains(petQuery, "func (q *PetQuery) Where") {
		t.Errorf("pet_gen.go has a Where* method but Pet has no edges:\n%s", petQuery)
	}
}

func TestGenerate_NoIterFileEmitted(t *testing.T) {
	_, _, entityDir := generateFromMinimalSchema(t)
	if _, err := os.Stat(filepath.Join(entityDir, "iter_gen.go")); !os.IsNotExist(err) {
		t.Errorf("iter_gen.go must NOT be emitted (replaced by typed.Client.Iter); stat err = %v", err)
	}
}

func TestGenerate_NoPageOptionsFileEmitted(t *testing.T) {
	_, _, entityDir := generateFromMinimalSchema(t)
	if _, err := os.Stat(filepath.Join(entityDir, "page_options_gen.go")); !os.IsNotExist(err) {
		t.Errorf("page_options_gen.go must NOT be emitted (dead code); stat err = %v", err)
	}
}

func TestGenerate_CLIImportsSchemaByFullPath(t *testing.T) {
	// A schema package physically nested below the module root must be
	// imported by its FULL path, not module/<pkgname>.
	srcDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(srcDir, "go.mod"), []byte("module example.com/proj\n\ngo 1.25\n"), 0o644); err != nil {
		t.Fatalf("go.mod: %v", err)
	}
	// schema dir nested two levels below module root
	schemaDir := filepath.Join(srcDir, "movies", "schema")
	if err := os.MkdirAll(schemaDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	src := `package schema

type Studio struct {
	UID   string   ` + "`json:\"uid,omitempty\"`" + `
	DType []string ` + "`json:\"dgraph.type,omitempty\"`" + `
	Name  string   ` + "`json:\"name\"`" + `
}
`
	if err := os.WriteFile(filepath.Join(schemaDir, "studio.go"), []byte(src), 0o644); err != nil {
		t.Fatalf("studio.go: %v", err)
	}
	pkg, err := parser.Parse(schemaDir)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	// Confirm the parser resolved the full nested import path.
	const wantImportPath = "example.com/proj/movies/schema"
	if pkg.SchemaImportPath != wantImportPath {
		t.Fatalf("parser resolved SchemaImportPath = %q, want %q", pkg.SchemaImportPath, wantImportPath)
	}

	outDir := t.TempDir()
	cliDir := filepath.Join(outDir, "cmd", "movies")
	if err := os.MkdirAll(cliDir, 0o755); err != nil {
		t.Fatalf("mkdir cliDir: %v", err)
	}
	cfg := Config{
		SchemaDir:               schemaDir,
		SchemaClientDir:         schemaDir,
		EntityDir:               outDir,
		EntityClientDir:         outDir,
		CLIDir:                  cliDir,
		EntityPackageName:       "movies",
		EntityClientPackageName: "movies",
		SchemaClientPackageName: "schema",
		SchemaAlias:             "schema",
		SchemaImportPath:        pkg.SchemaImportPath,
		CLIName:                 "movies",
	}
	if err := Generate(pkg, cfg); err != nil {
		t.Fatalf("generate: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(cliDir, "main.go"))
	if err != nil {
		t.Fatalf("reading generated CLI: %v", err)
	}
	out := string(data)
	if !strings.Contains(out, `"example.com/proj/movies/schema"`) {
		t.Errorf("CLI must import the schema package by its full path; got:\n%s", out)
	}
	if strings.Contains(out, `"example.com/proj/schema"`) {
		t.Errorf("CLI imports the truncated (wrong) schema path example.com/proj/schema")
	}
}

// TestGenerate_WrapperQueryByField checks that <Entity>Query gains a
// By<Field> method for each indexed scalar field whose Go type maps to a
// known filter type (string, scalars.UUID), uses the matching filter import,
// and skips unindexed scalars + unsupported types.
func TestGenerate_WrapperQueryByField(t *testing.T) {
	srcDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(srcDir, "go.mod"),
		[]byte("module example.com/test\n\ngo 1.25\n"), 0o644); err != nil {
		t.Fatalf("writing go.mod: %v", err)
	}
	// Widget has:
	//   - Title: string with index=hash → expect ByTitle(filter.String)
	//   - Slug: string with NO index → expect no ByMethod
	//   - Count: int64 with index=int → expect no method (v1 skips non-string types)
	src := "package schema\n\n" +
		"type Widget struct {\n" +
		"\tUID   string   `json:\"uid,omitempty\"`\n" +
		"\tDType []string `json:\"dgraph.type,omitempty\"`\n" +
		"\tTitle string   `json:\"title,omitempty\" dgraph:\"index=hash\"`\n" +
		"\tSlug  string   `json:\"slug,omitempty\"`\n" +
		"\tCount int64    `json:\"count,omitempty\" dgraph:\"index=int\"`\n" +
		"}\n"
	if err := os.WriteFile(filepath.Join(srcDir, "schema.go"), []byte(src), 0o644); err != nil {
		t.Fatalf("writing schema.go: %v", err)
	}
	pkg, err := parser.Parse(srcDir)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	entityDir := filepath.Join(t.TempDir(), "entity")
	if err := os.MkdirAll(entityDir, 0o755); err != nil {
		t.Fatalf("mkdir entityDir: %v", err)
	}
	cfg := Config{
		SchemaDir:               srcDir,
		SchemaClientDir:         srcDir,
		EntityDir:               entityDir,
		EntityClientDir:         entityDir,
		EntityPackageName:       "entity",
		EntityClientPackageName: "entity",
		SchemaClientPackageName: "schema",
		SchemaAlias:             "schema",
		SchemaImportPath:        "example.com/test",
		CLIName:                 "test",
	}
	if err := Generate(pkg, cfg); err != nil {
		t.Fatalf("generate: %v", err)
	}

	widget := mustReadGen(t, entityDir, "widget_gen.go")

	// Title is indexed and string-typed → ByTitle is emitted, takes filter.String.
	for _, want := range []string{
		`func (q *WidgetQuery) ByTitle(filters ...filter.String) *WidgetQuery`,
		`b.EqGroupString("title", filters)`,
		`"github.com/matthewmcneely/modusgraph/typed/filter"`,
	} {
		if !strings.Contains(widget, want) {
			t.Errorf("widget_gen.go missing %q; got:\n%s", want, widget)
		}
	}

	// Slug is string but unindexed → no BySlug.
	if strings.Contains(widget, "BySlug") {
		t.Errorf("widget_gen.go should NOT have a BySlug method (Slug is unindexed); got:\n%s", widget)
	}

	// Count is int64 → unsupported in v1, no ByCount even though it's indexed.
	if strings.Contains(widget, "ByCount") {
		t.Errorf("widget_gen.go should NOT have a ByCount method (int64 unsupported in v1); got:\n%s", widget)
	}
}

// TestGenerate_WrapperQueryByField_UUIDType checks that a scalars.UUID-typed
// indexed field gets a By<Field> method using filter.UUID.
func TestGenerate_WrapperQueryByField_UUIDType(t *testing.T) {
	srcDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(srcDir, "go.mod"),
		[]byte("module example.com/test\n\ngo 1.25\n"), 0o644); err != nil {
		t.Fatalf("writing go.mod: %v", err)
	}
	// A minimal scalars package providing the UUID alias the field references.
	scalarsDir := filepath.Join(srcDir, "scalars")
	if err := os.MkdirAll(scalarsDir, 0o755); err != nil {
		t.Fatalf("mkdir scalars: %v", err)
	}
	if err := os.WriteFile(filepath.Join(scalarsDir, "scalars.go"),
		[]byte("package scalars\n\ntype UUID = string\n"), 0o644); err != nil {
		t.Fatalf("writing scalars.go: %v", err)
	}
	src := "package schema\n\n" +
		"import \"example.com/test/scalars\"\n\n" +
		"type Widget struct {\n" +
		"\tUID   string       `json:\"uid,omitempty\"`\n" +
		"\tDType []string     `json:\"dgraph.type,omitempty\"`\n" +
		"\tID    scalars.UUID `json:\"id,omitempty\" dgraph:\"index=hash upsert\"`\n" +
		"}\n"
	if err := os.WriteFile(filepath.Join(srcDir, "schema.go"), []byte(src), 0o644); err != nil {
		t.Fatalf("writing schema.go: %v", err)
	}
	pkg, err := parser.Parse(srcDir)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	entityDir := filepath.Join(t.TempDir(), "entity")
	if err := os.MkdirAll(entityDir, 0o755); err != nil {
		t.Fatalf("mkdir entityDir: %v", err)
	}
	cfg := Config{
		SchemaDir:               srcDir,
		SchemaClientDir:         srcDir,
		EntityDir:               entityDir,
		EntityClientDir:         entityDir,
		EntityPackageName:       "entity",
		EntityClientPackageName: "entity",
		SchemaClientPackageName: "schema",
		SchemaAlias:             "schema",
		SchemaImportPath:        "example.com/test",
		CLIName:                 "test",
	}
	if err := Generate(pkg, cfg); err != nil {
		t.Fatalf("generate: %v", err)
	}

	widget := mustReadGen(t, entityDir, "widget_gen.go")

	// Note: the UID field is recognised as the entity UID and skipped; the
	// scalars.UUID-typed ID field is what the test exercises.
	for _, want := range []string{
		`func (q *WidgetQuery) ByID(filters ...filter.UUID) *WidgetQuery`,
		`b.EqGroupUUID("id", filters)`,
	} {
		if !strings.Contains(widget, want) {
			t.Errorf("widget_gen.go missing %q; got:\n%s", want, widget)
		}
	}
}
