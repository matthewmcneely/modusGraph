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

func TestQuery_Filter(t *testing.T) {
	ctx := context.Background()
	c := typed.NewClient[widget](newConn(t))

	// Insert three widgets with distinct names.
	for _, name := range []string{"alpha", "beta", "gamma"} {
		if err := c.Add(ctx, &widget{Name: name}); err != nil {
			t.Fatalf("Add %s: %v", name, err)
		}
	}

	// Filter to exactly those whose name equals "beta" (index=exact allows eq()).
	got, err := c.Query(ctx).Filter(`eq(name, "beta")`).Nodes()
	if err != nil {
		t.Fatalf("Filter Nodes: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("Filter returned %d records, want 1", len(got))
	}
	if got[0].Name != "beta" {
		t.Fatalf("Filter returned Name=%q, want beta", got[0].Name)
	}
}

func TestQuery_OrderAscDesc(t *testing.T) {
	ctx := context.Background()
	c := typed.NewClient[widget](newConn(t))

	// Insert widgets with distinct Qty values in non-sorted order so a
	// stable natural ordering cannot hide a missing sort.
	qtys := []int{30, 10, 50, 20, 40}
	for i, q := range qtys {
		if err := c.Add(ctx, &widget{Name: "w", Qty: q}); err != nil {
			t.Fatalf("Add widget[%d]: %v", i, err)
		}
	}

	// Ascending.
	asc, err := c.Query(ctx).OrderAsc("qty").Nodes()
	if err != nil {
		t.Fatalf("OrderAsc Nodes: %v", err)
	}
	if len(asc) != len(qtys) {
		t.Fatalf("OrderAsc returned %d records, want %d", len(asc), len(qtys))
	}
	for i := range len(asc) - 1 {
		if asc[i].Qty > asc[i+1].Qty {
			t.Fatalf("OrderAsc: asc[%d].Qty=%d > asc[%d].Qty=%d; not ascending",
				i, asc[i].Qty, i+1, asc[i+1].Qty)
		}
	}

	// Descending.
	desc, err := c.Query(ctx).OrderDesc("qty").Nodes()
	if err != nil {
		t.Fatalf("OrderDesc Nodes: %v", err)
	}
	if len(desc) != len(qtys) {
		t.Fatalf("OrderDesc returned %d records, want %d", len(desc), len(qtys))
	}
	for i := range len(desc) - 1 {
		if desc[i].Qty < desc[i+1].Qty {
			t.Fatalf("OrderDesc: desc[%d].Qty=%d < desc[%d].Qty=%d; not descending",
				i, desc[i].Qty, i+1, desc[i+1].Qty)
		}
	}
}
