/*
 * SPDX-FileCopyrightText: © 2017-2026 Istari Digital, Inc.
 * SPDX-License-Identifier: Apache-2.0
 */

// Package search provides helpers for assembling fulltext / ranked search
// results across multiple typed query blocks.
package search

// MergeByID concatenates inputs into a single slice while preserving
// first-seen order and dropping any subsequent occurrence of an ID already
// emitted. The id function extracts a comparable identifier from each row.
//
// MergeByID is intended for use after typed.MultiQuery.Execute, when
// consumers want a single ranked slice from N per-field result sets:
// inputs[0] takes priority, inputs[1] fills in next, etc. A nil result
// indicates no rows survived (the inputs were all empty).
func MergeByID[T any](id func(T) string, inputs ...[]T) []T {
	seen := make(map[string]struct{})
	var out []T
	for _, in := range inputs {
		for _, row := range in {
			k := id(row)
			if _, dup := seen[k]; dup {
				continue
			}
			seen[k] = struct{}{}
			out = append(out, row)
		}
	}
	return out
}
