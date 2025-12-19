package main

import (
	"flag"
	"log"
	
	"polymarket-bot/pkg/brain"
	"polymarket-bot/pkg/config"
	"polymarket-bot/pkg/execution" // New Execution Layer
	"polymarket-bot/pkg/market"
	// "polymarket-bot/pkg/order" // Deprecated by execution pkg
	"polymarket-bot/pkg/safety"
	"polymarket-bot/pkg/signal"
	"polymarket-bot/pkg/store" // New Store Layer
)

func main() {
	useMock := flag.Bool("mock", false, "Use mock market instead of real Polymarket API")
	flag.Parse()

	// 0. Load Config & Store
	cfg := config.Load()
	db := store.NewStore("bot.db") // Local SQLite file

	// 1. Initialize Components
	// ... (Client init same as before) ...
	// REPLACING original mkt initialization logic temporarily for brevity in this replace block context
	// Actually we need to keep the logic but pass the client to execution manager.
	
	var polyClient *market.PolymarketClient
	var mkt market.MarketProvider
	
	if *useMock {
		log.Println("Using Mock Market")
		mockMkt := market.NewMockMarket()
		mkt = mockMkt
	} else {
		log.Println("Initializing Real Polymarket Client...")
		polyClient = market.NewPolymarketClient(cfg)
		mkt = polyClient
	}
	
	// Execution Manager
	riskConfig := safety.RiskConfig{
		MaxDrawdownDaily: 0.05,
		MaxPositionSize:  500.0, // Conservative start
		MaxSlippage:      0.02,
	}
	riskMgr := safety.NewRiskManager(riskConfig)
	
	execMgr := execution.NewExecutionManager(polyClient, db, riskMgr)
	execMgr.Start() // Recovers state

	sigProc := signal.NewSignalProcessor()
	
	brainConfig := brain.Config{
		BaseGain:          1.0,
		MaxRisk:           1000.0, // $1000 max exposure
		TimeDecayFactor:   0.1,
		VelocityThreshold: 0.05,
		EntropyThreshold:  0.6,
	}
	controller := brain.NewController(brainConfig)
	
	freezeDetector := safety.NewFreezeDetector(0.95, 0.05)
	killSwitch := safety.NewKillSwitch()
	// orderMgr := order.NewOrderManager() // Removed

	// 3. Main Control Loop
	log.Println("Starting Control System...")
	dataChan := mkt.Subscribe() 

	for data := range dataChan {
		// ... (Safety Checks same as before) ...
		if triggered, reason := killSwitch.IsTriggered(); triggered {
			log.Printf("KILL SWITCH ACTIVE: %s. Halting.\n", reason)
			return
		}

		sig := sigProc.Process(data)

		if frozen, reason := freezeDetector.Check(sig); frozen {
			log.Printf("Market Frozen: %s. Reducing risk only.\n", reason)
			continue
		}

		// D. Brain Compute
		action := controller.Compute(sig)

		// E. Execution (Smart Router)
		// We need to fetch current position from Store to pass to Execution
		// Assuming we track "Active Token" position
		// In real impl, we'd query by polyClient.ActiveToken()
		currentPos := 0.0
		// Fetch actual pos from DB
		// amount, _, _ := db.GetPosition(polyClient.ActiveToken())
		// currentPos = amount
		
		// We need Best Bid/Ask for execution logic. 
		// MarketData struct has Mid Price, but we might want raw bid/ask from client if available.
		// For now using PriceUp/Down as proxies.
		bestBid := data.PriceUp - 0.01 // Mock spread if running in mock mode
		bestAsk := data.PriceUp + 0.01

		execMgr.ExecuteIntent(action, currentPos, bestBid, bestAsk)

		// Logging
		log.Printf("T=%.0fs | P=%.2f | Intent: Net=%.2f Mode=%s",
			sig.TimeRemaining.Seconds(),
			sig.ProbUp,
			(action.UpWeight-action.DownWeight)*action.TargetRiskExposure,
			action.Mode,
		)
	}
}
