package modusgraph

import "reflect"

// Schema identifies a value as a record of a generated schema-defining type.
// modusgraph-gen-emitted schema structs implement this via a generated
// SchemaTypeName() method that returns the canonical entity name
// (e.g. "Studio"). The interface is intentionally minimal — a single method
// returning a useful piece of metadata.
//
// Plain user structs (not emitted by modusgraph-gen) do not implement Schema
// and are unaffected by the modusgraph.Client routing it enables; they pass
// through to the existing reflection-based dgman pipeline exactly as before.
type Schema interface {
	SchemaTypeName() string
}

// UnwrapSchema returns the schema-defining record contained in obj. If obj
// is nil, it is returned as-is. If obj is already a Schema, it is returned
// as-is. If obj exposes an Unwrap() method whose return value satisfies
// Schema, that return is substituted. Otherwise obj is returned unchanged.
//
// This is the bridge between modusgraph-gen-emitted wrapper types and the
// rest of modusgraph.Client. It is purely additive: types that don't
// implement Schema and don't have an Unwrap() method (i.e. existing
// modusgraph users' plain structs) pass through untouched.
//
// Note on errors.Unwrap overlap: Go's errors package uses Unwrap() error
// as the standard "give me the wrapped thing" method. UnwrapSchema's
// secondary check (the returned value must itself implement Schema) means
// an error wrapper is not mistaken for a modusgraph wrapper — the
// reflection probe finds Unwrap(), calls it, gets an error, fails the
// Schema check, and returns the original obj.
func UnwrapSchema(obj any) any {
	if obj == nil {
		return obj
	}
	if _, ok := obj.(Schema); ok {
		return obj
	}
	v := reflect.ValueOf(obj)
	if !v.IsValid() {
		return obj
	}
	// A typed nil pointer has a valid method set, but invoking Unwrap on a nil
	// receiver would panic if the method dereferences it. Leave it untouched.
	if v.Kind() == reflect.Pointer && v.IsNil() {
		return obj
	}
	m := v.MethodByName("Unwrap")
	if !m.IsValid() && v.Kind() != reflect.Pointer {
		// Unwrap may be declared with a pointer receiver while obj was passed by
		// value; a value's method set excludes pointer-receiver methods, so look
		// it up on an addressable copy.
		pv := reflect.New(v.Type())
		pv.Elem().Set(v)
		m = pv.MethodByName("Unwrap")
	}
	if !m.IsValid() {
		return obj
	}
	mt := m.Type()
	if mt.NumIn() != 0 || mt.NumOut() != 1 {
		return obj
	}
	inner := m.Call(nil)[0].Interface()
	if _, ok := inner.(Schema); ok {
		return inner
	}
	return obj
}
