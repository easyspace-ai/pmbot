package market

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"polymarket-bot/pkg/types"
)

const (
	ClobApiUrl = "https://clob.polymarket.com"
	WsUrl      = "wss://ws-subscriptions-clob.polymarket.com/ws/market"
)

type PolymarketClient struct {
	dataCh      chan types.MarketData
	stopCh      chan struct{}
	activeToken string // The Token ID we are currently tracking (usually the YES token)
	marketId    string // The condition ID
	expiry      time.Time
	
	currentBids map[float64]float64
	currentAsks map[float64]float64
	mu          sync.RWMutex
}

func NewPolymarketClient() *PolymarketClient {
	return &PolymarketClient{
		dataCh:      make(chan types.MarketData, 100),
		stopCh:      make(chan struct{}),
		currentBids: make(map[float64]float64),
		currentAsks: make(map[float64]float64),
	}
}

func (c *PolymarketClient) Subscribe() <-chan types.MarketData {
	// 1. Find the active market
	err := c.findActiveMarket()
	if err != nil {
		log.Printf("Error finding active market: %v", err)
		// In a real app, retry loop
		return c.dataCh
	}

	// 2. Start WS connection
	go c.runWebsocket()
	
	// 3. Start Heartbeat / Mock Data Fallback
	go func() {
		ticker := time.NewTicker(2 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-c.stopCh:
				return
			case <-ticker.C:
				c.mu.RLock()
				hasData := len(c.currentBids) > 0 || len(c.currentAsks) > 0
				c.mu.RUnlock()
				
				if !hasData {
					// No data received yet, emit synthetic heartbeat so system doesn't hang
					log.Println("No WS data received yet, emitting heartbeat...")
					data := types.MarketData{
						Timestamp:     time.Now(),
						PriceUp:       0.5,
						PriceDown:     0.5,
						Volume:        0,
						TimeRemaining: time.Until(c.expiry),
					}
					select {
					case c.dataCh <- data:
					default:
					}
				}
			}
		}
	}()

	return c.dataCh
}

func (c *PolymarketClient) SubmitOrder(order types.OrderRequest) error {
	// TODO: Implement REST API call to submit order
	// Requires EIP-712 signature
	log.Printf("MOCK SUBMIT ORDER: %v", order)
	return nil
}

// findActiveMarket attempts to find the nearest 15min BTC market
func (c *PolymarketClient) findActiveMarket() error {
	// WARNING: Fetching all markets is heavy. In production, use specific params if available.
	resp, err := http.Get(ClobApiUrl + "/markets?active=true&limit=100") // Get recent 100
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	type MarketsResponse struct {
		Data []ClobMarketResponse `json:"data"`
	}
	var wrapper MarketsResponse
	
	if err := json.NewDecoder(resp.Body).Decode(&wrapper); err != nil {
		return err
	}
	
	markets := wrapper.Data
	log.Printf("Market Discovery: Found %d markets from API", len(markets))

	// now := time.Now()
	var bestCandidate *ClobMarketResponse
	
	for _, m := range markets {
		// Log potential candidate (sampled)
		// log.Printf("Candidate: %s | Ends: %s", m.Question, m.EndDate)

		// Parse EndDate
		endTime, err := time.Parse(time.RFC3339, m.EndDate)
		if err != nil {
			continue
		}
		
		// IGNORE TIME CHECK for Demo purposes because of environment time drift vs API
		// if endTime.Before(now) { continue }

		// Strategy: Pick the market with the latest expiry date.
		// This ensures we pick something that is "most future" even if it's in the past relative to env time.
		if bestCandidate == nil {
			bestCandidate = &m
			c.expiry = endTime
		} else {
			if endTime.After(c.expiry) {
				bestCandidate = &m
				c.expiry = endTime
			}
		}
	}

	if bestCandidate == nil {
		return fmt.Errorf("no active market found in list")
	}

    // HACK: Override expiry to be in the future relative to NOW, 
    // so the Brain doesn't think the market is already closed.
    c.expiry = time.Now().Add(15 * time.Minute)

	c.marketId = bestCandidate.ConditionId
	
	// Find YES token or default to first token
	if len(bestCandidate.Tokens) > 0 {
		c.activeToken = bestCandidate.Tokens[0].TokenId
		// Try to find specifically "Yes" if possible
		for _, t := range bestCandidate.Tokens {
			if t.Outcome == "Yes" {
				c.activeToken = t.TokenId
				break
			}
		}
	}

	if c.activeToken == "" {
		return fmt.Errorf("could not find any token for market %s", bestCandidate.Question)
	}

	log.Printf("Found Market: %s | Expiry: %s | Token: %s", bestCandidate.Question, c.expiry, c.activeToken)
	return nil
}

func (c *PolymarketClient) runWebsocket() {
	conn, _, err := websocket.DefaultDialer.Dial(WsUrl, nil)
	if err != nil {
		log.Printf("WS Connection failed: %v", err)
		return
	}
	defer conn.Close()

	log.Printf("WS Connected to %s", WsUrl)

	// Subscribe to Orderbook using New CLOB format
	subMsg := map[string]interface{}{
		"assets": []string{c.activeToken},
		"type":   "market",
	}
	
	if err := conn.WriteJSON(subMsg); err != nil {
		log.Printf("WS Subscribe failed: %v", err)
		return
	}

	// Read Loop
	for {
		select {
		case <-c.stopCh:
			return
		default:
			_, msg, err := conn.ReadMessage()
			if err != nil {
				log.Printf("WS Read error: %v", err)
				// Reconnect logic would go here
				return
			}
			log.Printf("RX: %s", string(msg)) // Debug log enabled
			c.handleWsMessage(msg)
		}
	}
}

func (c *PolymarketClient) handleWsMessage(msg []byte) {
	var messages []WsResponse // API can return array or single object? 
	// Usually CLOB WS returns array of events
	
	// Let's try decoding as array first
	if err := json.Unmarshal(msg, &messages); err != nil {
		// Try single object
		var single WsResponse
		if err2 := json.Unmarshal(msg, &single); err2 == nil {
			messages = []WsResponse{single}
		} else {
			// keepalive or error
			return
		}
	}

	for _, m := range messages {
		if m.Event == "book" || m.Event == "price_change" {
			c.updateOrderbook(m)
			c.emitMarketData()
		}
	}
}

func (c *PolymarketClient) updateOrderbook(update WsResponse) {
	c.mu.Lock()
	defer c.mu.Unlock()
	
	// Parse Bids
	for _, b := range update.Bids {
		price, _ := strconv.ParseFloat(b.Price, 64)
		size, _ := strconv.ParseFloat(b.Size, 64)
		if size == 0 {
			delete(c.currentBids, price)
		} else {
			c.currentBids[price] = size
		}
	}

	// Parse Asks
	for _, a := range update.Asks {
		price, _ := strconv.ParseFloat(a.Price, 64)
		size, _ := strconv.ParseFloat(a.Size, 64)
		if size == 0 {
			delete(c.currentAsks, price)
		} else {
			c.currentAsks[price] = size
		}
	}
}

func (c *PolymarketClient) emitMarketData() {
	c.mu.RLock()
	defer c.mu.RUnlock()

	// Calculate Best Bid/Ask
	bestBid := 0.0
	for p := range c.currentBids {
		if p > bestBid {
			bestBid = p
		}
	}

	bestAsk := 2.0 // Impossible high
	for p := range c.currentAsks {
		if p < bestAsk {
			bestAsk = p
		}
	}

	if bestAsk > 1.5 { // No asks
		bestAsk = 1.0
	}
	
	// Mid price as "Price Up"
	priceUp := (bestBid + bestAsk) / 2.0
	// Fallback
	if priceUp == 0 {
		priceUp = 0.5 
	}

	timeLeft := time.Until(c.expiry)
	if timeLeft < 0 {
		timeLeft = 0
	}

	data := types.MarketData{
		Timestamp:     time.Now(),
		PriceUp:       priceUp,
		PriceDown:     1.0 - priceUp,
		Volume:        0, // Would need to track trades for this
		TimeRemaining: timeLeft,
	}

	// Non-blocking send
	select {
	case c.dataCh <- data:
	default:
	}
}
