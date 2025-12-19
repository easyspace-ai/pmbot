package risk

import (
	"testing"
	"time"

	"polymarket-btc-bot/internal/oms"
	"polymarket-btc-bot/internal/position"
	"polymarket-btc-bot/internal/types"
)

func TestSupervisor_DataQualityKill(t *testing.T) {
	s := New()
	pos := position.New()
	// make position trusted
	pos.OnFill(oms.Fill{MarketID: "X", Side: types.SideYes, Price: 0.5, Size: 1, Ts: time.Now().UTC()})

	rs := s.Evaluate(types.MarketTick{MarketID: "X", PYes: 0.5, DataQuality: 0.1}, pos)
	if !rs.KillSwitch {
		t.Fatalf("expected kill-switch")
	}
}
