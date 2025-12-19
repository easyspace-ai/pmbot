package execution

import (
	"log"
	"sync"
	"time"

	"polymarket-bot/pkg/market"
	"polymarket-bot/pkg/safety" // Add import
	"polymarket-bot/pkg/store"
	"polymarket-bot/pkg/types"
)

// ExecutionManager handles the lifecycle of orders
type ExecutionManager struct {
	client *market.PolymarketClient
	store  *store.Store
	risk   *safety.RiskManager // Add RiskManager
	
	// Local Cache of Nonce to prevent collisions
	nonceMu sync.Mutex
	nonce   int64

	// Channels
	orderUpdateCh chan types.OrderUpdate // From WS
}

func NewExecutionManager(client *market.PolymarketClient, db *store.Store, risk *safety.RiskManager) *ExecutionManager {
	return &ExecutionManager{
		client:        client,
		store:         db,
		risk:          risk,
		orderUpdateCh: make(chan types.OrderUpdate, 100),
	}
}

func (em *ExecutionManager) Start() {
	// 1. Recover Open Orders from DB
	em.reconcileOrders()

	// 2. Start listening to order updates (Simulated via Client callbacks in real version)
	go em.processOrderUpdates()
}

// ExecuteIntent converts high-level intent to specific orders
func (em *ExecutionManager) ExecuteIntent(action types.ControlAction, currentPos float64, bestBid, bestAsk float64) {
	// 1. Calculate Target Position Size
	// Action.TargetRiskExposure is in USDC.
	// If Price = 0.5, TargetPos = 1000 / 0.5 = 2000 shares.
	
	price := bestAsk
	if price == 0 { price = 0.5 } // Safety

	targetShares := action.TargetRiskExposure / price
	_ = targetShares // Suppress unused var (will use later for inventory skew logic)
	
	// Apply Direction Bias
	// Action.UpWeight (0-1). If 0.8, we want 80% Long Exposure?
	// Simplified: ControlAction returns "Net Target".
	// Let's assume TargetRiskExposure is signed? Or we use UpWeight to determine side.
	
	// Net Target = Exposure * (UpWeight - DownWeight)
	// If Up=1.0, Net = Exp * 1. Positive = Long YES.
	// If Down=1.0, Net = Exp * -1. Negative = Short YES (or Long NO).
	
	netTargetShares := (action.TargetRiskExposure / price) * (action.UpWeight - action.DownWeight)
	
	diff := netTargetShares - currentPos
	
	// Threshold to avoid dust trading
	if diff > -10 && diff < 10 {
		return
	}

	log.Printf("EXEC: Curr=%.0f, Target=%.0f, Diff=%.0f, Mode=%s", currentPos, netTargetShares, diff, action.Mode)

	// 2. Risk Check
	err := em.risk.CheckTrade(string(action.Mode), diff, price, bestBid, bestAsk)
	if err != nil {
		log.Printf("RISK REJECT: %v", err)
		return
	}

	// 3. Execution Strategy
	if action.Mode == types.ModeShock {
		// TAKER STRATEGY: Immediate execution
		side := "BUY"
		if diff < 0 {
			side = "SELL"
			diff = -diff
		}
		
		// In Shock mode, we cross the spread
		execPrice := bestAsk * 1.05 // 5% slippage allowance for Buy
		if side == "SELL" {
			execPrice = bestBid * 0.95
		}
		
		em.placeOrder(side, diff, execPrice, "IOC") // Immediate or Cancel
		
	} else {
		// MAKER STRATEGY: Passive execution
		// We want to post orders at best bid/ask
		// Implementation needed: Check if we already have open orders. 
		// If yes, reprice them. If no, place new.
		
		// Simplified for v1: Just place Limit Order at Mid Price or Best Bid
		side := "BUY"
		if diff < 0 {
			side = "SELL"
			diff = -diff
		}
		
		limitPrice := bestBid 
		if side == "BUY" {
			// Join the bid
			limitPrice = bestBid
			if limitPrice == 0 { limitPrice = bestAsk * 0.99 }
		} else {
			// Join the ask
			limitPrice = bestAsk
			if limitPrice == 0 { limitPrice = bestBid * 1.01 }
		}
		
		em.placeOrder(side, diff, limitPrice, "GTC")
	}
}

func (em *ExecutionManager) placeOrder(side string, size, price float64, type_ string) {
	// 1. Generate ID
	// 2. Store "NEW" state in DB
	// 3. Call Client.SubmitOrder
	// 4. Update DB on success/fail
	
	req := types.OrderRequest{
		Side:       side,
		Amount:     size,
		LimitPrice: price,
		ID:         "auto_" + time.Now().Format("150405"),
	}
	
	log.Printf(">>> PLACING ORDER: %s %.2f @ %.2f (%s)", side, size, price, type_)
	
	err := em.client.SubmitOrder(req)
	if err != nil {
		log.Printf("Order Failed: %v", err)
	} else {
		// Save to DB
		// em.store.SaveOrder(...)
	}
}

func (em *ExecutionManager) reconcileOrders() {
	orders, err := em.store.GetOpenOrders()
	if err != nil {
		log.Printf("DB Error: %v", err)
		return
	}
	log.Printf("Reconciled %d open orders from DB", len(orders))
	// Logic to check status with API and cancel if needed
}

func (em *ExecutionManager) processOrderUpdates() {
	// In a real system, this consumes from WS
}
