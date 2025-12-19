package safety

import (
	"polymarket-bot/pkg/types"
	"sync"
)

type FreezeDetector struct {
	ThresholdHigh float64
	ThresholdLow  float64
}

func NewFreezeDetector(high, low float64) *FreezeDetector {
	return &FreezeDetector{
		ThresholdHigh: high,
		ThresholdLow:  low,
	}
}

func (f *FreezeDetector) Check(signal types.MarketSignal) (bool, string) {
	if signal.ProbUp >= f.ThresholdHigh {
		return true, "Probability Up exceeded high threshold (Consensus Reached)"
	}
	if signal.ProbUp <= f.ThresholdLow {
		return true, "Probability Up below low threshold (Consensus Reached)"
	}
	// Entropy check could also be added here
	if signal.Entropy < 0.05 { // Very low entropy
		return true, "Entropy too low (Consensus Reached)"
	}
	return false, ""
}

type KillSwitch struct {
	mu        sync.RWMutex
	triggered bool
	reason    string
}

func NewKillSwitch() *KillSwitch {
	return &KillSwitch{
		triggered: false,
	}
}

func (k *KillSwitch) Trigger(reason string) {
	k.mu.Lock()
	defer k.mu.Unlock()
	k.triggered = true
	k.reason = reason
}

func (k *KillSwitch) Reset() {
	k.mu.Lock()
	defer k.mu.Unlock()
	k.triggered = false
	k.reason = ""
}

func (k *KillSwitch) IsTriggered() (bool, string) {
	k.mu.RLock()
	defer k.mu.RUnlock()
	return k.triggered, k.reason
}
