/*
 * SPDX-FileCopyrightText: © 2017-2026 Istari Digital, Inc.
 * SPDX-License-Identifier: Apache-2.0
 */

package modusgraph

// LoadOpt configures a LoadData call.
type LoadOpt func(*LoadOptions)

// LoadOptions controls the behaviour of LoadData.
// Zero values use defaults (BatchSize=1000, MutationWorkers=1).
type LoadOptions struct {
	// SchemaPath is the path to a Dgraph schema file applied before loading.
	// Empty means the schema must already exist.
	SchemaPath string

	// BatchSize is the number of NQuads per mutation batch.
	// Larger batches reduce gRPC round-trips but increase per-transaction
	// memory on the server. Default is 1000.
	BatchSize int

	// MutationWorkers is the number of concurrent goroutines submitting
	// mutations. Higher values increase throughput but put more load on the
	// Dgraph cluster. Default is 1 (sequential).
	MutationWorkers int
}

func (o LoadOptions) getBatchSize() int {
	if o.BatchSize <= 0 {
		return defaultBatchSize
	}
	return o.BatchSize
}

func (o LoadOptions) getMutationWorkers() int {
	if o.MutationWorkers <= 0 {
		return defaultMutationWorkers
	}
	return o.MutationWorkers
}

// WithSchema applies the given Dgraph schema file before loading data.
// The schema file should contain Dgraph Schema Definition Language.
// If not provided, the schema must already exist in the database.
func WithSchema(path string) LoadOpt {
	return func(o *LoadOptions) {
		o.SchemaPath = path
	}
}

// WithBatchSize sets the number of NQuads per mutation batch.
// Larger batches reduce the number of gRPC round-trips but increase
// per-transaction memory usage on the server. Default is 1000.
func WithBatchSize(n int) LoadOpt {
	return func(o *LoadOptions) {
		o.BatchSize = n
	}
}

// WithMutationWorkers sets the number of concurrent goroutines submitting
// mutations. Higher values increase throughput but put more load on the
// Dgraph cluster. Default is 1 (sequential).
func WithMutationWorkers(n int) LoadOpt {
	return func(o *LoadOptions) {
		o.MutationWorkers = n
	}
}

// WithLoadOptions applies all fields from the given LoadOptions struct.
// Zero-valued fields are ignored (defaults apply).
func WithLoadOptions(lo LoadOptions) LoadOpt {
	return func(o *LoadOptions) {
		if lo.SchemaPath != "" {
			o.SchemaPath = lo.SchemaPath
		}
		if lo.BatchSize > 0 {
			o.BatchSize = lo.BatchSize
		}
		if lo.MutationWorkers > 0 {
			o.MutationWorkers = lo.MutationWorkers
		}
	}
}
