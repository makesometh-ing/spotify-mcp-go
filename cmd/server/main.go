package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/makesometh-ing/spotify-mcp-go/internal/tools"
)

func main() {
	debug := flag.Bool("debug", false, "Enable debug logging to stderr")
	flag.Parse()

	logger := newLogger(*debug)
	defer func() { _ = logger.Sync() }()

	cfg, err := loadConfig(".env")
	if err != nil {
		fmt.Fprintf(os.Stderr, "fatal: %v\n", err)
		os.Exit(1)
	}

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	if err := run(ctx, cfg, tools.AllRegistrations(), tools.AllScopes(), os.Stdout, nil, logger); err != nil {
		fmt.Fprintf(os.Stderr, "fatal: %v\n", err)
		os.Exit(1)
	}
}
