/*
 * SPDX-FileCopyrightText: © 2017-2026 Istari Digital, Inc.
 * SPDX-License-Identifier: Apache-2.0
 */

package modusgraph_test

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	mg "github.com/matthewmcneely/modusgraph"
	"github.com/matthewmcneely/modusgraph/load"
	"github.com/stretchr/testify/require"
)

// TestClientLoadDataFile tests LoadData via the file:// URI (embedded engine) path.
func TestClientLoadDataFile(t *testing.T) {
	tmpDir := GetTempDir(t)
	client, cleanup := CreateTestClient(t, "file://"+tmpDir)
	defer cleanup()

	// Create RDF data directory and file.
	rdfDir := filepath.Join(tmpDir, "rdf_data")
	require.NoError(t, os.MkdirAll(rdfDir, 0755))

	rdfData := `_:alice <dgraph.type> "Person" .
_:alice <name> "Alice" .
_:alice <age> "30"^^<xs:int> .
_:bob <dgraph.type> "Person" .
_:bob <name> "Bob" .
_:bob <age> "25"^^<xs:int> .
_:alice <friend> _:bob .
`
	require.NoError(t, os.WriteFile(filepath.Join(rdfDir, "data.rdf"), []byte(rdfData), 0600))

	// Create schema file (outside the data dir).
	schemaFile := filepath.Join(tmpDir, "schema.dgraph")
	schemaData := `name: string @index(exact, term) .
age: int .
friend: [uid] @reverse .
type Person {
  name
  age
  friend
}
`
	require.NoError(t, os.WriteFile(schemaFile, []byte(schemaData), 0600))

	// Load data with schema.
	ctx := context.Background()
	err := client.LoadData(ctx, rdfDir, load.WithSchema(schemaFile))
	require.NoError(t, err)

	// Query for Alice and verify friend edge to Bob.
	const query = `{
		q(func: eq(name, "Alice")) {
			name
			age
			friend {
				name
				age
			}
		}
	}`

	resp, err := client.QueryRaw(ctx, query, nil)
	require.NoError(t, err)

	var result struct {
		Q []struct {
			Name   string `json:"name"`
			Age    int    `json:"age"`
			Friend []struct {
				Name string `json:"name"`
				Age  int    `json:"age"`
			} `json:"friend"`
		} `json:"q"`
	}
	require.NoError(t, json.Unmarshal(resp, &result))

	require.Len(t, result.Q, 1, "expected exactly one Alice node")
	require.Equal(t, "Alice", result.Q[0].Name)
	require.Equal(t, 30, result.Q[0].Age)
	require.Len(t, result.Q[0].Friend, 1, "Alice should have exactly one friend")
	require.Equal(t, "Bob", result.Q[0].Friend[0].Name)
	require.Equal(t, 25, result.Q[0].Friend[0].Age)
}

// NoSchemaNode is a test struct used by TestClientLoadDataFileNoSchema.
type NoSchemaNode struct {
	UID   string   `json:"uid,omitempty"`
	DType []string `json:"dgraph.type,omitempty"`
	Title string   `json:"title,omitempty" dgraph:"index=exact"`
}

// TestClientLoadDataFileNoSchema tests LoadData without WithSchema — the schema
// must already exist in the database.
func TestClientLoadDataFileNoSchema(t *testing.T) {
	tmpDir := GetTempDir(t)
	client, cleanup := CreateTestClient(t, "file://"+tmpDir)
	defer cleanup()

	ctx := context.Background()

	// Manually set up schema first via autoSchema.
	err := client.UpdateSchema(ctx, &NoSchemaNode{})
	require.NoError(t, err)

	// Write RDF file with no schema option.
	rdfDir := filepath.Join(tmpDir, "rdf_noschema")
	require.NoError(t, os.MkdirAll(rdfDir, 0755))
	rdf := `_:a <dgraph.type> "NoSchemaNode" .
_:a <title> "Hello" .
`
	require.NoError(t, os.WriteFile(filepath.Join(rdfDir, "data.rdf"), []byte(rdf), 0600))

	err = client.LoadData(ctx, rdfDir)
	require.NoError(t, err)
}

// TestClientLoadDataBadSchemaPath verifies that a bad schema path returns an error.
func TestClientLoadDataBadSchemaPath(t *testing.T) {
	tmpDir := GetTempDir(t)
	client, cleanup := CreateTestClient(t, "file://"+tmpDir)
	defer cleanup()

	rdfDir := filepath.Join(tmpDir, "rdf_empty")
	require.NoError(t, os.MkdirAll(rdfDir, 0755))
	require.NoError(t, os.WriteFile(filepath.Join(rdfDir, "data.rdf"), []byte(`_:a <name> "x" .`+"\n"), 0600))

	err := client.LoadData(context.Background(), rdfDir, load.WithSchema("/nonexistent/schema.dgraph"))
	require.Error(t, err)
	require.Contains(t, err.Error(), "read schema file")
}

// TestClientLoadDataEmptyDir verifies that an empty data directory returns an error.
func TestClientLoadDataEmptyDir(t *testing.T) {
	tmpDir := GetTempDir(t)
	client, cleanup := CreateTestClient(t, "file://"+tmpDir)
	defer cleanup()

	emptyDir := filepath.Join(tmpDir, "empty_rdf")
	require.NoError(t, os.MkdirAll(emptyDir, 0755))

	err := client.LoadData(context.Background(), emptyDir)
	require.Error(t, err)
	require.Contains(t, err.Error(), "no data files found")
}

// TestClientLoadDataWithOptions verifies that WithBatchSize and WithMutationWorkers
// are accepted and don't cause errors.
func TestClientLoadDataWithOptions(t *testing.T) {
	tmpDir := GetTempDir(t)
	client, cleanup := CreateTestClient(t, "file://"+tmpDir)
	defer cleanup()

	rdfDir := filepath.Join(tmpDir, "rdf_opts")
	require.NoError(t, os.MkdirAll(rdfDir, 0755))

	rdf := `_:x <dgraph.type> "OptsTestNode" .
_:x <name> "test" .
`
	require.NoError(t, os.WriteFile(filepath.Join(rdfDir, "data.rdf"), []byte(rdf), 0600))

	schemaFile := filepath.Join(tmpDir, "opts_schema.dgraph")
	require.NoError(t, os.WriteFile(schemaFile, []byte("name: string @index(exact) .\ntype OptsTestNode {\n  name: string\n}\n"), 0600))

	// Use all option funcs together.
	err := client.LoadData(context.Background(), rdfDir,
		load.WithSchema(schemaFile),
		load.WithBatchSize(5000),
		load.WithMutationWorkers(4),
	)
	require.NoError(t, err)
}

// TestClientLoadDataWithLoadOptions verifies the struct-based option func.
func TestClientLoadDataWithLoadOptions(t *testing.T) {
	tmpDir := GetTempDir(t)
	client, cleanup := CreateTestClient(t, "file://"+tmpDir)
	defer cleanup()

	rdfDir := filepath.Join(tmpDir, "rdf_struct_opts")
	require.NoError(t, os.MkdirAll(rdfDir, 0755))

	rdf := `_:y <dgraph.type> "StructOptsNode" .
_:y <name> "struct-test" .
`
	require.NoError(t, os.WriteFile(filepath.Join(rdfDir, "data.rdf"), []byte(rdf), 0600))

	schemaFile := filepath.Join(tmpDir, "struct_opts_schema.dgraph")
	require.NoError(t, os.WriteFile(schemaFile, []byte("name: string @index(exact) .\ntype StructOptsNode {\n  name: string\n}\n"), 0600))

	err := client.LoadData(context.Background(), rdfDir,
		load.WithOptions(load.Options{
			SchemaPath:      schemaFile,
			BatchSize:       10000,
			MutationWorkers: 8,
		}),
	)
	require.NoError(t, err)
}

// TestClientLoadDataGRPC tests LoadData via the dgraph:// URI (gRPC) path.
// This is the critical test — it verifies blank node resolution works across
// batches over gRPC.
func TestClientLoadDataGRPC(t *testing.T) {
	addr := os.Getenv("MODUSGRAPH_TEST_ADDR")
	if addr == "" {
		t.Skip("Skipping: MODUSGRAPH_TEST_ADDR not set")
	}

	ctx := context.Background()

	// Create client manually (not via CreateTestClient) so we can DropAll first.
	client, err := mg.NewClient("dgraph://" + addr)
	require.NoError(t, err)
	defer client.Close()

	require.NoError(t, client.DropAll(ctx))

	// Build RDF data: 100 LoadTestPerson nodes, each linked to the previous one.
	tmpDir := t.TempDir()
	rdfDir := filepath.Join(tmpDir, "rdf_data")
	require.NoError(t, os.MkdirAll(rdfDir, 0755))

	var rdf string
	for i := 0; i < 100; i++ {
		blank := fmt.Sprintf("_:person%d", i)
		rdf += fmt.Sprintf("%s <dgraph.type> \"LoadTestPerson\" .\n", blank)
		rdf += fmt.Sprintf("%s <name> \"Person %d\" .\n", blank, i)
		rdf += fmt.Sprintf("%s <age> \"%d\"^^<xs:int> .\n", blank, 20+i)
		if i > 0 {
			prev := fmt.Sprintf("_:person%d", i-1)
			rdf += fmt.Sprintf("%s <friend> %s .\n", blank, prev)
		}
	}
	require.NoError(t, os.WriteFile(filepath.Join(rdfDir, "data.rdf"), []byte(rdf), 0600))

	// Create schema file.
	schemaFile := filepath.Join(tmpDir, "schema.dgraph")
	schemaData := `name: string @index(exact, term) .
age: int .
friend: [uid] @reverse .
type LoadTestPerson {
  name
  age
  friend
}
`
	require.NoError(t, os.WriteFile(schemaFile, []byte(schemaData), 0600))

	// Load data with schema.
	err = client.LoadData(ctx, rdfDir, load.WithSchema(schemaFile))
	require.NoError(t, err)

	// Verify count is 100.
	countQuery := `{
		q(func: type(LoadTestPerson)) {
			count(uid)
		}
	}`
	resp, err := client.QueryRaw(ctx, countQuery, nil)
	require.NoError(t, err)

	var countResult struct {
		Q []struct {
			Count int `json:"count"`
		} `json:"q"`
	}
	require.NoError(t, json.Unmarshal(resp, &countResult))
	require.Len(t, countResult.Q, 1)
	require.Equal(t, 100, countResult.Q[0].Count, "expected 100 LoadTestPerson nodes")

	// Verify blank node resolution: Person 99's friend should be Person 98.
	friendQuery := `{
		q(func: eq(name, "Person 99")) {
			name
			friend {
				name
			}
		}
	}`
	resp, err = client.QueryRaw(ctx, friendQuery, nil)
	require.NoError(t, err)

	var friendResult struct {
		Q []struct {
			Name   string `json:"name"`
			Friend []struct {
				Name string `json:"name"`
			} `json:"friend"`
		} `json:"q"`
	}
	require.NoError(t, json.Unmarshal(resp, &friendResult))

	require.Len(t, friendResult.Q, 1, "expected exactly one Person 99 node")
	require.Equal(t, "Person 99", friendResult.Q[0].Name)
	require.Len(t, friendResult.Q[0].Friend, 1, "Person 99 should have exactly one friend")
	require.Equal(t, "Person 98", friendResult.Q[0].Friend[0].Name,
		"blank node resolution failed: Person 99's friend should be Person 98")

	// Clean up.
	require.NoError(t, client.DropAll(ctx))
}
