package strategy

import (
	"fmt"
	"strconv"

	clobtypes "polymarket-btc-bot/internal/clob/types"
)

// AffordableLiquidity represents liquidity available within price constraints
type AffordableLiquidity struct {
	TotalSize float64 // Total shares available
	TotalCost float64 // Total USDC cost
	AvgPrice  float64 // Average price
	BestPrice float64 // Best (lowest for buy, highest for sell) price
}

// AnalyzeOrderBookDepth analyzes order book depth to find affordable liquidity.
// This is adapted from CopyTrader's proven approach.
// For BUY orders: finds asks at or below maxPrice
// For SELL orders: finds bids at or above minPrice
func AnalyzeOrderBookDepth(book *clobtypes.OrderBookSummary, side string, maxBudget float64, maxPrice float64) (*AffordableLiquidity, error) {
	if book == nil {
		return nil, fmt.Errorf("order book is nil")
	}

	var levels []clobtypes.OrderSummary
	var isBuy bool

	if side == "BUY" || side == "YES" {
		isBuy = true
		levels = book.Asks
	} else if side == "SELL" || side == "NO" {
		isBuy = false
		levels = book.Bids
	} else {
		return nil, fmt.Errorf("unknown side: %s", side)
	}

	if len(levels) == 0 {
		return &AffordableLiquidity{
			TotalSize: 0,
			TotalCost: 0,
			AvgPrice:  0,
			BestPrice: 0,
		}, nil
	}

	// Parse first level for best price
	bestPrice, err := strconv.ParseFloat(levels[0].Price, 64)
	if err != nil {
		return nil, fmt.Errorf("failed to parse best price: %w", err)
	}

	// Calculate affordable liquidity
	totalSize := 0.0
	totalCost := 0.0
	remainingBudget := maxBudget

	for _, level := range levels {
		price, err := strconv.ParseFloat(level.Price, 64)
		if err != nil {
			continue // Skip invalid prices
		}

		size, err := strconv.ParseFloat(level.Size, 64)
		if err != nil {
			continue // Skip invalid sizes
		}

		// For BUY: stop if price exceeds maxPrice
		// For SELL: stop if price is below minPrice (which is maxPrice for sell)
		if isBuy && price > maxPrice {
			break
		}
		if !isBuy && price < maxPrice {
			break
		}

		levelCost := price * size

		if totalCost+levelCost <= remainingBudget {
			// Can take entire level
			totalSize += size
			totalCost += levelCost
		} else {
			// Partial fill at this level
			remainingForLevel := remainingBudget - totalCost
			partialSize := remainingForLevel / price
			totalSize += partialSize
			totalCost += remainingForLevel
			break
		}
	}

	avgPrice := 0.0
	if totalSize > 0 {
		avgPrice = totalCost / totalSize
	}

	return &AffordableLiquidity{
		TotalSize: totalSize,
		TotalCost: totalCost,
		AvgPrice:  avgPrice,
		BestPrice: bestPrice,
	}, nil
}

