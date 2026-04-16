/*
 * SPDX-FileCopyrightText: © 2017-2026 Istari Digital, Inc.
 * SPDX-License-Identifier: Apache-2.0
 */

package modusgraph_test

import (
	"context"
	"fmt"
	"os"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/matthewmcneely/modusgraph"
	"github.com/stretchr/testify/require"
)

// RetryEntity is a test struct with a unique index to provoke transaction conflicts.
type RetryEntity struct {
	UID   string   `json:"uid,omitempty"`
	DType []string `json:"dgraph.type,omitempty"`
	Name  string   `json:"name,omitempty" dgraph:"index=term,exact upsert"`
	Value int      `json:"value,omitempty"`
}

func TestConcurrentInsertsWithRetry(t *testing.T) {
	testCases := []struct {
		name string
		uri  string
		skip bool
	}{
		{
			name: "FileURI",
			uri:  "file://" + GetTempDir(t),
		},
		{
			name: "DgraphURI",
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
			const numWorkers = 8
			const entitiesPerWorker = 10

			var succeeded atomic.Int64
			var wg sync.WaitGroup

			for w := range numWorkers {
				wg.Add(1)
				go func() {
					defer wg.Done()
					for i := range entitiesPerWorker {
						entity := &RetryEntity{
							Name:  fmt.Sprintf("entity-%d-%d", w, i),
							Value: w*entitiesPerWorker + i,
						}
						err := client.Insert(ctx, entity)
						if err != nil {
							t.Errorf("worker %d entity %d: %v", w, i, err)
							return
						}
						succeeded.Add(1)
					}
				}()
			}
			wg.Wait()

			total := int64(numWorkers * entitiesPerWorker)
			require.Equal(t, total, succeeded.Load(),
				"all concurrent inserts should succeed with retry")
		})
	}
}

func TestMaxRetriesZeroDisablesRetry(t *testing.T) {
	uri := "file://" + GetTempDir(t)
	client, err := modusgraph.NewClient(uri,
		modusgraph.WithAutoSchema(true),
		modusgraph.WithMaxRetries(0),
	)
	require.NoError(t, err)
	defer func() {
		client.DropAll(context.Background())
		client.Close()
		modusgraph.Shutdown()
	}()

	ctx := context.Background()
	entity := &RetryEntity{Name: "no-retry-test", Value: 1}
	err = client.Insert(ctx, entity)
	require.NoError(t, err, "single insert should succeed even with retries disabled")
}
