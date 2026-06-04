package filter

import "fmt"

// AnyOfText adds a fulltext OR-match group: anyoftext(predicate, term).
// An empty term is a no-op.
func (b *Builder) AnyOfText(predicate, term string) {
	if term == "" {
		return
	}
	b.groups = append(b.groups, fmt.Sprintf("anyoftext(%s, %s)", predicate, b.param(term)))
}

// AllOfText adds a fulltext AND-match group: alloftext(predicate, term).
// An empty term is a no-op.
func (b *Builder) AllOfText(predicate, term string) {
	if term == "" {
		return
	}
	b.groups = append(b.groups, fmt.Sprintf("alloftext(%s, %s)", predicate, b.param(term)))
}
