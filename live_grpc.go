/*
 * SPDX-FileCopyrightText: © 2017-2026 Istari Digital, Inc.
 * SPDX-License-Identifier: Apache-2.0
 */

package modusgraph

import (
	"context"
	"fmt"
	"os"

	"github.com/dgraph-io/dgo/v250/protos/api"
)

// grpcMutator implements mutator for the gRPC (remote Dgraph) path.
type grpcMutator struct {
	pool *clientPool
}

func (m *grpcMutator) mutate(ctx context.Context, mu *api.Mutation) (map[string]string, error) {
	dc, err := m.pool.get()
	if err != nil {
		return nil, fmt.Errorf("get client from pool: %w", err)
	}
	defer m.pool.put(dc)

	mu.CommitNow = true
	txn := dc.NewTxn()
	defer txn.Discard(ctx)

	resp, err := txn.Mutate(ctx, mu)
	if err != nil {
		return nil, err
	}
	return resp.GetUids(), nil
}

// LoadData loads RDF or JSON data files from dataDir into the database.
// Files must have .rdf, .rdf.gz, .json, or .json.gz extensions.
func (c client) LoadData(ctx context.Context, dataDir string, opts ...LoadOpt) error {
	options := loadOptions{}
	for _, opt := range opts {
		opt(&options)
	}

	// Apply schema if requested.
	if options.schemaPath != "" {
		schemaData, err := os.ReadFile(options.schemaPath)
		if err != nil {
			return fmt.Errorf("read schema file [%s]: %w", options.schemaPath, err)
		}
		if err := c.alterSchema(ctx, string(schemaData)); err != nil {
			return fmt.Errorf("alter schema: %w", err)
		}
	}

	// Embedded engine path.
	if c.engine != nil {
		return c.engine.db0.LoadData(ctx, dataDir)
	}

	// gRPC path.
	ll := &liveLoader{
		mut:        &grpcMutator{pool: c.pool},
		uidAlloc:   nil,
		blankNodes: make(map[string]string),
		logger:     c.logger,
	}
	return loadData(ctx, ll, dataDir)
}

func (c client) alterSchema(ctx context.Context, schema string) error {
	if c.engine != nil {
		return c.engine.db0.AlterSchema(ctx, schema)
	}

	dc, err := c.pool.get()
	if err != nil {
		return err
	}
	defer c.pool.put(dc)

	return dc.Alter(ctx, &api.Operation{Schema: schema})
}
