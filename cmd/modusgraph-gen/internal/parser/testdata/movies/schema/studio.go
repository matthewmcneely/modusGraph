package schema

import (
	"time"

	dg "github.com/dolan-in/dgman/v2"
)

// Studio exercises field parsing: scalars, singular edges,
// multi-edges, primitive slices, opt-out via dgraph:"-", and various
// field type combinations.
type Studio struct {
	UID   string   `json:"uid,omitempty"`
	DType []string `json:"dgraph.type,omitempty"`

	// Scalar field — generates getter/setter.
	Name string `json:"name,omitempty" dgraph:"index=exact" validate:"required,min=2,max=200"`

	// Singular edge (*Entity).
	Founder *Director `json:"founder,omitempty"`

	// Singular edge (bare Entity value type).
	Headquarters Country `json:"headquarters,omitempty"`

	// Singular edge ([]*Entity with validate max=1).
	CurrentHead []*Director `json:"currentHead,omitempty" validate:"max=1"`

	// Singular edge ([]*Entity with validate max=1).
	Ceo []*Director `json:"ceo,omitempty" validate:"max=1"`

	// Singular edge ([]*Entity with validate len=1).
	HomeBase []*Country `json:"homeBase,omitempty" validate:"len=1"`

	// Singular edge ([]*Entity with validate len=1).
	ParentCompany []*Country `json:"parentCompany,omitempty" validate:"len=1"`

	// Multi-edge — generates slice getter/setter + append/remove helpers.
	Films []*Film `json:"films,omitempty"`

	// Pointer-slice edge ([]*Entity) — tests parser accepts []*Entity.
	Advisors []*Director `json:"advisors,omitempty"`

	// Primitive slices.
	Tags       []string    `json:"tags,omitempty"`
	Scores     []int       `json:"scores,omitempty"`
	Weights    []float64   `json:"weights,omitempty"`
	Flags      []bool      `json:"flags,omitempty"`
	Milestones []time.Time `json:"milestones,omitempty"`

	// Int field — tests non-string CLI flag support.
	YearFounded int `json:"yearFounded,omitempty" validate:"gte=1800,lte=2100"`

	// Float field — tests float CLI flag support.
	Revenue float64 `json:"revenue,omitempty" validate:"gte=0"`

	// Bool field.
	Active bool `json:"active,omitempty"`

	// time.Time field.
	CreatedAt time.Time `json:"createdAt,omitempty"`

	// Vector field.
	Embedding *dg.VectorFloat32 `json:"embedding,omitempty" dgraph:"index=hnsw(metric:cosine)"`

	// Opted-out field (dgraph:"-") — skipped entirely.
	Internal string `json:"internal,omitempty" dgraph:"-"`
}
