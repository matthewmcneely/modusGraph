/*
 * SPDX-FileCopyrightText: © 2017-2026 Istari Digital, Inc.
 * SPDX-License-Identifier: Apache-2.0
 */

package modusgraph

import (
	"context"
	"errors"
	"fmt"
	"math/rand/v2"
	"os"
	"sync"
	"time"

	"github.com/dgraph-io/dgo/v250"
	"github.com/dgraph-io/dgo/v250/protos/api"
	"github.com/dgraph-io/dgraph/v25/protos/pb"
	"github.com/matthewmcneely/modusgraph/load"
)

// grpcUIDAllocator implements uidAllocator for the gRPC path by calling
// dgo.Dgraph.AllocateUIDs in bulk. UIDs are leased in batches to minimise
// round-trips to the Zero leader.
type grpcUIDAllocator struct {
	pool    *clientPool
	mu      sync.Mutex
	nextUID uint64
	maxUID  uint64 // exclusive upper bound of the current lease
}

const uidLeaseBatch = 10000

func (a *grpcUIDAllocator) LeaseUIDs(n uint64) (*pb.AssignedIds, error) {
	a.mu.Lock()
	defer a.mu.Unlock()

	if a.nextUID+n > a.maxUID {
		alloc := uidLeaseBatch
		if n > uidLeaseBatch {
			alloc = int(n)
		}
		dc, err := a.pool.get()
		if err != nil {
			return nil, fmt.Errorf("get client from pool for UID allocation: %w", err)
		}
		start, end, err := dc.AllocateUIDs(context.Background(), uint64(alloc))
		a.pool.put(dc)
		if err != nil {
			return nil, fmt.Errorf("allocate UIDs: %w", err)
		}
		a.nextUID = start
		a.maxUID = end
	}

	uid := a.nextUID
	a.nextUID += n
	return &pb.AssignedIds{StartId: uid}, nil
}

// grpcMutator implements mutator for the gRPC (remote Dgraph) path.
// It retries aborted transactions with exponential backoff since concurrent
// mutation workers can cause Dgraph transaction conflicts.
type grpcMutator struct {
	pool       *clientPool
	maxRetries int
}

const (
	grpcMutateMaxRetries = 5
	grpcMutateBaseDelay  = 100 * time.Millisecond
	grpcMutateMaxDelay   = 5 * time.Second
)

func (m *grpcMutator) mutate(ctx context.Context, mu *api.Mutation) (map[string]string, error) {
	maxRetries := m.maxRetries
	if maxRetries <= 0 {
		maxRetries = grpcMutateMaxRetries
	}

	for attempt := 0; ; attempt++ {
		dc, err := m.pool.get()
		if err != nil {
			return nil, fmt.Errorf("get client from pool: %w", err)
		}

		mu.CommitNow = true
		txn := dc.NewTxn()
		resp, err := txn.Mutate(ctx, mu)
		txn.Discard(ctx)
		m.pool.put(dc)

		if err == nil {
			return resp.GetUids(), nil
		}
		if !errors.Is(err, dgo.ErrAborted) || attempt >= maxRetries {
			return nil, err
		}
		// Exponential backoff with jitter.
		delay := grpcMutateBaseDelay * time.Duration(1<<uint(attempt))
		if delay > grpcMutateMaxDelay {
			delay = grpcMutateMaxDelay
		}
		delay += time.Duration(float64(delay) * 0.1 * rand.Float64())
		select {
		case <-time.After(delay):
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
}

// LoadData loads RDF or JSON data files from dataDir into the database.
// Files must have .rdf, .rdf.gz, .json, or .json.gz extensions.
func (c client) LoadData(ctx context.Context, dataDir string, opts ...load.Option) error {
	options := load.Options{}
	for _, opt := range opts {
		opt(&options)
	}

	// Apply schema if requested.
	if options.SchemaPath != "" {
		schemaData, err := os.ReadFile(options.SchemaPath)
		if err != nil {
			return fmt.Errorf("read schema file [%s]: %w", options.SchemaPath, err)
		}
		if err := c.alterSchema(ctx, string(schemaData)); err != nil {
			return fmt.Errorf("alter schema: %w", err)
		}
	}

	// Embedded engine path.
	if c.engine != nil {
		return c.engine.db0.LoadData(ctx, dataDir)
	}

	// gRPC path: pre-allocate UIDs locally (like the embedded path) so
	// mutations are independent and can execute concurrently.
	ll := &liveLoader{
		mut:        &grpcMutator{pool: c.pool},
		uidAlloc:   &grpcUIDAllocator{pool: c.pool},
		blankNodes: make(map[string]string),
		logger:     c.logger,
	}
	return loadData(ctx, ll, dataDir, options)
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
