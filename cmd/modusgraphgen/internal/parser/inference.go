package parser

import (
	"github.com/matthewmcneely/modusgraph/cmd/modusgraphgen/internal/model"
)

// applyInference applies higher-level inference rules to an entity after its
// fields have been parsed. This includes detecting searchability, determining
// which fields support year-range filters, and so on.
//
// Inference rules:
//
//   - Searchable: An entity is searchable if it has a string field with
//     "fulltext" in its index list. The SearchField is set to that field's name.
//
//   - Relationships (edges): Already detected during struct parsing based on
//     whether the field type is []OtherEntity.
//
//   - Reverse edges: Already detected during tag parsing. A field is a reverse
//     edge if its predicate starts with "~" or the dgraph tag contains "reverse".
//
//   - Year-filterable: A field with index=year (present in Indexes) and GoType
//     containing "time.Time" can be filtered by year range. This is recorded in
//     the field's Indexes and TypeHint for the generator to use.
//
//   - Hash-filterable: A field with index=hash supports exact-match lookups.
func applyInference(entity *model.Entity) {
	for _, f := range entity.Fields {
		if f.IsUID || f.IsDType {
			continue
		}
		// Searchable: string field with fulltext index.
		if isStringType(f.GoType) && hasIndex(f.Indexes, "fulltext") {
			entity.Searchable = true
			entity.SearchField = f.Name
			break // Use the first one found.
		}
	}
}

// isStringType returns true if the Go type represents a string.
func isStringType(goType string) bool {
	return goType == "string"
}

// hasIndex returns true if the given index name appears in the index list.
func hasIndex(indexes []string, name string) bool {
	for _, idx := range indexes {
		if idx == name {
			return true
		}
	}
	return false
}
