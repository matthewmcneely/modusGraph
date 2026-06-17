/*
 * SPDX-FileCopyrightText: © 2017-2026 Istari Digital, Inc.
 * SPDX-License-Identifier: Apache-2.0
 */

package modusgraph_test

import (
	"context"
	"testing"

	mg "github.com/matthewmcneely/modusgraph"
	"github.com/stretchr/testify/require"
)

// studioRecord is a schema-defining record (implements mg.Schema). studioWrapper
// wraps it and exposes Unwrap, exactly as a modusgraph-gen wrapper would.
type studioRecord struct {
	UID   string   `json:"uid,omitempty"`
	DType []string `json:"dgraph.type,omitempty"`
	Name  string   `json:"name,omitempty" dgraph:"index=exact"`
}

func (s *studioRecord) SchemaTypeName() string { return "studioRecord" }

type studioWrapper struct{ inner *studioRecord }

func (w *studioWrapper) Unwrap() *studioRecord { return w.inner }

// TestClientUnwrapsWrapperThroughRealMutation exercises the real client path,
// not UnwrapSchema in isolation: it inserts a wrapper and reads it back. If a
// mutation method stopped calling UnwrapSchema, the wrapper (which has no usable
// dgraph fields of its own) would not persist Name and the inner UID would stay
// empty — so this test fails on that regression.
func TestClientUnwrapsWrapperThroughRealMutation(t *testing.T) {
	client, err := mg.NewClient("file://"+GetTempDir(t), mg.WithAutoSchema(true))
	require.NoError(t, err)
	defer client.Close()

	ctx := context.Background()
	inner := &studioRecord{Name: "Acme"}
	wrapper := &studioWrapper{inner: inner}

	require.NoError(t, client.Insert(ctx, wrapper))
	require.NotEmpty(t, inner.UID,
		"Insert did not route the wrapper to its inner record")

	var got studioRecord
	require.NoError(t, client.Get(ctx, &got, inner.UID))
	require.Equal(t, "Acme", got.Name)
}
