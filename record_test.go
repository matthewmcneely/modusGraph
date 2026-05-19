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
