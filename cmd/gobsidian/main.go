package main

import (
	"fmt"
	"os"

	"go.uber.org/zap"

	"gobsidian-cli/internal/cli"
	"gobsidian-cli/internal/plugin"
	"gobsidian-cli/internal/plugins/livesync"
)

func main() {
	logger, err := zap.NewProduction()
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
	defer func() { _ = logger.Sync() }()
	registry := plugin.NewRegistry()
	if err := livesync.Register(registry, logger); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
	if err := cli.Run(os.Args[1:], os.Stdout, os.Stderr, registry); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}
