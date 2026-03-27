package parser

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/matthewmcneely/modusgraph/cmd/modusgraph-gen/internal/model"
)

func moviesDir(t *testing.T) string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	return filepath.Join(filepath.Dir(thisFile), "testdata", "movies")
}

func TestParseMoviesPackage(t *testing.T) {
	dir := moviesDir(t)
	pkg, err := Parse(dir)
	if err != nil {
		t.Fatalf("Parse(%s) failed: %v", dir, err)
	}

	if pkg.Name != "movies" {
		t.Errorf("package name = %q, want %q", pkg.Name, "movies")
	}

	// Build a map for easy lookup.
	entityMap := make(map[string]*model.Entity, len(pkg.Entities))
	for i := range pkg.Entities {
		entityMap[pkg.Entities[i].Name] = &pkg.Entities[i]
	}

	t.Run("AllEntitiesDetected", func(t *testing.T) {
		expected := []string{
			"Film", "Director", "Actor", "Performance",
			"Genre", "Country", "Rating", "ContentRating", "Location",
			"Studio",
		}
		for _, name := range expected {
			if _, ok := entityMap[name]; !ok {
				t.Errorf("entity %q not found; detected entities: %v", name, entityNames(pkg.Entities))
			}
		}
		if len(pkg.Entities) != len(expected) {
			t.Errorf("got %d entities, want %d; detected: %v", len(pkg.Entities), len(expected), entityNames(pkg.Entities))
		}
	})

	t.Run("FilmSearchable", func(t *testing.T) {
		film := entityMap["Film"]
		if film == nil {
			t.Fatal("Film entity not found")
		}
		if !film.Searchable {
			t.Error("Film should be searchable")
		}
		if film.SearchField != "Name" {
			t.Errorf("Film.SearchField = %q, want %q", film.SearchField, "Name")
		}
	})

	t.Run("FilmInitialReleaseDate", func(t *testing.T) {
		film := entityMap["Film"]
		if film == nil {
			t.Fatal("Film entity not found")
		}
		f := findField(film.Fields, "InitialReleaseDate")
		if f == nil {
			t.Fatal("Film.InitialReleaseDate field not found")
		}
		if f.Predicate != "initial_release_date" {
			t.Errorf("predicate = %q, want %q", f.Predicate, "initial_release_date")
		}
		if !hasIndex(f.Indexes, "year") {
			t.Errorf("indexes = %v, want to contain %q", f.Indexes, "year")
		}
		if f.GoType != "time.Time" {
			t.Errorf("GoType = %q, want %q", f.GoType, "time.Time")
		}
	})

	t.Run("FilmGenresEdge", func(t *testing.T) {
		film := entityMap["Film"]
		if film == nil {
			t.Fatal("Film entity not found")
		}
		f := findField(film.Fields, "Genres")
		if f == nil {
			t.Fatal("Film.Genres field not found")
		}
		if !f.IsEdge {
			t.Error("Genres should be an edge")
		}
		if f.EdgeEntity != "Genre" {
			t.Errorf("EdgeEntity = %q, want %q", f.EdgeEntity, "Genre")
		}
		if f.Predicate != "genre" {
			t.Errorf("predicate = %q, want %q", f.Predicate, "genre")
		}
		if !f.IsReverse {
			t.Error("Genres should have reverse flag set")
		}
		if !f.HasCount {
			t.Error("Genres should have count flag set")
		}
	})

	t.Run("DirectorFilmsPredicate", func(t *testing.T) {
		dir := entityMap["Director"]
		if dir == nil {
			t.Fatal("Director entity not found")
		}
		f := findField(dir.Fields, "Films")
		if f == nil {
			t.Fatal("Director.Films field not found")
		}
		if f.Predicate != "director.film" {
			t.Errorf("predicate = %q, want %q", f.Predicate, "director.film")
		}
		if !f.IsEdge {
			t.Error("Director.Films should be an edge")
		}
		if f.EdgeEntity != "Film" {
			t.Errorf("EdgeEntity = %q, want %q", f.EdgeEntity, "Film")
		}
		if !f.IsReverse {
			t.Error("Director.Films should have reverse flag set")
		}
		if !f.HasCount {
			t.Error("Director.Films should have count flag set")
		}
	})

	t.Run("GenreFilmsReverse", func(t *testing.T) {
		genre := entityMap["Genre"]
		if genre == nil {
			t.Fatal("Genre entity not found")
		}
		f := findField(genre.Fields, "Films")
		if f == nil {
			t.Fatal("Genre.Films field not found")
		}
		if f.Predicate != "~genre" {
			t.Errorf("predicate = %q, want %q", f.Predicate, "~genre")
		}
		if !f.IsReverse {
			t.Error("Genre.Films should be a reverse edge")
		}
		if !f.IsEdge {
			t.Error("Genre.Films should be an edge")
		}
	})

	t.Run("ActorFilmsPredicate", func(t *testing.T) {
		actor := entityMap["Actor"]
		if actor == nil {
			t.Fatal("Actor entity not found")
		}
		f := findField(actor.Fields, "Films")
		if f == nil {
			t.Fatal("Actor.Films field not found")
		}
		if f.Predicate != "actor.film" {
			t.Errorf("predicate = %q, want %q", f.Predicate, "actor.film")
		}
		if !f.IsEdge {
			t.Error("Actor.Films should be an edge")
		}
		if f.EdgeEntity != "Performance" {
			t.Errorf("EdgeEntity = %q, want %q", f.EdgeEntity, "Performance")
		}
		if !f.HasCount {
			t.Error("Actor.Films should have count flag set")
		}
	})

	t.Run("PerformanceCharacterNote", func(t *testing.T) {
		perf := entityMap["Performance"]
		if perf == nil {
			t.Fatal("Performance entity not found")
		}
		f := findField(perf.Fields, "CharacterNote")
		if f == nil {
			t.Fatal("Performance.CharacterNote field not found")
		}
		if f.Predicate != "performance.character_note" {
			t.Errorf("predicate = %q, want %q", f.Predicate, "performance.character_note")
		}
	})

	t.Run("LocationGeoIndex", func(t *testing.T) {
		loc := entityMap["Location"]
		if loc == nil {
			t.Fatal("Location entity not found")
		}
		f := findField(loc.Fields, "Loc")
		if f == nil {
			t.Fatal("Location.Loc field not found")
		}
		if !hasIndex(f.Indexes, "geo") {
			t.Errorf("indexes = %v, want to contain %q", f.Indexes, "geo")
		}
		if f.TypeHint != "geo" {
			t.Errorf("TypeHint = %q, want %q", f.TypeHint, "geo")
		}
	})

	t.Run("LocationEmailUpsert", func(t *testing.T) {
		loc := entityMap["Location"]
		if loc == nil {
			t.Fatal("Location entity not found")
		}
		f := findField(loc.Fields, "Email")
		if f == nil {
			t.Fatal("Location.Email field not found")
		}
		if !f.Upsert {
			t.Error("Email should have upsert flag set")
		}
		if !hasIndex(f.Indexes, "exact") {
			t.Errorf("indexes = %v, want to contain %q", f.Indexes, "exact")
		}
	})

	t.Run("ContentRatingReverse", func(t *testing.T) {
		cr := entityMap["ContentRating"]
		if cr == nil {
			t.Fatal("ContentRating entity not found")
		}
		f := findField(cr.Fields, "Films")
		if f == nil {
			t.Fatal("ContentRating.Films field not found")
		}
		if f.Predicate != "~rated" {
			t.Errorf("predicate = %q, want %q", f.Predicate, "~rated")
		}
		if !f.IsReverse {
			t.Error("ContentRating.Films should be a reverse edge")
		}
	})

	t.Run("StudioPrivateFields", func(t *testing.T) {
		studio := entityMap["Studio"]
		if studio == nil {
			t.Fatal("Studio entity not found")
		}

		// Private scalar: name
		name := findField(studio.Fields, "name")
		if name == nil {
			t.Fatal("Studio.name field not found")
		}
		if !name.IsPrivate {
			t.Error("Studio.name should be private")
		}
		if name.GoType != "string" {
			t.Errorf("Studio.name GoType = %q, want %q", name.GoType, "string")
		}

		// Private singular edge: founder (*Director)
		founder := findField(studio.Fields, "founder")
		if founder == nil {
			t.Fatal("Studio.founder field not found")
		}
		if !founder.IsPrivate {
			t.Error("Studio.founder should be private")
		}
		if !founder.IsEdge {
			t.Error("Studio.founder should be an edge")
		}
		if founder.EdgeEntity != "Director" {
			t.Errorf("Studio.founder EdgeEntity = %q, want %q", founder.EdgeEntity, "Director")
		}
		if !founder.IsSingularEdge {
			t.Error("Studio.founder should be a singular edge (*Director)")
		}

		// Private singular edge via validate:"max=1": currentHead
		currentHead := findField(studio.Fields, "currentHead")
		if currentHead == nil {
			t.Fatal("Studio.currentHead field not found")
		}
		if !currentHead.IsSingularEdge {
			t.Error("Studio.currentHead should be a singular edge (validate:\"max=1\")")
		}
		if !currentHead.IsEdge {
			t.Error("Studio.currentHead should be an edge")
		}

		// Private multi-edge: films
		films := findField(studio.Fields, "films")
		if films == nil {
			t.Fatal("Studio.films field not found")
		}
		if !films.IsPrivate {
			t.Error("Studio.films should be private")
		}
		if !films.IsEdge {
			t.Error("Studio.films should be an edge")
		}
		if films.IsSingularEdge {
			t.Error("Studio.films should NOT be a singular edge")
		}

		// Private primitive slice: tags
		tags := findField(studio.Fields, "tags")
		if tags == nil {
			t.Fatal("Studio.tags field not found")
		}
		if !tags.IsPrivate {
			t.Error("Studio.tags should be private")
		}
		if tags.IsEdge {
			t.Error("Studio.tags should NOT be an edge (primitive slice)")
		}

		// Exported field: Founded
		founded := findField(studio.Fields, "Founded")
		if founded == nil {
			t.Fatal("Studio.Founded field not found")
		}
		if founded.IsPrivate {
			t.Error("Studio.Founded should NOT be private")
		}

		// Opted-out field: Internal (dgraph:"-") — should NOT be in fields
		internal := findField(studio.Fields, "Internal")
		if internal != nil {
			t.Error("Studio.Internal should be skipped (dgraph:\"-\")")
		}

		// No json tag field: tempCache — should NOT be in fields
		tempCache := findField(studio.Fields, "tempCache")
		if tempCache != nil {
			t.Error("Studio.tempCache should be skipped (no json tag)")
		}

		// Pointer-slice edge: advisors []*Director — should be detected as edge
		advisors := findField(studio.Fields, "advisors")
		if advisors == nil {
			t.Fatal("Studio.advisors field not found")
		}
		if !advisors.IsEdge {
			t.Error("Studio.advisors should be an edge ([]*Director)")
		}
		if advisors.EdgeEntity != "Director" {
			t.Errorf("Studio.advisors EdgeEntity = %q, want %q", advisors.EdgeEntity, "Director")
		}
		if advisors.IsSingularEdge {
			t.Error("Studio.advisors should NOT be a singular edge")
		}
	})

	// Comprehensive edge field detection tests covering all combinations:
	// []Entity (public), []*Entity (public), []Entity (private), []*Entity (private),
	// *Entity (private singular), []Entity+validate:"max=1" (private singular)
	t.Run("EdgeFieldVariants", func(t *testing.T) {
		// Film: Genres []Genre (public, []Entity)
		film := entityMap["Film"]
		if film == nil {
			t.Fatal("Film entity not found")
		}
		genres := findField(film.Fields, "Genres")
		if genres == nil {
			t.Fatal("Film.Genres not found")
		}
		if !genres.IsEdge || genres.EdgeEntity != "Genre" || genres.IsPrivate || genres.IsSingularEdge {
			t.Errorf("Film.Genres: IsEdge=%v EdgeEntity=%q IsPrivate=%v IsSingularEdge=%v; want edge to Genre, public, multi",
				genres.IsEdge, genres.EdgeEntity, genres.IsPrivate, genres.IsSingularEdge)
		}

		// Film: Directors []*Director (public, []*Entity)
		directors := findField(film.Fields, "Directors")
		if directors == nil {
			t.Fatal("Film.Directors not found")
		}
		if !directors.IsEdge || directors.EdgeEntity != "Director" || directors.IsPrivate || directors.IsSingularEdge {
			t.Errorf("Film.Directors: IsEdge=%v EdgeEntity=%q IsPrivate=%v IsSingularEdge=%v; want edge to Director, public, multi",
				directors.IsEdge, directors.EdgeEntity, directors.IsPrivate, directors.IsSingularEdge)
		}

		// Studio: films []Film (private, []Entity)
		studio := entityMap["Studio"]
		if studio == nil {
			t.Fatal("Studio entity not found")
		}
		films := findField(studio.Fields, "films")
		if films == nil {
			t.Fatal("Studio.films not found")
		}
		if !films.IsEdge || films.EdgeEntity != "Film" || !films.IsPrivate || films.IsSingularEdge {
			t.Errorf("Studio.films: IsEdge=%v EdgeEntity=%q IsPrivate=%v IsSingularEdge=%v; want edge to Film, private, multi",
				films.IsEdge, films.EdgeEntity, films.IsPrivate, films.IsSingularEdge)
		}

		// Studio: advisors []*Director (private, []*Entity)
		advisors := findField(studio.Fields, "advisors")
		if advisors == nil {
			t.Fatal("Studio.advisors not found")
		}
		if !advisors.IsEdge || advisors.EdgeEntity != "Director" || !advisors.IsPrivate || advisors.IsSingularEdge {
			t.Errorf("Studio.advisors: IsEdge=%v EdgeEntity=%q IsPrivate=%v IsSingularEdge=%v; want edge to Director, private, multi",
				advisors.IsEdge, advisors.EdgeEntity, advisors.IsPrivate, advisors.IsSingularEdge)
		}

		// Studio: headquarters Country (private, bare Entity singular)
		hq := findField(studio.Fields, "headquarters")
		if hq == nil {
			t.Fatal("Studio.headquarters not found")
		}
		if !hq.IsEdge || hq.EdgeEntity != "Country" || !hq.IsPrivate || !hq.IsSingularEdge {
			t.Errorf("Studio.headquarters: IsEdge=%v EdgeEntity=%q IsPrivate=%v IsSingularEdge=%v; want edge to Country, private, singular",
				hq.IsEdge, hq.EdgeEntity, hq.IsPrivate, hq.IsSingularEdge)
		}
		if hq.GoType != "Country" {
			t.Errorf("Studio.headquarters GoType = %q, want %q", hq.GoType, "Country")
		}

		// Studio: founder *Director (private, *Entity singular)
		founder := findField(studio.Fields, "founder")
		if founder == nil {
			t.Fatal("Studio.founder not found")
		}
		if !founder.IsEdge || founder.EdgeEntity != "Director" || !founder.IsPrivate || !founder.IsSingularEdge {
			t.Errorf("Studio.founder: IsEdge=%v EdgeEntity=%q IsPrivate=%v IsSingularEdge=%v; want edge to Director, private, singular",
				founder.IsEdge, founder.EdgeEntity, founder.IsPrivate, founder.IsSingularEdge)
		}

		// Studio: currentHead []Director+validate:"max=1" (private, singular via validate)
		currentHead := findField(studio.Fields, "currentHead")
		if currentHead == nil {
			t.Fatal("Studio.currentHead not found")
		}
		if !currentHead.IsEdge || currentHead.EdgeEntity != "Director" || !currentHead.IsPrivate || !currentHead.IsSingularEdge {
			t.Errorf("Studio.currentHead: IsEdge=%v EdgeEntity=%q IsPrivate=%v IsSingularEdge=%v; want edge to Director, private, singular",
				currentHead.IsEdge, currentHead.EdgeEntity, currentHead.IsPrivate, currentHead.IsSingularEdge)
		}

		// Studio: ceo []*Director+validate:"max=1" (private, []*Entity singular via validate)
		ceo := findField(studio.Fields, "ceo")
		if ceo == nil {
			t.Fatal("Studio.ceo not found")
		}
		if !ceo.IsEdge || ceo.EdgeEntity != "Director" || !ceo.IsPrivate || !ceo.IsSingularEdge {
			t.Errorf("Studio.ceo: IsEdge=%v EdgeEntity=%q IsPrivate=%v IsSingularEdge=%v; want edge to Director, private, singular",
				ceo.IsEdge, ceo.EdgeEntity, ceo.IsPrivate, ceo.IsSingularEdge)
		}

		// Studio: homeBase []Country+validate:"len=1" (private, []Entity singular via len=1)
		homeBase := findField(studio.Fields, "homeBase")
		if homeBase == nil {
			t.Fatal("Studio.homeBase not found")
		}
		if !homeBase.IsEdge || homeBase.EdgeEntity != "Country" || !homeBase.IsPrivate || !homeBase.IsSingularEdge {
			t.Errorf("Studio.homeBase: IsEdge=%v EdgeEntity=%q IsPrivate=%v IsSingularEdge=%v; want edge to Country, private, singular",
				homeBase.IsEdge, homeBase.EdgeEntity, homeBase.IsPrivate, homeBase.IsSingularEdge)
		}

		// Studio: parentCompany []*Country+validate:"len=1" (private, []*Entity singular via len=1)
		parentCompany := findField(studio.Fields, "parentCompany")
		if parentCompany == nil {
			t.Fatal("Studio.parentCompany not found")
		}
		if !parentCompany.IsEdge || parentCompany.EdgeEntity != "Country" || !parentCompany.IsPrivate || !parentCompany.IsSingularEdge {
			t.Errorf("Studio.parentCompany: IsEdge=%v EdgeEntity=%q IsPrivate=%v IsSingularEdge=%v; want edge to Country, private, singular",
				parentCompany.IsEdge, parentCompany.EdgeEntity, parentCompany.IsPrivate, parentCompany.IsSingularEdge)
		}
	})

	t.Run("AllEntitiesSearchable", func(t *testing.T) {
		// These entities should be searchable (have Name with fulltext index):
		// Film, Director, Actor, Genre, Country, Rating, ContentRating, Location
		searchable := []string{"Film", "Director", "Actor", "Genre", "Country", "Rating", "ContentRating", "Location"}
		for _, name := range searchable {
			e := entityMap[name]
			if e == nil {
				t.Errorf("entity %q not found", name)
				continue
			}
			if !e.Searchable {
				t.Errorf("entity %q should be searchable", name)
			}
			if e.SearchField != "Name" {
				t.Errorf("entity %q SearchField = %q, want %q", name, e.SearchField, "Name")
			}
		}
		// Performance should NOT be searchable (no Name field with fulltext).
		perf := entityMap["Performance"]
		if perf != nil && perf.Searchable {
			t.Error("Performance should NOT be searchable")
		}
	})
}

func TestParseDgraphTag(t *testing.T) {
	tests := []struct {
		name     string
		tag      string
		expected model.Field
	}{
		{
			name: "index only",
			tag:  "index=hash,term,trigram,fulltext",
			expected: model.Field{
				Indexes: []string{"hash", "term", "trigram", "fulltext"},
			},
		},
		{
			name: "predicate with space-separated index",
			tag:  "predicate=initial_release_date index=year",
			expected: model.Field{
				Predicate: "initial_release_date",
				Indexes:   []string{"year"},
			},
		},
		{
			name: "predicate with reverse and count",
			tag:  "predicate=genre,reverse,count",
			expected: model.Field{
				Predicate: "genre",
				IsReverse: true,
				HasCount:  true,
			},
		},
		{
			name: "count only",
			tag:  "count",
			expected: model.Field{
				HasCount: true,
			},
		},
		{
			name: "index with type hint",
			tag:  "index=geo,type=geo",
			expected: model.Field{
				Indexes:  []string{"geo"},
				TypeHint: "geo",
			},
		},
		{
			name: "index with upsert",
			tag:  "index=exact,upsert",
			expected: model.Field{
				Indexes: []string{"exact"},
				Upsert:  true,
			},
		},
		{
			name: "tilde predicate",
			tag:  "predicate=~genre",
			expected: model.Field{
				Predicate: "~genre",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var f model.Field
			parseDgraphTag(tt.tag, &f)

			if f.Predicate != tt.expected.Predicate {
				t.Errorf("Predicate = %q, want %q", f.Predicate, tt.expected.Predicate)
			}
			if f.IsReverse != tt.expected.IsReverse {
				t.Errorf("IsReverse = %v, want %v", f.IsReverse, tt.expected.IsReverse)
			}
			if f.HasCount != tt.expected.HasCount {
				t.Errorf("HasCount = %v, want %v", f.HasCount, tt.expected.HasCount)
			}
			if f.Upsert != tt.expected.Upsert {
				t.Errorf("Upsert = %v, want %v", f.Upsert, tt.expected.Upsert)
			}
			if f.TypeHint != tt.expected.TypeHint {
				t.Errorf("TypeHint = %q, want %q", f.TypeHint, tt.expected.TypeHint)
			}
			if len(f.Indexes) != len(tt.expected.Indexes) {
				t.Errorf("Indexes = %v, want %v", f.Indexes, tt.expected.Indexes)
			} else {
				for i := range f.Indexes {
					if f.Indexes[i] != tt.expected.Indexes[i] {
						t.Errorf("Indexes[%d] = %q, want %q", i, f.Indexes[i], tt.expected.Indexes[i])
					}
				}
			}
		})
	}
}

func TestParseValidateTag(t *testing.T) {
	tests := []struct {
		name           string
		tag            string
		wantSingular   bool
	}{
		{"max=1", "max=1", true},
		{"len=1", "len=1", true},
		{"required,max=1", "required,max=1", true},
		{"min=0,max=1", "min=0,max=1", true},
		{"required,len=1", "required,len=1", true},
		{"max=10", "max=10", false},
		{"required", "required", false},
		{"min=2,max=100", "min=2,max=100", false},
		{"empty", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var f model.Field
			parseValidateTag(tt.tag, &f)
			if f.IsSingularEdge != tt.wantSingular {
				t.Errorf("parseValidateTag(%q): IsSingularEdge = %v, want %v", tt.tag, f.IsSingularEdge, tt.wantSingular)
			}
		})
	}
}

func TestParseDgraphTagSkip(t *testing.T) {
	var f model.Field
	parseDgraphTag("-", &f)
	if !f.IsSkipped {
		t.Error("parseDgraphTag(\"-\"): IsSkipped should be true")
	}
	// Ensure no other fields were set.
	if f.Predicate != "" {
		t.Errorf("Predicate should be empty, got %q", f.Predicate)
	}
}

func TestParseMultiNameDeclaration(t *testing.T) {
	// Verify that parseStruct handles "A, B Type" declarations
	// by generating fields for each name. Note: in real structs with
	// tags, multi-name declarations share a single tag — but the parser
	// should still emit a Field for each name.
	src := `package test

type MultiName struct {
	UID   string   ` + "`json:\"uid,omitempty\"`" + `
	DType []string ` + "`json:\"dgraph.type,omitempty\"`" + `
	A, B  string
}
`
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "test.go", src, parser.AllErrors)
	if err != nil {
		t.Fatal(err)
	}

	var entity model.Entity
	found := false
	for _, decl := range file.Decls {
		genDecl, ok := decl.(*ast.GenDecl)
		if !ok {
			continue
		}
		for _, spec := range genDecl.Specs {
			typeSpec, ok := spec.(*ast.TypeSpec)
			if !ok {
				continue
			}
			st, ok := typeSpec.Type.(*ast.StructType)
			if !ok {
				continue
			}
			entity, found = parseStruct(typeSpec.Name.Name, st, map[string]bool{})
			break
		}
	}

	if !found {
		t.Fatal("MultiName struct not detected as entity")
	}

	// A and B have no json tag so they should be skipped.
	// Only UID and DType should be in fields.
	// This verifies the multi-name loop runs without error.
	for _, f := range entity.Fields {
		if f.Name == "A" || f.Name == "B" {
			t.Errorf("Field %q should be skipped (no json tag)", f.Name)
		}
	}
}

func TestReadModulePath(t *testing.T) {
	t.Run("FromMoviesProject", func(t *testing.T) {
		dir := moviesDir(t)
		got := readModulePath(dir)
		want := "github.com/mlwelles/modusGraphMoviesProject"
		if got != want {
			t.Errorf("readModulePath(%s) = %q, want %q", dir, got, want)
		}
	})

	t.Run("FromModusGraph", func(t *testing.T) {
		_, thisFile, _, _ := runtime.Caller(0)
		dir := filepath.Dir(thisFile)
		got := readModulePath(dir)
		want := "github.com/matthewmcneely/modusgraph"
		if got != want {
			t.Errorf("readModulePath(%s) = %q, want %q", dir, got, want)
		}
	})

	t.Run("EmptyForNonExistentDir", func(t *testing.T) {
		got := readModulePath(filepath.Join(t.TempDir(), "nonexistent-subdir"))
		if got != "" {
			t.Errorf("readModulePath(nonexistent) = %q, want empty string", got)
		}
	})

	t.Run("FromTempGoMod", func(t *testing.T) {
		dir := t.TempDir()
		gomod := filepath.Join(dir, "go.mod")
		if err := os.WriteFile(gomod, []byte("module example.com/test-project\n\ngo 1.21\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		got := readModulePath(dir)
		want := "example.com/test-project"
		if got != want {
			t.Errorf("readModulePath(%s) = %q, want %q", dir, got, want)
		}
	})

	t.Run("WalksUpToParent", func(t *testing.T) {
		dir := t.TempDir()
		gomod := filepath.Join(dir, "go.mod")
		if err := os.WriteFile(gomod, []byte("module example.com/parent-project\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		subdir := filepath.Join(dir, "sub", "package")
		if err := os.MkdirAll(subdir, 0o755); err != nil {
			t.Fatal(err)
		}
		got := readModulePath(subdir)
		want := "example.com/parent-project"
		if got != want {
			t.Errorf("readModulePath(%s) = %q, want %q", subdir, got, want)
		}
	})
}

func TestCollectImports(t *testing.T) {
	dir := moviesDir(t)
	pkg, err := Parse(dir)
	if err != nil {
		t.Fatalf("Parse(%s) failed: %v", dir, err)
	}

	// The movies project imports "time" in film.go, which should appear
	// in the imports map.
	if path, ok := pkg.Imports["time"]; !ok {
		t.Error("expected 'time' in imports map")
	} else if path != "time" {
		t.Errorf("imports[time] = %q, want %q", path, "time")
	}
}

func TestModulePathPopulated(t *testing.T) {
	dir := moviesDir(t)
	pkg, err := Parse(dir)
	if err != nil {
		t.Fatalf("Parse(%s) failed: %v", dir, err)
	}

	if pkg.ModulePath != "github.com/mlwelles/modusGraphMoviesProject" {
		t.Errorf("ModulePath = %q, want %q", pkg.ModulePath, "github.com/mlwelles/modusGraphMoviesProject")
	}
}

// findField returns the field with the given name, or nil if not found.
func findField(fields []model.Field, name string) *model.Field {
	for i := range fields {
		if fields[i].Name == name {
			return &fields[i]
		}
	}
	return nil
}

// entityNames returns the names of all entities for diagnostic output.
func entityNames(entities []model.Entity) []string {
	names := make([]string, len(entities))
	for i, e := range entities {
		names[i] = e.Name
	}
	return names
}
