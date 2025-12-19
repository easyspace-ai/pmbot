package risk

import (
	"math"
	"time"

	"polymarket-btc-bot/internal/position"
	"polymarket-btc-bot/internal/types"
)

// Supervisor decides whether the system is allowed to trade.
// It can be extended with multiple reporters (ws health, rest latency, drift, etc.).
type Supervisor struct {
	// DataQualityMin triggers kill-switch when adapter quality falls below this.
	DataQualityMin float64

	// FreezeP and FreezeEntropy are additional guardrails. Brain also detects freeze.
	FreezeP       float64
	FreezeEntropy float64
}

func New() *Supervisor {
	return &Supervisor{
		DataQualityMin: 0.6,
		FreezeP:        0.99,
		FreezeEntropy:  0.05,
	}
}

func (s *Supervisor) Evaluate(t types.MarketTick, pos *position.Truth) types.RiskState {
	rs := types.RiskState{Ts: time.Now().UTC()}

	if t.DataQuality > 0 && t.DataQuality < s.DataQualityMin {
		rs.KillSwitch = true
		rs.Reason = "data_quality_low"
		return rs
	}

	// Position truth must be trusted to trade.
	if pos != nil && !pos.Trusted() {
		rs.KillSwitch = true
		rs.Reason = "position_untrusted"
		return rs
	}

	p := t.PYes
	H := entropy(p)
	if (p >= s.FreezeP || p <= 1-s.FreezeP) && H <= s.FreezeEntropy {
		rs.Freeze = true
		rs.Reason = "consensus_frozen"
	}

	return rs
}

func entropy(p float64) float64 {
	// keep local copy to avoid import cycles. Accuracy is enough for gatekeeping.
	if p <= 0 || p >= 1 {
		return 0
	}
	return -(p*math.Log(p) + (1-p)*math.Log(1-p))
}
