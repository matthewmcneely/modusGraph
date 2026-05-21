/*
 * SPDX-FileCopyrightText: © 2017-2026 Istari Digital, Inc.
 * SPDX-License-Identifier: Apache-2.0
 */

package typed

import (
	"iter"

	dg "github.com/dolan-in/dgman/v2"
)

// Query is a fluent, type-safe query builder over records of type T. Builder
// methods return *Query[T] for chaining, except Var and GroupBy, which change
// the result shape and transition to *RawQuery; terminal methods (Nodes,
// First, IterNodes) execute the query and decode typed results.
//
// A Query is single-use. Builder methods mutate the underlying query in place
// and return the same *Query, so a Query value should be built as one chain
// and handed to a single terminal. It is not safe to save a Query to a
// variable and branch it into independent queries: every branch shares — and
// keeps mutating — the same underlying query.
//
// Repeated builder calls do not all behave the same way. Filter, Limit,
// Offset, After, Cascade, As, Name, RootFunc, and Vars overwrite: the last
// call wins. OrderAsc and OrderDesc accumulate: each call adds to the query.
//
// Limit and Offset additionally record the bounds that IterNodes pages
// within — a Limit caps the rows it streams, an Offset is its start.
type Query[T any] struct {
	q      *dg.Query
	limit  int // caller-set row cap; 0 = unbounded
	offset int // caller-set starting offset; 0 = none
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

// As assigns a dgraph query-variable name to the query block. Repeated calls
// overwrite.
func (qb *Query[T]) As(varName string) *Query[T] {
	qb.q.As(varName)
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
	var out []T
	if err := qb.q.Nodes(&out); err != nil {
		return nil, err
	}
	return out, nil
}

// First executes the query with an implicit Limit(1) and returns the first
// record, or (nil, nil) if the query matched no rows.
func (qb *Query[T]) First() (*T, error) {
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
// repeat rows. On error it yields a final (nil, err) and stops.
func (qb *Query[T]) IterNodes() iter.Seq2[*T, error] {
	return func(yield func(*T, error) bool) {
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
// (for example UID, Query, NodesAndCount).
func (qb *Query[T]) Raw() *dg.Query {
	return qb.q
}

// RawQuery is a query whose result is not a slice of T — produced by the
// shape-changing builders Query.Var and Query.GroupBy. A RawQuery deliberately
// exposes no typed node terminal: its result must be decoded by the caller
// through the underlying dgman query, obtained via Raw.
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
