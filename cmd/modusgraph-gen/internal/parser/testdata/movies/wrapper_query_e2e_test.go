// Package movies_test provides end-to-end tests that exercise the generated
// wrapper-side query builder (movies.FilmQuery) against a real, local
// file-backed modusgraph client. The text-generation tests in the parser
// suite prove the wrapper query code is emitted; these tests prove it
// actually runs — that FilmClient.Query routes the fluent chain through
// typed.Query, that terminals wrap their results, and that the wrapper layer
// adds no extra round-trips. Like unwrap_e2e_test.go, this file lives inside
// the testdata tree because the generated package imports modusgraph, which
// would cause an import cycle from the root test package.
//
// None of these tests call t.Parallel(): the modusgraph engine is a strict
// process-wide singleton (only one client may exist at a time), so the tests
// must run sequentially. Each test gets its own t.TempDir()-backed client that
// t.Cleanup closes before the next test starts.
package movies_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/go-logr/logr/funcr"
	modusgraph "github.com/matthewmcneely/modusgraph"
	movies "github.com/matthewmcneely/modusgraph/cmd/modusgraph-gen/internal/parser/testdata/movies"
	moviesSchema "github.com/matthewmcneely/modusgraph/cmd/modusgraph-gen/internal/parser/testdata/movies/schema"
)

// newConn builds a local file-backed modusgraph client for a test. Each call
// uses a fresh t.TempDir(); the client is closed via t.Cleanup. Because the
// modusgraph engine is a process-wide singleton, only one such client may be
// live at a time — tests using newConn must run sequentially (no t.Parallel).
func newConn(t *testing.T) modusgraph.Client {
	t.Helper()
	conn, err := modusgraph.NewClient("file://"+t.TempDir(), modusgraph.WithAutoSchema(true))
	if err != nil {
		t.Fatalf("modusgraph.NewClient: %v", err)
	}
	t.Cleanup(conn.Close)
	return conn
}

// newCountingConn builds a file-backed modusgraph client like newConn but
// wires in a logr.Logger that counts dgman query executions. dgman logs every
// executed query at verbosity 3 with the message "execute query"; the returned
// *int is incremented once per such log line.
//
// dgman's logger is process-global, so tests that use newCountingConn must NOT
// call t.Parallel(): a parallel test sharing the global logger would corrupt
// the count.
func newCountingConn(t *testing.T, count *int) modusgraph.Client {
	t.Helper()
	logger := funcr.New(func(_, args string) {
		// funcr renders the message into args as `"msg"="execute query"`.
		// Match that exact pair so unrelated dgman/pool log lines (which log
		// other messages) are not counted.
		if strings.Contains(args, `"msg"="execute query"`) {
			*count++
		}
	}, funcr.Options{Verbosity: 3})
	conn, err := modusgraph.NewClient("file://"+t.TempDir(),
		modusgraph.WithAutoSchema(true), modusgraph.WithLogger(logger))
	if err != nil {
		t.Fatalf("modusgraph.NewClient: %v", err)
	}
	t.Cleanup(conn.Close)
	return conn
}

// addFilm builds a Film wrapper via the generated option constructors and
// inserts it through the generated FilmClient. Film is the entity under test:
// its Name field carries dgraph:"index=hash,..." (so eq(name, ...) filters
// work) and its InitialReleaseDate field is a time.Time with dgraph index=year
// stored under predicate initial_release_date (so orderasc on that predicate
// works) — two distinct fields, one for filtering and one for ordering.
func addFilm(ctx context.Context, t *testing.T, client *movies.Client, name string, year int) {
	t.Helper()
	w := movies.NewFilm(
		movies.WithFilmName(name),
		movies.WithFilmInitialReleaseDate(time.Date(year, 1, 1, 0, 0, 0, 0, time.UTC)),
	)
	if err := client.Film.Add(ctx, w); err != nil {
		t.Fatalf("Film.Add(%q): %v", name, err)
	}
}

// TestWrapperQuery_NodesWrapsResults inserts three Film records and verifies
// FilmQuery.Nodes returns three non-nil *movies.Film wrappers whose accessors
// read back the inserted data. This proves Nodes wraps each schema result
// rather than merely counting rows.
func TestWrapperQuery_NodesWrapsResults(t *testing.T) {
	ctx := context.Background()
	client := movies.NewClient(newConn(t))

	want := map[string]int{
		"Toy Story":  1995,
		"Metropolis": 1927,
		"Moonrise":   2012,
	}
	for name, year := range want {
		addFilm(ctx, t, client, name, year)
	}

	got, err := client.Film.Query(ctx).Nodes()
	if err != nil {
		t.Fatalf("Nodes: %v", err)
	}
	if len(got) != len(want) {
		t.Fatalf("Nodes returned %d wrappers, want %d", len(got), len(want))
	}
	for i, w := range got {
		if w == nil {
			t.Fatalf("Nodes result [%d] is a nil *movies.Film", i)
		}
		wantYear, ok := want[w.Name()]
		if !ok {
			t.Fatalf("Nodes returned unexpected film name %q", w.Name())
		}
		// The accessor must read back the exact value that was inserted.
		if gotYear := w.InitialReleaseDate().Year(); gotYear != wantYear {
			t.Fatalf("film %q: accessor read release year=%d, want %d",
				w.Name(), gotYear, wantYear)
		}
	}
}

// TestWrapperQuery_FirstReturnsWrapped inserts one Film and verifies
// FilmQuery.First returns a non-nil *movies.Film wrapper whose accessors
// reflect the inserted values.
func TestWrapperQuery_FirstReturnsWrapped(t *testing.T) {
	ctx := context.Background()
	client := movies.NewClient(newConn(t))

	addFilm(ctx, t, client, "Spirited Away", 2001)

	got, err := client.Film.Query(ctx).First()
	if err != nil {
		t.Fatalf("First: %v", err)
	}
	if got == nil {
		t.Fatal("First returned a nil *movies.Film on a non-empty result set")
	}
	if got.Name() != "Spirited Away" {
		t.Fatalf("First wrapper Name()=%q, want Spirited Away", got.Name())
	}
	if gotYear := got.InitialReleaseDate().Year(); gotYear != 2001 {
		t.Fatalf("First wrapper release year=%d, want 2001", gotYear)
	}
}

// TestWrapperQuery_FirstEmptyReturnsNil verifies that FilmQuery.First on a
// fresh client with no rows returns (nil, nil). This exercises the s == nil
// branch of the generated First, which must return a nil wrapper rather than
// wrapping a nil schema pointer.
func TestWrapperQuery_FirstEmptyReturnsNil(t *testing.T) {
	ctx := context.Background()
	client := movies.NewClient(newConn(t))

	got, err := client.Film.Query(ctx).First()
	if err != nil {
		t.Fatalf("First on empty client: unexpected error %v", err)
	}
	if got != nil {
		t.Fatalf("First on empty client returned %+v, want nil", got)
	}
}

// TestWrapperQuery_ChainFilterOrderLimit drives a full fluent chain
// (Filter + OrderAsc + Limit) through the wrapper and asserts the exact window
// of wrapped results. It proves FilmQuery delegates the whole chain to
// typed.Query: the filter selects on the name predicate, the order sorts on
// the distinct initial_release_date predicate, and the limit caps.
func TestWrapperQuery_ChainFilterOrderLimit(t *testing.T) {
	ctx := context.Background()
	client := movies.NewClient(newConn(t))

	// Five "keep" films with deliberately unsorted release years, plus one
	// "drop" film the filter excludes.
	for _, year := range []int{1995, 1927, 2012, 1937, 1968} {
		addFilm(ctx, t, client, "keep", year)
	}
	addFilm(ctx, t, client, "drop", 1900)

	// Filter to name=keep -> years {1927,1937,1968,1995,2012}; OrderAsc on the
	// initial_release_date predicate sorts them; Limit(3) keeps the first
	// three: {1927, 1937, 1968}.
	got, err := client.Film.Query(ctx).
		Filter(`eq(name, "keep")`).
		OrderAsc("initial_release_date").
		Limit(3).
		Nodes()
	if err != nil {
		t.Fatalf("chain Nodes: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("Filter+OrderAsc+Limit(3) returned %d wrappers, want 3", len(got))
	}
	wantYears := []int{1927, 1937, 1968}
	for i, w := range got {
		if w == nil {
			t.Fatalf("chain result [%d] is a nil *movies.Film", i)
		}
		if w.Name() != "keep" {
			t.Fatalf("chain result [%d] Name()=%q, want keep (filter leaked)", i, w.Name())
		}
		if gotYear := w.InitialReleaseDate().Year(); gotYear != wantYears[i] {
			t.Fatalf("chain result [%d] release year=%d, want %d (window = %v)",
				i, gotYear, wantYears[i], wantYears)
		}
	}
}

// TestWrapperQuery_SingleQuery proves the wrapper layer adds no extra database
// round-trips. Using a query-counting client, it asserts that building a
// fluent chain executes zero queries, that the Nodes terminal executes exactly
// one, and that the First terminal on a fresh builder executes exactly one.
//
// This test uses the process-global dgman logger and so must NOT call
// t.Parallel(); it uses its own unique t.TempDir() client via newCountingConn.
func TestWrapperQuery_SingleQuery(t *testing.T) {
	ctx := context.Background()
	var queries int
	client := movies.NewClient(newCountingConn(t, &queries))

	// Insert via WrapFilm to also exercise that constructor path.
	for i := range 2 {
		w := movies.WrapFilm(&moviesSchema.Film{
			Name:               "w",
			InitialReleaseDate: time.Date(1990+i, 1, 1, 0, 0, 0, 0, time.UTC),
		})
		if err := client.Film.Add(ctx, w); err != nil {
			t.Fatalf("Film.Add %d: %v", i, err)
		}
	}

	// Building a chain executes nothing: builder methods only mutate the AST.
	before := queries
	q := client.Film.Query(ctx).
		Filter(`eq(name, "w")`).
		OrderAsc("initial_release_date").
		Limit(10)
	if queries != before {
		t.Fatalf("wrapper builder methods executed %d queries, want 0", queries-before)
	}

	// The Nodes terminal runs exactly one query.
	if _, err := q.Nodes(); err != nil {
		t.Fatalf("Nodes: %v", err)
	}
	if got := queries - before; got != 1 {
		t.Fatalf("wrapper Nodes executed %d queries, want exactly 1", got)
	}

	// A fresh builder's First terminal also runs exactly one query.
	before = queries
	if _, err := client.Film.Query(ctx).First(); err != nil {
		t.Fatalf("First: %v", err)
	}
	if got := queries - before; got != 1 {
		t.Fatalf("wrapper First executed %d queries, want exactly 1", got)
	}
}
