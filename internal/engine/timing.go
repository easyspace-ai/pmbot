package engine

import (
	"time"
)

// TimingBreakdown tracks detailed timing for order execution
type TimingBreakdown struct {
	TickReceivedMS    float64 // Time to receive and process tick
	SignalsMS         float64 // Time to update signals
	RiskMS            float64 // Time to evaluate risk
	BrainMS           float64 // Time for brain decision
	OMSMS             float64 // Time for OMS processing
	SettingsMS        float64 // Time to get settings/config
	CalculationMS     float64 // Time to calculate order parameters
	TokenCacheMS      float64 // Time to cache token info
	GetOrderBookMS    float64 // Time to get order book (API call)
	AnalysisMS        float64 // Time to analyze order book depth
	FillCalcMS        float64 // Time to calculate fill
	PlaceOrderMS      float64 // Time to place order (API call)
	PositionUpdateMS  float64 // Time to update position tracking
	TotalMS           float64 // Total time
	LatencyFromTickMS float64 // Time from market tick to execution
}

// TimingTracker helps track timing for order execution
type TimingTracker struct {
	startTime time.Time
	timings   map[string]time.Time
}

// NewTimingTracker creates a new timing tracker
func NewTimingTracker() *TimingTracker {
	return &TimingTracker{
		startTime: time.Now(),
		timings:   make(map[string]time.Time),
	}
}

// Mark records a timing checkpoint
func (t *TimingTracker) Mark(name string) {
	t.timings[name] = time.Now()
}

// GetDuration returns duration between two checkpoints in milliseconds
func (t *TimingTracker) GetDuration(start, end string) float64 {
	startTime, ok1 := t.timings[start]
	endTime, ok2 := t.timings[end]
	if !ok1 || !ok2 {
		return 0
	}
	return float64(endTime.Sub(startTime).Microseconds()) / 1000
}

// GetDurationFromStart returns duration from start in milliseconds
func (t *TimingTracker) GetDurationFromStart(checkpoint string) float64 {
	checkpointTime, ok := t.timings[checkpoint]
	if !ok {
		return 0
	}
	return float64(checkpointTime.Sub(t.startTime).Microseconds()) / 1000
}

// GetTotalMS returns total elapsed time in milliseconds
func (t *TimingTracker) GetTotalMS() float64 {
	return float64(time.Since(t.startTime).Microseconds()) / 1000
}

// ToBreakdown converts timing tracker to breakdown structure
func (t *TimingTracker) ToBreakdown(tickTime time.Time) TimingBreakdown {
	return TimingBreakdown{
		TickReceivedMS:    t.GetDuration("start", "tick_received"),
		SignalsMS:         t.GetDuration("tick_received", "signals"),
		RiskMS:            t.GetDuration("signals", "risk"),
		BrainMS:           t.GetDuration("risk", "brain"),
		OMSMS:             t.GetDuration("brain", "oms"),
		SettingsMS:        t.GetDuration("start", "settings"),
		CalculationMS:     t.GetDuration("settings", "calculation"),
		TokenCacheMS:      t.GetDuration("calculation", "token_cache"),
		GetOrderBookMS:    t.GetDuration("token_cache", "orderbook"),
		AnalysisMS:        t.GetDuration("orderbook", "analysis"),
		FillCalcMS:        t.GetDuration("analysis", "fill_calc"),
		PlaceOrderMS:      t.GetDuration("fill_calc", "place_order"),
		PositionUpdateMS:  t.GetDuration("place_order", "position_update"),
		TotalMS:           t.GetTotalMS(),
		LatencyFromTickMS: float64(time.Since(tickTime).Milliseconds()),
	}
}

