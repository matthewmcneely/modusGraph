/*
 * SPDX-FileCopyrightText: © 2017-2026 Istari Digital, Inc.
 * SPDX-License-Identifier: Apache-2.0
 */

package typed

import (
	"context"
	"strings"
	"testing"

	dg "github.com/dolan-in/dgman/v2"
	"github.com/matthewmcneely/modusgraph"
)

type ivPet struct {
	UID   string   `json:"uid,omitempty"`
	DType []string `json:"dgraph.type,omitempty"`
	Name  string   `json:"name,omitempty" dgraph:"index=exact"`
}

type ivOwner struct {
	UID   string   `json:"uid,omitempty"`
	DType []string `json:"dgraph.type,omitempty"`
	Name  string   `json:"name,omitempty" dgraph:"index=exact"`
	Pets  []*ivPet `json:"pets,omitempty"`
}

// TestEdgeBlocksRenderServerSideVar asserts a WhereEdge query renders as a
// server-side var block consumed via uid(mgMatched). The matched roots are
// bound on the server and never inlined into a uid(<literal>, ...) list, which
// is what keeps WhereEdge bounded regardless of how many roots match — the
// concern that motivated replacing the eager client-side pre-pass.
func TestEdgeBlocksRenderServerSideVar(t *testing.T) {
	conn, err := modusgraph.NewClient("file://"+t.TempDir(), modusgraph.WithAutoSchema(true))
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	t.Cleanup(conn.Close)

	qb := NewClient[ivOwner](conn).Query(context.Background()).
		Filter(`eq(name, "Alice")`).
		WhereEdge("pets", `eq(name, "Fido")`)

	dql := dg.NewQueryBlock(qb.edgeBlocks(true)...).String()

	for _, want := range []string{
		"mgMatched as var(", // matched roots bound server-side
		"@cascade",          // edge constraint enforced by cascade
		"uid(mgMatched)",    // data and count blocks consume the var
		"count(uid)",        // count block present
	} {
		if !strings.Contains(dql, want) {
			t.Errorf("rendered DQL missing %q:\n%s", want, dql)
		}
	}
	// The owner UIDs must never be inlined into the query — that was the
	// unbounded behavior. A server-side var carries no uid(0x...) literal list.
	if strings.Contains(dql, "uid(0x") {
		t.Errorf("rendered DQL inlines UID literals (unbounded):\n%s", dql)
	}
}
