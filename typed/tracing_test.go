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

	_, span := currentTracer().StartSpan(context.Background(), "get", "Widget")
	span.End(nil)

	if rec.op != "get" || rec.collection != "Widget" {
		t.Fatalf("installed tracer not invoked: %+v", rec)
	}
	if !rec.ended {
		t.Fatal("span.End was not called")
	}

	// nil restores the no-op tracer, which must not panic.
	SetTracer(nil)
	_, span = currentTracer().StartSpan(context.Background(), "x", "Y")
	span.End(nil)
}

// TestSetTracer_ConcurrentWithReads asserts SetTracer is safe to call while
// other goroutines read the tracer. Run under -race, a plain package var would
// flag a data race here; the atomic.Pointer holder does not.
func TestSetTracer_ConcurrentWithReads(t *testing.T) {
	t.Cleanup(func() { SetTracer(nil) })

	done := make(chan struct{})
	go func() {
		defer close(done)
		for range 1000 {
			_, span := currentTracer().StartSpan(context.Background(), "get", "Widget")
			span.End(nil)
		}
	}()
	for i := range 1000 {
		if i%2 == 0 {
			SetTracer(&recordingTracer{})
		} else {
			SetTracer(nil)
		}
	}
	<-done
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
