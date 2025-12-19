package engine

import (
	"context"
	"log/slog"
	"time"

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

// Engine is the single-threaded state core.
// All methods must be called from Run() goroutine only.
type Engine struct {
	log *slog.Logger

	bus   *bus.Bus
	audit audit.Sink

	market market.Adapter

	signals *signal.Layer
	brain   *brain.Controller
	risk    *risk.Supervisor
	oms     *oms.OMS
	pos     *position.Truth

	lastTick *types.MarketTick
	lastRisk types.RiskState

	currentMarketID string
}

type Config struct {
	BusBuffer int
}

func New(log *slog.Logger, cfg Config, a market.Adapter, s *signal.Layer, b *brain.Controller, r *risk.Supervisor, o *oms.OMS, p *position.Truth, sink audit.Sink) *Engine {
	if log == nil {
		log = slog.Default()
	}
	if sink == nil {
		sink = audit.NopSink{}
	}
	return &Engine{
		log:      log,
		bus:      bus.New(cfg.BusBuffer),
		audit:    sink,
		market:   a,
		signals:  s,
		brain:    b,
		risk:     r,
		oms:      o,
		pos:      p,
		lastRisk: types.RiskState{Ts: time.Now().UTC()},
	}
}

func (e *Engine) Bus() *bus.Bus { return e.bus }

func (e *Engine) Run(ctx context.Context) error {
	// Start market adapter (WS/REST etc) to publish events.
	if e.market != nil {
		if err := e.market.Start(ctx, e.bus); err != nil {
			return err
		}
	}

	defer e.bus.Close()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case ev, ok := <-e.bus.Chan():
			if !ok {
				return nil
			}
			e.audit.Write(ev)
			e.handleEvent(ctx, ev)
		}
	}
}

func (e *Engine) handleEvent(ctx context.Context, ev types.Event) {
	switch ev.Type {
	case types.EventMarketSnapshot:
		snap, ok := ev.Payload.(types.MarketSnapshot)
		if !ok {
			e.log.Warn("bad payload type", "event", ev.Type)
			return
		}
		// New cycle boundary: cancel outstanding orders and clear per-cycle state.
		if snap.MarketID != "" && snap.MarketID != e.currentMarketID {
			// Best-effort cancel before resetting.
			if e.oms != nil && e.market != nil {
				e.oms.CancelAll(ctx, e.market)
			}
			if e.oms != nil {
				e.oms.Reset()
			}
			if e.pos != nil {
				e.pos.Reset()
			}
			if e.signals != nil {
				e.signals.Reset()
			}
			e.lastTick = nil
			e.lastRisk = types.RiskState{Ts: time.Now().UTC()}
			e.currentMarketID = snap.MarketID
			e.log.Info("cycle switched",
				"market_id", snap.MarketID,
				"slug", snap.MarketSlug,
				"cycle_start", snap.CycleStart.Format(time.RFC3339),
				"end", snap.EndDate.Format(time.RFC3339),
			)
		}

	case types.EventMarketTick:
		tick, ok := ev.Payload.(types.MarketTick)
		if !ok {
			e.log.Warn("bad payload type", "event", ev.Type)
			return
		}
		e.lastTick = &tick

		if e.oms != nil {
			e.oms.OnTick(tick)
		}

		if e.signals != nil {
			e.signals.OnTick(tick)
		}

		// Update risk supervisors first.
		if e.risk != nil {
			e.lastRisk = e.risk.Evaluate(tick, e.pos)
		}

		// If kill-switch is active, OMS must not create new exposure.
		if e.lastRisk.KillSwitch {
			if e.oms != nil {
				e.oms.OnRisk(ctx, e.lastRisk, e.market)
			}
			return
		}

		var intent types.Intent
		if e.brain != nil {
			intent = e.brain.Decide(tick, e.signals, e.lastRisk)
		}

		if e.oms != nil {
			e.oms.OnIntent(ctx, intent, e.market)
		}

	case types.EventOrderUpdate:
		if e.oms == nil {
			return
		}
		upd, ok := ev.Payload.(oms.OrderUpdate)
		if !ok {
			e.log.Warn("bad payload type", "event", ev.Type)
			return
		}
		e.oms.OnOrderUpdate(upd)
		if e.pos != nil {
			e.pos.OnOrderUpdate(upd)
		}

	case types.EventFill:
		if e.pos == nil {
			return
		}
		fill, ok := ev.Payload.(oms.Fill)
		if !ok {
			e.log.Warn("bad payload type", "event", ev.Type)
			return
		}
		e.pos.OnFill(fill)

	case types.EventRisk:
		// Direct risk events (from adapters/health checks) can force kill-switch.
		rs, ok := ev.Payload.(types.RiskState)
		if !ok {
			e.log.Warn("bad payload type", "event", ev.Type)
			return
		}
		e.lastRisk = rs
		if e.oms != nil {
			e.oms.OnRisk(ctx, rs, e.market)
		}
	}
}
