/*
 * SPDX-FileCopyrightText: © 2017-2026 Istari Digital, Inc.
 * SPDX-License-Identifier: Apache-2.0
 */

package typed_test

import (
	"context"
	"testing"

	"github.com/matthewmcneely/modusgraph"
	"github.com/matthewmcneely/modusgraph/typed"
)

// widget is a minimal schema struct used to exercise the typed package.
type widget struct {
	UID   string   `json:"uid,omitempty"`
	DType []string `json:"dgraph.type,omitempty"`
	Name  string   `json:"name,omitempty" dgraph:"index=exact"`
	Qty   int      `json:"qty,omitempty" dgraph:"index=int"`
}

// newConn builds a local file-backed modusgraph client for a test.
func newConn(t *testing.T) modusgraph.Client {
	t.Helper()
	conn, err := modusgraph.NewClient("file://"+t.TempDir(), modusgraph.WithAutoSchema(true))
	if err != nil {
		t.Fatalf("modusgraph.NewClient: %v", err)
	}
	t.Cleanup(conn.Close)
	return conn
}

func TestClient_AddPopulatesUIDAndGetReadsBack(t *testing.T) {
	ctx := context.Background()
	c := typed.NewClient[widget](newConn(t))

	w := &widget{Name: "sprocket", Qty: 3}
	if err := c.Add(ctx, w); err != nil {
		t.Fatalf("Add: %v", err)
	}
	if w.UID == "" {
		t.Fatal("Add did not populate UID on the passed struct")
	}

	got, err := c.Get(ctx, w.UID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Name != "sprocket" || got.Qty != 3 {
		t.Fatalf("Get returned %+v, want Name=sprocket Qty=3", got)
	}
}

func TestClient_Update(t *testing.T) {
	ctx := context.Background()
	c := typed.NewClient[widget](newConn(t))

	w := &widget{Name: "gear", Qty: 1}
	if err := c.Add(ctx, w); err != nil {
		t.Fatalf("Add: %v", err)
	}
	w.Qty = 99
	if err := c.Update(ctx, w); err != nil {
		t.Fatalf("Update: %v", err)
	}

	got, err := c.Get(ctx, w.UID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Qty != 99 {
		t.Fatalf("Update did not persist; Qty = %d, want 99", got.Qty)
	}
}

func TestClient_Delete(t *testing.T) {
	ctx := context.Background()
	c := typed.NewClient[widget](newConn(t))

	w := &widget{Name: "bolt"}
	if err := c.Add(ctx, w); err != nil {
		t.Fatalf("Add: %v", err)
	}
	if err := c.Delete(ctx, w.UID); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := c.Get(ctx, w.UID); err == nil {
		t.Fatal("Get after Delete returned no error; expected not-found")
	}
}
