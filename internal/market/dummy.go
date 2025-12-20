package market

import (
	"context"
	"math"
	"math/rand"
	"time"

	"github.com/sirupsen/logrus"

	"polymarket-btc-bot/internal/bus"
	"polymarket-btc-bot/internal/oms"
	"polymarket-btc-bot/internal/types"
)

// Dummy adapter emits a synthetic probability curve to exercise the engine.
type Dummy struct {
	dur time.Duration
	log *logrus.Logger

	start time.Time
}

func NewDummy(dur time.Duration, log *logrus.Logger) *Dummy {
	if dur <= 0 {
		dur = 15 * time.Minute
	}
	if log == nil {
		log = logrus.New()
	}
	return &Dummy{dur: dur, log: log}
}

func (d *Dummy) Start(ctx context.Context, bus *bus.Bus) error {
	d.start = time.Now().UTC()

	go func() {
		ticker := time.NewTicker(250 * time.Millisecond)
		defer ticker.Stop()
		rng := rand.New(rand.NewSource(time.Now().UnixNano()))

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

				// Generate YES side prices
				bidYes := p - 0.01
				askYes := p + 0.01

				// Generate NO side prices
				pNo := 1.0 - p
				bidNo := pNo - 0.01
				askNo := pNo + 0.01

				// Randomly inject arbitrage opportunity (10% chance)
				// Reduce asks so that askYes + askNo < 1.0
				if rng.Float64() < 0.10 {
					discount := 0.03 // Create 3 cent arbitrage
					askYes -= discount/2
					askNo -= discount/2
					d.log.Debug("Injecting arbitrage opportunity!")
				}

				_ = bus.Publish(ctx, types.Event{
					Type: types.EventMarketTick,
					Payload: types.MarketTick{
						MarketID:      "DUMMY-BTC-15M",
						PYes:          p,
						BestBid:       bidYes,
						BestAsk:       askYes,
						BestBidNo:     bidNo,
						BestAskNo:     askNo,
						TimeRemaining: rem,
						DataQuality:   0.95,
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
	d.log.WithFields(map[string]interface{}{
		"cid":  req.ClientOrderID,
		"side": req.Side.String(),
		"price": req.Price,
		"size": req.Size,
	}).Info("dummy place")
	return oms.PlaceOrderResult{ExchangeOrderID: "DUMMY-EX-" + req.ClientOrderID, Accepted: true}, nil
}

// MergePositions 实现 Adapter 接口（在 dummy 模式下模拟合并）
func (d *Dummy) MergePositions(ctx context.Context, amount float64) (string, error) {
	d.log.WithField("amount", amount).Info("dummy merge positions")
	return "0xdummy_merge_tx_hash", nil
}
