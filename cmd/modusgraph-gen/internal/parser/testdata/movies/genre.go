package movies

type Genre struct {
	UID   string   `json:"uid,omitempty"`
	DType []string `json:"dgraph.type,omitempty"`
	Name  string   `json:"name,omitempty" dgraph:"index=hash,term,trigram,fulltext"`
	Films []Film   `json:"films,omitempty" dgraph:"predicate=~genre reverse"`
}
