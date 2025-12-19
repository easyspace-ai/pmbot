package market

import (
	"context"
	"log/slog"
	"math"
	"time"

	"polymarket-btc-bot/internal/engine"
	"polymarket-btc-bot/internal/oms"
	"polymarket-btc-bot/internal/types"
)

// Dummy adapter emits a synthetic probability curve to exercise the engine.
type Dummy struct {
	dur time.Duration
	log *slog.Logger

	start time.Time
}

func NewDummy(dur time.Duration, log *slog.Logger) *Dummy {
	if dur <= 0 {
		dur = 15 * time.Minute
	}
	if log == nil {
		log = slog.Default()
	}
	return &Dummy{dur: dur, log: log}
}

func (d *Dummy) Start(ctx context.Context, bus *engine.Bus) error {
	d.start = time.Now().UTC()

	go func() {
		ticker := time.NewTicker(250 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case now := <-ticker.C:
				elapsed := now.Sub(d.start)
				rem := d.dur - elapsed
				if rem < 0 {
					rem = 0
				}

				// Smooth oscillation plus drift to mimic consensus formation.
				x := float64(elapsed) / float64(d.dur)
				p := 0.5 + 0.15*math.Sin(10*math.Pi*x) + 0.35*(x-0.5)
				if p < 0.01 {
					p = 0.01
				}
				if p > 0.99 {
					p = 0.99
				}

				_ = bus.Publish(ctx, types.Event{
					Type: types.EventMarketTick,
					Payload: types.MarketTick{
						MarketID:       "DUMMY-BTC-15M",
						PYes:           p,
						BestBid:        p - 0.01,
						BestAsk:        p + 0.01,
						TimeRemaining:  rem,
						DataQuality:    0.95,
					},
				})

				if rem == 0 {
					return
				}
			}
		}
	}()

	return nil
}

func (d *Dummy) PlaceOrder(ctx context.Context, req oms.PlaceOrderRequest) (oms.PlaceOrderResult, error) {
	_ = ctx
	// In dummy mode, accept but do not generate fills.
	d.log.Info("dummy place", "cid", req.ClientOrderID, "side", req.Side.String(), "price", req.Price, "size", req.Size)
	return oms.PlaceOrderResult{ExchangeOrderID: "DUMMY-EX-" + req.ClientOrderID, Accepted: true}, nil
}

func (d *Dummy) CancelOrder(ctx context.Context, req oms.CancelOrderRequest) (oms.CancelOrderResult, error) {
	_ = ctx
	d.log.Info("dummy cancel", "cid", req.ClientOrderID, "oid", req.ExchangeOrderID)
	return oms.CancelOrderResult{Ok: true}, nil
}
