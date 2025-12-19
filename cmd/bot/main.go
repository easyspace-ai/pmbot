package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"polymarket-btc-bot/internal/audit"
	"polymarket-btc-bot/internal/brain"
	"polymarket-btc-bot/internal/engine"
	"polymarket-btc-bot/internal/market"
	"polymarket-btc-bot/internal/oms"
	"polymarket-btc-bot/internal/position"
	"polymarket-btc-bot/internal/risk"
	signals "polymarket-btc-bot/internal/signal"
)

func main() {
	log := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	var adapter market.Adapter
	if os.Getenv("POLY_ADAPTER") == "polymarket" {
		adapter = market.NewPolymarketFromEnv(log)
	} else {
		// Default to dummy so the project runs out-of-box.
		adapter = market.NewDummy(15*time.Minute, log)
	}

	en := engine.New(
		log,
		engine.Config{BusBuffer: 4096},
		adapter,
		signals.New(),
		brain.New(),
		risk.New(),
		oms.New(),
		position.New(),
		audit.NopSink{},
	)

	if err := en.Run(ctx); err != nil {
		log.Error("engine stopped", "err", err)
		os.Exit(1)
	}
}
