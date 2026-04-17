/*
 * SPDX-FileCopyrightText: © 2017-2026 Istari Digital, Inc.
 * SPDX-License-Identifier: Apache-2.0
 */

package load

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestOptionsDefaults(t *testing.T) {
	var opts Options

	assert.Equal(t, DefaultBatchSize, opts.GetBatchSize(), "default batch size")
	assert.Equal(t, DefaultMutationWorkers, opts.GetMutationWorkers(), "default mutation workers")
}

func TestOptionsZeroValues(t *testing.T) {
	opts := Options{BatchSize: 0, MutationWorkers: 0}

	assert.Equal(t, DefaultBatchSize, opts.GetBatchSize(), "zero batch size should use default")
	assert.Equal(t, DefaultMutationWorkers, opts.GetMutationWorkers(), "zero workers should use default")
}

func TestOptionsNegativeValues(t *testing.T) {
	opts := Options{BatchSize: -1, MutationWorkers: -5}

	assert.Equal(t, DefaultBatchSize, opts.GetBatchSize(), "negative batch size should use default")
	assert.Equal(t, DefaultMutationWorkers, opts.GetMutationWorkers(), "negative workers should use default")
}

func TestOptionsExplicitValues(t *testing.T) {
	opts := Options{BatchSize: 5000, MutationWorkers: 8}

	assert.Equal(t, 5000, opts.GetBatchSize())
	assert.Equal(t, 8, opts.GetMutationWorkers())
}

func TestWithBatchSizeOption(t *testing.T) {
	var opts Options
	WithBatchSize(10000)(&opts)
	assert.Equal(t, 10000, opts.BatchSize)
}

func TestWithMutationWorkersOption(t *testing.T) {
	var opts Options
	WithMutationWorkers(16)(&opts)
	assert.Equal(t, 16, opts.MutationWorkers)
}

func TestWithSchemaOption(t *testing.T) {
	var opts Options
	WithSchema("/path/to/schema.dgraph")(&opts)
	assert.Equal(t, "/path/to/schema.dgraph", opts.SchemaPath)
}

func TestWithOptionsFullOverride(t *testing.T) {
	var opts Options

	WithOptions(Options{
		SchemaPath:      "/schema.dgraph",
		BatchSize:       5000,
		MutationWorkers: 4,
	})(&opts)

	assert.Equal(t, "/schema.dgraph", opts.SchemaPath)
	assert.Equal(t, 5000, opts.BatchSize)
	assert.Equal(t, 4, opts.MutationWorkers)
}

func TestWithOptionsZeroFieldsIgnored(t *testing.T) {
	opts := Options{
		SchemaPath:      "/existing.dgraph",
		BatchSize:       2000,
		MutationWorkers: 8,
	}

	WithOptions(Options{})(&opts)

	assert.Equal(t, "/existing.dgraph", opts.SchemaPath)
	assert.Equal(t, 2000, opts.BatchSize)
	assert.Equal(t, 8, opts.MutationWorkers)
}

func TestWithOptionsPartialOverride(t *testing.T) {
	opts := Options{
		SchemaPath:      "/old.dgraph",
		BatchSize:       2000,
		MutationWorkers: 8,
	}

	WithOptions(Options{BatchSize: 10000})(&opts)

	assert.Equal(t, "/old.dgraph", opts.SchemaPath, "SchemaPath should be preserved")
	assert.Equal(t, 10000, opts.BatchSize, "BatchSize should be overridden")
	assert.Equal(t, 8, opts.MutationWorkers, "MutationWorkers should be preserved")
}

func TestOptionFuncsCompose(t *testing.T) {
	var opts Options

	fns := []Option{
		WithBatchSize(1000),
		WithMutationWorkers(4),
		WithSchema("/a.dgraph"),
		WithBatchSize(5000),
	}
	for _, fn := range fns {
		fn(&opts)
	}

	assert.Equal(t, "/a.dgraph", opts.SchemaPath)
	assert.Equal(t, 5000, opts.BatchSize)
	assert.Equal(t, 4, opts.MutationWorkers)
}

// FileMatch tests

func TestMatchFileNilMatchesAll(t *testing.T) {
	var opts Options
	assert.True(t, opts.MatchFile("anything.txt"))
	assert.True(t, opts.MatchFile("data.rdf"))
	assert.True(t, opts.MatchFile(""))
}

func TestNewExtensionMatch(t *testing.T) {
	m := NewExtensionMatch(".csv", ".tsv")

	assert.True(t, m.Match("data.csv"))
	assert.True(t, m.Match("/dir/data.tsv"))
	assert.False(t, m.Match("data.rdf"))
}

func TestNewExtensionMatchNoExtensions(t *testing.T) {
	m := NewExtensionMatch()
	assert.False(t, m.Match("data.rdf"))
	assert.False(t, m.Match(""))
}

func TestNewExtensionMatchOverlappingSuffixes(t *testing.T) {
	m := NewExtensionMatch(".gz", ".rdf.gz")

	assert.True(t, m.Match("data.rdf.gz"), ".rdf.gz matches .gz")
	assert.True(t, m.Match("data.tar.gz"), ".tar.gz matches .gz")
	assert.False(t, m.Match("data.rdf"), "plain .rdf should not match")
}

func TestNewExtensionMatchEmptyPath(t *testing.T) {
	m := NewExtensionMatch(".rdf")
	assert.False(t, m.Match(""))
}

func TestNewExtensionMatchFullPathMatching(t *testing.T) {
	m := NewExtensionMatch(".rdf", ".rdf.gz")

	assert.True(t, m.Match("/var/data/import/users.rdf"))
	assert.True(t, m.Match("/var/data/import/users.rdf.gz"))
	assert.False(t, m.Match("/var/data/import/users.csv"))
	assert.False(t, m.Match("/var/data/import/schema.dgraph"))
}

func TestFileMatchFunc(t *testing.T) {
	var f FileMatch = FileMatchFunc(func(path string) bool {
		return path == "special.rdf"
	})

	assert.True(t, f.Match("special.rdf"))
	assert.False(t, f.Match("other.rdf"))
}

func TestWithFileMatch(t *testing.T) {
	var opts Options
	custom := NewExtensionMatch(".nq", ".nq.gz")
	WithFileMatch(custom)(&opts)

	assert.NotNil(t, opts.FileMatch)
	assert.True(t, opts.MatchFile("data.nq"))
	assert.False(t, opts.MatchFile("data.csv"))
}

func TestWithOptionsIncludesFileMatch(t *testing.T) {
	custom := NewExtensionMatch(".nq")
	var opts Options

	WithOptions(Options{FileMatch: custom})(&opts)
	assert.NotNil(t, opts.FileMatch)
	assert.True(t, opts.MatchFile("x.nq"))
}

func TestWithOptionsNilFileMatchPreservesExisting(t *testing.T) {
	custom := NewExtensionMatch(".nq")
	opts := Options{FileMatch: custom}

	WithOptions(Options{})(&opts)
	assert.NotNil(t, opts.FileMatch)
	assert.True(t, opts.MatchFile("x.nq"))
}

// FilterFiles method tests

func TestFilterFilesNilMatchReturnsAll(t *testing.T) {
	var opts Options
	input := []string{"a.rdf", "b.json", "c.txt", "d.csv"}
	assert.Equal(t, input, opts.FilterFiles(input))
}

func TestFilterFilesWithMatch(t *testing.T) {
	opts := Options{
		FileMatch: NewExtensionMatch(".rdf", ".json"),
	}
	input := []string{"a.rdf", "b.json", "c.txt", "d.csv", "e.rdf.gz"}
	assert.Equal(t, []string{"a.rdf", "b.json"}, opts.FilterFiles(input))
}

func TestFilterFilesEmptyInput(t *testing.T) {
	opts := Options{
		FileMatch: NewExtensionMatch(".rdf"),
	}
	assert.Nil(t, opts.FilterFiles(nil))
	assert.Nil(t, opts.FilterFiles([]string{}))
}

func TestFilterFilesNoMatches(t *testing.T) {
	opts := Options{
		FileMatch: NewExtensionMatch(".nq"),
	}
	input := []string{"a.rdf", "b.json"}
	assert.Nil(t, opts.FilterFiles(input))
}

func TestFilterFilesPreservesOrder(t *testing.T) {
	opts := Options{
		FileMatch: NewExtensionMatch(".rdf"),
	}
	input := []string{"c.rdf", "a.txt", "b.rdf", "d.csv", "a.rdf"}
	assert.Equal(t, []string{"c.rdf", "b.rdf", "a.rdf"}, opts.FilterFiles(input),
		"filtered files should preserve original order")
}

func TestFilterFilesDoesNotMutateInput(t *testing.T) {
	opts := Options{
		FileMatch: NewExtensionMatch(".rdf"),
	}
	input := []string{"a.rdf", "b.txt", "c.rdf"}
	inputCopy := make([]string, len(input))
	copy(inputCopy, input)

	opts.FilterFiles(input)
	assert.Equal(t, inputCopy, input, "FilterFiles should not mutate the input slice")
}

// Pipeline tests — FilterFiles then SortFiles

func TestFilterThenSort(t *testing.T) {
	opts := Options{
		FileMatch: NewExtensionMatch(".rdf", ".rdf.gz"),
		SortFiles: FileSort(func(files []string) []string {
			// Reverse sort
			out := make([]string, len(files))
			for i, f := range files {
				out[len(files)-1-i] = f
			}
			return out
		}),
	}

	input := []string{"z.rdf", "a.csv", "m.rdf.gz", "b.rdf", "x.json"}

	// Step 1: filter
	filtered := opts.FilterFiles(input)
	assert.Equal(t, []string{"z.rdf", "m.rdf.gz", "b.rdf"}, filtered)

	// Step 2: sort
	sorted := opts.SortFiles(filtered)
	assert.Equal(t, []string{"b.rdf", "m.rdf.gz", "z.rdf"}, sorted)
}

func TestFilterWithoutSortLeavesOrder(t *testing.T) {
	opts := Options{
		FileMatch: NewExtensionMatch(".rdf"),
		// SortFiles intentionally nil
	}

	input := []string{"c.rdf", "a.rdf", "b.rdf"}
	filtered := opts.FilterFiles(input)
	assert.Equal(t, []string{"c.rdf", "a.rdf", "b.rdf"}, filtered,
		"without SortFiles, original order is preserved")
}

func TestSortWithoutFilterUsesAllFiles(t *testing.T) {
	opts := Options{
		// FileMatch intentionally nil — all files match
		SortFiles: FileSort(func(files []string) []string {
			// Reverse
			out := make([]string, len(files))
			for i, f := range files {
				out[len(files)-1-i] = f
			}
			return out
		}),
	}

	input := []string{"a.rdf", "b.json", "c.txt"}
	filtered := opts.FilterFiles(input)
	assert.Equal(t, input, filtered, "nil FileMatch returns all files")

	sorted := opts.SortFiles(filtered)
	assert.Equal(t, []string{"c.txt", "b.json", "a.rdf"}, sorted)
}

// SortFiles tests

func TestWithFileSort(t *testing.T) {
	var opts Options

	reverse := FileSort(func(files []string) []string {
		out := make([]string, len(files))
		for i, f := range files {
			out[len(files)-1-i] = f
		}
		return out
	})
	WithFileSort(reverse)(&opts)

	assert.NotNil(t, opts.SortFiles)
	result := opts.SortFiles([]string{"a.rdf", "b.rdf", "c.rdf"})
	assert.Equal(t, []string{"c.rdf", "b.rdf", "a.rdf"}, result)
}

func TestSortFilesNilByDefault(t *testing.T) {
	var opts Options
	assert.Nil(t, opts.SortFiles)
}

func TestWithOptionsIncludesSortFiles(t *testing.T) {
	identity := FileSort(func(files []string) []string { return files })
	var opts Options

	WithOptions(Options{SortFiles: identity})(&opts)
	assert.NotNil(t, opts.SortFiles)
}

func TestWithOptionsNilSortFilesPreservesExisting(t *testing.T) {
	identity := FileSort(func(files []string) []string { return files })
	opts := Options{SortFiles: identity}

	WithOptions(Options{})(&opts)
	assert.NotNil(t, opts.SortFiles)
}
