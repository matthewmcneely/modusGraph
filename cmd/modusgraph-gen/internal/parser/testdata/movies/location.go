package movies

type Location struct {
	UID   string    `json:"uid,omitempty"`
	DType []string  `json:"dgraph.type,omitempty"`
	Name  string    `json:"name,omitempty" dgraph:"index=hash,term,trigram,fulltext"`
	Loc   []float64 `json:"loc,omitempty" dgraph:"index=geo type=geo"`
	Email string    `json:"email,omitempty" dgraph:"index=exact upsert"`
}
