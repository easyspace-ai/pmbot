package main

import (
	"fmt"
	"math/rand"
	"time"

	"polymarket-bot/pkg/brain"
	"polymarket-bot/pkg/market"
	"polymarket-bot/pkg/order"
	"polymarket-bot/pkg/safety"
	"polymarket-bot/pkg/signal"
	"polymarket-bot/pkg/types"
)

func main() {
	// 1. Initialize Components
	mkt := market.NewMockMarket()
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

	// 2. Start Simulation Loop (Data Producer)
	go func() {
		price := 0.5
		timeLeft := 900.0 // 15 mins
		
		for timeLeft > 0 {
			// Random walk for price
			change := (rand.Float64() - 0.5) * 0.05
			price += change
			if price > 0.99 { price = 0.99 }
			if price < 0.01 { price = 0.01 }

			data := types.MarketData{
				Timestamp:     time.Now(),
				PriceUp:       price,
				PriceDown:     1 - price,
				Volume:        1000,
				TimeRemaining: time.Duration(timeLeft) * time.Second,
			}

			mkt.DataCh <- data
			timeLeft -= 1.0
			time.Sleep(100 * time.Millisecond) // Speed up simulation
		}
	}()

	// 3. Main Control Loop
	fmt.Println("Starting Control System...")
	dataChan := mkt.Subscribe()

	for data := range dataChan {
		// A. Check Kill Switch
		if triggered, reason := killSwitch.IsTriggered(); triggered {
			fmt.Printf("KILL SWITCH ACTIVE: %s. Halting.\n", reason)
			return
		}

		// B. Process Signal
		sig := sigProc.Process(data)

		// C. Check Freeze
		if frozen, reason := freezeDetector.Check(sig); frozen {
			fmt.Printf("Market Frozen: %s. Reducing risk only.\n", reason)
			// In a real system, we'd switch to a specific "Reduce Only" mode here
			continue
		}

		// D. Brain Compute
		action := controller.Compute(sig)

		// E. Order Execution
		orderMgr.ProcessAction(action)

		// Logging
		fmt.Printf("T=%.0fs | P=%.2f | V=%.3f | Ent=%.2f | Mode=%s | Intent: Up=%.2f Down=%.2f Exp=%.2f\n",
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
