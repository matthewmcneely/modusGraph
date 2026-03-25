package movies

// Studio exercises private field parsing: private scalars, singular edges,
// multi-edges, primitive slices, opt-out via dgraph:"-", and fields without
// json tags.
type Studio struct {
	UID   string   `json:"uid,omitempty"`
	DType []string `json:"dgraph.type,omitempty"`

	// Private scalar field — generates getter/setter.
	name string `json:"name,omitempty" dgraph:"index=exact"`

	// Private singular edge (*Entity) — generates *Director getter/setter.
	founder *Director `json:"founder,omitempty"`

	// Private singular edge ([]Entity with validate max=1).
	currentHead []Director `json:"currentHead,omitempty" validate:"max=1"`

	// Private multi-edge — generates slice getter/setter + append/remove helpers.
	films []Film `json:"films,omitempty"`

	// Private primitive slice — generates slice getter/setter + helpers.
	tags []string `json:"tags,omitempty"`

	// Exported field — no accessors generated, direct access.
	Founded string `json:"founded,omitempty"`

	// Opted-out field (dgraph:"-") — skipped entirely.
	Internal string `json:"internal,omitempty" dgraph:"-"`

	// No json tag — skipped entirely.
	tempCache string
}
