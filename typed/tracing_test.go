/*
 * SPDX-FileCopyrightText: © 2017-2026 Istari Digital, Inc.
 * SPDX-License-Identifier: Apache-2.0
 */

package typed

import (
	"context"
	"testing"
)

func TestSetTracer_InstallsAndResets(t *testing.T) {
	t.Cleanup(func() { SetTracer(nil) })

	rec := &recordingTracer{}
	SetTracer(rec)

	_, span := tracer.StartSpan(context.Background(), "get", "Widget")
	span.End(nil)

	if rec.op != "get" || rec.collection != "Widget" {
		t.Fatalf("installed tracer not invoked: %+v", rec)
	}
	if !rec.ended {
		t.Fatal("span.End was not called")
	}

	// nil restores the no-op tracer, which must not panic.
	SetTracer(nil)
	_, span = tracer.StartSpan(context.Background(), "x", "Y")
	span.End(nil)
}

type recordingTracer struct {
	op, collection string
	ended          bool
}

func (r *recordingTracer) StartSpan(ctx context.Context, op, collection string) (context.Context, Span) {
	r.op, r.collection = op, collection
	return ctx, &recordingSpan{r}
}

type recordingSpan struct{ r *recordingTracer }

func (s *recordingSpan) End(error) { s.r.ended = true }
