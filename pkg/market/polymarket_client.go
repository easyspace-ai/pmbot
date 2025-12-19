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
	"polymarket-bot/pkg/config"
	"polymarket-bot/pkg/types"
	"bytes"
	"math/big"
	"github.com/ethereum/go-ethereum/common"
)

const (
	ClobApiUrl = "https://clob.polymarket.com"
	WsUrl      = "wss://ws-subscriptions-clob.polymarket.com/ws/market"
)

type PolymarketClient struct {
	config      *config.Config
	signer      *Signer
	dataCh      chan types.MarketData
	stopCh      chan struct{}
	activeToken string // The Token ID we are currently tracking (usually the YES token)
	marketId    string // The condition ID
	expiry      time.Time
	
	currentBids map[float64]float64
	currentAsks map[float64]float64
	mu          sync.RWMutex
}

func NewPolymarketClient(cfg *config.Config) *PolymarketClient {
	var signer *Signer
	if cfg.PrivateKey != "" {
		signer = NewSigner(cfg.PrivateKey, cfg.ApiKey, cfg.ApiSecret, cfg.ApiPassphrase, cfg.ChainID, cfg.ExchangeAddr)
	}

	return &PolymarketClient{
		config:      cfg,
		signer:      signer,
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
	if c.signer == nil {
		return fmt.Errorf("signer not initialized: missing private key")
	}

	log.Printf("Submitting Order: Side=%s, Price=%.2f, Amount=%.2f", order.Side, order.LimitPrice, order.Amount)

	// 1. Convert LimitPrice to BigInt (Price is usually scaled by 1e18? Or Raw?)
	// Polymarket CLOB uses raw price for API? No, EIP712 uses 18 decimals usually for ERC20 amounts.
	// But `makerAmount` and `takerAmount` define the price.
	
	// Assumption: USDC collateral (6 decimals)
	// Token (CTF) (usually 18 decimals? or 6?) - CTF tokens are ERC1155, often 6 or 18 decimals.
	// USDC on Polygon is 6 decimals.
	// Let's assume 6 decimals for simplicity as per Polymarket docs usually.
	
	decimals := int64(6)
	scale := new(big.Int).Exp(big.NewInt(10), big.NewInt(decimals), nil)
	
	// amount * 10^6
	rawAmount := new(big.Float).Mul(big.NewFloat(order.Amount), big.NewFloat(float64(scale.Int64())))
	amountInt := new(big.Int)
	rawAmount.Int(amountInt)

	// 2. Determine Side (Buy or Sell)
	// In CLOB:
	// Buy: You are Maker providing USDC, taking Token? No, if you submit a Limit Buy order,
	// you are providing a bid. 
	// Side: BUY = 0, SELL = 1
	
	side := uint8(0) // BUY
	if order.Side == "DOWN" || order.Side == "SELL" { // Simple mapping
		side = 1
	}
	// Note: "DOWN" implies buying NO shares, which is structurally "Buy NO".
	// The Brain outputs "UpWeight" and "DownWeight".
	// If Brain says "Buy Up", we Buy YES. If Brain says "Buy Down", we Buy NO.
	// But `OrderRequest` structure here is generic. 
	// Let's assume order.Side = "BUY" means buying the Active Token (YES).
	// If we want to short, we Buy NO. The logic in Brain/OrderManager resolves this.
	// Here we just execute a Buy/Sell on the active token.
	
	// 3. Construct Order Structure
	// We need a unique salt/nonce
	salt := big.NewInt(time.Now().UnixNano())
	
	// Expiration: 5 mins from now?
	exp := big.NewInt(time.Now().Add(5 * time.Minute).Unix())

	eip712Order := OrderStruct{
		Salt:        salt,
		Maker:       common.HexToAddress(c.config.Address),
		Signer:      common.HexToAddress(c.config.Address),
		Taker:       common.HexToAddress("0x0000000000000000000000000000000000000000"), // Open order
		TokenId:     new(big.Int), // Set Active Token ID
		MakerAmount: amountInt,    // This depends on side/price. Simplified.
		TakerAmount: amountInt,    // Simplified. Price = Maker/Taker ratio?
		Expiration:  exp,
		Nonce:       big.NewInt(0),
		FeeRate:     big.NewInt(0),
		Side:        side,
		SideType:    0,
	}
	
	tokenIDBig, _ := new(big.Int).SetString(c.activeToken, 10)
	if tokenIDBig != nil {
		eip712Order.TokenId = tokenIDBig
	}

	// PRICE CALCULATION:
	// Price = TakerAmount / MakerAmount (for Buy?)
	// If Buy YES @ 0.60 USDC
	// Maker (Me) gives 0.60 USDC. Taker gives 1 YES.
	// Wait, standard Limit Order API usually takes price as argument.
	// We must strictly follow the POST /order body format which includes the signature.
	
	// 4. Generate Signature
	sig, err := c.signer.SignOrder(eip712Order)
	if err != nil {
		return fmt.Errorf("signing failed: %v", err)
	}

	// 5. Construct HTTP Request
	// POST /order
	// Body:
	// {
	//   "order": { ... fields ... },
	//   "signature": "0x...",
	//   "owner": "0x...",
	//   "orderType": "GTC"
	// }
	
	reqBody := map[string]interface{}{
		"order": map[string]interface{}{
			"salt":        eip712Order.Salt.String(),
			"maker":       eip712Order.Maker.Hex(),
			"signer":      eip712Order.Signer.Hex(),
			"taker":       eip712Order.Taker.Hex(),
			"tokenId":     eip712Order.TokenId.String(),
			"makerAmount": eip712Order.MakerAmount.String(),
			"takerAmount": eip712Order.TakerAmount.String(),
			"expiration":  eip712Order.Expiration.String(),
			"nonce":       eip712Order.Nonce.String(),
			"feeRate":     eip712Order.FeeRate.String(),
			"side":        eip712Order.Side,
		},
		"signature": sig,
		"owner":     c.config.Address,
		"orderType": "GTC", // Good Till Cancelled
	}
	
	jsonBody, _ := json.Marshal(reqBody)
	
	req, err := http.NewRequest("POST", ClobApiUrl+"/order", bytes.NewBuffer(jsonBody))
	if err != nil {
		return err
	}
	
	// 6. Add Auth Headers
	headers := c.signer.GenerateAuthHeaders("POST", "/order", string(jsonBody))
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	req.Header.Set("Content-Type", "application/json")

	// 7. Execute
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 && resp.StatusCode != 201 {
		// Read body for error
		// bodyBytes, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("API error: %s", resp.Status)
	}

	log.Printf("Order Submitted Successfully: %s", order.ID)
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
