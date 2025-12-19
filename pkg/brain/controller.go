package brain

import (
	"math"
	"polymarket-bot/pkg/types"
)

type Controller struct {
	config Config
}

type Config struct {
	BaseGain          float64
	MaxRisk           float64
	TimeDecayFactor   float64
	VelocityThreshold float64
	EntropyThreshold  float64
}

func NewController(cfg Config) *Controller {
	return &Controller{
		config: cfg,
	}
}

// Compute calculates the next control action based on the market signal and current inventory
func (c *Controller) Compute(signal types.MarketSignal, currentInventory float64) types.ControlAction {
	// 1. Calculate Auto-Gain
	gain := c.calculateAutoGain(signal)

	// Inventory Skew: If we are near max risk, reduce gain to prevent over-exposure
	// This acts as a soft cap or damper
	usage := math.Abs(currentInventory) / c.config.MaxRisk
	if usage > 0.8 {
		gain *= (1.0 - (usage - 0.8) * 2) // Linearly decay to 0.6x at usage=1.0?
		// At usage=1.0, factor = 1 - 0.4 = 0.6.
		if gain < 0.1 { gain = 0.1 }
	}

	// 2. Calculate Weights for Dual-Channel
	// w (normal weight) depends on stability. High velocity/accel -> lower w (more shock)
	shockFactor := math.Abs(signal.Velocity) + math.Abs(signal.Acceleration)*0.5
	w := 1.0 / (1.0 + shockFactor*10.0) // Sigmoid-like decay
	if w < 0 {
		w = 0
	}
	if w > 1 {
		w = 1
	}

	// 3. Dual-Channel Outputs
	normalOutput := c.normalChannel(signal)
	shockOutput := c.shockChannel(signal)

	// 4. Mix Outputs
	// Output here is essentially the target risk bias or directional intent
	// Let's define Output as a value between -1 (Full Down) and 1 (Full Up) for simplicity in internal logic,
	// then map to weights.
	mixedOutput := w*normalOutput + (1-w)*shockOutput

	// 5. Apply Gain and Time Pressure
	// Time pressure reduces overall exposure capability
	timePressure := c.calculateTimePressure(signal.TimeRemaining.Seconds())
	finalExposure := c.config.MaxRisk * gain * timePressure

	// 6. Construct Action
	// Convert mixedOutput (-1 to 1) to Up/Down weights
	// mixedOutput = 0 -> Up 0.5, Down 0.5 (Neutral)
	// mixedOutput = 1 -> Up 1.0, Down 0.0
	// mixedOutput = -1 -> Up 0.0, Down 1.0
	
	// Normalize mixedOutput to [0, 1] for UpWeight
	normalizedIntent := (mixedOutput + 1) / 2
	upWeight := normalizedIntent
	downWeight := 1 - normalizedIntent

	mode := types.ModeNormal
	if w < 0.5 {
		mode = types.ModeShock
	}

	return types.ControlAction{
		TargetRiskExposure: finalExposure,
		UpWeight:           upWeight,
		DownWeight:         downWeight,
		Mode:               mode,
		IsFrozen:           false, // Freeze logic is handled by safety layer usually, but Brain can respect it if passed in
		Reason:             "calculated",
	}
}

// normalChannel handles steady state adjustments
// Strategies: Mean reversion, entropy harvesting
func (c *Controller) normalChannel(signal types.MarketSignal) float64 {
	// Simple logic: If Entropy is high, stay neutral/market make. 
	// If P is drifting, gently follow.
	// For this example, let's assume it follows momentum slightly
	return (signal.ProbUp - 0.5) * 0.5 // Gentle bias
}

// shockChannel handles extreme moves
// Strategies: Momentum following, fast adaptation
func (c *Controller) shockChannel(signal types.MarketSignal) float64 {
	// React strongly to velocity
	return math.Copysign(1.0, signal.Velocity) // Full commitment to the direction of velocity
}

func (c *Controller) calculateAutoGain(signal types.MarketSignal) float64 {
	gain := c.config.BaseGain

	// High Entropy -> Higher Gain (More opportunity)
	if signal.Entropy > c.config.EntropyThreshold {
		gain *= 1.2
	}

	// High Velocity -> Higher Gain (Catch the move)
	if math.Abs(signal.Velocity) > c.config.VelocityThreshold {
		gain *= 1.5
	}

	return gain
}

func (c *Controller) calculateTimePressure(secondsRemaining float64) float64 {
	// Simple linear decay or sigmoid
	// Assume 15 min = 900 seconds
	// If remaining < 60s, reduce risk significantly
	if secondsRemaining <= 0 {
		return 0
	}
	if secondsRemaining < 60 {
		return secondsRemaining / 60.0
	}
	return 1.0
}
