package vaultops

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSearchFiltersTagsAndObsidianExcludes(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "projects/alpha.md", "---\ntags: [project, active]\n---\nneedle")
	writeFile(t, root, "archive/beta.md", "---\ntags: [project]\n---\nneedle")
	writeFile(t, root, "draft.tmp.md", "needle #project")
	writeFile(t, root, ".obsidian/app.json", `{"userIgnoreFilters":["archive/","*.tmp.md"]}`)

	out, err := Search(root, "main", SearchOptions{Query: "needle", Tags: []string{"project"}, Limit: 10})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if out.Total != 1 || out.Results[0].Path != "projects/alpha.md" {
		t.Fatalf("unexpected search results: %#v", out)
	}
}

func TestSearchRejectsNegativeOffsetAndMalformedFrontmatter(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "note.md", "needle")
	if _, err := Search(root, "main", SearchOptions{Query: "needle", Offset: -1}); err == nil {
		t.Fatal("expected negative offset to fail")
	}
	writeFile(t, root, "bad.md", "---\ntags: [broken\n---\nneedle")
	if _, err := Search(root, "main", SearchOptions{Query: "needle"}); err == nil {
		t.Fatal("expected malformed frontmatter to fail")
	}
}

func TestReadListAndResolveProtectVaultBoundary(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "note.md", "one\ntwo\nthree")
	writeFile(t, root, "folder/nested.md", "nested")
	writeFile(t, root, ".obsidian/hidden.md", "hidden")

	read, err := Read(root, "main", ReadOptions{Ref: "note", Range: "2:3"})
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if read.Content != "two\nthree" || !read.Truncated {
		t.Fatalf("unexpected read result: %#v", read)
	}
	list, err := List(root, "main", ListOptions{Recursive: true, Type: "note"})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if paths := entryPaths(list.Entries); strings.Join(paths, ",") != "folder/nested.md,note.md" {
		t.Fatalf("unexpected list paths: %#v", paths)
	}
	if _, err := ResolveNote(root, "../outside.md", false); err == nil {
		t.Fatal("expected traversal to fail")
	}
}

func TestMoveUpdatesCommonObsidianLinks(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "old.md", "old")
	writeFile(t, root, "refs.md", "[[old]] and [old](old.md)")
	writeFile(t, root, "nested/refs.md", "[old](../old.md)")

	out, err := Move(root, "main", MoveOptions{From: "old", To: "folder/new", UpdateLinks: true})
	if err != nil {
		t.Fatalf("Move: %v", err)
	}
	if out.From != "old.md" || out.To != "folder/new.md" || len(out.UpdatedLinks) != 2 {
		t.Fatalf("unexpected move response: %#v", out)
	}
	if got := readFile(t, root, "refs.md"); got != "[[new]] and [old](folder/new.md)" {
		t.Fatalf("unexpected root link update: %q", got)
	}
	if got := readFile(t, root, "nested/refs.md"); got != "[old](../folder/new.md)" {
		t.Fatalf("unexpected nested link update: %q", got)
	}
}

func TestMoveOverwritePreservesDestinationThroughBackup(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "old.md", "old")
	writeFile(t, root, "folder/new.md", "replace me")

	out, err := Move(root, "main", MoveOptions{From: "old", To: "folder/new", Overwrite: true})
	if err != nil {
		t.Fatalf("Move overwrite: %v", err)
	}
	if out.To != "folder/new.md" || readFile(t, root, "folder/new.md") != "old" {
		t.Fatalf("unexpected overwrite result: %#v", out)
	}
	if _, err := os.Stat(filepath.Join(root, "old.md")); !os.IsNotExist(err) {
		t.Fatalf("expected source to be moved, err=%v", err)
	}
}

func TestFrontmatterYAMLValueAndDeleteLastKey(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "note.md", "body")
	set, err := FrontmatterSet(root, "main", "note", "tags", "[project, active]")
	if err != nil {
		t.Fatalf("FrontmatterSet: %v", err)
	}
	if _, ok := set.Value.([]any); !ok {
		t.Fatalf("expected YAML list value, got %#v", set.Value)
	}
	deleted, err := FrontmatterDelete(root, "main", "note", "tags")
	if err != nil {
		t.Fatalf("FrontmatterDelete: %v", err)
	}
	if !deleted.Changed || readFile(t, root, "note.md") != "body" {
		t.Fatalf("expected empty frontmatter removal, response=%#v content=%q", deleted, readFile(t, root, "note.md"))
	}
}

func TestFrontmatterSupportsCRLF(t *testing.T) {
	fm, body, err := ParseFrontmatter("---\r\ntags: [project]\r\n---\r\nbody")
	if err != nil {
		t.Fatalf("ParseFrontmatter: %v", err)
	}
	if body != "body" {
		t.Fatalf("unexpected body: %q", body)
	}
	tags := Tags(fm, body)
	if len(tags) != 1 || tags[0] != "project" {
		t.Fatalf("unexpected tags: %#v", tags)
	}
	if _, body, err := ParseFrontmatter("---\ntitle: Note\n---"); err != nil || body != "" {
		t.Fatalf("expected EOF closing delimiter to parse, body=%q err=%v", body, err)
	}
}

func writeFile(t *testing.T, root, rel, content string) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
}

func readFile(t *testing.T, root, rel string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	return string(data)
}

func entryPaths(entries []Entry) []string {
	paths := make([]string, 0, len(entries))
	for _, entry := range entries {
		paths = append(paths, entry.Path)
	}
	return paths
}
