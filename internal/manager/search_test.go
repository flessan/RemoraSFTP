package manager

import (
	"context"
	"testing"
	"time"

	"remorasftp/internal/apppaths"
	"remorasftp/internal/config"
	"remorasftp/internal/credentials"
	"remorasftp/internal/events"
	"remorasftp/internal/protocoltest"
	"remorasftp/internal/trust"
)

// newTestSearchManager wires a manager around an in-memory FS. Search does
// not touch the transfer subsystem, so no transfers.Manager is needed here
// (and importing one from the manager package's tests would itself create
// the manager -> transfers -> manager cycle this file exists to avoid).
func newTestSearchManager(t *testing.T, fs *protocoltest.FakeFS) (*Manager, string) {
	t.Helper()
	dir := t.TempDir()
	apppaths.SetDir(dir)
	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	creds, err := credentials.New(dir)
	if err != nil {
		t.Fatal(err)
	}
	tr, err := trust.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	bus := events.NewBus(dir, "off")
	mgr := New(cfg, creds, tr, bus)
	mgr.InjectTestSession("sess", TestSession{
		ConnID: "test-conn", ConnName: "Test", Protocol: config.ProtoSFTP,
		Host: "fake", StartDir: "/", Client: fs, ConnectedAt: time.Now(),
	})
	return mgr, "sess"
}

func TestSearch(t *testing.T) {
	fs := protocoltest.NewFakeFS()
	mgr, sess := newTestSearchManager(t, fs)
	fs.Add("/web", true, nil)
	fs.Add("/web/index.html", false, []byte("<html></html>"))
	fs.Add("/web/js", true, nil)
	fs.Add("/web/js/app.js", false, []byte("console.log(1)"))
	fs.Add("/web/secret.log", false, []byte("x"))
	fs.Add("/.hidden", true, nil)
	fs.Add("/.hidden/x.html", false, []byte("<i></i>"))

	// Extension search, recursive, hidden excluded.
	res, _, err := mgr.Search(context.Background(), sess, SearchQuery{
		Path: "/", Extension: "html", Recursive: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(res) != 1 || res[0].Path != "/web/index.html" {
		t.Fatalf("hidden html must be excluded, got %+v", res)
	}

	// With hidden included.
	res, _, err = mgr.Search(context.Background(), sess, SearchQuery{
		Path: "/", Extension: ".html", Recursive: true, IncludeHidden: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(res) != 2 {
		t.Fatalf("want 2 html files, got %d", len(res))
	}

	// Non-recursive: only the starting dir.
	res, _, err = mgr.Search(context.Background(), sess, SearchQuery{
		Path: "/web", Query: "app",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(res) != 0 {
		t.Fatalf("non-recursive must not descend, got %+v", res)
	}

	// Recursive query match.
	res, _, err = mgr.Search(context.Background(), sess, SearchQuery{
		Path: "/", Query: "app", Recursive: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(res) != 1 || res[0].Name != "app.js" {
		t.Fatalf("query match failed: %+v", res)
	}

	// Case sensitivity.
	res, _, _ = mgr.Search(context.Background(), sess, SearchQuery{
		Path: "/", Query: "APP", Recursive: true, CaseSensitive: true,
	})
	if len(res) != 0 {
		t.Fatalf("case-sensitive query must not match, got %+v", res)
	}
}

// TestSearchQueryMatches pins the match semantics:
//   - CaseSensitive=false (the default, matching the search UI) is
//     case-insensitive;
//   - StartsWith=true does prefix matching (a non-prefix substring does not
//     match);
//   - extension matching respects CaseSensitive;
//   - an extension without a leading dot is normalized;
//   - when both a query and an extension are given, both must match.
func TestSearchQueryMatches(t *testing.T) {
	cases := []struct {
		name string
		q    SearchQuery
		file string
		ext  string // the query extension, as Search passes it to matches
		want bool
	}{
		// Case-sensitive prefix.
		{"case-sensitive prefix match", SearchQuery{Query: "read", StartsWith: true, CaseSensitive: true}, "readme.txt", "", true},
		{"case-sensitive prefix case mismatch", SearchQuery{Query: "read", StartsWith: true, CaseSensitive: true}, "README.md", "", false},
		{"case-sensitive prefix not a prefix", SearchQuery{Query: "read", StartsWith: true, CaseSensitive: true}, "unread.md", "", false},

		// Case-insensitive prefix (default).
		{"case-insensitive prefix match", SearchQuery{Query: "read", StartsWith: true}, "readme.txt", "", true},
		{"case-insensitive prefix case variant", SearchQuery{Query: "read", StartsWith: true}, "README.md", "", true},
		{"case-insensitive prefix not a prefix", SearchQuery{Query: "read", StartsWith: true}, "unread.md", "", false},
		{"case-insensitive prefix query case variant", SearchQuery{Query: "READ", StartsWith: true}, "README.md", "", true},

		// Substring (StartsWith=false).
		{"substring match", SearchQuery{Query: "app"}, "my-app.js", "", true},
		{"substring case-insensitive", SearchQuery{Query: "APP"}, "my-app.js", "", true},

		// Case-sensitive extension.
		{"case-sensitive extension match", SearchQuery{Extension: ".txt", CaseSensitive: true}, "a.txt", ".txt", true},
		{"case-sensitive extension case mismatch", SearchQuery{Extension: ".txt", CaseSensitive: true}, "a.TXT", ".txt", false},
		{"case-sensitive extension different ext", SearchQuery{Extension: ".txt", CaseSensitive: true}, "a.tar", ".txt", false},

		// Case-insensitive extension (default).
		{"case-insensitive extension match", SearchQuery{Extension: ".txt"}, "a.TXT", ".txt", true},
		{"case-insensitive extension different ext", SearchQuery{Extension: ".txt"}, "a.tar", ".txt", false},

		// Extension without a leading dot is normalized.
		{"extension without leading dot normalized", SearchQuery{Extension: "txt"}, "a.txt", "txt", true},
		{"extension without leading dot normalized no match", SearchQuery{Extension: "txt"}, "a.tar", "txt", false},

		// Query + extension: both must match.
		{"query and extension both match", SearchQuery{Query: "a", Extension: ".txt"}, "a.txt", ".txt", true},
		{"query matches extension does not", SearchQuery{Query: "a", Extension: ".txt"}, "a.md", ".txt", false},
		{"extension matches query does not", SearchQuery{Query: "zzz", Extension: ".txt"}, "a.txt", ".txt", false},

		// No criteria matches nothing.
		{"no criteria matches nothing", SearchQuery{}, "a.txt", "", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.q.matches(tc.file, tc.ext); got != tc.want {
				t.Errorf("matches(%q, %q) = %v, want %v", tc.file, tc.ext, got, tc.want)
			}
		})
	}
}
