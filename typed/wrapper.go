/*
 * SPDX-FileCopyrightText: © 2017-2026 Istari Digital, Inc.
 * SPDX-License-Identifier: Apache-2.0
 */

package typed

import (
	"context"
	"encoding/json"

	"github.com/matthewmcneely/modusgraph"
)

// Wrapper is the generic base embedded by generated wrapper/entity types. It
// holds the backing schema struct in an unexported field; the only access is
// the exported Unwrap. Embedding Wrapper gives a wrapper type its Unwrap, JSON
// marshaling, and validation for free.
type Wrapper[S any] struct {
	s *S
}

// WrapValue builds a Wrapper around s. Generated New<E>/Wrap<E> constructors
// use this to populate the embedded base.
func WrapValue[S any](s *S) Wrapper[S] {
	return Wrapper[S]{s: s}
}

// Unwrap returns the backing schema struct. modusgraph uses this (via
// reflection) to substitute the schema struct when a wrapper crosses the
// client boundary.
func (w Wrapper[S]) Unwrap() *S {
	return w.s
}

// MarshalJSON delegates to the schema struct so its json tags drive output.
// The receiver is a pointer, so marshaling only engages through a pointer
// (*Entity, not Entity): a value-typed wrapper falls back to reflection over
// the unexported field and silently emits an empty object. Generated entities
// are always used as pointers.
func (w *Wrapper[S]) MarshalJSON() ([]byte, error) {
	return json.Marshal(w.s)
}

// UnmarshalJSON lazily allocates the backing struct if needed, then delegates.
// Safe to call on a zero-value Wrapper.
func (w *Wrapper[S]) UnmarshalJSON(data []byte) error {
	if w.s == nil {
		w.s = new(S)
	}
	return json.Unmarshal(data, w.s)
}

// Validate runs v against the backing schema struct. v is satisfied by
// *github.com/go-playground/validator/v10.Validate.
func (w *Wrapper[S]) Validate(ctx context.Context, v modusgraph.StructValidator) error {
	return v.StructCtx(ctx, w.s)
}
