/*
 * SPDX-FileCopyrightText: © 2017-2026 Istari Digital, Inc.
 * SPDX-License-Identifier: Apache-2.0
 */

package typed

import (
	"context"
	"iter"

	"github.com/matthewmcneely/modusgraph"
)

// Client provides type-safe CRUD and query operations over records of type T.
// T is the schema struct (for example schema.Actor); modusgraph reflects over
// the struct's dgraph/json tags, so T needs no constraint.
type Client[T any] struct {
	conn modusgraph.Client
}

// NewClient binds a Client[T] to conn.
func NewClient[T any](conn modusgraph.Client) *Client[T] {
	return &Client[T]{conn: conn}
}

// Get loads the T with the given UID.
func (c *Client[T]) Get(ctx context.Context, uid string) (rec *T, err error) {
	ctx, span := tracer.StartSpan(ctx, "get", entityName[T]())
	defer func() { span.End(err) }()
	var out T
	if err = c.conn.Get(ctx, &out, uid); err != nil {
		return nil, err
	}
	return &out, nil
}

// Add inserts a new T. modusgraph writes the assigned UID back into rec.
func (c *Client[T]) Add(ctx context.Context, rec *T) (err error) {
	ctx, span := tracer.StartSpan(ctx, "add", entityName[T]())
	defer func() { span.End(err) }()
	return c.conn.Insert(ctx, rec)
}

// Update modifies an existing T (must have its UID set).
func (c *Client[T]) Update(ctx context.Context, rec *T) (err error) {
	ctx, span := tracer.StartSpan(ctx, "update", entityName[T]())
	defer func() { span.End(err) }()
	return c.conn.Update(ctx, rec)
}

// Upsert inserts or updates rec, matching against predicates. With no
// predicates, the first field tagged dgraph:"upsert" is used.
func (c *Client[T]) Upsert(ctx context.Context, rec *T, predicates ...string) (err error) {
	ctx, span := tracer.StartSpan(ctx, "upsert", entityName[T]())
	defer func() { span.End(err) }()
	return c.conn.Upsert(ctx, rec, predicates...)
}

// Delete removes the T with the given UID.
func (c *Client[T]) Delete(ctx context.Context, uid string) (err error) {
	ctx, span := tracer.StartSpan(ctx, "delete", entityName[T]())
	defer func() { span.End(err) }()
	return c.conn.Delete(ctx, []string{uid})
}

// Query returns a typed query builder for T. conn and ctx are carried so the
// builder can run a WhereEdge pre-pass (see Query.WhereEdge) if one is needed.
func (c *Client[T]) Query(ctx context.Context) *Query[T] {
	var z T
	return &Query[T]{q: c.conn.Query(ctx, &z), conn: c.conn, ctx: ctx}
}

// defaultPageSize is the page size IterNodes uses to page through results.
const defaultPageSize = 50

// Iter returns an iterator over every T, paging transparently so large result
// sets are not materialized at once. It yields each record in turn; on error
// it yields a final (nil, err) and stops. All pages execute against one
// read-only transaction, so the iteration reads a single consistent snapshot.
func (c *Client[T]) Iter(ctx context.Context) iter.Seq2[*T, error] {
	return c.Query(ctx).IterNodes()
}
