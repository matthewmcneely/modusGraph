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
//
// -no-cli is passed because the generated CLI imports github.com/alecthomas/kong,
// which is not (and should not be) a dependency of the modusgraph module itself.
// Adding kong to go.mod would cause `go mod tidy` to strip it, since nothing in
// the compiled tree uses it. This fixture exists to exercise the schema + wrapper
// layers; the cross-package e2e tests (unwrap_e2e_test.go) import the wrapper
// package directly and do not need a CLI. CLI generation correctness is covered
// by TestGenerate_CLIImportsSchemaByFullPath in the generator package.
package movies

//go:generate go run github.com/matthewmcneely/modusgraph/cmd/modusgraph-gen -entities -no-cli
