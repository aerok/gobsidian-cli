package app

import (
	"context"
	"errors"
	"testing"

	"gobsidian-cli/internal/config"
	"gobsidian-cli/internal/plugin"
)

type appFakeDriver struct {
	err error
}

func (d appFakeDriver) Sync(_ context.Context, target config.Target) (plugin.SyncResult, error) {
	return plugin.SyncResult{Vault: target.Name, FilesWritten: 1}, d.err
}

func (d appFakeDriver) Status(_ context.Context, target config.Target) (plugin.StatusResult, error) {
	return plugin.StatusResult{Vault: target.Name, TrackedFiles: 1}, d.err
}

func TestSyncRunsSelectedTargetsAndReportsErrors(t *testing.T) {
	reg := plugin.NewRegistry()
	_ = reg.Register("ok", appFakeDriver{})
	_ = reg.Register("bad", appFakeDriver{err: errors.New("boom")})
	runner := New(reg)
	cfg := config.Config{Plugin: "bad", Targets: []config.Target{{Name: "personal"}, {Name: "work"}}}
	res := runner.Sync(context.Background(), cfg)
	if res.OK || len(res.Vaults) != 2 || len(res.Errors) != 2 {
		t.Fatalf("unexpected sync response: %#v", res)
	}
	one, err := cfg.FilterTargets("personal")
	if err != nil {
		t.Fatalf("FilterTargets: %v", err)
	}
	one.Plugin = "ok"
	res = runner.Sync(context.Background(), one)
	if !res.OK || len(res.Vaults) != 1 || res.Vaults[0].Vault != "personal" {
		t.Fatalf("unexpected selected sync response: %#v", res)
	}
}
