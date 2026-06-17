/*
 * SPDX-FileCopyrightText: © 2017-2026 Istari Digital, Inc.
 * SPDX-License-Identifier: Apache-2.0
 */

package typed

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"

	dg "github.com/dolan-in/dgman/v2"
	"github.com/matthewmcneely/modusgraph"
)

// MultiQuery batches N homogeneous-type Query[T] blocks into a single
// Dgraph multi-block request. All blocks return rows of the same T; the
// per-block result is keyed by the block name supplied at Add.
//
// Dgraph executes the blocks concurrently on the server side; the entire
// batch costs one gRPC round-trip.
type MultiQuery[T any] struct {
	conn   modusgraph.Client
	names  []string
	blocks map[string]*Query[T]
}

// NewMultiQuery constructs a MultiQuery bound to conn.
func NewMultiQuery[T any](conn modusgraph.Client) *MultiQuery[T] {
	return &MultiQuery[T]{
		conn:   conn,
		blocks: make(map[string]*Query[T]),
	}
}

// Add registers a named block. Names must be unique within one MultiQuery, and
// each *Query[T] may be added only once: Execute names the block's underlying
// dgman query in place, so registering the same Query pointer under two names
// would make both blocks render with whichever name was applied last. Both
// conditions are programming errors and panic rather than fail at runtime.
func (mq *MultiQuery[T]) Add(name string, q *Query[T]) *MultiQuery[T] {
	if _, exists := mq.blocks[name]; exists {
		panic(fmt.Sprintf("multi_query: duplicate block name %q", name))
	}
	for existingName, existing := range mq.blocks {
		if existing == q {
			panic(fmt.Sprintf("multi_query: Query already added as %q; build a separate Query per block", existingName))
		}
	}
	mq.names = append(mq.names, name)
	mq.blocks[name] = q
	return mq
}

// BlockNames returns the registered block names in insertion order.
func (mq *MultiQuery[T]) BlockNames() []string {
	out := make([]string, len(mq.names))
	copy(out, mq.names)
	return out
}

// Execute runs every registered block in a single Dgraph round-trip and
// returns the per-block results, keyed by the block name supplied at Add.
// A block that matched no rows appears as an empty (non-nil) slice in the
// result map; the key is always present.
//
// Execute rejects blocks that carry WhereEdge constraints — those require a
// runtime pre-pass that cannot be folded into the multi-block batch. Run such
// queries individually with Query.Nodes.
//
// Dgraph keys response JSON by predicate name (e.g. resourceName), but Go
// structs typically use their json tag (e.g. name). Execute remaps the keys
// per T's tags before decoding so a schema that uses `dgraph:"predicate=..."`
// with a divergent `json:"..."` decodes correctly — matching the behavior of
// dgman's QueryBlock.Scan path.
func (mq *MultiQuery[T]) Execute(ctx context.Context) (map[string][]T, error) {
	if len(mq.names) == 0 {
		return map[string][]T{}, nil
	}

	rawBlocks := make([]*dg.Query, 0, len(mq.names))
	for _, name := range mq.names {
		block := mq.blocks[name]
		if len(block.edges) != 0 {
			return nil, fmt.Errorf(
				"multi_query: block %q carries WhereEdge constraints; "+
					"MultiQuery cannot batch edge-filtered blocks", name)
		}
		// Name the underlying dgman query so blocks do not collide on the
		// default "data" name and so the response JSON keys are predictable.
		block.q.Name(name)
		rawBlocks = append(rawBlocks, block.q)
	}

	dql := dg.NewQueryBlock(rawBlocks...).String()
	raw, err := mq.conn.QueryRaw(ctx, dql, nil)
	if err != nil {
		return nil, fmt.Errorf("multi_query: dgraph: %w", err)
	}

	var perBlockRaw map[string]json.RawMessage
	if err := json.Unmarshal(raw, &perBlockRaw); err != nil {
		return nil, fmt.Errorf("multi_query: decoding response: %w", err)
	}

	var zero T
	predMap := buildPredicateToJSONMap(reflect.TypeOf(zero))

	out := make(map[string][]T, len(mq.names))
	for _, name := range mq.names {
		body, ok := perBlockRaw[name]
		if !ok {
			out[name] = []T{}
			continue
		}
		if len(predMap) > 0 {
			remapped, err := remapArrayKeys(body, predMap)
			if err == nil {
				body = remapped
			}
		}
		var rows []T
		if err := json.Unmarshal(body, &rows); err != nil {
			return nil, fmt.Errorf("multi_query: decoding block %q: %w", name, err)
		}
		if rows == nil {
			rows = []T{}
		}
		out[name] = rows
	}
	return out, nil
}

// buildPredicateToJSONMap returns a map from dgraph predicate name → JSON tag
// name for fields on T where the two differ. Mirrors dgman's unexported helper
// of the same name; we need our own because the multi-block response from
// QueryRaw bypasses dgman's scan path.
func buildPredicateToJSONMap(t reflect.Type) map[string]string {
	for t != nil && t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	if t == nil || t.Kind() != reflect.Struct {
		return nil
	}
	result := make(map[string]string)
	for i := 0; i < t.NumField(); i++ {
		field := t.Field(i)
		jsonTag := field.Tag.Get("json")
		if jsonTag == "" || jsonTag == "-" {
			continue
		}
		jsonName := strings.Split(jsonTag, ",")[0]
		if jsonName == "" {
			continue
		}
		dgraphTag := field.Tag.Get("dgraph")
		if dgraphTag == "" {
			continue
		}
		var predName string
		for part := range strings.FieldsSeq(dgraphTag) {
			if p, ok := strings.CutPrefix(part, "predicate="); ok {
				predName = p
				break
			}
		}
		if predName == "" || predName == jsonName {
			continue
		}
		if predName == "uid" || predName == "dgraph.type" {
			continue
		}
		result[predName] = jsonName
	}
	return result
}

// remapArrayKeys rewrites top-level keys in each object of a JSON array using
// the predicate → JSON-tag map. Nested objects are left untouched (search
// callers iterate scalar predicates of the root type; edge fields are
// hydrated lazily, not in the multi-block response).
func remapArrayKeys(data json.RawMessage, predMap map[string]string) (json.RawMessage, error) {
	var rows []map[string]json.RawMessage
	if err := json.Unmarshal(data, &rows); err != nil {
		return data, err
	}
	for i, row := range rows {
		for k, v := range row {
			if newK, ok := predMap[k]; ok && newK != k {
				delete(row, k)
				row[newK] = v
			}
		}
		rows[i] = row
	}
	return json.Marshal(rows)
}
