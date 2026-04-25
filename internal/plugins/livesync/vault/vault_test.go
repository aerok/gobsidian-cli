package vault

import (
	"os"
	"path/filepath"
	"testing"
)

func TestWriteSnapshotCreatesUpdatesAndMovesDeletedFilesToTrash(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "old.md"), []byte("old"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	snapshot := map[string]File{
		"notes/new.md": {Path: "notes/new.md", Content: []byte("hello")},
		"old.md":       {Path: "old.md", Deleted: true},
	}
	if err := WriteSnapshot(root, snapshot); err != nil {
		t.Fatalf("WriteSnapshot returned error: %v", err)
	}

	got, err := os.ReadFile(filepath.Join(root, "notes/new.md"))
	if err != nil {
		t.Fatalf("ReadFile new: %v", err)
	}
	if string(got) != "hello" {
		t.Fatalf("unexpected content: %q", string(got))
	}
	if _, err := os.Stat(filepath.Join(root, "old.md")); !os.IsNotExist(err) {
		t.Fatalf("old.md should be moved away, stat err=%v", err)
	}
	trashEntries, err := os.ReadDir(filepath.Join(root, ".gobsidian", "trash"))
	if err != nil {
		t.Fatalf("ReadDir trash: %v", err)
	}
	if len(trashEntries) == 0 {
		t.Fatalf("expected deleted file in trash")
	}
}

func TestWriteSnapshotReplacesSymlinkInsteadOfFollowingIt(t *testing.T) {
	root := t.TempDir()
	outside := filepath.Join(t.TempDir(), "outside.md")
	if err := os.WriteFile(outside, []byte("outside"), 0o644); err != nil {
		t.Fatalf("WriteFile outside: %v", err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "note.md")); err != nil {
		t.Fatalf("Symlink: %v", err)
	}
	if err := WriteSnapshot(root, map[string]File{
		"note.md": {Path: "note.md", Content: []byte("inside")},
	}); err != nil {
		t.Fatalf("WriteSnapshot returned error: %v", err)
	}
	outsideData, err := os.ReadFile(outside)
	if err != nil {
		t.Fatalf("ReadFile outside: %v", err)
	}
	if string(outsideData) != "outside" {
		t.Fatalf("outside file should not be modified, got %q", string(outsideData))
	}
	info, err := os.Lstat(filepath.Join(root, "note.md"))
	if err != nil {
		t.Fatalf("Lstat note: %v", err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		t.Fatalf("note.md should be a regular file, got symlink")
	}
	inside, err := os.ReadFile(filepath.Join(root, "note.md"))
	if err != nil || string(inside) != "inside" {
		t.Fatalf("unexpected note content %q err=%v", string(inside), err)
	}
}

func TestScanSkipsStateDirectoryAndHashesFiles(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".gobsidian"), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "note.md"), []byte("hello"), 0o644); err != nil {
		t.Fatalf("WriteFile note: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, ".gobsidian", "state.json"), []byte("{}"), 0o644); err != nil {
		t.Fatalf("WriteFile state: %v", err)
	}

	files, err := Scan(root)
	if err != nil {
		t.Fatalf("Scan returned error: %v", err)
	}
	if len(files) != 1 {
		t.Fatalf("expected 1 file, got %d", len(files))
	}
	if files["note.md"].Hash == "" {
		t.Fatalf("expected file hash")
	}
	if _, ok := files[".gobsidian/state.json"]; ok {
		t.Fatalf("state directory should be skipped")
	}
}

func TestScanCreatesMissingVaultRoot(t *testing.T) {
	root := filepath.Join(t.TempDir(), "missing-vault")

	files, err := Scan(root)
	if err != nil {
		t.Fatalf("Scan returned error: %v", err)
	}
	if len(files) != 0 {
		t.Fatalf("expected no files in newly created vault, got %d", len(files))
	}
	if info, err := os.Stat(root); err != nil || !info.IsDir() {
		t.Fatalf("expected vault root to be created, info=%v err=%v", info, err)
	}
}
