/*
 * SPDX-FileCopyrightText: © 2017-2026 Istari Digital, Inc.
 * SPDX-License-Identifier: Apache-2.0
 */

package typed

import (
	"context"
	"iter"
	"strconv"
	"strings"

	dg "github.com/dolan-in/dgman/v2"
	"github.com/matthewmcneely/modusgraph"
)

// Query is a fluent, type-safe query builder over records of type T. Builder
// methods return *Query[T] for chaining, except As, Var, and GroupBy, which
// change the result shape and transition to *RawQuery; terminal methods
// (Nodes, First, IterNodes) execute the query and decode typed results.
//
// A Query is single-use. Builder methods mutate the underlying query in place
// and return the same *Query, so a Query value should be built as one chain
// and handed to a single terminal. It is not safe to save a Query to a
// variable and branch it into independent queries: every branch shares — and
// keeps mutating — the same underlying query.
//
// Repeated builder calls do not all behave the same way. Filter, Limit,
// Offset, After, Cascade, Name, RootFunc, and Vars overwrite: the last call
// wins. OrderAsc, OrderDesc, and WhereEdge accumulate: each call adds to the
// query.
//
// Limit and Offset additionally record the bounds that IterNodes pages
// within — a Limit caps the rows it streams, an Offset is its start.
type Query[T any] struct {
	q      *dg.Query
	conn   modusgraph.Client // runs the WhereEdge pre-pass; set by Client.Query
	ctx    context.Context   // carried for the WhereEdge pre-pass query
	limit  int               // caller-set row cap; 0 = unbounded
	offset int               // caller-set starting offset; 0 = none
	edges  []edgeFilter      // accumulated WhereEdge constraints; empty = none
}

// edgeFilter is one accumulated WhereEdge constraint: a dgraph @filter
// expression scoped to an outbound edge predicate of T.
type edgeFilter struct {
	predicate string
	filter    string
	params    []any
}

// Filter adds a dgraph @filter expression. params bind to placeholders.
func (qb *Query[T]) Filter(filter string, params ...any) *Query[T] {
	qb.q.Filter(filter, params...)
	return qb
}

// OrderAsc orders results ascending by clause.
func (qb *Query[T]) OrderAsc(clause string) *Query[T] {
	qb.q.OrderAsc(clause)
	return qb
}

// OrderDesc orders results descending by clause.
func (qb *Query[T]) OrderDesc(clause string) *Query[T] {
	qb.q.OrderDesc(clause)
	return qb
}

// Limit caps the number of results. dgman names this First; it is renamed
// here so it does not collide with the First terminal.
func (qb *Query[T]) Limit(n int) *Query[T] {
	qb.limit = n
	qb.q.First(n)
	return qb
}

// Offset skips the first n results.
func (qb *Query[T]) Offset(n int) *Query[T] {
	qb.offset = n
	qb.q.Offset(n)
	return qb
}

// After returns results with UID greater than uid (cursor pagination).
func (qb *Query[T]) After(uid string) *Query[T] {
	qb.q.After(uid)
	return qb
}

// Cascade drops nodes missing any of the given predicates (all, if none given).
func (qb *Query[T]) Cascade(predicates ...string) *Query[T] {
	qb.q.Cascade(predicates...)
	return qb
}

// RootFunc overrides the query root function. dgman's default root function
// is type(<NodeType>); RootFunc replaces it with an expression such as
// eq(name, "Alice") or has(email). Repeated calls overwrite.
func (qb *Query[T]) RootFunc(rootFunc string) *Query[T] {
	qb.q.RootFunc(rootFunc)
	return qb
}

// Name sets the query block name. It defaults to "data"; dgman uses the name
// to both generate and decode the query, so a renamed block still decodes
// into []T. Repeated calls overwrite.
func (qb *Query[T]) Name(queryName string) *Query[T] {
	qb.q.Name(queryName)
	return qb
}

// Vars supplies GraphQL variables for a parameterized query: funcDef is the
// query function definition (for example "getByName($n: string)") and vars
// binds each variable. The query then executes via dgraph's QueryWithVars
// path. Repeated calls overwrite.
func (qb *Query[T]) Vars(funcDef string, vars map[string]string) *Query[T] {
	qb.q.Vars(funcDef, vars)
	return qb
}

// WhereEdge constrains results to records that have at least one `predicate`
// edge whose target node satisfies the dgraph @filter expression. params bind
// to $N placeholders within filter, exactly as Filter binds them.
//
// Where Filter constrains T's own scalar predicates, WhereEdge constrains a
// neighbouring node reached over an edge. dgraph's root @filter cannot express
// that, so a query carrying WhereEdge constraints executes in two steps: a
// pre-pass resolves the UIDs of roots that satisfy every constraint, then the
// main query runs against uid(...) — keeping ordering, pagination, and result
// projection on the normal path. See
// docs/specs/2026-05-21-query-edge-filter-design.md.
//
// WhereEdge accumulates: multiple calls AND together (a record must satisfy
// every edge constraint). It is the substrate behind the generated
// <Entity>Query.Where<Edge> methods.
func (qb *Query[T]) WhereEdge(predicate, filter string, params ...any) *Query[T] {
	qb.edges = append(qb.edges, edgeFilter{predicate: predicate, filter: filter, params: params})
	return qb
}

// As names the query block as a dgraph query variable. dgraph requires such a
// variable be consumed by another block, which a single-block typed query
// cannot do, so As transitions out of the typed query: it returns a *RawQuery,
// which exposes no node terminal.
func (qb *Query[T]) As(varName string) *RawQuery {
	qb.q.As(varName)
	return &RawQuery{q: qb.q}
}

// Var marks the query block as a dgraph var block. A var block computes query
// variables and returns no data of its own, so Var transitions out of the
// typed query: it returns a *RawQuery, which exposes no node terminal.
func (qb *Query[T]) Var() *RawQuery {
	qb.q.Var()
	return &RawQuery{q: qb.q}
}

// GroupBy adds an @groupby(predicate) aggregation. A grouped query returns
// aggregation groups rather than a slice of T, so GroupBy transitions out of
// the typed query: it returns a *RawQuery, which exposes no node terminal.
func (qb *Query[T]) GroupBy(predicate string) *RawQuery {
	qb.q.GroupBy(predicate)
	return &RawQuery{q: qb.q}
}

// Nodes executes the query and returns all matching records.
func (qb *Query[T]) Nodes() ([]T, error) {
	matched, err := qb.resolveRoots()
	if err != nil {
		return nil, err
	}
	if !matched {
		return nil, nil
	}
	var out []T
	if err := qb.q.Nodes(&out); err != nil {
		return nil, err
	}
	return out, nil
}

// First executes the query with an implicit Limit(1) and returns the first
// record, or (nil, nil) if the query matched no rows.
func (qb *Query[T]) First() (*T, error) {
	matched, err := qb.resolveRoots()
	if err != nil {
		return nil, err
	}
	if !matched {
		return nil, nil
	}
	var out []T
	if err := qb.q.First(1).Nodes(&out); err != nil {
		return nil, err
	}
	if len(out) == 0 {
		return nil, nil
	}
	return &out[0], nil
}

// IterNodes executes the query and returns an iterator over matching records,
// paging transparently so a large result set is never materialized at once.
//
// IterNodes is a terminal operation: it drives Offset/Limit internally as it
// pages and leaves the builder spent — do not call another terminal on the
// same Query afterward. A Limit set on the query caps the total number of
// rows streamed; an Offset is the starting point.
//
// All pages execute against one read-only transaction, so the iteration reads
// a single consistent snapshot: a concurrent writer cannot make it skip or
// repeat rows. A WhereEdge pre-pass, when present, runs once before paging
// begins, in its own transaction. On error it yields a final (nil, err) and
// stops.
func (qb *Query[T]) IterNodes() iter.Seq2[*T, error] {
	return func(yield func(*T, error) bool) {
		matched, err := qb.resolveRoots()
		if err != nil {
			yield(nil, err)
			return
		}
		if !matched {
			return // edge constraints present, but no root matched
		}
		remaining := qb.limit // 0 = unbounded
		for off := qb.offset; ; off += defaultPageSize {
			size := defaultPageSize
			if remaining > 0 && remaining < size {
				size = remaining // shrink the last page so it can't overshoot the cap
			}
			var page []T
			if err := qb.q.Offset(off).First(size).Nodes(&page); err != nil {
				yield(nil, err)
				return
			}
			for i := range page {
				if !yield(&page[i], nil) {
					return // consumer broke out
				}
			}
			if remaining > 0 {
				if remaining -= len(page); remaining <= 0 {
					return // hit the caller's Limit
				}
			}
			if len(page) < size {
				return // result set exhausted
			}
		}
	}
}

// Raw returns the underlying dgman query for operations Query does not wrap
// (for example UID, Query, NodesAndCount). Raw does not carry WhereEdge
// constraints — those are resolved only when a terminal runs.
func (qb *Query[T]) Raw() *dg.Query {
	return qb.q
}

// RawQuery is a query whose result is not a slice of T — produced by the
// shape-changing builders Query.As, Query.Var, and Query.GroupBy. A RawQuery
// deliberately exposes no typed node terminal: its result must be decoded by
// the caller through the underlying dgman query, obtained via Raw.
type RawQuery struct {
	q *dg.Query
}

// Raw returns the underlying dgman query, for the caller to execute and decode.
func (r *RawQuery) Raw() *dg.Query {
	return r.q
}

// String returns the generated DQL.
func (r *RawQuery) String() string {
	return r.q.String()
}

// As names the block as a dgraph query variable. See Query.As.
func (r *RawQuery) As(varName string) *RawQuery {
	r.q.As(varName)
	return r
}

// Var marks the block as a dgraph var block. See Query.Var.
func (r *RawQuery) Var() *RawQuery {
	r.q.Var()
	return r
}

// GroupBy adds an @groupby(predicate) aggregation. See Query.GroupBy.
func (r *RawQuery) GroupBy(predicate string) *RawQuery {
	r.q.GroupBy(predicate)
	return r
}

// resolveRoots runs the WhereEdge pre-pass when the query carries edge
// constraints, rewriting the main query's root function to the matching UIDs.
// It returns matched=false when constraints are present but no root satisfied
// them — callers then return an empty result without running the main query.
// With no edge constraints it is a no-op returning matched=true.
func (qb *Query[T]) resolveRoots() (matched bool, err error) {
	if len(qb.edges) == 0 {
		return true, nil
	}
	uids, err := qb.matchedUIDs()
	if err != nil {
		return false, err
	}
	if len(uids) == 0 {
		return false, nil
	}
	qb.q.RootFunc("uid(" + strings.Join(uids, ", ") + ")")
	return true, nil
}

// matchedUIDs runs the pre-pass: an @cascade query over T that keeps only
// nodes whose every WhereEdge predicate has a target matching its filter, and
// returns those nodes' UIDs.
func (qb *Query[T]) matchedUIDs() ([]string, error) {
	var z T
	pre := qb.conn.Query(qb.ctx, &z)
	body, params := qb.edgeMatchBody()
	pre.Cascade().Query(body, params...)

	var rows []struct {
		UID string `json:"uid"`
	}
	if err := pre.Nodes(&rows); err != nil {
		return nil, err
	}
	uids := make([]string, len(rows))
	for i := range rows {
		uids[i] = rows[i].UID
	}
	return uids, nil
}

// edgeMatchBody renders the selection set for the pre-pass: uid plus one
// aliased, filtered block per WhereEdge constraint. The caller adds a bare
// @cascade, which then drops any node with an empty block — so a survivor
// satisfies every constraint. Blocks are aliased mg_e0, mg_e1, ... so two
// constraints on the same predicate do not collide as duplicate fields. Each
// fragment's $N placeholders are shifted to stay bound to its own params once
// every fragment's params are concatenated into one slice.
func (qb *Query[T]) edgeMatchBody() (body string, params []any) {
	var b strings.Builder
	b.WriteString("{\n\tuid\n")
	for i, e := range qb.edges {
		b.WriteString("\tmg_e")
		b.WriteString(strconv.Itoa(i))
		b.WriteString(" : ")
		b.WriteString(e.predicate)
		b.WriteString(" @filter(")
		b.WriteString(shiftPlaceholders(e.filter, len(params)))
		b.WriteString(") { uid }\n")
		params = append(params, e.params...)
	}
	b.WriteString("}")
	return b.String(), params
}

// shiftPlaceholders rewrites dgman ordinal placeholders ($1, $2, ...) in expr,
// adding delta to each index. WhereEdge filters are written independently, each
// numbering its params from $1; concatenating them into one pre-pass body
// needs every fragment renumbered against the combined params slice. A '$' not
// followed by a digit is left as-is, matching dgman's parseQueryWithParams.
func shiftPlaceholders(expr string, delta int) string {
	if delta == 0 || !strings.ContainsRune(expr, '$') {
		return expr
	}
	var b strings.Builder
	for i := 0; i < len(expr); i++ {
		if expr[i] != '$' {
			b.WriteByte(expr[i])
			continue
		}
		j := i + 1
		for j < len(expr) && expr[j] >= '0' && expr[j] <= '9' {
			j++
		}
		if j == i+1 { // '$' not followed by digits — leave verbatim
			b.WriteByte('$')
			continue
		}
		n, _ := strconv.Atoi(expr[i+1 : j])
		b.WriteByte('$')
		b.WriteString(strconv.Itoa(n + delta))
		i = j - 1
	}
	return b.String()
}
