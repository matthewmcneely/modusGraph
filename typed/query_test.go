/*
 * SPDX-FileCopyrightText: © 2017-2026 Istari Digital, Inc.
 * SPDX-License-Identifier: Apache-2.0
 */

package typed_test

import (
	"context"
	"strings"
	"testing"

	dg "github.com/dolan-in/dgman/v2"
	"github.com/go-logr/logr/funcr"
	"github.com/matthewmcneely/modusgraph"
	"github.com/matthewmcneely/modusgraph/typed"
)

// newCountingConn builds a file-backed modusgraph client exactly like newConn,
// but wires in a logr.Logger that counts dgman query executions. dgman logs
// every executed query at verbosity 3 with the message "execute query"; the
// returned *int is incremented once per such log line.
//
// dgman's logger is process-global, and modusgraph allows only one live
// file-backed engine per process (see modusgraph.ErrSingletonOnly). Each call
// uses a fresh t.TempDir() URI for data isolation. Tests that use
// newCountingConn must NOT call t.Parallel(): a second live client would hit
// the engine singleton, and parallel tests would also corrupt the shared
// query count.
func newCountingConn(t *testing.T, count *int) modusgraph.Client {
	t.Helper()
	logger := funcr.New(func(_, args string) {
		// funcr renders the message into args as `"msg"="execute query"`.
		// Match that exact pair so unrelated dgman/pool log lines (which log
		// other messages, e.g. "executeQuery" for query blocks) are ignored.
		if strings.Contains(args, `"msg"="execute query"`) {
			*count++
		}
	}, funcr.Options{Verbosity: 3})
	conn, err := modusgraph.NewClient("file://"+t.TempDir(),
		modusgraph.WithAutoSchema(true), modusgraph.WithLogger(logger))
	if err != nil {
		t.Fatalf("modusgraph.NewClient: %v", err)
	}
	t.Cleanup(conn.Close)
	return conn
}

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

func TestQuery_OffsetSkipsResults(t *testing.T) {
	ctx := context.Background()
	c := typed.NewClient[widget](newConn(t))

	// Five widgets with distinct, deliberately unsorted Qty values.
	qtys := []int{40, 10, 50, 20, 30}
	for i, q := range qtys {
		if err := c.Add(ctx, &widget{Name: "w", Qty: q}); err != nil {
			t.Fatalf("Add widget[%d]: %v", i, err)
		}
	}

	// Ordering ascending by qty gives 10,20,30,40,50; Offset(2) drops the
	// first two, so 3 rows remain and the first is the 3rd-smallest (30).
	got, err := c.Query(ctx).OrderAsc("qty").Offset(2).Nodes()
	if err != nil {
		t.Fatalf("Offset Nodes: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("OrderAsc.Offset(2) returned %d records, want 3", len(got))
	}
	if got[0].Qty != 30 {
		t.Fatalf("first row after Offset(2) has Qty=%d, want 30 (3rd-smallest)", got[0].Qty)
	}
}

func TestQuery_AfterCursor(t *testing.T) {
	ctx := context.Background()
	c := typed.NewClient[widget](newConn(t))

	for i := range 5 {
		if err := c.Add(ctx, &widget{Name: "w", Qty: i}); err != nil {
			t.Fatalf("Add %d: %v", i, err)
		}
	}

	// First pass: grab all rows so we can pick a non-last cursor UID.
	all, err := c.Query(ctx).Nodes()
	if err != nil {
		t.Fatalf("first Nodes: %v", err)
	}
	if len(all) < 3 {
		t.Fatalf("expected at least 3 widgets, got %d", len(all))
	}
	cursor := all[1].UID // a non-last row

	// After(cursor) uses default UID ordering to skip past the cursor node.
	got, err := c.Query(ctx).After(cursor).Nodes()
	if err != nil {
		t.Fatalf("After Nodes: %v", err)
	}
	if len(got) == 0 {
		t.Fatal("After(cursor) returned no rows; expected the rows past the cursor")
	}
	for _, w := range got {
		if w.UID <= cursor {
			t.Fatalf("After(%s) returned UID %s, which is not strictly greater than the cursor",
				cursor, w.UID)
		}
	}
}

func TestQuery_CascadeDropsIncompleteNodes(t *testing.T) {
	ctx := context.Background()
	c := typed.NewClient[widget](newConn(t))

	// Widgets with Qty > 0 carry a qty predicate. Widgets with Qty left 0
	// have it omitted entirely (json tag is omitempty), so they have no qty
	// predicate at all.
	withQty := []int{5, 9, 13}
	for _, q := range withQty {
		if err := c.Add(ctx, &widget{Name: "has-qty", Qty: q}); err != nil {
			t.Fatalf("Add qty=%d: %v", q, err)
		}
	}
	for i := range 4 {
		if err := c.Add(ctx, &widget{Name: "no-qty"}); err != nil {
			t.Fatalf("Add no-qty[%d]: %v", i, err)
		}
	}

	// @cascade(qty) drops any node that lacks the qty predicate.
	got, err := c.Query(ctx).Cascade("qty").Nodes()
	if err != nil {
		t.Fatalf("Cascade Nodes: %v", err)
	}
	if len(got) != len(withQty) {
		t.Fatalf("Cascade(qty) returned %d records, want %d (only the qty-bearing widgets)",
			len(got), len(withQty))
	}
	for _, w := range got {
		if w.Qty == 0 {
			t.Fatalf("Cascade(qty) returned a widget with Qty=0 (no qty predicate): %+v", w)
		}
	}
}

func TestQuery_FilterOrderLimitOffsetCombined(t *testing.T) {
	ctx := context.Background()
	c := typed.NewClient[widget](newConn(t))

	// A known set: five "keep" widgets plus a "drop" widget the filter excludes.
	for _, q := range []int{50, 20, 40, 10, 30} {
		if err := c.Add(ctx, &widget{Name: "keep", Qty: q}); err != nil {
			t.Fatalf("Add keep qty=%d: %v", q, err)
		}
	}
	if err := c.Add(ctx, &widget{Name: "drop", Qty: 99}); err != nil {
		t.Fatalf("Add drop: %v", err)
	}

	// Filter to name=keep -> qtys {10,20,30,40,50}; OrderAsc -> sorted;
	// Offset(1) drops 10; Limit(2) keeps {20,30}.
	got, err := c.Query(ctx).
		Filter(`eq(name, "keep")`).
		OrderAsc("qty").
		Offset(1).
		Limit(2).
		Nodes()
	if err != nil {
		t.Fatalf("combined chain Nodes: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("combined chain returned %d records, want 2", len(got))
	}
	if got[0].Qty != 20 || got[1].Qty != 30 {
		t.Fatalf("combined chain window = [%d, %d], want [20, 30]", got[0].Qty, got[1].Qty)
	}
}

func TestQuery_FirstOnMultipleRows(t *testing.T) {
	ctx := context.Background()
	c := typed.NewClient[widget](newConn(t))

	for _, q := range []int{30, 10, 20} {
		if err := c.Add(ctx, &widget{Name: "w", Qty: q}); err != nil {
			t.Fatalf("Add qty=%d: %v", q, err)
		}
	}

	// First on an ascending-by-qty query yields exactly the smallest row.
	got, err := c.Query(ctx).OrderAsc("qty").First()
	if err != nil {
		t.Fatalf("First: %v", err)
	}
	if got == nil {
		t.Fatal("First returned nil on a non-empty result set")
	}
	if got.Qty != 10 {
		t.Fatalf("First on OrderAsc(qty) returned Qty=%d, want 10 (smallest)", got.Qty)
	}
}

func TestQuery_NodesEmptyResult(t *testing.T) {
	ctx := context.Background()
	c := typed.NewClient[widget](newConn(t)) // fresh client, no inserts

	got, err := c.Query(ctx).Nodes()
	if err != nil {
		t.Fatalf("Nodes on empty client: unexpected error %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("Nodes on empty client returned %d records, want 0", len(got))
	}
}

func TestQuery_OrderAccumulates(t *testing.T) {
	ctx := context.Background()
	c := typed.NewClient[widget](newConn(t))

	// OrderAsc and OrderDesc accumulate: both clauses must survive on the
	// same query. dgman renders them as "orderasc:"/"orderdesc:" in the
	// generated query string.
	q := c.Query(ctx).OrderAsc("name").OrderDesc("qty")
	s := q.Raw().String()
	if !strings.Contains(s, "orderasc: name") {
		t.Fatalf("query string missing ascending name order; got:\n%s", s)
	}
	if !strings.Contains(s, "orderdesc: qty") {
		t.Fatalf("query string missing descending qty order; got:\n%s", s)
	}
}

func TestQuery_CascadeOverwrites(t *testing.T) {
	ctx := context.Background()
	c := typed.NewClient[widget](newConn(t))

	// Cascade overwrites: the second call wins, the first predicate is gone.
	// dgman renders predicates as @cascade(pred1,pred2,...) with no spaces.
	q := c.Query(ctx).Cascade("name").Cascade("qty")
	s := q.Raw().String()
	if !strings.Contains(s, "@cascade(qty)") {
		t.Fatalf("second Cascade(qty) not rendered in query string; got:\n%s", s)
	}
	if strings.Contains(s, "@cascade(name)") {
		t.Fatalf("first Cascade(name) still present after overwrite; got:\n%s", s)
	}
}

func TestQuery_TerminalRunsTwice(t *testing.T) {
	ctx := context.Background()
	c := typed.NewClient[widget](newConn(t))

	for _, n := range []string{"a", "b", "c"} {
		if err := c.Add(ctx, &widget{Name: n}); err != nil {
			t.Fatalf("Add %s: %v", n, err)
		}
	}

	// A terminal is re-runnable: calling Nodes twice on the same builder
	// succeeds both times and yields equal-length results.
	q := c.Query(ctx)
	first, err := q.Nodes()
	if err != nil {
		t.Fatalf("first Nodes: %v", err)
	}
	second, err := q.Nodes()
	if err != nil {
		t.Fatalf("second Nodes: %v", err)
	}
	if len(first) != len(second) {
		t.Fatalf("Nodes run twice returned %d then %d records; want equal lengths",
			len(first), len(second))
	}
}

func TestQuery_BuilderAliasesAndOverwrites(t *testing.T) {
	ctx := context.Background()
	c := typed.NewClient[widget](newConn(t))

	// (i) Filter overwrites: after two Filter calls only the last survives.
	q := c.Query(ctx)
	q.Filter(`eq(name, "alpha")`)
	q.Filter(`eq(name, "beta")`)
	s := q.Raw().String()
	if strings.Contains(s, `eq(name, "alpha")`) {
		t.Fatalf("Filter did not overwrite: filter A still present in:\n%s", s)
	}
	if !strings.Contains(s, `eq(name, "beta")`) {
		t.Fatalf("Filter B missing after overwrite; got:\n%s", s)
	}

	// (ii) The builder aliases: a saved reference and further mutation observe
	// the same underlying query. ref and q point at the same *Query, so a
	// mutation through one is visible through the other. This documents the
	// single-use footgun: you cannot branch a saved builder.
	ref := q
	if ref != q {
		t.Fatal("builder reference is not identical to the original *Query")
	}
	q.OrderAsc("name")
	if ref.Raw().String() != q.Raw().String() {
		t.Fatal("mutating q did not affect ref; builder is expected to alias a shared query")
	}
	if !strings.Contains(ref.Raw().String(), "orderasc: name") {
		t.Fatalf("order applied via q not visible through ref; got:\n%s", ref.Raw().String())
	}
}

func TestQuery_RawRoundTrips(t *testing.T) {
	ctx := context.Background()
	c := typed.NewClient[widget](newConn(t))
	if err := c.Add(ctx, &widget{Name: "raw-target", Qty: 7}); err != nil {
		t.Fatalf("Add: %v", err)
	}

	// Take the raw *dg.Query, apply a dgman-only builder method directly,
	// then execute via the raw query's own Nodes(&dst).
	var raw *dg.Query = c.Query(ctx).Raw()
	raw.OrderAsc("qty")

	var dst []widget
	if err := raw.Nodes(&dst); err != nil {
		t.Fatalf("raw query Nodes: %v", err)
	}
	if len(dst) != 1 {
		t.Fatalf("raw query returned %d records, want 1", len(dst))
	}
	if dst[0].Name != "raw-target" || dst[0].Qty != 7 {
		t.Fatalf("raw query returned %+v, want Name=raw-target Qty=7", dst[0])
	}
}

func TestQuery_SingleQueryPerTerminal(t *testing.T) {
	// Uses the global dgman logger; must not run in parallel.
	ctx := context.Background()
	// queriesExecuted is incremented by newCountingConn's logger each time
	// dgman runs a query, so it reflects real database round-trips.
	var queriesExecuted int
	c := typed.NewClient[widget](newCountingConn(t, &queriesExecuted))

	for i := range 2 {
		if err := c.Add(ctx, &widget{Name: "w", Qty: i}); err != nil {
			t.Fatalf("Add %d: %v", i, err)
		}
	}

	// Building the chain runs no queries: builder methods only mutate the AST.
	before := queriesExecuted
	q := c.Query(ctx).Filter(`eq(name, "w")`).OrderAsc("qty").Limit(10)
	if queriesExecuted != before {
		t.Fatalf("builder methods executed %d queries, want 0", queriesExecuted-before)
	}

	// The Nodes terminal runs exactly one query.
	if _, err := q.Nodes(); err != nil {
		t.Fatalf("Nodes: %v", err)
	}
	if got := queriesExecuted - before; got != 1 {
		t.Fatalf("Nodes executed %d queries, want exactly 1", got)
	}

	// A fresh builder's First terminal also runs exactly one query.
	before = queriesExecuted
	if _, err := c.Query(ctx).First(); err != nil {
		t.Fatalf("First: %v", err)
	}
	if got := queriesExecuted - before; got != 1 {
		t.Fatalf("First executed %d queries, want exactly 1", got)
	}
}
