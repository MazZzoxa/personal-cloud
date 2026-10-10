package storage

import (
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"

	_ "modernc.org/sqlite"

	"personal-cloud/server/internal/database"
)

func newTestStore(t *testing.T, files map[string]string) *Store {
	t.Helper()
	root := t.TempDir()
	for rel, content := range files {
		abs := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(abs, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = db.Close() })
	if err := database.Initialize(db); err != nil {
		t.Fatal(err)
	}
	return New(root, db)
}

func paths(result SearchResult) []string {
	out := make([]string, 0, len(result.Items))
	for _, item := range result.Items {
		out = append(out, item.Path)
	}
	return out
}

func TestSearchMatchesEveryWordAndRanksNameMatchesFirst(t *testing.T) {
	store := newTestStore(t, map[string]string{
		"report-2024.pdf":        "x",
		"docs/annual report.txt": "hello world",
		"notes/Ёлка.txt":         "привет",
		"img/photo.png":          "x",
	})

	result, err := store.Search(SearchOptions{Query: "report"})
	if err != nil {
		t.Fatal(err)
	}
	got := paths(result)
	if len(got) != 2 || got[0] != "report-2024.pdf" || got[1] != "docs/annual report.txt" {
		t.Fatalf("unexpected ranking: %v", got)
	}

	result, err = store.Search(SearchOptions{Query: "annual report"})
	if err != nil {
		t.Fatal(err)
	}
	if got := paths(result); len(got) != 1 || got[0] != "docs/annual report.txt" {
		t.Fatalf("all words must match: %v", got)
	}

	result, err = store.Search(SearchOptions{Query: "ёлка"})
	if err != nil {
		t.Fatal(err)
	}
	if got := paths(result); len(got) != 1 || got[0] != "notes/Ёлка.txt" {
		t.Fatalf("ё and е must be treated the same: %v", got)
	}
}

func TestSearchFilters(t *testing.T) {
	store := newTestStore(t, map[string]string{
		"report-2024.pdf":        "x",
		"docs/annual report.txt": "hello world",
		"img/photo.png":          "x",
	})

	result, err := store.Search(SearchOptions{Query: "report", Category: CategoryDocument})
	if err != nil {
		t.Fatal(err)
	}
	if got := paths(result); len(got) != 1 || got[0] != "report-2024.pdf" {
		t.Fatalf("category filter: %v", got)
	}

	result, err = store.Search(SearchOptions{Query: "report ext:txt"})
	if err != nil {
		t.Fatal(err)
	}
	if got := paths(result); len(got) != 1 || got[0] != "docs/annual report.txt" {
		t.Fatalf("ext operator: %v", got)
	}

	result, err = store.Search(SearchOptions{Query: "type:image"})
	if err != nil {
		t.Fatal(err)
	}
	if got := paths(result); len(got) != 1 || got[0] != "img/photo.png" {
		t.Fatalf("type operator: %v", got)
	}

	result, err = store.Search(SearchOptions{Query: "report", Path: "docs"})
	if err != nil {
		t.Fatal(err)
	}
	if got := paths(result); len(got) != 1 || got[0] != "docs/annual report.txt" {
		t.Fatalf("folder scope: %v", got)
	}

	if _, err := store.Search(SearchOptions{Query: "report", Category: "nonsense"}); err == nil {
		t.Fatal("expected an error for an unknown category")
	}

	result, err = store.Search(SearchOptions{Query: "   "})
	if err != nil || len(result.Items) != 0 {
		t.Fatalf("blank query must return nothing: %v %v", result.Items, err)
	}
}

func TestSearchInsideTextFiles(t *testing.T) {
	store := newTestStore(t, map[string]string{
		"docs/annual report.txt": "first line\nsecond line has the Hello word",
		"img/photo.png":          "hello",
	})

	result, err := store.Search(SearchOptions{Query: "hello"})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Items) != 0 {
		t.Fatalf("content must not be searched unless requested: %v", paths(result))
	}

	result, err = store.Search(SearchOptions{Query: "hello", Content: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Items) != 1 {
		t.Fatalf("expected one content hit, got %v", paths(result))
	}
	hit := result.Items[0]
	if hit.Path != "docs/annual report.txt" || hit.Match != "content" || hit.Line != 2 || !strings.Contains(hit.Snippet, "Hello") {
		t.Fatalf("unexpected content hit: %+v", hit)
	}
}
