package types

import "time"

// Added OrderUpdate struct
type OrderUpdate struct {
	OrderID     string
	Status      string
	FilledSize  float64
	Timestamp   time.Time
}

// Existing structs...
// MarketData represents the raw market data from Polymarket
type MarketData struct {
	Timestamp     time.Time
	PriceUp       float64
	PriceDown     float64
	Volume        float64
	TimeRemaining time.Duration
}

// MarketSignal represents the processed signals for the control system
type MarketSignal struct {
	Timestamp     time.Time
	ProbUp        float64
	ProbDown      float64
	Entropy       float64
	Velocity      float64 // Change in probability per second
	Acceleration  float64 // Change in velocity per second
	TimeRemaining time.Duration
}

// ControlAction represents the output intent from the Brain
type ControlAction struct {
	TargetRiskExposure float64 // Target risk exposure (0.0 - 1.0)
	UpWeight           float64 // Relative weight for Up position
	DownWeight         float64 // Relative weight for Down position
	Mode               ControlMode
	IsFrozen           bool
	Reason             string
}

type ControlMode string

const (
	ModeNormal   ControlMode = "Normal"
	ModeShock    ControlMode = "Shock"
	ModeSurvival ControlMode = "Survival"
	ModeFreeze   ControlMode = "Freeze"
)

// OrderRequest represents a request to the Order State Machine
type OrderRequest struct {
	ID        string
	Side      string // "UP" or "DOWN" (or "BUY"/"SELL")
	Amount    float64
	LimitPrice float64
	Timestamp time.Time
}
