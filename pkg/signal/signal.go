package signal

import (
	"math"
	"polymarket-bot/pkg/types"
)

type SignalProcessor struct {
	lastSignal *types.MarketSignal
}

func NewSignalProcessor() *SignalProcessor {
	return &SignalProcessor{}
}

// Process converts raw MarketData into MarketSignal
func (s *SignalProcessor) Process(data types.MarketData) types.MarketSignal {
	probUp := data.PriceUp // Assuming price represents probability 0-1
	probDown := data.PriceDown

	// Normalize probabilities if they don't sum to 1 (optional, depending on market mechanics)
	sum := probUp + probDown
	if sum > 0 {
		probUp /= sum
		probDown /= sum
	}

	entropy := s.calculateEntropy(probUp)
	velocity := 0.0
	acceleration := 0.0

	if s.lastSignal != nil {
		dt := data.Timestamp.Sub(s.lastSignal.Timestamp).Seconds()
		if dt > 0 {
			// Calculate Velocity (dP/dt)
			// Using ProbUp as the reference
			dProb := probUp - s.lastSignal.ProbUp
			velocity = dProb / dt

			// Calculate Acceleration (dV/dt)
			dVel := velocity - s.lastSignal.Velocity
			acceleration = dVel / dt
		}
	}

	currentSignal := types.MarketSignal{
		Timestamp:     data.Timestamp,
		ProbUp:        probUp,
		ProbDown:      probDown,
		Entropy:       entropy,
		Velocity:      velocity,
		Acceleration:  acceleration,
		TimeRemaining: data.TimeRemaining,
	}

	s.lastSignal = &currentSignal
	return currentSignal
}

// calculateEntropy = - (p * log p + (1-p) * log(1-p))
func (s *SignalProcessor) calculateEntropy(p float64) float64 {
	if p <= 0 || p >= 1 {
		return 0
	}
	return -(p*math.Log(p) + (1-p)*math.Log(1-p))
}
