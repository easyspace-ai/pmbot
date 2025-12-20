package engine

import (
	"context"
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

// Engine is the single-threaded state core.
// All methods must be called from Run() goroutine only.
type Engine struct {
	log *logrus.Logger

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

	// Cycle transition state (close-book).
	transitioning    bool
	pendingSnapshot  *types.MarketSnapshot
	transitionStart  time.Time
	lastFillTs       time.Time
}

type Config struct {
	BusBuffer int
}

func New(log *logrus.Logger, cfg Config, a market.Adapter, s *signal.Layer, b *brain.Controller, r *risk.Supervisor, o *oms.OMS, p *position.Truth, sink audit.Sink) *Engine {
	if log == nil {
		log = logrus.New()
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
			e.log.WithField("event", ev.Type).Warn("bad payload type")
			return
		}
		// New cycle boundary: enter transition mode, close book, then reset state.
		if snap.MarketID != "" && snap.MarketID != e.currentMarketID {
			e.pendingSnapshot = &snap
			if !e.transitioning {
				e.transitioning = true
				e.transitionStart = time.Now().UTC()
				e.lastFillTs = time.Time{}

				// Freeze trading immediately; try to cancel all outstanding orders.
				if e.oms != nil && e.market != nil {
					e.oms.CancelAll(ctx, e.market)
				}
			}
			// If currentMarketID is empty (first boot), we can apply immediately (no close-book needed).
			if e.currentMarketID == "" {
				e.applyNewCycle(ctx, snap)
				e.transitioning = false
				e.pendingSnapshot = nil
			}
			return
		}

	case types.EventMarketTick:
		tick, ok := ev.Payload.(types.MarketTick)
		if !ok {
			e.log.WithField("event", ev.Type).Warn("bad payload type")
			return
		}
		
		// Start timing tracker for this tick processing
		tracker := NewTimingTracker()
		tracker.Mark("start")
		tickTime := ev.TsLocal
		if tickTime.IsZero() {
			tickTime = time.Now().UTC()
		}
		
		e.lastTick = &tick

		// 实时打印 UP/DOWN 价格
		upPrice := tick.PYes
		downPrice := 1 - tick.PYes
		// 格式化为易读的价格显示（保留4位小数，同时显示百分比）
		e.log.Infof("📊 UP: %.4f (%.2f%%) | DOWN: %.4f (%.2f%%)", 
			upPrice, upPrice*100, downPrice, downPrice*100)

		tracker.Mark("tick_received")
		if e.oms != nil {
			e.oms.OnTick(tick)
		}

		tracker.Mark("signals")
		if e.signals != nil {
			e.signals.OnTick(tick)
		}

		// Update risk supervisors first.
		tracker.Mark("risk")
		if e.risk != nil {
			e.lastRisk = e.risk.Evaluate(tick, e.pos)
		}

		// During cycle transition we do not trade; we only process reconciliation events.
		if e.transitioning {
			e.log.Infof("⏸️ [Engine] 周期切换中，跳过交易处理")
			e.maybeCompleteTransition(ctx)
			return
		}

		// If kill-switch is active, OMS must not create new exposure.
		if e.lastRisk.KillSwitch {
			e.log.Infof("⏸️ [Engine] KillSwitch 激活，跳过交易处理: %s", e.lastRisk.Reason)
			if e.oms != nil {
				e.oms.OnRisk(ctx, e.lastRisk, e.market)
			}
			return
		}

		tracker.Mark("brain")
		var intent types.Intent
		if e.brain != nil {
			intent = e.brain.Decide(tick, e.signals, e.lastRisk)
			// 调试日志：打印 Brain 生成的 Intent
			e.log.Infof("🧠 [Brain] 生成 Intent: BiasYes=%.4f, RiskDeltaMax=%.2f, Freeze=%v, ModeMix=%.4f, TimeRemaining=%v",
				intent.BiasYes, intent.RiskDeltaMax, intent.Freeze, intent.ModeMix, tick.TimeRemaining)
		} else {
			e.log.Warnf("⚠️ [Engine] Brain 为 nil，无法生成 Intent")
		}

		tracker.Mark("oms")
		if e.oms != nil {
			// Get position snapshot for strategy
			var posSnapshot struct {
				YesShares  float64
				NoShares   float64
				Confidence float64
			}
			if e.pos != nil {
				y, n, _, conf, _ := e.pos.Snapshot()
				posSnapshot.YesShares = y
				posSnapshot.NoShares = n
				posSnapshot.Confidence = conf
			} else {
				e.log.Infof("⚠️ [Engine] Position 为 nil，使用零持仓")
			}
			
			// If OMS has a strategy executor, it will use it with position info
			// Otherwise falls back to simple logic
			e.oms.OnIntent(ctx, intent, posSnapshot, e.market)
		} else {
			e.log.Warnf("⚠️ [Engine] OMS 为 nil，无法处理 Intent")
		}
		
		// Log timing breakdown if processing took significant time (>10ms)
		totalMS := tracker.GetTotalMS()
		if totalMS > 10 {
			breakdown := tracker.ToBreakdown(tickTime)
			e.log.WithFields(map[string]interface{}{
				"total_ms":            breakdown.TotalMS,
				"tick_received_ms":    breakdown.TickReceivedMS,
				"signals_ms":          breakdown.SignalsMS,
				"risk_ms":             breakdown.RiskMS,
				"brain_ms":            breakdown.BrainMS,
				"oms_ms":              breakdown.OMSMS,
				"latency_from_tick_ms": breakdown.LatencyFromTickMS,
			}).Debug("tick processing timing")
		}

	case types.EventOrderUpdate:
		if e.oms == nil {
			return
		}
		upd, ok := ev.Payload.(oms.OrderUpdate)
		if !ok {
			e.log.WithField("event", ev.Type).Warn("bad payload type")
			return
		}
		e.oms.OnOrderUpdate(upd)
		if e.pos != nil {
			e.pos.OnOrderUpdate(upd)
		}
		if e.transitioning {
			e.maybeCompleteTransition(ctx)
		}

	case types.EventFill:
		if e.pos == nil {
			return
		}
		fill, ok := ev.Payload.(oms.Fill)
		if !ok {
			e.log.WithField("event", ev.Type).Warn("bad payload type")
			return
		}
		e.pos.OnFill(fill)
		e.lastFillTs = time.Now().UTC()
		if e.transitioning {
			e.maybeCompleteTransition(ctx)
		}

	case types.EventRisk:
		// Direct risk events (from adapters/health checks) can force kill-switch.
		rs, ok := ev.Payload.(types.RiskState)
		if !ok {
			e.log.WithField("event", ev.Type).Warn("bad payload type")
			return
		}
		e.lastRisk = rs
		if e.oms != nil {
			e.oms.OnRisk(ctx, rs, e.market)
		}
	}
}

func (e *Engine) maybeCompleteTransition(ctx context.Context) {
	if !e.transitioning || e.pendingSnapshot == nil {
		return
	}

	// Conditions:
	// 1) No locally tracked open orders
	// 2) No fills observed for a short quiet window
	// 3) Timeout protection
	now := time.Now().UTC()
	if now.Sub(e.transitionStart) > 12*time.Second {
		// Can't safely close; stop trading.
		e.lastRisk = types.RiskState{KillSwitch: true, Freeze: true, Reason: "cycle_transition_timeout", Ts: now}
		e.transitioning = false
		return
	}

	openOrders := false
	if e.oms != nil {
		openOrders = e.oms.HasOpenOrders()
	}
	quiet := e.lastFillTs.IsZero() || now.Sub(e.lastFillTs) > 2*time.Second

	if openOrders {
		// Keep attempting cancels while waiting.
		if e.oms != nil && e.market != nil {
			e.oms.CancelAll(ctx, e.market)
		}
		return
	}
	if !quiet {
		return
	}

	// Close-book finished; apply new cycle.
	e.applyNewCycle(ctx, *e.pendingSnapshot)
	e.transitioning = false
	e.pendingSnapshot = nil
}

func (e *Engine) applyNewCycle(ctx context.Context, snap types.MarketSnapshot) {
	// Capture any residual local position and trip kill-switch if non-zero.
	var residualYes, residualNo, residualConf float64
	if e.pos != nil {
		y, n, _, conf, _ := e.pos.Snapshot()
		residualYes, residualNo, residualConf = y, n, conf
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

	// If we had any residual local position, stop trading for safety.
	if residualYes != 0 || residualNo != 0 || residualConf != 0 {
		e.lastRisk = types.RiskState{
			KillSwitch: true,
			Freeze:     true,
			Reason:     "residual_position_on_cycle_switch",
			Ts:         time.Now().UTC(),
		}
	}

	e.log.WithFields(map[string]interface{}{
		"market_id":   snap.MarketID,
		"slug":        snap.MarketSlug,
		"cycle_start": snap.CycleStart.Format(time.RFC3339),
		"end":         snap.EndDate.Format(time.RFC3339),
	}).Info("cycle switched")

	_ = ctx
}
