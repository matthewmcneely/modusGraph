// Package model defines the intermediate representation used between the parser
// and the code generator. The parser populates these types from Go struct ASTs;
// the generator reads them to emit typed client code.
package model

// Package represents the fully parsed target package and all its entities.
type Package struct {
	Name          string            // Go package name, e.g. "movies"
	ModulePath    string            // Full module path, e.g. "github.com/mlwelles/modusGraphMoviesProject"
	Imports       map[string]string // Package alias → import path, e.g. "enums" → "github.com/.../enums"
	Entities      []Entity          // All detected entities (structs with UID + DType)
	CLIName       string            // Name for CLI binary (kong.Name), defaults to Name if empty
	WithValidator bool              // Whether the generated CLI enables struct validation
}

// Entity represents a single Dgraph type derived from a Go struct.
type Entity struct {
	Name        string  // Go struct name, e.g. "Film"
	Fields      []Field // All exported fields from the struct
	Searchable  bool    // True if the entity has a string field with index=fulltext
	SearchField string  // Name of the field with fulltext index (empty if not searchable)
}

// Field represents a single exported field within an entity struct.
type Field struct {
	Name       string   // Go field name, e.g. "InitialReleaseDate"
	GoType     string   // Go type as string, e.g. "time.Time", "string", "[]Genre"
	JSONTag    string   // Value from the json struct tag, e.g. "initialReleaseDate"
	Predicate  string   // Resolved Dgraph predicate name
	IsEdge     bool     // True if the field type is a slice of another entity
	EdgeEntity string   // Target entity name for edge fields, e.g. "Genre"
	IsReverse  bool     // True if dgraph tag contains "reverse" or predicate starts with "~"
	HasCount   bool     // True if dgraph tag contains "count"
	Indexes    []string // Parsed index directives, e.g. ["hash", "term", "trigram", "fulltext"]
	TypeHint   string   // Value from dgraph "type=" directive, e.g. "geo", "datetime"
	IsUID      bool     // True if the field represents the UID
	IsDType    bool     // True if the field represents the DType (dgraph.type)
	OmitEmpty  bool     // True if json tag contains ",omitempty"
	Upsert     bool     // True if dgraph tag contains "upsert"
}
