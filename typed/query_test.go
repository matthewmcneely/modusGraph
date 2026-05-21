/*
 * SPDX-FileCopyrightText: © 2017-2026 Istari Digital, Inc.
 * SPDX-License-Identifier: Apache-2.0
 */

package typed_test

import (
	"context"
	"testing"

	"github.com/matthewmcneely/modusgraph/typed"
)

func TestQuery_NodesReturnsAll(t *testing.T) {
	ctx := context.Background()
	c := typed.NewClient[widget](newConn(t))
	for _, n := range []string{"a", "b", "c"} {
		if err := c.Add(ctx, &widget{Name: n}); err != nil {
			t.Fatalf("Add %s: %v", n, err)
		}
	}

	got, err := c.Query(ctx).Nodes()
	if err != nil {
		t.Fatalf("Nodes: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("Nodes returned %d records, want 3", len(got))
	}
}

func TestQuery_LimitCapsResults(t *testing.T) {
	ctx := context.Background()
	c := typed.NewClient[widget](newConn(t))
	for i := range 5 {
		if err := c.Add(ctx, &widget{Name: "w", Qty: i}); err != nil {
			t.Fatalf("Add %d: %v", i, err)
		}
	}

	got, err := c.Query(ctx).Limit(2).Nodes()
	if err != nil {
		t.Fatalf("Nodes: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("Limit(2) returned %d records, want 2", len(got))
	}
}

func TestQuery_FirstReturnsAMatch(t *testing.T) {
	ctx := context.Background()
	c := typed.NewClient[widget](newConn(t))
	if err := c.Add(ctx, &widget{Name: "only", Qty: 7}); err != nil {
		t.Fatalf("Add: %v", err)
	}

	got, err := c.Query(ctx).First()
	if err != nil {
		t.Fatalf("First: %v", err)
	}
	if got == nil || got.Name != "only" {
		t.Fatalf("First returned %+v, want Name=only", got)
	}
}

func TestQuery_FirstNoMatchReturnsNilNil(t *testing.T) {
	ctx := context.Background()
	c := typed.NewClient[widget](newConn(t))

	got, err := c.Query(ctx).First()
	if err != nil {
		t.Fatalf("First on empty: unexpected error %v", err)
	}
	if got != nil {
		t.Fatalf("First on empty returned %+v, want nil", got)
	}
}

func TestQuery_BuilderChainCompilesAndRuns(t *testing.T) {
	ctx := context.Background()
	c := typed.NewClient[widget](newConn(t))
	if err := c.Add(ctx, &widget{Name: "x", Qty: 1}); err != nil {
		t.Fatalf("Add: %v", err)
	}

	// Every builder method must return *Query[widget] so the chain stays typed.
	_, err := c.Query(ctx).
		OrderAsc("qty").
		Offset(0).
		Limit(10).
		Cascade().
		Nodes()
	if err != nil {
		t.Fatalf("builder chain Nodes: %v", err)
	}
}

func TestQuery_RawExposesUnderlyingBuilder(t *testing.T) {
	c := typed.NewClient[widget](newConn(t))
	if c.Query(context.Background()).Raw() == nil {
		t.Fatal("Raw() returned nil; expected the underlying *dg.Query")
	}
}
