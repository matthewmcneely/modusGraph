package movies

type Performance struct {
	UID           string   `json:"uid,omitempty"`
	DType         []string `json:"dgraph.type,omitempty"`
	CharacterNote string   `json:"characterNote,omitempty" dgraph:"predicate=performance.character_note"`
}
