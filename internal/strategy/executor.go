package strategy

import (
	"fmt"
	"math"

	"polymarket-btc-bot/internal/types"
)

// getMaxSlippage returns the maximum allowed slippage based on token price.
// Lower prices are more volatile, so we allow more slippage.
// This is adapted from CopyTrader's proven approach.
func getMaxSlippage(price float64) float64 {
	switch {
	case price < 0.10:
		return 2.00 // 200% - can pay up to 3x the price
	case price < 0.20:
		return 0.80 // 80% - can pay up to 1.8x
	case price < 0.30:
		return 0.50 // 50% - can pay up to 1.5x
	case price < 0.40:
		return 0.30 // 30% - can pay up to 1.3x
	default:
		return 0.20 // 20% - can pay up to 1.2x
	}
}

// PositionSnapshot provides position information without creating import cycles.
type PositionSnapshot struct {
	YesShares  float64
	NoShares   float64
	Confidence float64
}

// Executor translates Brain Intent into concrete trading actions.
// It implements the "strategy layer" that Brain's control system lacks.
type Executor struct{}

func New() *Executor {
	return &Executor{}
}

// Execute converts Intent into concrete order decisions.
// This is where the "strategy" logic lives:
// - How to balance YES/NO positions
// - How to allocate risk budget
// - How to price orders optimally
// - How to handle Normal vs Shock modes
type OrderDecision struct {
	Side      types.Side
	Price     float64
	Size      float64
	Reason    string
	CancelAll bool // If true, cancel all existing orders first
}

// Execute processes Intent and returns order decisions.
func (e *Executor) Execute(tick types.MarketTick, intent types.Intent, pos PositionSnapshot) []OrderDecision {
	decisions := []OrderDecision{}

	// If freeze, cancel all and exit
	if intent.Freeze {
		return []OrderDecision{{CancelAll: true, Reason: "freeze_active"}}
	}

	// Check position confidence
	if pos.Confidence < 0.7 {
		// Position not trusted, don't trade
		return decisions
	}

	// Calculate desired position based on Intent
	desiredYes, desiredNo := e.calculateDesiredPosition(tick, intent, pos.YesShares, pos.NoShares)

	// Determine what orders to place
	decisions = e.generateOrders(tick, intent, pos.YesShares, pos.NoShares, desiredYes, desiredNo)

	return decisions
}

// calculateDesiredPosition determines target YES/NO shares based on Intent.
// This implements the "dual-channel control" and "bidirectional position" logic.
func (e *Executor) calculateDesiredPosition(
	tick types.MarketTick,
	intent types.Intent,
	currentYes, currentNo float64,
) (desiredYes, desiredNo float64) {
	// Base risk budget
	budget := intent.RiskDeltaMax
	if budget <= 0 {
		return currentYes, currentNo
	}

	// Normal mode: mean-reversion, balanced positions
	// Shock mode: directional, asymmetric positions
	modeMix := intent.ModeMix // 1 = fully Normal, 0 = fully Shock

	// Calculate target allocation based on bias
	biasYes := intent.BiasYes // [-1, 1]

	// Normal mode allocation: aim for balanced positions
	normalYesAlloc := 0.5 + biasYes*0.2  // Slight bias, but stay balanced
	normalNoAlloc := 1.0 - normalYesAlloc

	// Shock mode allocation: directional, asymmetric
	shockYesAlloc := 0.5 + biasYes*0.4  // Strong directional bias
	shockNoAlloc := 1.0 - shockYesAlloc

	// Mix the two modes
	yesAlloc := modeMix*normalYesAlloc + (1-modeMix)*shockYesAlloc
	noAlloc := modeMix*normalNoAlloc + (1-modeMix)*shockNoAlloc

	// Calculate total desired notional value
	// For binary markets: YES + NO ≈ 1 (with small spread)
	totalCost := tick.BestAsk + (1 - tick.BestAsk) // Approximation
	if totalCost <= 0 {
		totalCost = 1.0
	}

	// Allocate budget between YES and NO
	yesBudget := budget * yesAlloc
	noBudget := budget * noAlloc

	// Convert to shares
	yesPrice := tick.BestAsk
	if yesPrice <= 0 {
		yesPrice = tick.PYes
	}
	noPrice := 1 - tick.BestAsk // More accurate than 1 - PYes
	if noPrice <= 0 {
		noPrice = 1 - tick.PYes
	}

	desiredYes = currentYes + (yesBudget / clampPrice(yesPrice))
	desiredNo = currentNo + (noBudget / clampPrice(noPrice))

	return desiredYes, desiredNo
}

// generateOrders creates order decisions to move from current to desired position.
func (e *Executor) generateOrders(
	tick types.MarketTick,
	intent types.Intent,
	currentYes, currentNo, desiredYes, desiredNo float64,
) []OrderDecision {
	decisions := []OrderDecision{}

	// Calculate deltas
	deltaYes := desiredYes - currentYes
	deltaNo := desiredNo - currentNo

	// Minimum order size (Polymarket requirement: ~5 shares)
	minSize := 5.0

	// Generate YES order if needed
	if math.Abs(deltaYes) >= minSize {
		side := types.SideYes
		if deltaYes < 0 {
			// Would need to sell, but Polymarket binary markets don't support selling
			// Instead, buy NO to reduce YES exposure
			// For now, skip selling
			return decisions
		}

		price := e.calculateOrderPrice(tick, side, intent.ModeMix)
		size := math.Abs(deltaYes)

		decisions = append(decisions, OrderDecision{
			Side:   side,
			Price:  price,
			Size:   size,
			Reason: e.formatReason("YES", deltaYes, intent.ModeMix),
		})
	}

	// Generate NO order if needed
	if math.Abs(deltaNo) >= minSize {
		side := types.SideNo
		if deltaNo < 0 {
			// Would need to sell NO, buy YES instead
			// For now, skip selling
			return decisions
		}

		price := e.calculateOrderPrice(tick, side, intent.ModeMix)
		size := math.Abs(deltaNo)

		decisions = append(decisions, OrderDecision{
			Side:   side,
			Price:  price,
			Size:   size,
			Reason: e.formatReason("NO", deltaNo, intent.ModeMix),
		})
	}

	return decisions
}

// calculateOrderPrice determines optimal order price based on mode and market conditions.
// Now includes dynamic slippage tolerance based on token price.
func (e *Executor) calculateOrderPrice(tick types.MarketTick, side types.Side, modeMix float64) float64 {
	var basePrice float64
	var maxAllowedPrice float64

	if side == types.SideYes {
		// Normal mode: use ask price (aggressive)
		// Shock mode: use midpoint or slightly better (less aggressive)
		if modeMix > 0.5 {
			// Normal mode: take liquidity
			basePrice = tick.BestAsk
		} else {
			// Shock mode: provide liquidity (midpoint)
			if tick.BestBid > 0 && tick.BestAsk > 0 {
				basePrice = (tick.BestBid + tick.BestAsk) / 2
			} else {
				basePrice = tick.PYes
			}
		}
		if basePrice <= 0 {
			basePrice = tick.PYes
		}

		// Apply dynamic slippage tolerance
		maxSlippage := getMaxSlippage(basePrice)
		maxAllowedPrice = basePrice * (1 + maxSlippage)

		// Use basePrice, but ensure we don't exceed maxAllowedPrice
		price := basePrice
		if price > maxAllowedPrice {
			price = maxAllowedPrice
		}
		return clampPrice(price)
	} else {
		// NO side
		noAsk := 1 - tick.BestBid // NO ask is inverse of YES bid
		noBid := 1 - tick.BestAsk // NO bid is inverse of YES ask

		if modeMix > 0.5 {
			// Normal mode: take liquidity
			basePrice = noAsk
		} else {
			// Shock mode: provide liquidity
			if noBid > 0 && noAsk > 0 {
				basePrice = (noBid + noAsk) / 2
			} else {
				basePrice = 1 - tick.PYes
			}
		}
		if basePrice <= 0 {
			basePrice = 1 - tick.PYes
		}

		// Apply dynamic slippage tolerance
		maxSlippage := getMaxSlippage(basePrice)
		maxAllowedPrice = basePrice * (1 + maxSlippage)

		// Use basePrice, but ensure we don't exceed maxAllowedPrice
		price := basePrice
		if price > maxAllowedPrice {
			price = maxAllowedPrice
		}
		return clampPrice(price)
	}
}

func (e *Executor) formatReason(side string, delta float64, modeMix float64) string {
	mode := "Normal"
	if modeMix < 0.5 {
		mode = "Shock"
	}
	return fmt.Sprintf("%s mode: %s delta=%.2f", mode, side, delta)
}

func clampPrice(p float64) float64 {
	if p < 0.01 {
		return 0.01
	}
	if p > 0.99 {
		return 0.99
	}
	return p
}


