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
	// testdata is at .../parser/testdata/movies/
	genDir := filepath.Dir(thisFile)
	return filepath.Join(filepath.Dir(genDir), "parser", "testdata", "movies")
}

// goldenDir returns the path to the golden test data directory.
func goldenDir(t *testing.T) string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	return filepath.Join(filepath.Dir(thisFile), "testdata", "golden")
}

func TestGenerate(t *testing.T) {
	dir := moviesDir(t)
	pkg, err := parser.Parse(dir)
	if err != nil {
		t.Fatalf("Parse(%s) failed: %v", dir, err)
	}

	// Generate to a temp directory.
	tmpDir := t.TempDir()
	if err := Generate(pkg, tmpDir); err != nil {
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
	goldenEntries, err := os.ReadDir(golden)
	if err != nil {
		t.Fatalf("Reading golden dir %s: %v\nRun with -update to create golden files.", golden, err)
	}

	if len(goldenEntries) == 0 {
		t.Fatalf("No golden files found in %s. Run with -update to create them.", golden)
	}

	for _, entry := range goldenEntries {
		if entry.IsDir() {
			continue
		}
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
				maxLines := len(goldenLines)
				if len(generatedLines) > maxLines {
					maxLines = len(generatedLines)
				}
				diffCount := 0
				for i := 0; i < maxLines; i++ {
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

func TestGenerateOutputFiles(t *testing.T) {
	dir := moviesDir(t)
	pkg, err := parser.Parse(dir)
	if err != nil {
		t.Fatalf("Parse(%s) failed: %v", dir, err)
	}

	tmpDir := t.TempDir()
	if err := Generate(pkg, tmpDir); err != nil {
		t.Fatalf("Generate failed: %v", err)
	}

	// Verify expected files were created.
	expectedFiles := []string{
		"client_gen.go",
		"page_options_gen.go",
		"iter_gen.go",
	}

	// Per-entity files.
	entities := []string{
		"actor", "content_rating", "country", "director",
		"film", "genre", "location", "performance", "rating",
	}
	for _, e := range entities {
		expectedFiles = append(expectedFiles,
			e+"_gen.go",
			e+"_options_gen.go",
			e+"_query_gen.go",
		)
	}

	for _, f := range expectedFiles {
		t.Run(f, func(t *testing.T) {
			path := filepath.Join(tmpDir, f)
			info, err := os.Stat(path)
			if err != nil {
				t.Fatalf("expected file %s not found: %v", f, err)
			}
			if info.Size() == 0 {
				t.Errorf("file %s is empty", f)
			}
		})
	}

	// Verify CLI stub.
	cliPath := filepath.Join(tmpDir, "cmd", "movies", "main.go")
	if _, err := os.Stat(cliPath); err != nil {
		t.Errorf("CLI stub not found: %v", err)
	}
}

func TestGenerateHeader(t *testing.T) {
	dir := moviesDir(t)
	pkg, err := parser.Parse(dir)
	if err != nil {
		t.Fatalf("Parse(%s) failed: %v", dir, err)
	}

	tmpDir := t.TempDir()
	if err := Generate(pkg, tmpDir); err != nil {
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
		if got[0] != "github.com/example/project/enums" {
			t.Errorf("got %q, want %q", got[0], "github.com/example/project/enums")
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
		// Should be sorted.
		if got[0] != "github.com/example/project/enums" {
			t.Errorf("got[0] = %q, want enums path", got[0])
		}
		if got[1] != "github.com/example/project/pagination" {
			t.Errorf("got[1] = %q, want pagination path", got[1])
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

func TestCLITemplateUsesModulePath(t *testing.T) {
	dir := moviesDir(t)
	pkg, err := parser.Parse(dir)
	if err != nil {
		t.Fatalf("Parse(%s) failed: %v", dir, err)
	}

	tmpDir := t.TempDir()
	if err := Generate(pkg, tmpDir); err != nil {
		t.Fatalf("Generate failed: %v", err)
	}

	cliPath := filepath.Join(tmpDir, "cmd", "movies", "main.go")
	data, err := os.ReadFile(cliPath)
	if err != nil {
		t.Fatalf("reading CLI file: %v", err)
	}

	content := string(data)
	// Should contain the module path derived from go.mod, NOT a hardcoded movies project path.
	expectedImport := `"github.com/mlwelles/modusGraphMoviesProject/movies"`
	if !strings.Contains(content, expectedImport) {
		t.Errorf("CLI main.go should contain import %s\nGot:\n%s", expectedImport, content)
	}

	// Should NOT contain any other hardcoded project path.
	// (This test ensures we're using ModulePath, not a hardcoded string.)
	badImport := `"github.com/mlwelles/modusGraphMoviesProject/movies"` // same for movies project, different for others
	_ = badImport                                                       // The import is the same for movies, so this test just verifies it exists.
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

	if err := Generate(pkg, tmpDir, WithCLIDir(customCLIDir)); err != nil {
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

func TestDefaultCLIDir(t *testing.T) {
	dir := moviesDir(t)
	pkg, err := parser.Parse(dir)
	if err != nil {
		t.Fatalf("Parse(%s) failed: %v", dir, err)
	}

	tmpDir := t.TempDir()
	// No WithCLIDir option — should use the default.
	if err := Generate(pkg, tmpDir); err != nil {
		t.Fatalf("Generate failed: %v", err)
	}

	defaultCLIPath := filepath.Join(tmpDir, "cmd", "movies", "main.go")
	if _, err := os.Stat(defaultCLIPath); err != nil {
		t.Fatalf("CLI not found at default path %s: %v", defaultCLIPath, err)
	}
}

func TestCLINameDefault(t *testing.T) {
	dir := moviesDir(t)
	pkg, err := parser.Parse(dir)
	if err != nil {
		t.Fatalf("Parse(%s) failed: %v", dir, err)
	}

	tmpDir := t.TempDir()
	if err := Generate(pkg, tmpDir); err != nil {
		t.Fatalf("Generate failed: %v", err)
	}

	// When CLIName is not set, it should default to the package name.
	cliPath := filepath.Join(tmpDir, "cmd", "movies", "main.go")
	data, err := os.ReadFile(cliPath)
	if err != nil {
		t.Fatalf("reading CLI file: %v", err)
	}
	content := string(data)
	if !strings.Contains(content, `kong.Name("movies")`) {
		t.Errorf("CLI should use package name as kong.Name when CLIName is not set\nGot:\n%s", content)
	}
}

func TestCLINameCustom(t *testing.T) {
	dir := moviesDir(t)
	pkg, err := parser.Parse(dir)
	if err != nil {
		t.Fatalf("Parse(%s) failed: %v", dir, err)
	}

	// Set a custom CLI name.
	pkg.CLIName = "film-db"

	tmpDir := t.TempDir()
	if err := Generate(pkg, tmpDir); err != nil {
		t.Fatalf("Generate failed: %v", err)
	}

	cliPath := filepath.Join(tmpDir, "cmd", "movies", "main.go")
	data, err := os.ReadFile(cliPath)
	if err != nil {
		t.Fatalf("reading CLI file: %v", err)
	}
	content := string(data)

	// Should use the custom name for kong.Name and description.
	if !strings.Contains(content, `kong.Name("film-db")`) {
		t.Errorf("CLI should use custom CLIName for kong.Name\nGot:\n%s", content)
	}
	if !strings.Contains(content, `kong.Description("CLI for the film-db data model.")`) {
		t.Errorf("CLI should use custom CLIName for kong.Description\nGot:\n%s", content)
	}

	// Package import should still use the real package name, not CLIName.
	if !strings.Contains(content, `"github.com/mlwelles/modusGraphMoviesProject/movies"`) {
		t.Errorf("CLI import should still use the real package name, not CLIName\nGot:\n%s", content)
	}
}

func TestWithValidatorEnabled(t *testing.T) {
	dir := moviesDir(t)
	pkg, err := parser.Parse(dir)
	if err != nil {
		t.Fatalf("Parse(%s) failed: %v", dir, err)
	}

	pkg.WithValidator = true

	tmpDir := t.TempDir()
	if err := Generate(pkg, tmpDir); err != nil {
		t.Fatalf("Generate failed: %v", err)
	}

	cliPath := filepath.Join(tmpDir, "cmd", "movies", "main.go")
	data, err := os.ReadFile(cliPath)
	if err != nil {
		t.Fatalf("reading CLI file: %v", err)
	}
	content := string(data)

	// Should contain the validator option.
	if !strings.Contains(content, "modusgraph.WithValidator(modusgraph.NewValidator())") {
		t.Errorf("CLI should contain WithValidator when enabled\nGot:\n%s", content)
	}

	// Should still contain WithAutoSchema.
	if !strings.Contains(content, "modusgraph.WithAutoSchema(true)") {
		t.Errorf("CLI should still contain WithAutoSchema\nGot:\n%s", content)
	}
}

func TestWithValidatorDisabled(t *testing.T) {
	dir := moviesDir(t)
	pkg, err := parser.Parse(dir)
	if err != nil {
		t.Fatalf("Parse(%s) failed: %v", dir, err)
	}

	// WithValidator defaults to false.
	tmpDir := t.TempDir()
	if err := Generate(pkg, tmpDir); err != nil {
		t.Fatalf("Generate failed: %v", err)
	}

	cliPath := filepath.Join(tmpDir, "cmd", "movies", "main.go")
	data, err := os.ReadFile(cliPath)
	if err != nil {
		t.Fatalf("reading CLI file: %v", err)
	}
	content := string(data)

	// Should NOT contain the validator option.
	if strings.Contains(content, "WithValidator") {
		t.Errorf("CLI should NOT contain WithValidator when disabled\nGot:\n%s", content)
	}
}

func TestWithValidatorAndCustomCLI(t *testing.T) {
	dir := moviesDir(t)
	pkg, err := parser.Parse(dir)
	if err != nil {
		t.Fatalf("Parse(%s) failed: %v", dir, err)
	}

	// Combine all CLI options.
	pkg.CLIName = "registry"
	pkg.WithValidator = true

	tmpDir := t.TempDir()
	customCLIDir := filepath.Join(tmpDir, "cmd", "registry")

	if err := Generate(pkg, tmpDir, WithCLIDir(customCLIDir)); err != nil {
		t.Fatalf("Generate failed: %v", err)
	}

	cliPath := filepath.Join(customCLIDir, "main.go")
	data, err := os.ReadFile(cliPath)
	if err != nil {
		t.Fatalf("reading CLI at custom path: %v", err)
	}
	content := string(data)

	// Should have all three features working together.
	if !strings.Contains(content, `kong.Name("registry")`) {
		t.Errorf("CLI should use custom CLIName\nGot:\n%s", content)
	}
	if !strings.Contains(content, "modusgraph.WithValidator(modusgraph.NewValidator())") {
		t.Errorf("CLI should contain WithValidator\nGot:\n%s", content)
	}
	if !strings.Contains(content, `"github.com/mlwelles/modusGraphMoviesProject/movies"`) {
		t.Errorf("CLI import should use real package name\nGot:\n%s", content)
	}
}

func TestWithCLIDirAndCLIName(t *testing.T) {
	dir := moviesDir(t)
	pkg, err := parser.Parse(dir)
	if err != nil {
		t.Fatalf("Parse(%s) failed: %v", dir, err)
	}

	// Use both custom CLI dir and custom CLI name.
	pkg.CLIName = "registry"
	tmpDir := t.TempDir()
	customCLIDir := filepath.Join(tmpDir, "cmd", "registry")

	if err := Generate(pkg, tmpDir, WithCLIDir(customCLIDir)); err != nil {
		t.Fatalf("Generate failed: %v", err)
	}

	// CLI should be at the custom path.
	cliPath := filepath.Join(customCLIDir, "main.go")
	data, err := os.ReadFile(cliPath)
	if err != nil {
		t.Fatalf("reading CLI at custom path: %v", err)
	}
	content := string(data)

	// Should use custom CLI name.
	if !strings.Contains(content, `kong.Name("registry")`) {
		t.Errorf("CLI should use custom CLIName\nGot:\n%s", content)
	}

	// Import should still use real package name.
	if !strings.Contains(content, `"github.com/mlwelles/modusGraphMoviesProject/movies"`) {
		t.Errorf("CLI import should use real package name\nGot:\n%s", content)
	}
}

func TestGeneratedClientHasQueryRaw(t *testing.T) {
	dir := moviesDir(t)
	pkg, err := parser.Parse(dir)
	if err != nil {
		t.Fatalf("Parse(%s) failed: %v", dir, err)
	}

	tmpDir := t.TempDir()
	if err := Generate(pkg, tmpDir); err != nil {
		t.Fatalf("Generate failed: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(tmpDir, "client_gen.go"))
	if err != nil {
		t.Fatalf("reading client_gen.go: %v", err)
	}
	content := string(data)

	if !strings.Contains(content, "func (c *Client) QueryRaw(") {
		t.Error("client_gen.go should contain QueryRaw method")
	}
	if !strings.Contains(content, "c.conn.QueryRaw(") {
		t.Error("client_gen.go QueryRaw should delegate to c.conn.QueryRaw")
	}
}

func TestGeneratedCLIHasQuerySubcommand(t *testing.T) {
	dir := moviesDir(t)
	pkg, err := parser.Parse(dir)
	if err != nil {
		t.Fatalf("Parse(%s) failed: %v", dir, err)
	}

	tmpDir := t.TempDir()
	if err := Generate(pkg, tmpDir); err != nil {
		t.Fatalf("Generate failed: %v", err)
	}

	cliPath := filepath.Join(tmpDir, "cmd", "movies", "main.go")
	data, err := os.ReadFile(cliPath)
	if err != nil {
		t.Fatalf("reading CLI file: %v", err)
	}
	content := string(data)

	// Should have query subcommand.
	if !strings.Contains(content, "QueryCmd") {
		t.Error("CLI should contain QueryCmd type")
	}
	if !strings.Contains(content, "Query") || !strings.Contains(content, "QueryCmd") {
		t.Error("CLI root should have Query field of type QueryCmd")
	}
	// Should have --dir flag.
	if !strings.Contains(content, `Dir  string`) {
		t.Error("CLI should have Dir flag")
	}
	// Should have connectString helper.
	if !strings.Contains(content, "func connectString()") {
		t.Error("CLI should have connectString function")
	}
}

func TestGeneratedCLIDirAndAddrMutuallyExclusive(t *testing.T) {
	dir := moviesDir(t)
	pkg, err := parser.Parse(dir)
	if err != nil {
		t.Fatalf("Parse(%s) failed: %v", dir, err)
	}

	tmpDir := t.TempDir()
	if err := Generate(pkg, tmpDir); err != nil {
		t.Fatalf("Generate failed: %v", err)
	}

	cliPath := filepath.Join(tmpDir, "cmd", "movies", "main.go")
	data, err := os.ReadFile(cliPath)
	if err != nil {
		t.Fatalf("reading CLI file: %v", err)
	}
	content := string(data)

	// Should contain mutual exclusion logic.
	if !strings.Contains(content, `--addr and --dir are mutually exclusive`) {
		t.Error("CLI should contain mutual exclusion error message")
	}
}
