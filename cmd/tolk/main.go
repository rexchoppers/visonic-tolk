package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/rexchoppers/visonic-tolk/internal/tolk"
)

var version = "dev"

func main() {
	log := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: tolk.LogLevel()}))

	cfg := tolk.FromEnv()
	log.Info("starting",
		"version", version,
		"panel", cfg.PanelAddr,
		"monitor", cfg.MonitorAddr,
		"visonic", cfg.VisonicAddr,
	)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := tolk.New(cfg, log).Run(ctx); err != nil {
		log.Error("stopped", "err", err)
		os.Exit(1)
	}

	log.Info("stopped")
}
