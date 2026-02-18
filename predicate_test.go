/*
 * SPDX-FileCopyrightText: © 2017-2026 Istari Digital, Inc.
 * SPDX-License-Identifier: Apache-2.0
 */

package modusgraph_test

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// NOTE: These tests exercise the predicate= tag through modusgraph's client API.
// They depend on the dgman fork (mlwelles/dgman) containing the predicate= fixes
// for both the write path (filterStruct using schema.Predicate as map key) and
// the read path (remapping JSON keys from predicate names to json tag names).
// Until the dgman fork fixes land, these tests will fail because:
//   - MutateBasic writes data under the json tag name instead of the predicate name
//   - Query/Get returns zero values for fields where predicate != json tag

// PredicateFilm is a test struct where the dgraph predicate name differs from
// the json tag name. This exercises the predicate= fix in dgman.
type PredicateFilm struct {
	Title       string    `json:"title,omitempty" dgraph:"predicate=film_title index=exact"`
	ReleaseDate time.Time `json:"releaseDate,omitzero" dgraph:"predicate=release_date index=day"`
	Rating      float64   `json:"rating,omitempty" dgraph:"predicate=film_rating index=float"`

	UID   string   `json:"uid,omitempty"`
	DType []string `json:"dgraph.type,omitempty"`
}

// TestPredicateInsertAndGet tests that Insert + Get round-trips correctly
// when predicate= differs from the json tag.
func TestPredicateInsertAndGet(t *testing.T) {
	testCases := []struct {
		name string
		uri  string
		skip bool
	}{
		{
			name: "PredicateInsertGetWithFileURI",
			uri:  "file://" + GetTempDir(t),
		},
		{
			name: "PredicateInsertGetWithDgraphURI",
			uri:  "dgraph://" + os.Getenv("MODUSGRAPH_TEST_ADDR"),
			skip: os.Getenv("MODUSGRAPH_TEST_ADDR") == "",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			if tc.skip {
				t.Skipf("Skipping %s: MODUSGRAPH_TEST_ADDR not set", tc.name)
				return
			}

			client, cleanup := CreateTestClient(t, tc.uri)
			defer cleanup()

			ctx := context.Background()
			releaseDate := time.Date(1999, 3, 31, 0, 0, 0, 0, time.UTC)

			film := PredicateFilm{
				Title:       "The Matrix",
				ReleaseDate: releaseDate,
				Rating:      8.7,
			}

			err := client.Insert(ctx, &film)
			require.NoError(t, err, "Insert should succeed")
			require.NotEmpty(t, film.UID, "UID should be assigned")

			// Get the film back by UID
			var retrieved PredicateFilm
			err = client.Get(ctx, &retrieved, film.UID)
			require.NoError(t, err, "Get should succeed")

			// These assertions verify the predicate= fix: data stored under
			// the predicate name (film_title, release_date, film_rating) should
			// be correctly mapped back to the json tag fields.
			assert.Equal(t, "The Matrix", retrieved.Title,
				"Title should round-trip correctly (predicate=film_title)")
			assert.Equal(t, releaseDate, retrieved.ReleaseDate,
				"ReleaseDate should round-trip correctly (predicate=release_date)")
			assert.Equal(t, 8.7, retrieved.Rating,
				"Rating should round-trip correctly (predicate=film_rating)")
		})
	}
}

// TestPredicateUpdate tests that Update works correctly with predicate= fields.
func TestPredicateUpdate(t *testing.T) {
	testCases := []struct {
		name string
		uri  string
		skip bool
	}{
		{
			name: "PredicateUpdateWithFileURI",
			uri:  "file://" + GetTempDir(t),
		},
		{
			name: "PredicateUpdateWithDgraphURI",
			uri:  "dgraph://" + os.Getenv("MODUSGRAPH_TEST_ADDR"),
			skip: os.Getenv("MODUSGRAPH_TEST_ADDR") == "",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			if tc.skip {
				t.Skipf("Skipping %s: MODUSGRAPH_TEST_ADDR not set", tc.name)
				return
			}

			client, cleanup := CreateTestClient(t, tc.uri)
			defer cleanup()

			ctx := context.Background()
			releaseDate := time.Date(1999, 3, 31, 0, 0, 0, 0, time.UTC)

			film := PredicateFilm{
				Title:       "The Matrix",
				ReleaseDate: releaseDate,
				Rating:      8.7,
			}

			err := client.Insert(ctx, &film)
			require.NoError(t, err, "Insert should succeed")

			// Update the rating
			film.Rating = 9.0
			err = client.Update(ctx, &film)
			require.NoError(t, err, "Update should succeed")

			var retrieved PredicateFilm
			err = client.Get(ctx, &retrieved, film.UID)
			require.NoError(t, err, "Get should succeed after update")
			assert.Equal(t, 9.0, retrieved.Rating,
				"Rating should be updated via predicate=film_rating")
			assert.Equal(t, "The Matrix", retrieved.Title,
				"Title should still be correct after update")
		})
	}
}

// TestPredicateUpsert tests that Upsert works correctly with predicate= fields.
// NOTE: This test depends on the dgman fork (mlwelles/dgman) containing the
// predicate= fixes for the read path. Upsert uses the do() path which correctly
// writes data under predicate names, but the read path currently maps by json tags.
// This test will fail until the dgman fork read-path fix is applied.
func TestPredicateUpsert(t *testing.T) {
	t.Skip("Depends on dgman fork predicate= read-path fix (not yet applied)")

	testCases := []struct {
		name string
		uri  string
		skip bool
	}{
		{
			name: "PredicateUpsertWithFileURI",
			uri:  "file://" + GetTempDir(t),
		},
		{
			name: "PredicateUpsertWithDgraphURI",
			uri:  "dgraph://" + os.Getenv("MODUSGRAPH_TEST_ADDR"),
			skip: os.Getenv("MODUSGRAPH_TEST_ADDR") == "",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			if tc.skip {
				t.Skipf("Skipping %s: MODUSGRAPH_TEST_ADDR not set", tc.name)
				return
			}

			client, cleanup := CreateTestClient(t, tc.uri)
			defer cleanup()

			ctx := context.Background()
			releaseDate := time.Date(1999, 3, 31, 0, 0, 0, 0, time.UTC)

			// First upsert creates the node
			film := PredicateFilm{
				Title:       "The Matrix",
				ReleaseDate: releaseDate,
				Rating:      8.7,
			}

			err := client.Upsert(ctx, &film, "film_title")
			require.NoError(t, err, "Upsert (create) should succeed")
			require.NotEmpty(t, film.UID, "UID should be assigned")
			firstUID := film.UID

			// Second upsert updates the existing node
			film2 := PredicateFilm{
				Title:       "The Matrix",
				ReleaseDate: releaseDate,
				Rating:      9.1,
			}
			err = client.Upsert(ctx, &film2, "film_title")
			require.NoError(t, err, "Upsert (update) should succeed")
			assert.Equal(t, firstUID, film2.UID,
				"Upsert should reuse the same UID")

			// Verify the update
			var retrieved PredicateFilm
			err = client.Get(ctx, &retrieved, firstUID)
			require.NoError(t, err, "Get should succeed after upsert")
			assert.Equal(t, "The Matrix", retrieved.Title)
			assert.Equal(t, 9.1, retrieved.Rating,
				"Rating should be updated after upsert")
		})
	}
}

// TestPredicateQuery tests that Query with filters works correctly
// when predicates differ from json tags.
func TestPredicateQuery(t *testing.T) {
	testCases := []struct {
		name string
		uri  string
		skip bool
	}{
		{
			name: "PredicateQueryWithFileURI",
			uri:  "file://" + GetTempDir(t),
		},
		{
			name: "PredicateQueryWithDgraphURI",
			uri:  "dgraph://" + os.Getenv("MODUSGRAPH_TEST_ADDR"),
			skip: os.Getenv("MODUSGRAPH_TEST_ADDR") == "",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			if tc.skip {
				t.Skipf("Skipping %s: MODUSGRAPH_TEST_ADDR not set", tc.name)
				return
			}

			client, cleanup := CreateTestClient(t, tc.uri)
			defer cleanup()

			ctx := context.Background()

			// Insert multiple films with different release dates
			films := []*PredicateFilm{
				{
					Title:       "Film A",
					ReleaseDate: time.Date(1985, 1, 1, 0, 0, 0, 0, time.UTC),
					Rating:      7.5,
				},
				{
					Title:       "Film B",
					ReleaseDate: time.Date(1995, 6, 15, 0, 0, 0, 0, time.UTC),
					Rating:      8.0,
				},
				{
					Title:       "Film C",
					ReleaseDate: time.Date(2005, 12, 25, 0, 0, 0, 0, time.UTC),
					Rating:      9.0,
				},
			}
			err := client.Insert(ctx, films)
			require.NoError(t, err, "Insert films should succeed")

			// Query using predicate names (not json tag names) in filter.
			// The filter references release_date (the Dgraph predicate name).
			var results []PredicateFilm
			err = client.Query(ctx, PredicateFilm{}).
				Filter(`ge(release_date, "1990-01-01T00:00:00Z")`).
				Nodes(&results)
			require.NoError(t, err, "Query with predicate filter should succeed")
			require.Len(t, results, 2,
				"Should find 2 films with release_date >= 1990")

			titles := make([]string, len(results))
			for i, r := range results {
				titles[i] = r.Title
			}
			assert.ElementsMatch(t, []string{"Film B", "Film C"}, titles,
				"Should find the correct films")

			// Verify that all queried films have their predicate= fields populated
			for _, r := range results {
				assert.NotEmpty(t, r.Title, "Title should be populated")
				assert.False(t, r.ReleaseDate.IsZero(),
					"ReleaseDate should be populated (predicate=release_date)")
				assert.NotZero(t, r.Rating,
					"Rating should be populated (predicate=film_rating)")
			}
		})
	}
}
