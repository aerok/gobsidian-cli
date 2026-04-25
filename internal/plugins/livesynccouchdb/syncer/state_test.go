package syncer

import (
	"os"
	"path/filepath"
	"testing"
)

func TestStateRoundTrip(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), ".gobsidian", "state.json")
	state := State{
		CouchSince: "42",
		Files: map[string]FileState{
			"note.md": {Hash: "abc", RemoteRev: "1-a", Mtime: 1000, Size: 3},
		},
		LastSync:  2000,
		LastError: "boom",
	}
	if err := SaveState(statePath, state); err != nil {
		t.Fatalf("SaveState returned error: %v", err)
	}
	loaded, err := LoadState(statePath)
	if err != nil {
		t.Fatalf("LoadState returned error: %v", err)
	}
	if loaded.CouchSince != "42" {
		t.Fatalf("unexpected couch since: %q", loaded.CouchSince)
	}
	if loaded.Files["note.md"].Hash != "abc" {
		t.Fatalf("unexpected file state: %#v", loaded.Files["note.md"])
	}
	if loaded.LastSync != 2000 || loaded.LastError != "boom" {
		t.Fatalf("unexpected metadata: %#v", loaded)
	}
}

func TestLoadStateReturnsEmptyWhenMissing(t *testing.T) {
	state, err := LoadState(filepath.Join(t.TempDir(), ".gobsidian", "state.json"))
	if err != nil {
		t.Fatalf("LoadState returned error: %v", err)
	}
	if state.Files == nil {
		t.Fatalf("Files map should be initialized")
	}
}

func TestSaveStateCreatesParentDirectory(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), ".gobsidian", "state.json")
	if err := SaveState(statePath, State{}); err != nil {
		t.Fatalf("SaveState returned error: %v", err)
	}
	if _, err := os.Stat(statePath); err != nil {
		t.Fatalf("state file was not written in expected location: %v", err)
	}
}
