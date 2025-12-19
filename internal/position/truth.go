package position

import (
	"sync"
	"time"

	"polymarket-btc-bot/internal/oms"
	"polymarket-btc-bot/internal/types"
)

// Truth maintains the best-known position state and a confidence score.
// It is updated only by the single-thread engine, but protected for safe reads.
type Truth struct {
	mu sync.RWMutex

	marketID string

	yesShares float64
	noShares  float64
	cash      float64

	confidence float64
	updatedAt  time.Time

	lastRisk types.RiskState
}

func New() *Truth {
	return &Truth{confidence: 0.0}
}

func (t *Truth) OnOrderUpdate(upd oms.OrderUpdate) {
	t.mu.Lock()
	defer t.mu.Unlock()
	// Updates alone do not change position, but can impact confidence if state becomes inconsistent.
	_ = upd
	if t.confidence < 0.5 {
		t.confidence = 0.5
	}
	t.updatedAt = time.Now().UTC()
}

func (t *Truth) OnFill(f oms.Fill) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if f.Side == types.SideYes {
		t.yesShares += f.Size
	} else if f.Side == types.SideNo {
		t.noShares += f.Size
	}
	// Cash accounting is adapter-specific; leave as 0 for now.
	if t.confidence < 0.9 {
		t.confidence = 0.9
	}
	t.updatedAt = time.Now().UTC()
}

func (t *Truth) Trusted() bool {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.confidence >= 0.7
}

func (t *Truth) Snapshot() (yes, no, cash, confidence float64, updatedAt time.Time) {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.yesShares, t.noShares, t.cash, t.confidence, t.updatedAt
}
