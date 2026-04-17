/*
 * SPDX-FileCopyrightText: © 2017-2026 Istari Digital, Inc.
 * SPDX-License-Identifier: Apache-2.0
 */

package modusgraph

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestLoadOptionsDefaults(t *testing.T) {
	var opts LoadOptions

	assert.Equal(t, 1000, opts.getBatchSize(), "default batch size")
	assert.Equal(t, 1, opts.getMutationWorkers(), "default mutation workers")
}

func TestLoadOptionsZeroValues(t *testing.T) {
	opts := LoadOptions{BatchSize: 0, MutationWorkers: 0}

	assert.Equal(t, 1000, opts.getBatchSize(), "zero batch size should use default")
	assert.Equal(t, 1, opts.getMutationWorkers(), "zero workers should use default")
}

func TestLoadOptionsNegativeValues(t *testing.T) {
	opts := LoadOptions{BatchSize: -1, MutationWorkers: -5}

	assert.Equal(t, 1000, opts.getBatchSize(), "negative batch size should use default")
	assert.Equal(t, 1, opts.getMutationWorkers(), "negative workers should use default")
}

func TestLoadOptionsExplicitValues(t *testing.T) {
	opts := LoadOptions{BatchSize: 5000, MutationWorkers: 8}

	assert.Equal(t, 5000, opts.getBatchSize())
	assert.Equal(t, 8, opts.getMutationWorkers())
}

func TestWithBatchSizeOpt(t *testing.T) {
	var opts LoadOptions
	WithBatchSize(10000)(&opts)
	assert.Equal(t, 10000, opts.BatchSize)
}

func TestWithMutationWorkersOpt(t *testing.T) {
	var opts LoadOptions
	WithMutationWorkers(16)(&opts)
	assert.Equal(t, 16, opts.MutationWorkers)
}

func TestWithSchemaOpt(t *testing.T) {
	var opts LoadOptions
	WithSchema("/path/to/schema.dgraph")(&opts)
	assert.Equal(t, "/path/to/schema.dgraph", opts.SchemaPath)
}

func TestWithLoadOptionsOpt(t *testing.T) {
	var opts LoadOptions

	WithLoadOptions(LoadOptions{
		SchemaPath:      "/schema.dgraph",
		BatchSize:       5000,
		MutationWorkers: 4,
	})(&opts)

	assert.Equal(t, "/schema.dgraph", opts.SchemaPath)
	assert.Equal(t, 5000, opts.BatchSize)
	assert.Equal(t, 4, opts.MutationWorkers)
}

func TestWithLoadOptionsZeroFieldsIgnored(t *testing.T) {
	opts := LoadOptions{
		SchemaPath:      "/existing.dgraph",
		BatchSize:       2000,
		MutationWorkers: 8,
	}

	// Applying a struct with zero fields should not overwrite existing values.
	WithLoadOptions(LoadOptions{})(&opts)

	assert.Equal(t, "/existing.dgraph", opts.SchemaPath)
	assert.Equal(t, 2000, opts.BatchSize)
	assert.Equal(t, 8, opts.MutationWorkers)
}

func TestWithLoadOptionsPartialOverride(t *testing.T) {
	opts := LoadOptions{
		SchemaPath:      "/old.dgraph",
		BatchSize:       2000,
		MutationWorkers: 8,
	}

	// Only override BatchSize.
	WithLoadOptions(LoadOptions{BatchSize: 10000})(&opts)

	assert.Equal(t, "/old.dgraph", opts.SchemaPath, "SchemaPath should be preserved")
	assert.Equal(t, 10000, opts.BatchSize, "BatchSize should be overridden")
	assert.Equal(t, 8, opts.MutationWorkers, "MutationWorkers should be preserved")
}

func TestOptFuncsComposeCorrectly(t *testing.T) {
	var opts LoadOptions

	// Apply multiple opts in sequence — last writer wins for each field.
	fns := []LoadOpt{
		WithBatchSize(1000),
		WithMutationWorkers(4),
		WithSchema("/a.dgraph"),
		WithBatchSize(5000), // overrides
	}
	for _, fn := range fns {
		fn(&opts)
	}

	assert.Equal(t, "/a.dgraph", opts.SchemaPath)
	assert.Equal(t, 5000, opts.BatchSize)
	assert.Equal(t, 4, opts.MutationWorkers)
}
