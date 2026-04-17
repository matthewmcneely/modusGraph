/*
 * SPDX-FileCopyrightText: © 2017-2026 Istari Digital, Inc.
 * SPDX-License-Identifier: Apache-2.0
 */

// Package load provides option types for configuring modusgraph.Client.LoadData calls.
//
// Usage:
//
//	client.LoadData(ctx, dataDir,
//	    load.WithBatchSize(10000),
//	    load.WithMutationWorkers(8),
//	    load.WithSchema("schema.dgraph"),
//	)
package load

import "strings"

// Option configures a LoadData call.
type Option func(*Options)

// FileMatcher selects which files in the data directory to load.
// A nil FileMatcher matches all files.
type FileMatcher interface {
	Match(path string) bool
}

// NewFileMatcher adapts a plain function to the FileMatcher interface.
type NewFileMatcher func(path string) bool

// Match implements FileMatcher.
func (f NewFileMatcher) Match(path string) bool { return f(path) }

// FileSort reorders the list of data files before processing.
type FileSort func([]string) []string

// DefaultExtensions is the default set of file extensions loaded by LoadData.
var DefaultExtensions = []string{".rdf", ".rdf.gz", ".json", ".json.gz"}

// ExtensionsMatcher matches files by suffix.
type ExtensionsMatcher struct {
	exts []string
}

func (m *ExtensionsMatcher) Match(path string) bool {
	for _, ext := range m.exts {
		if strings.HasSuffix(path, ext) {
			return true
		}
	}
	return false
}

// NewExtensionsMatcher returns a FileMatcher that accepts files whose path ends
// with any of the given suffixes.
func NewExtensionsMatcher(exts ...string) FileMatcher {
	return &ExtensionsMatcher{exts: exts}
}

// Options control the behavior of LoadData.
// Zero values use defaults (BatchSize=1000, MutationWorkers=1).
//
// File processing pipeline: walk directory → FilterFiles (applies FileMatcher) → SortFiles → process.
type Options struct {
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

	// FileMatcher, if set, selects which individual files to include during
	// directory walking. A nil FileMatcher matches all files.
	FileMatcher FileMatcher

	// SortFiles, if set, controls the order in which data files are processed.
	// Called after FilterFiles. By default files are in the lexicographic
	// order returned by filepath.Walk.
	SortFiles FileSort
}

// DefaultBatchSize is the default number of NQuads per mutation batch.
const DefaultBatchSize = 1000

// DefaultMutationWorkers is the default number of concurrent mutation goroutines.
const DefaultMutationWorkers = 1

// GetBatchSize returns the effective batch size, defaulting to DefaultBatchSize.
func (o Options) GetBatchSize() int {
	if o.BatchSize <= 0 {
		return DefaultBatchSize
	}
	return o.BatchSize
}

// GetMutationWorkers returns the effective worker count, defaulting to DefaultMutationWorkers.
func (o Options) GetMutationWorkers() int {
	if o.MutationWorkers <= 0 {
		return DefaultMutationWorkers
	}
	return o.MutationWorkers
}

// MatchFile reports whether an individual path should be included.
// If FileMatcher is nil, all files match.
func (o Options) MatchFile(path string) bool {
	if o.FileMatcher == nil {
		return true
	}
	return o.FileMatcher.Match(path)
}

// FilterFiles returns only the files from the input that pass MatchFile.
func (o Options) FilterFiles(files []string) []string {
	if o.FileMatcher == nil {
		return files
	}
	var out []string
	for _, f := range files {
		if o.FileMatcher.Match(f) {
			out = append(out, f)
		}
	}
	return out
}

// WithSchema applies the given Dgraph schema file before loading data.
func WithSchema(path string) Option {
	return func(o *Options) {
		o.SchemaPath = path
	}
}

// WithBatchSize sets the number of NQuads per mutation batch.
// Default is 1000.
func WithBatchSize(n int) Option {
	return func(o *Options) {
		o.BatchSize = n
	}
}

// WithMutationWorkers sets the number of concurrent mutation goroutines.
// Default is 1 (sequential).
func WithMutationWorkers(n int) Option {
	return func(o *Options) {
		o.MutationWorkers = n
	}
}

// WithFileMatcher sets a FileMatcher for per-file matching during directory walking.
// A nil filter matches all files.
func WithFileMatcher(f FileMatcher) Option {
	return func(o *Options) {
		o.FileMatcher = f
	}
}

// WithFileSort sets a function that reorders data files before processing.
// Called after FilterFiles.
func WithFileSort(fn FileSort) Option {
	return func(o *Options) {
		o.SortFiles = fn
	}
}

// WithOptions applies all non-zero fields from the given Options struct.
func WithOptions(lo Options) Option {
	return func(o *Options) {
		if lo.SchemaPath != "" {
			o.SchemaPath = lo.SchemaPath
		}
		if lo.BatchSize > 0 {
			o.BatchSize = lo.BatchSize
		}
		if lo.MutationWorkers > 0 {
			o.MutationWorkers = lo.MutationWorkers
		}
		if lo.FileMatcher != nil {
			o.FileMatcher = lo.FileMatcher
		}
		if lo.SortFiles != nil {
			o.SortFiles = lo.SortFiles
		}
	}
}
