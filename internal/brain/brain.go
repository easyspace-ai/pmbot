package brain

import (
	"math"
	"time"

	"polymarket-btc-bot/internal/signal"
	"polymarket-btc-bot/internal/types"
)

// Controller maps signals to Intent.
// This is deliberately simple initially; it will be tuned via playback.
type Controller struct {
	// Risk budget scaling.
	MaxRiskPerTick float64

	// Time decay controls how quickly the system de-risks near settlement.
	TimeHalfLife time.Duration

	// Freeze thresholds.
	FreezeP        float64
	FreezeEntropy  float64
	FreezeMinTime  time.Duration
}

func New() *Controller {
	return &Controller{
		MaxRiskPerTick: 5.0,
		TimeHalfLife:   5 * time.Minute,
		FreezeP:        0.98,
		FreezeEntropy:  0.08,
		FreezeMinTime:  30 * time.Second,
	}
}

func (c *Controller) Decide(t types.MarketTick, s *signal.Layer, r types.RiskState) types.Intent {
	p := clamp01(t.PYes)
	H := signal.Entropy(p)

	// Time pressure: reduce aggressiveness as settlement approaches.
	decay := 1.0
	if t.TimeRemaining > 0 && c.TimeHalfLife > 0 {
		ratio := float64(minDur(t.TimeRemaining, c.TimeHalfLife)) / float64(c.TimeHalfLife)
		decay = clamp01(math.Sqrt(ratio))
	}
	if decay < 0.2 {
		decay = 0.2
	}

	freeze := r.Freeze
	if !freeze {
		if (p >= c.FreezeP || p <= 1-c.FreezeP) && H <= c.FreezeEntropy && t.TimeRemaining > c.FreezeMinTime {
			freeze = true
		}
	}

	// Bias: mean-reversion to 0.5 during high entropy; follow consensus during low entropy.
	// This is a placeholder that matches the design intent (continuous control),
	// and will be replaced by the tuned dual-channel controller.
	consensus := 2*(p-0.5) // [-1,1]
	anti := -consensus
	w := clamp01(H / 0.6931471805599453) // normalize by max entropy at p=0.5
	bias := w*anti + (1-w)*consensus

	// Mode mix: more shock when acceleration is high or entropy collapses.
	acc := 0.0
	if s != nil {
		acc = s.Acceleration()
	}
	modeMix := clamp01(1 - (math.Abs(acc)*5 + (1 - w)))

	riskDelta := c.MaxRiskPerTick * decay
	if freeze {
		riskDelta = 0
	}

	return types.Intent{
		MarketID:      t.MarketID,
		BiasYes:       clamp11(bias),
		RiskDeltaMax:  riskDelta,
		ModeMix:       modeMix,
		Freeze:        freeze,
		Ts:            time.Now().UTC(),
	}
}

func clamp01(x float64) float64 {
	if x < 0 {
		return 0
	}
	if x > 1 {
		return 1
	}
	return x
}

func clamp11(x float64) float64 {
	if x < -1 {
		return -1
	}
	if x > 1 {
		return 1
	}
	return x
}

func minDur(a, b time.Duration) time.Duration {
	if a < b {
		return a
	}
	return b
}
