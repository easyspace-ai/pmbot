package execution

import (
	"log"
	"math" // Add import
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

	// 2. Start listening to order updates
	// em.orderUpdateCh = em.client.SubscribeOrders() 
	// We consume directly in processOrderUpdates
	
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
		// MAKER STRATEGY: Passive execution (Repricing)
		side := "BUY"
		if diff < 0 {
			side = "SELL"
			diff = -diff
		}
		
		limitPrice := bestBid 
		if side == "BUY" {
			limitPrice = bestBid
			if limitPrice == 0 { limitPrice = bestAsk * 0.99 }
		} else {
			limitPrice = bestAsk
			if limitPrice == 0 { limitPrice = bestBid * 1.01 }
		}
		
		// 1. Check existing orders
		orders, _ := em.store.GetOpenOrders()
		activeToken := em.client.ActiveToken()
		
		var existingOrderID string
		var existingPrice float64
		
		for _, o := range orders {
			if o["token_id"] == activeToken && o["side"] == side {
				existingOrderID = o["id"].(string)
				existingPrice = o["price"].(float64)
				break
			}
		}
		
		if existingOrderID != "" {
			// Check Reprice Condition (e.g. price deviation > 1%)
			// Or if we are not at top of book
			priceDiff := math.Abs(existingPrice - limitPrice)
			if priceDiff > limitPrice * 0.005 { // 0.5% tolerance
				log.Printf("Repricing Order %s: Old=%.2f New=%.2f", existingOrderID, existingPrice, limitPrice)
				// Cancel
				em.cancelOrder(existingOrderID)
				// Place New (Next Loop? Or Immediately?)
				// Immediate to avoid missing out
				em.placeOrder(side, diff, limitPrice, "GTC")
			}
			// If price is good, do nothing (keep resting)
		} else {
			// No open order, place one
			em.placeOrder(side, diff, limitPrice, "GTC")
		}
	}
}

func (em *ExecutionManager) cancelOrder(orderID string) {
	err := em.client.CancelOrder(orderID)
	if err != nil {
		log.Printf("Cancel Failed: %v", err)
	} else {
		em.store.UpdateOrderStatus(orderID, "CANCELED", 0)
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
		// Use auto-generated ID as Client ID. Exchange ID might be different but we don't have it synchronously unless API returns it.
		// For EIP712, we can pre-calculate Order Hash if we want deep tracking.
		// For now, storing under ID.
		tokenID := em.client.ActiveToken()
		err = em.store.SaveOrder(req.ID, req.ID, tokenID, side, "NEW", price, size)
		if err != nil {
			log.Printf("Failed to save order to DB: %v", err)
		}
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
	updates := em.client.SubscribeOrders()
	for update := range updates {
		log.Printf("Order Update: ID=%s Status=%s Filled=%.2f", update.OrderID, update.Status, update.FilledSize)
		
		// Update DB
		err := em.store.UpdateOrderStatus(update.OrderID, update.Status, update.FilledSize)
		if err != nil {
			log.Printf("Failed to update order status in DB: %v", err)
		}
		
		// Update Risk Manager (Equity / Position) if Filled
		// Ideally we delta update based on FilledSize change.
		// For now, simple logging. Real impl would query positions from chain or update local tracking.
	}
}
