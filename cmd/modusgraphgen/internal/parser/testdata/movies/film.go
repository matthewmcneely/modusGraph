package movies

import "time"

type Film struct {
	UID                string          `json:"uid,omitempty"`
	DType              []string        `json:"dgraph.type,omitempty"`
	Name               string          `json:"name,omitempty" dgraph:"index=hash,term,trigram,fulltext"`
	InitialReleaseDate time.Time       `json:"initialReleaseDate,omitempty" dgraph:"predicate=initial_release_date index=year"`
	Tagline            string          `json:"tagline,omitempty"`
	Genres             []Genre         `json:"genres,omitempty" dgraph:"predicate=genre reverse count"`
	Countries          []Country       `json:"countries,omitempty" dgraph:"predicate=country reverse"`
	Ratings            []Rating        `json:"ratings,omitempty" dgraph:"predicate=rating reverse"`
	ContentRatings     []ContentRating `json:"contentRatings,omitempty" dgraph:"predicate=rated reverse"`
	Starring           []Performance   `json:"starring,omitempty" dgraph:"count"`
}
