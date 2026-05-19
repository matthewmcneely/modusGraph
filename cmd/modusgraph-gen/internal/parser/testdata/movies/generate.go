// Package movies anchors the //go:generate directive for the wrapper-parent
// layout: schema files live in ./schema/, and modusgraph-gen emits wrappers
// here (the parent of schema/). Running `go generate ./...` from this
// directory passes -entities to generate the full wrapper layer — both the
// schema side and the method-based wrapper types. The -entities flag is
// required because the wrapper layer is opt-in (off by default); the
// cross-package e2e tests in unwrap_e2e_test.go import the movies wrapper
// package and call movies.WrapStudio, so the fixture must keep emitting the
// full two-layer output. See docs/specs/2026-05-18-public-wrapper-types-design.md
// for the flag and path-resolution rules.
package movies

//go:generate go run github.com/matthewmcneely/modusgraph/cmd/modusgraph-gen -entities
