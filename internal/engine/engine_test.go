package engine

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	"polymarket-btc-bot/internal/audit"
	"polymarket-btc-bot/internal/brain"
	"polymarket-btc-bot/internal/market"
	"polymarket-btc-bot/internal/oms"
	"polymarket-btc-bot/internal/position"
	"polymarket-btc-bot/internal/risk"
	"polymarket-btc-bot/internal/signal"
)

func TestEngine_Run_Dummy(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	adapter := market.NewDummy(2*time.Second, slog.New(slog.NewTextHandler(io.Discard, &slog.HandlerOptions{})))
	en := New(
		slog.Default(),
		Config{BusBuffer: 128},
		adapter,
		signal.New(),
		brain.New(),
		risk.New(),
		oms.New(),
		position.New(),
		audit.NopSink{},
	)

	_ = en.Run(ctx)
}
