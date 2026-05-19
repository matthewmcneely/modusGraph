// Package movies anchors the //go:generate directive for the wrapper-parent
// layout: schema files live in ./schema/, and modusgraph-gen emits wrappers
// here (the parent of schema/). Running `go generate ./...` from this
// directory invokes the generator with unflagged defaults — see
// docs/specs/2026-05-18-public-wrapper-types-design.md for the resolution
// rules.
package movies

//go:generate go run github.com/matthewmcneely/modusgraph/cmd/modusgraph-gen
