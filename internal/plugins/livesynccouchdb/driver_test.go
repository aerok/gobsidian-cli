package livesynccouchdb

import (
	"path/filepath"
	"testing"

	"go.uber.org/zap"

	"gobsidian-cli/internal/config"
	"gobsidian-cli/internal/plugins/livesynccouchdb/syncer"
)

func TestStatusReadsStateAndDefaultsStatePath(t *testing.T) {
	root := t.TempDir()
	target := config.Target{
		Name:  "personal",
		Vault: config.VaultConfig{Path: root},
	}
	statePath := filepath.Join(root, ".gobsidian", "state.json")
	if err := syncer.SaveState(statePath, syncer.State{
		CouchSince: "42",
		Files: map[string]syncer.FileState{
			"note.md": {Hash: "hash"},
		},
		LastSync: 1000,
	}); err != nil {
		t.Fatalf("SaveState: %v", err)
	}
	driver := New(zap.NewNop())
	status, err := driver.Status(t.Context(), target)
	if err != nil {
		t.Fatalf("Status returned error: %v", err)
	}
	if status.Vault != "personal" || status.Plugin != PluginName || status.StatePath != statePath {
		t.Fatalf("unexpected status: %#v", status)
	}
	if status.CouchSince != "42" || status.TrackedFiles != 1 {
		t.Fatalf("unexpected state fields: %#v", status)
	}
}
