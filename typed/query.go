/*
 * SPDX-FileCopyrightText: © 2017-2026 Istari Digital, Inc.
 * SPDX-License-Identifier: Apache-2.0
 */

package typed

import (
	dg "github.com/dolan-in/dgman/v2"
)

// Query is a fluent, type-safe query builder over records of type T. Builder
// methods return *Query[T] for chaining; terminal methods (Nodes, First)
// execute the query and decode typed results.
type Query[T any] struct {
	q *dg.Query
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
	qb.q.First(n)
	return qb
}

// Offset skips the first n results.
func (qb *Query[T]) Offset(n int) *Query[T] {
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

// Raw returns the underlying dgman query for operations Query does not wrap
// (Var, As, Name, RootFunc, GroupBy, Vars).
func (qb *Query[T]) Raw() *dg.Query {
	return qb.q
}
