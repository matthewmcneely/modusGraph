/*
 * SPDX-FileCopyrightText: © 2017-2026 Istari Digital, Inc.
 * SPDX-License-Identifier: Apache-2.0
 */

package modusgraph

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/dgraph-io/dgo/v250"
	"github.com/dgraph-io/dgo/v250/protos/api"
	"github.com/dgraph-io/dgraph/v25/protos/pb"
	"github.com/matthewmcneely/modusgraph/load"
)

// grpcUIDAllocator implements UIDAllocator for the gRPC path by calling
// dgo.Dgraph.AllocateUIDs in bulk. UIDs are leased in batches to minimise
// round-trips to the Zero leader.
type grpcUIDAllocator struct {
	pool    *clientPool
	mu      sync.Mutex
	nextUID uint64
	maxUID  uint64 // exclusive upper bound of the current lease
}

const uidLeaseBatch uint64 = 10000

func (a *grpcUIDAllocator) LeaseUIDs(n uint64) (*pb.AssignedIds, error) {
	a.mu.Lock()
	defer a.mu.Unlock()

	if a.nextUID+n > a.maxUID {
		alloc := uidLeaseBatch
		if n > uidLeaseBatch {
			alloc = n
		}
		dc, err := a.pool.get()
		if err != nil {
			return nil, fmt.Errorf("get client from pool for UID allocation: %w", err)
		}
		start, end, err := dc.AllocateUIDs(context.TODO(), alloc)
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

// grpcMutator implements Mutator for the gRPC (remote Dgraph) path.
// It retries aborted transactions using the same backoff policy as
// RetryPolicy (exponential backoff with jitter).
type grpcMutator struct {
	pool   *clientPool
	policy RetryPolicy
}

func (m *grpcMutator) Mutate(ctx context.Context, mu *api.Mutation) (map[string]string, error) {
	policy := m.policy
	if policy.MaxRetries <= 0 {
		policy = DefaultRetryPolicy
	}
	mu.CommitNow = true

	for attempt := 0; ; attempt++ {
		dc, err := m.pool.get()
		if err != nil {
			return nil, fmt.Errorf("get client from pool: %w", err)
		}

		txn := dc.NewTxn()
		resp, err := txn.Mutate(ctx, mu)
		txn.Discard(ctx)
		m.pool.put(dc)

		if err == nil {
			return resp.GetUids(), nil
		}
		if !errors.Is(err, dgo.ErrAborted) || attempt >= policy.MaxRetries {
			return nil, err
		}

		d := policy.delay(attempt)
		select {
		case <-sleepTimer(d):
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
}

// sleepTimer returns a channel that fires after d. Extracted for consistency
// with retry.go (both use the same mechanism).
func sleepTimer(d time.Duration) <-chan time.Time {
	return time.After(d)
}

// LoadData loads RDF or JSON data files from dataDir into the database.
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

	// Both paths go through loadData() so that caller options (FileMatch,
	// SortFiles, BatchSize, MutationWorkers) are always respected.
	var ll *liveLoader
	if c.engine != nil {
		ll = &liveLoader{
			mut:        &namespaceMutator{ns: c.engine.db0},
			uidAlloc:   c.engine,
			blankNodes: make(map[string]string),
			logger:     c.engine.logger,
		}
	} else {
		ll = &liveLoader{
			mut:        &grpcMutator{pool: c.pool},
			uidAlloc:   &grpcUIDAllocator{pool: c.pool},
			blankNodes: make(map[string]string),
			logger:     c.logger,
		}
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
