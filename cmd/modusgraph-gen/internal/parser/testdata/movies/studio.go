package movies

import (
	"time"

	dg "github.com/dolan-in/dgman/v2"
)

// Studio exercises private field parsing: private scalars, singular edges,
// multi-edges, primitive slices, opt-out via dgraph:"-", and fields without
// json tags.
type Studio struct {
	UID   string   `json:"uid,omitempty"`
	DType []string `json:"dgraph.type,omitempty"`

	// Private scalar field — generates getter/setter.
	name string `json:"name,omitempty" dgraph:"index=exact" validate:"required,min=2,max=200"`

	// Private singular edge (*Entity) — generates *Director getter/setter.
	founder *Director `json:"founder,omitempty"`

	// Private singular edge (bare Entity value type).
	headquarters Country `json:"headquarters,omitempty"`

	// Private singular edge ([]Entity with validate max=1).
	currentHead []Director `json:"currentHead,omitempty" validate:"max=1"`

	// Private singular edge ([]*Entity with validate max=1).
	ceo []*Director `json:"ceo,omitempty" validate:"max=1"`

	// Private singular edge ([]Entity with validate len=1).
	homeBase []Country `json:"homeBase,omitempty" validate:"len=1"`

	// Private singular edge ([]*Entity with validate len=1).
	parentCompany []*Country `json:"parentCompany,omitempty" validate:"len=1"`

	// Private multi-edge — generates slice getter/setter + append/remove helpers.
	films []Film `json:"films,omitempty"`

	// Pointer-slice edge ([]*Entity) — tests parser accepts both []Entity and []*Entity.
	advisors []*Director `json:"advisors,omitempty"`

	// Private primitive slices — generates slice getter/setter + helpers.
	tags       []string    `json:"tags,omitempty"`
	scores     []int       `json:"scores,omitempty"`
	weights    []float64   `json:"weights,omitempty"`
	flags      []bool      `json:"flags,omitempty"`
	milestones []time.Time `json:"milestones,omitempty"`

	// Private int field — tests non-string CLI flag support.
	yearFounded int `json:"yearFounded,omitempty" validate:"gte=1800,lte=2100"`

	// Private float field — tests float CLI flag support.
	revenue float64 `json:"revenue,omitempty" validate:"gte=0"`

	// Private bool field.
	active bool `json:"active,omitempty"`

	// Private time.Time field.
	createdAt time.Time `json:"createdAt,omitempty"`

	// Private vector field.
	embedding *dg.VectorFloat32 `json:"embedding,omitempty" dgraph:"index=hnsw(metric:cosine)"`

	// Exported field — no accessors generated, direct access.
	Founded string `json:"founded,omitempty"`

	// Opted-out field (dgraph:"-") — skipped entirely.
	Internal string `json:"internal,omitempty" dgraph:"-"`

	// No json tag — skipped entirely.
	tempCache string
}
