# Merge Query into Generated CLI — Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Merge the standalone `cmd/query` tool into the code-generated CLI as a `query` subcommand, rename `cmd/modusgraphgen` to `cmd/modusgraph-gen`, add a `QueryRaw` method to the generated client, and update consumer projects.

**Architecture:** Template-only approach — embed query logic directly in `cli.go.tmpl` and add `QueryRaw` to `client.go.tmpl`. The generator directory moves from `cmd/modusgraphgen` to `cmd/modusgraph-gen`. All connection modes (`--addr` gRPC, `--dir` embedded) become global CLI flags. The standalone `cmd/query` is preserved with a deprecation notice.

**Tech Stack:** Go, text/template, Kong CLI framework, modusgraph Client interface

---

### Task 1: Rename cmd/modusgraphgen → cmd/modusgraph-gen

**Files:**
- Rename: `cmd/modusgraphgen/` → `cmd/modusgraph-gen/`
- Modify: `cmd/modusgraph-gen/main.go` (update doc comment)
- Modify: `cmd/modusgraph-gen/internal/generator/generator.go:18` (update import path)
- Modify: `cmd/modusgraph-gen/internal/generator/generator_test.go:12` (update import paths)
- Modify: `cmd/modusgraph-gen/internal/parser/testdata/movies/generate.go` (update go:generate directive)

**Step 1: Move the directory**

```bash
git mv cmd/modusgraphgen cmd/modusgraph-gen
```

**Step 2: Update the doc comment in main.go**

In `cmd/modusgraph-gen/main.go`, change line 7:
```go
// Old:
//	go run github.com/matthewmcneely/modusgraph/cmd/modusgraphgen [flags]
// New:
//	go run github.com/matthewmcneely/modusgraph/cmd/modusgraph-gen [flags]
```

**Step 3: Update internal import paths**

In `cmd/modusgraph-gen/main.go`, update imports:
```go
// Old:
"github.com/matthewmcneely/modusgraph/cmd/modusgraphgen/internal/generator"
"github.com/matthewmcneely/modusgraph/cmd/modusgraphgen/internal/parser"
// New:
"github.com/matthewmcneely/modusgraph/cmd/modusgraph-gen/internal/generator"
"github.com/matthewmcneely/modusgraph/cmd/modusgraph-gen/internal/parser"
```

In `cmd/modusgraph-gen/internal/generator/generator.go:18`, update:
```go
// Old:
"github.com/matthewmcneely/modusgraph/cmd/modusgraphgen/internal/model"
// New:
"github.com/matthewmcneely/modusgraph/cmd/modusgraph-gen/internal/model"
```

In `cmd/modusgraph-gen/internal/generator/generator_test.go:12`, update:
```go
// Old:
"github.com/matthewmcneely/modusgraph/cmd/modusgraphgen/internal/model"
"github.com/matthewmcneely/modusgraph/cmd/modusgraphgen/internal/parser"
// New:
"github.com/matthewmcneely/modusgraph/cmd/modusgraph-gen/internal/model"
"github.com/matthewmcneely/modusgraph/cmd/modusgraph-gen/internal/parser"
```

**Step 4: Update test fixture go:generate directive**

In `cmd/modusgraph-gen/internal/parser/testdata/movies/generate.go`:
```go
// Old:
//go:generate go run github.com/matthewmcneely/modusgraph/cmd/modusgraphgen
// New:
//go:generate go run github.com/matthewmcneely/modusgraph/cmd/modusgraph-gen
```

**Step 5: Verify build and tests**

```bash
go build ./cmd/modusgraph-gen/...
go test ./cmd/modusgraph-gen/...
```
Expected: Build succeeds, all tests pass.

**Step 6: Commit**

```bash
git add -A && git commit -m "refactor: rename cmd/modusgraphgen to cmd/modusgraph-gen"
```

---

### Task 2: Add deprecation notice to cmd/query

**Files:**
- Modify: `cmd/query/main.go`

**Step 1: Add deprecation comment**

At the top of `cmd/query/main.go`, add after the license header (before `package main`):
```go
// Deprecated: Use the generated CLI's "query" subcommand instead.
// Example: movies query '{ q(func: has(name@en)) { uid name@en } }'
// This standalone tool will be removed in a future release.
```

**Step 2: Add runtime deprecation warning**

At the start of `main()`, before flag parsing, add:
```go
fmt.Fprintln(os.Stderr, "WARNING: cmd/query is deprecated. Use the generated CLI's 'query' subcommand instead.")
```

**Step 3: Verify it still works**

```bash
go build ./cmd/query/...
```
Expected: Build succeeds.

**Step 4: Commit**

```bash
git add cmd/query/main.go && git commit -m "chore: add deprecation notice to standalone cmd/query"
```

---

### Task 3: Add QueryRaw method to client.go.tmpl

**Files:**
- Modify: `cmd/modusgraph-gen/internal/generator/templates/client.go.tmpl`

**Step 1: Update the template**

Add at the end of `client.go.tmpl`, before the closing (after the `Close()` method):

```
// QueryRaw executes a raw DQL query against the database.
// The query parameter is the Dgraph query string (DQL syntax).
// The vars parameter is an optional map of variable names to values for parameterized queries.
func (c *Client) QueryRaw(ctx context.Context, query string, vars map[string]string) ([]byte, error) {
	return c.conn.QueryRaw(ctx, query, vars)
}
```

Also add `"context"` to the import block in the template:

```go
import (
	"context"

	"github.com/matthewmcneely/modusgraph"
)
```

**Step 2: Verify generation compiles**

```bash
go test ./cmd/modusgraph-gen/internal/generator/ -run TestGenerateOutputFiles -v
```
Expected: PASS (generated files still compile, but golden tests will fail — that's expected until we update them).

**Step 3: Commit**

```bash
git add cmd/modusgraph-gen/internal/generator/templates/client.go.tmpl
git commit -m "feat: add QueryRaw method to generated client template"
```

---

### Task 4: Add query subcommand and --dir/--addr flags to cli.go.tmpl

**Files:**
- Modify: `cmd/modusgraph-gen/internal/generator/templates/cli.go.tmpl`

**Step 1: Replace the entire cli.go.tmpl with the new version**

The new template adds:
- `Dir` global flag (mutually exclusive with `Addr`)
- `Query` subcommand with `--pretty`, `--timeout` flags
- `connectString()` helper function
- stdin fallback for query input

New full `cli.go.tmpl`:

```
package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/alecthomas/kong"
	"github.com/matthewmcneely/modusgraph"
	"{{.ModulePath}}/{{.Name}}"
)

// CLI is the root command parsed by Kong.
var CLI struct {
	Addr string `help:"Dgraph gRPC address." default:"dgraph://localhost:9080" env:"DGRAPH_ADDR"`
	Dir  string `help:"Local database directory (embedded mode, mutually exclusive with --addr)." env:"DGRAPH_DIR"`

	Query QueryCmd `cmd:"" help:"Execute a raw DQL query."`
{{- range .Entities}}
	{{.Name}} {{.Name}}Cmd `cmd:"" help:"Manage {{.Name}} entities."`
{{- end}}
}

// QueryCmd executes a raw DQL query against the database.
type QueryCmd struct {
	Query   string        `arg:"" optional:"" help:"DQL query string (reads stdin if omitted)."`
	Pretty  bool          `help:"Pretty-print JSON output." default:"true" negatable:""`
	Timeout time.Duration `help:"Query timeout." default:"30s"`
}

func (c *QueryCmd) Run(client *{{.Name}}.Client) error {
	query := c.Query
	if query == "" {
		// Read from stdin.
		reader := bufio.NewReader(os.Stdin)
		var sb strings.Builder
		for {
			line, err := reader.ReadString('\n')
			sb.WriteString(line)
			if err != nil {
				if err != io.EOF {
					return fmt.Errorf("reading stdin: %w", err)
				}
				break
			}
		}
		query = strings.TrimSpace(sb.String())
	}

	if query == "" {
		return fmt.Errorf("empty query: provide a DQL query as an argument or via stdin")
	}

	ctx, cancel := context.WithTimeout(context.Background(), c.Timeout)
	defer cancel()

	resp, err := client.QueryRaw(ctx, query, nil)
	if err != nil {
		return err
	}

	if c.Pretty {
		var data any
		if err := json.Unmarshal(resp, &data); err != nil {
			return fmt.Errorf("parsing response: %w", err)
		}
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(data)
	}
	_, err = fmt.Println(string(resp))
	return err
}

{{range .Entities}}
// {{.Name}}Cmd groups subcommands for {{.Name}}.
type {{.Name}}Cmd struct {
	Get    {{.Name}}GetCmd    `cmd:"" help:"Get a {{.Name}} by UID."`
	List   {{.Name}}ListCmd   `cmd:"" help:"List {{.Name}} entities."`
	Add    {{.Name}}AddCmd    `cmd:"" help:"Add a new {{.Name}}."`
	Delete {{.Name}}DeleteCmd `cmd:"" help:"Delete a {{.Name}} by UID."`
{{- if .Searchable}}
	Search {{.Name}}SearchCmd `cmd:"" help:"Search {{.Name}} by {{.SearchField}}."`
{{- end}}
}

type {{.Name}}GetCmd struct {
	UID string `arg:"" required:"" help:"The UID of the {{.Name}}."`
}

func (c *{{.Name}}GetCmd) Run(client *{{$.Name}}.Client) error {
	result, err := client.{{.Name}}.Get(context.Background(), c.UID)
	if err != nil {
		return err
	}
	return printJSON(result)
}

type {{.Name}}ListCmd struct {
	First  int `help:"Maximum results to return." default:"10"`
	Offset int `help:"Number of results to skip." default:"0"`
}

func (c *{{.Name}}ListCmd) Run(client *{{$.Name}}.Client) error {
	results, err := client.{{.Name}}.List(context.Background(),
		{{$.Name}}.First(c.First), {{$.Name}}.Offset(c.Offset))
	if err != nil {
		return err
	}
	return printJSON(results)
}

type {{.Name}}AddCmd struct {
{{- range scalarFields .Fields}}{{if and (not .IsUID) (not .IsDType)}}
	{{.Name}} string `help:"Set {{.Name}}." name:"{{toLower .Name}}"`
{{- end}}{{end}}
}

func (c *{{.Name}}AddCmd) Run(client *{{$.Name}}.Client) error {
	v := &{{$.Name}}.{{.Name}}{
{{- range scalarFields .Fields}}{{if and (not .IsUID) (not .IsDType) (eq .GoType "string")}}
		{{.Name}}: c.{{.Name}},
{{- end}}{{end}}
	}
	if err := client.{{.Name}}.Add(context.Background(), v); err != nil {
		return err
	}
	return printJSON(v)
}

type {{.Name}}DeleteCmd struct {
	UID string `arg:"" required:"" help:"The UID to delete."`
}

func (c *{{.Name}}DeleteCmd) Run(client *{{$.Name}}.Client) error {
	return client.{{.Name}}.Delete(context.Background(), c.UID)
}
{{if .Searchable}}
type {{.Name}}SearchCmd struct {
	Term   string `arg:"" required:"" help:"The search term."`
	First  int    `help:"Maximum results to return." default:"10"`
	Offset int    `help:"Number of results to skip." default:"0"`
}

func (c *{{.Name}}SearchCmd) Run(client *{{$.Name}}.Client) error {
	results, err := client.{{.Name}}.Search(context.Background(), c.Term,
		{{$.Name}}.First(c.First), {{$.Name}}.Offset(c.Offset))
	if err != nil {
		return err
	}
	return printJSON(results)
}
{{end}}
{{end}}

func printJSON(v any) error {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}

func connectString() (string, error) {
	if CLI.Dir != "" {
		if CLI.Addr != "dgraph://localhost:9080" {
			return "", fmt.Errorf("--addr and --dir are mutually exclusive")
		}
		return fmt.Sprintf("file://%s", filepath.Clean(CLI.Dir)), nil
	}
	return CLI.Addr, nil
}

func main() {
	ctx := kong.Parse(&CLI,
		kong.Name("{{.CLIName}}"),
		kong.Description("CLI for the {{.CLIName}} data model."),
	)

	connStr, err := connectString()
	if err != nil {
		fmt.Fprintf(os.Stderr, "%v\n", err)
		os.Exit(1)
	}

	client, err := {{.Name}}.New(connStr,
		modusgraph.WithAutoSchema(true),
{{- if .WithValidator}}
		modusgraph.WithValidator(modusgraph.NewValidator()),
{{- end}}
	)
	if err != nil {
		fmt.Fprintf(os.Stderr, "connect: %v\n", err)
		os.Exit(1)
	}
	defer client.Close()

	err = ctx.Run(client)
	ctx.FatalIfErrorf(err)
}
```

**Step 2: Verify generation compiles**

```bash
go test ./cmd/modusgraph-gen/internal/generator/ -run TestGenerateOutputFiles -v
```
Expected: PASS.

**Step 3: Commit**

```bash
git add cmd/modusgraph-gen/internal/generator/templates/cli.go.tmpl
git commit -m "feat: add query subcommand and --dir/--addr flags to generated CLI"
```

---

### Task 5: Update generator tests and golden files

**Files:**
- Modify: `cmd/modusgraph-gen/internal/generator/generator_test.go`
- Update: `cmd/modusgraph-gen/internal/generator/testdata/golden/client_gen.go` (via -update flag)

**Step 1: Add test for QueryRaw in generated client**

Add this test to `generator_test.go`:

```go
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
```

**Step 2: Add test for query subcommand in generated CLI**

```go
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
	if !strings.Contains(content, `Query QueryCmd`) {
		t.Error("CLI root should have Query field")
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
```

**Step 3: Run all tests (they will fail on golden diff)**

```bash
go test ./cmd/modusgraph-gen/... -v
```
Expected: New tests PASS, golden test FAILS (expected — golden files are stale).

**Step 4: Update golden files**

```bash
go test ./cmd/modusgraph-gen/internal/generator/ -update -v
```
Expected: Golden files updated successfully.

**Step 5: Verify all tests pass**

```bash
go test ./cmd/modusgraph-gen/... -v
```
Expected: All tests PASS.

**Step 6: Commit**

```bash
git add -A && git commit -m "test: add query subcommand tests and update golden files"
```

---

### Task 6: Push to feature branch and update PR on upstream

**Step 1: Push changes to origin**

```bash
git push origin feature/add-modusgraphgen
```

**Step 2: Update existing PR #10 on upstream (matthewmcneely/modusgraph)**

The PR at https://github.com/matthewmcneely/modusgraph/pull/10 should auto-update since we pushed to the same branch.

Verify:
```bash
gh pr view 10 --repo matthewmcneely/modusgraph
```

**Step 3: Create new PR to fork's main branch**

```bash
gh pr create \
  --repo mlwelles/modusGraph \
  --base main \
  --head feature/add-modusgraphgen \
  --title "feat: merge query command into generated CLI and rename to modusgraph-gen" \
  --body "$(cat <<'EOF'
## Summary
- Renames `cmd/modusgraphgen` to `cmd/modusgraph-gen`
- Adds `query` subcommand to generated CLI (accepts DQL as arg or stdin)
- Adds `QueryRaw` method to generated Go client
- Adds `--dir` flag for embedded Dgraph mode (mutually exclusive with `--addr`)
- Preserves `cmd/query` with deprecation notice
- Updates golden tests

## Usage
\`\`\`bash
movies query '{ q(func: has(name@en), first: 5) { uid name@en } }'
echo '{ q(func: has(name)) { uid } }' | movies query
movies --dir /tmp/db query '{ q(func: has(name)) { uid } }'
\`\`\`
EOF
)"
```

---

### Task 7: Update modusGraphMoviesProject to use merged fork

**Files:**
- Modify: `/Users/mwelles/Developer/mlwelles/modusGraphMoviesProject/go.mod`
- Modify: `/Users/mwelles/Developer/mlwelles/modusGraphMoviesProject/movies/generate.go`
- Regenerate: all `*_gen.go` files and `cmd/movies/main.go`
- Modify: `/Users/mwelles/Developer/mlwelles/modusGraphMoviesProject/README.md`

**Step 1: Update go.mod replace directive**

The go.mod needs a replace directive pointing to the updated fork. First, add a tool directive for the renamed generator:

In `go.mod`, update the tool line:
```
// Old:
tool github.com/mlwelles/modusGraphGen
// New:
tool github.com/matthewmcneely/modusgraph/cmd/modusgraph-gen
```

Remove the old replace for modusGraphGen (no longer needed since generator is now in the modusgraph repo).

Ensure the modusgraph replace points to the latest fork commit:
```
replace github.com/matthewmcneely/modusgraph => github.com/mlwelles/modusGraph <latest-commit-version>
```

Run `go mod tidy`.

**Step 2: Update generate.go directive**

```go
// Old:
//go:generate go run github.com/mlwelles/modusGraphGen
// New:
//go:generate go run github.com/matthewmcneely/modusgraph/cmd/modusgraph-gen
```

**Step 3: Regenerate code**

```bash
go generate ./movies/...
```

**Step 4: Verify build**

```bash
go build ./...
```

**Step 5: Verify the CLI has the query subcommand**

```bash
go run ./movies/cmd/movies --help
go run ./movies/cmd/movies query --help
```

**Step 6: Update README.md**

Add a section documenting the new `query` subcommand and the dual connection modes. Describe both what standard modusGraph provides (typed CRUD, search, query builders, iterators, validation) and what the new query functionality adds (raw DQL queries via CLI and Go client).

**Step 7: Run tests**

```bash
go test ./... -short
```
Expected: Tests pass (integration tests skip without Dgraph).

**Step 8: Commit and push**

```bash
git add -A
git commit -m "feat: switch to modusgraph-gen from modusgraph repo, add query subcommand"
git push origin main
```

---

### Task 8: Update go-registry-poc to use merged fork

**Files:**
- Modify: `/Users/mwelles/Developer/istari-digital/go-registry-poc/go.mod`
- Modify: `/Users/mwelles/Developer/istari-digital/go-registry-poc/repository/generate.go`
- Regenerate: all `*_gen.go` files and `cmd/registry/main.go`
- Modify: `/Users/mwelles/Developer/istari-digital/go-registry-poc/README.md`

**Step 1: Update go.mod**

Replace the tool and replace directives:
```
// Remove:
tool github.com/mlwelles/modusGraphGen
replace github.com/mlwelles/modusGraphGen => github.com/mlwelles/modusGraphGen v1.3.0

// Add:
tool github.com/matthewmcneely/modusgraph/cmd/modusgraph-gen
```

Ensure the modusgraph replace points to the latest fork commit:
```
replace github.com/matthewmcneely/modusgraph => github.com/mlwelles/modusGraph <latest-commit-version>
```

Run `go mod tidy`.

**Step 2: Update generate.go directive**

```go
// Old:
//go:generate go run github.com/mlwelles/modusGraphGen -cli-dir ../cmd/registry -cli-name registry -with-validator
// New:
//go:generate go run github.com/matthewmcneely/modusgraph/cmd/modusgraph-gen -cli-dir ../cmd/registry -cli-name registry -with-validator
```

**Step 3: Regenerate code**

```bash
go generate ./repository/...
```

**Step 4: Verify build**

```bash
go build ./...
```

**Step 5: Verify the CLI has the query subcommand**

```bash
go run ./cmd/registry --help
go run ./cmd/registry query --help
```

**Step 6: Update README.md**

Add documentation for the `query` subcommand. Describe standard modusGraph features and the new raw query capability.

**Step 7: Run tests**

```bash
go test ./... -short
```
Expected: Tests pass.

**Step 8: Commit and push**

First commit any pending module rename changes, then the new changes:
```bash
git add -A
git commit -m "feat: switch to modusgraph-gen, add query subcommand support"
git push origin main
```

---

## Task Summary

| Task | Description | Depends On |
|------|-------------|------------|
| 1 | Rename cmd/modusgraphgen → cmd/modusgraph-gen | — |
| 2 | Deprecate cmd/query | — |
| 3 | Add QueryRaw to client.go.tmpl | 1 |
| 4 | Add query subcommand to cli.go.tmpl | 1, 3 |
| 5 | Update tests and golden files | 3, 4 |
| 6 | Push to feature branch, update PRs | 5 |
| 7 | Update modusGraphMoviesProject | 6 |
| 8 | Update go-registry-poc | 6 |
