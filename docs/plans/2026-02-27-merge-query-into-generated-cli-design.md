# Design: Merge Query Command into Generated CLI & Client

**Date:** 2026-02-27
**Status:** Approved

## Summary

Merge the standalone `cmd/query` tool's raw DQL query functionality into the
code-generated CLI as a `query` subcommand. Rename `cmd/modusgraphgen` to
`cmd/modusgraph-gen`. Add a `QueryRaw` method to the generated Go client so the
programmatic API mirrors the CLI. Update consumer projects
(modusGraphMoviesProject, go-registry-poc) to use the merged functionality.

## Decisions

| Decision | Choice |
|----------|--------|
| Approach | Template-only: embed query in cli.go.tmpl and client.go.tmpl |
| Generator directory | `cmd/modusgraph-gen` (binary: `modusgraph-gen`) |
| Connection modes | Global `--addr` and `--dir` flags, mutually exclusive |
| Query input | Positional arg with stdin fallback |
| Query flags | `--pretty` (default true), `--timeout` (default 30s) |
| Verbosity | Global `-v` flag on root CLI |
| cmd/query | Preserved with deprecation notice |
| Generated client | Gains `QueryRaw(ctx, query, vars)` method |

## Directory Structure (After)

```
cmd/
├── modusgraph-gen/                # RENAMED from cmd/modusgraphgen
│   ├── main.go                    # Binary: modusgraph-gen
│   └── internal/
│       ├── model/model.go
│       ├── parser/
│       │   ├── parser.go
│       │   ├── inference.go
│       │   └── testdata/movies/
│       └── generator/
│           ├── generator.go
│           └── templates/
│               ├── cli.go.tmpl        # MODIFIED: query subcommand + --dir/--addr
│               ├── client.go.tmpl     # MODIFIED: QueryRaw method
│               └── ... (other templates unchanged)
└── query/
    └── main.go                    # DEPRECATED (preserved for backwards compat)
```

## Generated Client API Addition

```go
// QueryRaw executes a raw DQL query against the database.
func (c *Client) QueryRaw(ctx context.Context, query string, vars map[string]string) ([]byte, error) {
    return c.conn.QueryRaw(ctx, query, vars)
}
```

## Generated CLI Changes

### Root CLI Struct

```go
var CLI struct {
    Addr  string `help:"Dgraph gRPC address." default:"dgraph://localhost:9080" env:"DGRAPH_ADDR"`
    Dir   string `help:"Local database directory (embedded mode)." env:"DGRAPH_DIR"`
    Query QueryCmd `cmd:"" help:"Execute a raw DQL query."`
    // ... entity commands unchanged
}
```

### Query Subcommand

```go
type QueryCmd struct {
    Query   string        `arg:"" optional:"" help:"DQL query string (reads stdin if omitted)."`
    Pretty  bool          `help:"Pretty-print JSON output." default:"true"`
    Timeout time.Duration `help:"Query timeout." default:"30s"`
}
```

Usage examples:
```bash
movies query '{ q(func: has(name@en), first: 5) { uid name@en } }'
echo '{ q(func: has(name@en)) { uid } }' | movies query
movies query --pretty=false '{ q(func: uid(0x1)) { uid name } }'
movies --dir /tmp/db query '{ q(func: has(name)) { uid } }'
```

### Connection Logic

```go
func connectString() string {
    if CLI.Dir != "" {
        if CLI.Addr != "dgraph://localhost:9080" {
            fmt.Fprintln(os.Stderr, "error: --addr and --dir are mutually exclusive")
            os.Exit(1)
        }
        return fmt.Sprintf("file://%s", filepath.Clean(CLI.Dir))
    }
    return CLI.Addr
}
```

## Consumer Project Updates

### modusGraphMoviesProject
- Update `go:generate` to reference `cmd/modusgraph-gen`
- Update go.mod replace directive to point to updated fork
- Re-run `go generate` to regenerate code with query subcommand
- Verify `movies query '...'` works alongside `movies film list` etc.
- Update README with query subcommand documentation

### go-registry-poc
- Same changes as modusGraphMoviesProject
- Verify `registry query '...'` works alongside `registry resource list` etc.
- Update README with query subcommand documentation

## Golden Test Impact

The golden test file `cmd/movies/main.go` will need updating to include the
query subcommand and new `--dir` flag. Run with `-update` flag to regenerate.

## What This Enables

After this change, a generated CLI provides:

**Standard modusGraph generated features:**
- Per-entity CRUD: `get`, `list`, `add`, `delete`
- Per-entity search: `search <term>` (for entities with fulltext indexes)
- Typed Go client with sub-clients per entity
- Fluent query builders per entity
- Auto-paging iterators (Go 1.23+ `iter.Seq2`)
- Functional options for mutations
- Auto-schema management
- Optional struct validation

**New query functionality:**
- Raw DQL queries via CLI: `<binary> query '<dql>'`
- Raw DQL queries via Go client: `client.QueryRaw(ctx, query, vars)`
- Support for both remote (gRPC) and embedded (file://) Dgraph connections
