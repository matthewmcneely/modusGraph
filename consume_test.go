/*
 * SPDX-FileCopyrightText: © 2017-2026 Istari Digital, Inc.
 * SPDX-License-Identifier: Apache-2.0
 */

package modusgraph_test

import (
	"context"
	"testing"

	"github.com/matthewmcneely/modusgraph"
)

type consumeJTI struct {
	UID   string   `json:"uid,omitempty"`
	DType []string `json:"dgraph.type,omitempty"`
	JTI   string   `json:"jti,omitempty" dgraph:"index=hash upsert unique"`
}

func newConsumeClient(t *testing.T) modusgraph.Client {
	t.Helper()
	conn, err := modusgraph.NewClient("file://"+t.TempDir(), modusgraph.WithAutoSchema(true))
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	t.Cleanup(conn.Close)
	return conn
}

func TestLoadOrStore(t *testing.T) {
	conn := newConsumeClient(t)
	ctx := context.Background()

	first := &consumeJTI{JTI: "abc"}
	loaded, err := conn.LoadOrStore(ctx, first, "jti")
	if err != nil {
		t.Fatalf("first LoadOrStore: %v", err)
	}
	if loaded {
		t.Fatal("first store: want loaded=false (newly created)")
	}

	second := &consumeJTI{JTI: "abc"}
	loaded, err = conn.LoadOrStore(ctx, second, "jti")
	if err != nil {
		t.Fatalf("second LoadOrStore: %v", err)
	}
	if !loaded {
		t.Fatal("second store: want loaded=true (already existed)")
	}
	// On the loaded=true path, LoadOrStore must hydrate the passed object with
	// the existing record. Assert it carries the existing node's UID, so a
	// regression that returns loaded=true without populating fields is caught.
	if second.UID == "" {
		t.Fatal("second store: loaded record was not hydrated (UID empty)")
	}
	if second.UID != first.UID {
		t.Fatalf("second store: want existing node UID %q, got %q", first.UID, second.UID)
	}
}

type consumeState struct {
	UID    string   `json:"uid,omitempty"`
	DType  []string `json:"dgraph.type,omitempty"`
	State  string   `json:"state,omitempty" dgraph:"index=hash upsert"`
	Secret string   `json:"secret,omitempty"`
}

func TestLoadAndDelete(t *testing.T) {
	conn := newConsumeClient(t)
	ctx := context.Background()

	if err := conn.Insert(ctx, &consumeState{State: "s1", Secret: "shh"}); err != nil {
		t.Fatalf("Insert: %v", err)
	}

	var got consumeState
	loaded, err := conn.LoadAndDelete(ctx, &got, "s1", "state")
	if err != nil {
		t.Fatalf("LoadAndDelete: %v", err)
	}
	if !loaded {
		t.Fatal("first consume: want loaded=true")
	}
	if got.Secret != "shh" {
		t.Fatalf("want prior secret %q, got %q", "shh", got.Secret)
	}

	var again consumeState
	loaded, err = conn.LoadAndDelete(ctx, &again, "s1", "state")
	if err != nil {
		t.Fatalf("second LoadAndDelete: %v", err)
	}
	if loaded {
		t.Fatal("second consume: want loaded=false (already consumed)")
	}
}

// LoadAndDelete concatenates the predicate name straight into the DQL filter, so
// a name carrying DQL metacharacters must be rejected before it reaches the query
// rather than corrupting or injecting it. The key value is separately
// parameterized, so only the predicate name is at risk here.
func TestLoadAndDeleteRejectsInvalidPredicate(t *testing.T) {
	conn := newConsumeClient(t)
	ctx := context.Background()

	for _, pred := range []string{
		"state), has(secret", // break out of eq(...) and inject another function
		"state, uid",         // extra argument
		"state\"",            // stray quote
		"has(secret)",        // whole different function
		"state val",          // embedded whitespace
	} {
		var got consumeState
		loaded, err := conn.LoadAndDelete(ctx, &got, "s1", pred)
		if err == nil {
			t.Fatalf("predicate %q: want error, got nil (loaded=%v)", pred, loaded)
		}
		if loaded {
			t.Fatalf("predicate %q: want loaded=false on rejection, got true", pred)
		}
	}

	// A valid predicate still works: the guard must not reject legitimate names.
	if err := conn.Insert(ctx, &consumeState{State: "ok", Secret: "shh"}); err != nil {
		t.Fatalf("Insert: %v", err)
	}
	var got consumeState
	loaded, err := conn.LoadAndDelete(ctx, &got, "ok", "state")
	if err != nil {
		t.Fatalf("valid predicate: unexpected error: %v", err)
	}
	if !loaded {
		t.Fatal("valid predicate: want loaded=true")
	}
}

// A new public API must return errors on nil input rather than panic. A nil
// interface would panic in reflect.TypeOf(nil).Kind(); a typed nil pointer would
// pass the pointer-kind check and then panic when uidOf dereferences it.
func TestLoadAndDeleteRejectsNilObject(t *testing.T) {
	conn := newConsumeClient(t)
	ctx := context.Background()

	if _, err := conn.LoadAndDelete(ctx, nil, "s1", "state"); err == nil {
		t.Fatal("nil interface: want error, got nil")
	}
	if _, err := conn.LoadAndDelete(ctx, (*consumeState)(nil), "s1", "state"); err == nil {
		t.Fatal("typed nil pointer: want error, got nil")
	}
}
