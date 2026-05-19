package modusgraph

import (
	"errors"
	"testing"
)

type fakeRecord struct{ name string }

func (f *fakeRecord) SchemaTypeName() string { return f.name }

type fakeWrapper struct{ inner *fakeRecord }

func (w *fakeWrapper) Unwrap() *fakeRecord { return w.inner }

type fakeNonSchema struct{ X string }

func TestUnwrapSchema_PassthroughForPlainStruct(t *testing.T) {
	in := &fakeNonSchema{X: "hi"}
	out := unwrapSchema(in)
	if out != any(in) {
		t.Fatalf("expected passthrough, got %T", out)
	}
}

func TestUnwrapSchema_PassthroughForSchemaStruct(t *testing.T) {
	in := &fakeRecord{name: "Studio"}
	out := unwrapSchema(in)
	if out != any(in) {
		t.Fatalf("expected passthrough for direct Schema, got %T", out)
	}
}

func TestUnwrapSchema_UnwrapsWrapper(t *testing.T) {
	inner := &fakeRecord{name: "Studio"}
	w := &fakeWrapper{inner: inner}
	out := unwrapSchema(w)
	if out != any(inner) {
		t.Fatalf("expected unwrapped inner, got %T (%v)", out, out)
	}
}

func TestUnwrapSchema_IgnoresErrorsUnwrap(t *testing.T) {
	// errors.New("x") has no Unwrap; wrap one to get something with Unwrap() error.
	inner := errors.New("inner")
	outer := &wrappedErr{err: inner}
	out := unwrapSchema(outer)
	if out != any(outer) {
		t.Fatalf("expected passthrough for error wrapper, got %T", out)
	}
}

type wrappedErr struct{ err error }

func (w *wrappedErr) Error() string { return w.err.Error() }
func (w *wrappedErr) Unwrap() error { return w.err }

func TestUnwrapSchema_NilInput(t *testing.T) {
	if out := unwrapSchema(nil); out != nil {
		t.Fatalf("expected nil for nil input, got %v", out)
	}
}

// recordingClient is the minimal surface needed to verify that wrappers
// passed to the Client interface get unwrapped before reaching internal
// reflection. It records whatever it received and returns nil. Each method
// applies obj = unwrapSchema(obj) at the top, mirroring the patch landing
// in this task.
type recordingClient struct {
	seen []any
}

func (c *recordingClient) capture(obj any) any {
	obj = unwrapSchema(obj)
	c.seen = append(c.seen, obj)
	return obj
}

func TestUnwrapSchema_CaptureForwardsInner(t *testing.T) {
	inner := &fakeRecord{name: "Studio"}
	w := &fakeWrapper{inner: inner}
	c := &recordingClient{}
	got := c.capture(w)
	if got != any(inner) {
		t.Fatalf("expected inner record, got %T (%v)", got, got)
	}
	if len(c.seen) != 1 || c.seen[0] != any(inner) {
		t.Fatalf("expected recording to hold inner record, got %v", c.seen)
	}
}

func TestUnwrapSchema_CapturePassthroughForPlain(t *testing.T) {
	plain := &fakeNonSchema{X: "y"}
	c := &recordingClient{}
	got := c.capture(plain)
	if got != any(plain) {
		t.Fatalf("expected plain struct passthrough, got %T", got)
	}
}

func TestUnwrapSchema_VariadicUnwrapsEachElement(t *testing.T) {
	innerA := &fakeRecord{name: "Studio"}
	innerB := &fakeRecord{name: "Film"}
	templates := []any{
		&fakeWrapper{inner: innerA},
		innerB, // already a Schema; passthrough
	}
	for i, obj := range templates {
		templates[i] = unwrapSchema(obj)
	}
	if templates[0] != any(innerA) {
		t.Fatalf("template[0]: expected innerA, got %T", templates[0])
	}
	if templates[1] != any(innerB) {
		t.Fatalf("template[1]: expected innerB (passthrough), got %T", templates[1])
	}
}
