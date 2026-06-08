/*
 * SPDX-FileCopyrightText: © 2017-2026 Istari Digital, Inc.
 * SPDX-License-Identifier: Apache-2.0
 */

package typed_test

import (
	"context"
	"testing"

	"github.com/matthewmcneely/modusgraph"
	"github.com/matthewmcneely/modusgraph/typed"
)

type jti struct {
	UID   string   `json:"uid,omitempty"`
	DType []string `json:"dgraph.type,omitempty"`
	JTI   string   `json:"jti,omitempty" dgraph:"index=hash upsert unique"`
}

func newTypedConn(t *testing.T) modusgraph.Client {
	t.Helper()
	conn, err := modusgraph.NewClient("file://"+t.TempDir(), modusgraph.WithAutoSchema(true))
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	t.Cleanup(conn.Close)
	return conn
}

func TestTypedLoadOrStore(t *testing.T) {
	c := typed.NewClient[jti](newTypedConn(t))
	ctx := context.Background()

	rec, loaded, err := c.LoadOrStore(ctx, &jti{JTI: "abc"}, "jti")
	if err != nil {
		t.Fatalf("first: %v", err)
	}
	if loaded {
		t.Fatal("first: want loaded=false")
	}
	if rec.UID == "" {
		t.Fatal("first: want a UID assigned")
	}

	_, loaded, err = c.LoadOrStore(ctx, &jti{JTI: "abc"}, "jti")
	if err != nil {
		t.Fatalf("second: %v", err)
	}
	if !loaded {
		t.Fatal("second: want loaded=true")
	}
}
