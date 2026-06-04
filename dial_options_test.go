/*
 * SPDX-FileCopyrightText: © 2017-2026 Istari Digital, Inc.
 * SPDX-License-Identifier: Apache-2.0
 */

package modusgraph

import (
	"testing"

	"google.golang.org/grpc"
)

func TestWithGRPCDialOptionAppends(t *testing.T) {
	var o clientOptions
	WithGRPCDialOption(grpc.WithUserAgent("a"))(&o)
	WithGRPCDialOption(grpc.WithUserAgent("b"))(&o)
	if got := len(o.grpcDialOptions); got != 2 {
		t.Fatalf("expected 2 dial options, got %d", got)
	}
}

func TestKeyDistinguishesGRPCDialOptions(t *testing.T) {
	base := client{uri: "dgraph://localhost:9080"}
	withOpt := client{uri: "dgraph://localhost:9080"}
	WithGRPCDialOption(grpc.WithUserAgent("x"))(&withOpt.options)
	if base.key() == withOpt.key() {
		t.Fatal("client.key() must differ when grpcDialOptions differ, else clients dedup incorrectly")
	}
}
