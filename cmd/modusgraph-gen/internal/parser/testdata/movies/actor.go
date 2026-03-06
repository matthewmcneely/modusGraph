package movies

type Actor struct {
	UID   string        `json:"uid,omitempty"`
	DType []string      `json:"dgraph.type,omitempty"`
	Name  string        `json:"name,omitempty" dgraph:"index=hash,term,trigram,fulltext"`
	Films []Performance `json:"films,omitempty" dgraph:"predicate=actor.film count"`
}
