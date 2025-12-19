package main

import (
	"flag"
	"log"
	
	"polymarket-bot/pkg/brain"
	"polymarket-bot/pkg/market"
	"polymarket-bot/pkg/order"
	"polymarket-bot/pkg/safety"
	"polymarket-bot/pkg/signal"
)

func main() {
	useMock := flag.Bool("mock", false, "Use mock market instead of real Polymarket API")
	flag.Parse()

	// 1. Initialize Components
	var mkt market.MarketProvider
	
	if *useMock {
		log.Println("Using Mock Market")
		mockMkt := market.NewMockMarket()
		mkt = mockMkt
		// Start Mock Simulation (same as before)
		go func() {
			// ... simple mock loop can be added here if needed, or rely on MockMarket implementation details
		}()
	} else {
		log.Println("Initializing Real Polymarket Client...")
		polyClient := market.NewPolymarketClient()
		mkt = polyClient
	}

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
	orderMgr := order.NewOrderManager()

	// 3. Main Control Loop
	log.Println("Starting Control System...")
	dataChan := mkt.Subscribe() // This blocks for real client until market is found

	for data := range dataChan {
		// A. Check Kill Switch
		if triggered, reason := killSwitch.IsTriggered(); triggered {
			log.Printf("KILL SWITCH ACTIVE: %s. Halting.\n", reason)
			return
		}

		// B. Process Signal
		sig := sigProc.Process(data)

		// C. Check Freeze
		if frozen, reason := freezeDetector.Check(sig); frozen {
			log.Printf("Market Frozen: %s. Reducing risk only.\n", reason)
			continue
		}

		// D. Brain Compute
		action := controller.Compute(sig)

		// E. Order Execution
		orderMgr.ProcessAction(action)

		// Logging
		log.Printf("T=%.0fs | P=%.2f | V=%.3f | Ent=%.2f | Mode=%s | Intent: Up=%.2f Down=%.2f Exp=%.2f\n",
			sig.TimeRemaining.Seconds(),
			sig.ProbUp,
			sig.Velocity,
			sig.Entropy,
			action.Mode,
			action.UpWeight,
			action.DownWeight,
			action.TargetRiskExposure,
		)
	}
}
