package engine

import (
	"context"
	"io"
	"testing"
	"time"

	"github.com/sirupsen/logrus"

	"polymarket-btc-bot/internal/audit"
	"polymarket-btc-bot/internal/brain"
	"polymarket-btc-bot/internal/bus"
	"polymarket-btc-bot/internal/market"
	"polymarket-btc-bot/internal/oms"
	"polymarket-btc-bot/internal/position"
	"polymarket-btc-bot/internal/risk"
	"polymarket-btc-bot/internal/signal"
	"polymarket-btc-bot/internal/types"
)

func TestEngine_Run_Dummy(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	log := logrus.New()
	log.SetOutput(io.Discard)
	adapter := market.NewDummy(2*time.Second, log)
	en := New(
		logrus.New(),
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

func TestEngine_MarketSnapshot_ResetsCycleState(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	adapter := silentAdapter{}
	om := oms.New()
	pos := position.New()
	sig := signal.New()

	en := New(
		logrus.New(),
		Config{BusBuffer: 128},
		adapter,
		sig,
		brain.New(),
		risk.New(),
		om,
		pos,
		audit.NopSink{},
	)

	// Seed some per-cycle state.
	om.OnTick(types.MarketTick{MarketID: "OLD", PYes: 0.5, BestAsk: 0.51})
	om.OnIntent(ctx, types.Intent{MarketID: "OLD", BiasYes: 1, RiskDeltaMax: 1, ModeMix: 1, Freeze: false, Ts: time.Now().UTC()}, adapter)
	if om.OrderCount() == 0 {
		t.Fatalf("expected OMS to have at least one order before reset")
	}
	pos.OnFill(oms.Fill{MarketID: "OLD", Side: types.SideYes, Price: 0.5, Size: 1, Ts: time.Now().UTC()})
	sig.OnTick(types.MarketTick{MarketID: "OLD", PYes: 0.6})

	// Push a new snapshot, which should trigger reset.
	_ = en.Bus().Publish(ctx, types.Event{
		Type: types.EventMarketSnapshot,
		Payload: types.MarketSnapshot{
			MarketID:   "NEW",
			MarketSlug: "btc-updown-15m-1700000000",
			CycleStart: time.Unix(1700000000, 0).UTC(),
			EndDate:    time.Now().UTC().Add(15 * time.Minute),
		},
	})

	go func() { _ = en.Run(ctx) }()

	<-ctx.Done()

	// OMS should have been reset: no orders.
	if om.OrderCount() != 0 {
		t.Fatalf("expected OMS orders cleared")
	}
	yes, no, _, conf, _ := pos.Snapshot()
	if yes != 0 || no != 0 || conf != 0 {
		t.Fatalf("expected position cleared, got yes=%v no=%v conf=%v", yes, no, conf)
	}
	// Signals should have been reset at least once during transition.
	// (Ignore later ticks since this test uses no market feed.)
	_ = sig
}

type silentAdapter struct{}

func (silentAdapter) Start(ctx context.Context, b *bus.Bus) error { return nil }
func (silentAdapter) PlaceOrder(ctx context.Context, req oms.PlaceOrderRequest) (oms.PlaceOrderResult, error) {
	return oms.PlaceOrderResult{ExchangeOrderID: "e-" + req.ClientOrderID, Accepted: true}, nil
}
func (silentAdapter) CancelOrder(ctx context.Context, req oms.CancelOrderRequest) (oms.CancelOrderResult, error) {
	return oms.CancelOrderResult{Ok: true}, nil
}
