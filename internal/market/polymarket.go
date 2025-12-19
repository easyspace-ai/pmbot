package market

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"regexp"
	"strconv"
	"time"

	"polymarket-btc-bot/internal/bus"
	"polymarket-btc-bot/internal/types"
)

// Polymarket is a minimal real-data adapter:
// - Market discovery via CLOB /markets
// - Price via polling CLOB /book?token_id=...
//
// Order placement/cancel is scaffolded but may require auth/signing details.
type Polymarket struct {
	log *slog.Logger

	http *http.Client

	baseURL string

	// Market selection
	marketSlugRegex *regexp.Regexp
	// If set, skip discovery and use these directly
	yesTokenID string
	noTokenID  string
	marketID   string

	// Polling
	pollInterval time.Duration

	endDate time.Time

	// Market microstructure
	minTickSize  string
	minOrderSize float64
	negRisk      bool

	// Trading (CLOB L1/L2 auth + EIP712 order signing)
	tradingEnabled bool
	chainID        int64
	privateKey     string
	address        string
	funder         string
	signatureType  uint8
	apiCreds       *apiCreds
}

type PolymarketConfig struct {
	BaseURL         string
	MarketSlugRegex string
	YesTokenID      string
	NoTokenID       string
	MarketID        string
	PollInterval    time.Duration

	TradingEnabled bool
	ChainID        int64
	PrivateKey     string
	Funder         string
	SignatureType  uint8

	// Optional: directly provide L2 API creds (skip L1 create/derive)
	APIKey        string
	APISecret     string
	APIPassphrase string
}

func NewPolymarketFromEnv(log *slog.Logger) *Polymarket {
	cfg := PolymarketConfig{
		BaseURL:         getenvDefault("POLY_CLOB_BASE_URL", "https://clob.polymarket.com"),
		MarketSlugRegex: os.Getenv("POLY_MARKET_SLUG_REGEX"),
		YesTokenID:      os.Getenv("POLY_YES_TOKEN_ID"),
		NoTokenID:       os.Getenv("POLY_NO_TOKEN_ID"),
		MarketID:        os.Getenv("POLY_MARKET_ID"),
		PollInterval:    parseDurationDefault(os.Getenv("POLY_POLL_INTERVAL"), 300*time.Millisecond),

		TradingEnabled: os.Getenv("POLY_TRADING_ENABLED") == "1",
		ChainID:        parseInt64Default(os.Getenv("POLY_CHAIN_ID"), 137),
		PrivateKey:     os.Getenv("POLY_PRIVATE_KEY"),
		Funder:         os.Getenv("POLY_FUNDER"),
		SignatureType:  uint8(parseInt64Default(os.Getenv("POLY_SIGNATURE_TYPE"), 0)),

		APIKey:        os.Getenv("POLY_API_KEY"),
		APISecret:     os.Getenv("POLY_API_SECRET"),
		APIPassphrase: os.Getenv("POLY_API_PASSPHRASE"),
	}
	return NewPolymarket(log, cfg)
}

func NewPolymarket(log *slog.Logger, cfg PolymarketConfig) *Polymarket {
	if log == nil {
		log = slog.Default()
	}
	re := (*regexp.Regexp)(nil)
	if cfg.MarketSlugRegex != "" {
		re = regexp.MustCompile(cfg.MarketSlugRegex)
	}
	return &Polymarket{
		log:             log,
		http:            &http.Client{Timeout: 15 * time.Second},
		baseURL:         cfg.BaseURL,
		marketSlugRegex: re,
		yesTokenID:      cfg.YesTokenID,
		noTokenID:       cfg.NoTokenID,
		marketID:        cfg.MarketID,
		pollInterval:    cfg.PollInterval,

		tradingEnabled: cfg.TradingEnabled,
		chainID:        cfg.ChainID,
		privateKey:     cfg.PrivateKey,
		funder:         cfg.Funder,
		signatureType:  cfg.SignatureType,
		apiCreds:       newApiCredsFromEnv(cfg.APIKey, cfg.APISecret, cfg.APIPassphrase),
	}
}

func (p *Polymarket) Start(ctx context.Context, b *bus.Bus) error {
	if p.yesTokenID == "" || p.noTokenID == "" || p.marketID == "" || p.endDate.IsZero() {
		if err := p.discover(ctx); err != nil {
			return err
		}
	}

	p.log.Info("polymarket adapter ready",
		"base", p.baseURL,
		"market_id", p.marketID,
		"yes_token_id", p.yesTokenID,
		"no_token_id", p.noTokenID,
		"end_date", p.endDate.Format(time.RFC3339),
		"poll_interval", p.pollInterval.String(),
		"trading_enabled", p.tradingEnabled,
	)

	if p.tradingEnabled {
		if err := p.initTrading(ctx); err != nil {
			return err
		}
	}

	go p.pollLoop(ctx, b)
	return nil
}

func (p *Polymarket) pollLoop(ctx context.Context, b *bus.Bus) {
	ticker := time.NewTicker(p.pollInterval)
	defer ticker.Stop()

	var lastGood time.Time
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			bestBid, bestAsk, ts, err := p.getBestBidAsk(ctx, p.yesTokenID)
			dq := 0.0
			if err == nil && bestBid > 0 && bestAsk > 0 && bestBid <= bestAsk && !ts.IsZero() {
				dq = 0.95
				if !lastGood.IsZero() && ts.Before(lastGood) {
					// out-of-order timestamp; reduce quality but keep running
					dq = 0.6
				}
				lastGood = ts
			} else {
				dq = 0.2
			}

			pYes := midpoint(bestBid, bestAsk)
			if pYes <= 0 {
				// fallback: if only one side present
				if bestAsk > 0 {
					pYes = bestAsk
				} else if bestBid > 0 {
					pYes = bestBid
				} else {
					pYes = 0.5
				}
			}

			rem := time.Until(p.endDate)
			if rem < 0 {
				rem = 0
			}

			_ = b.Publish(ctx, types.Event{
				Type: types.EventMarketTick,
				Payload: types.MarketTick{
					MarketID:      p.marketID,
					PYes:          clamp01(pYes),
					BestBid:       clamp01(bestBid),
					BestAsk:       clamp01(bestAsk),
					TimeRemaining: rem,
					DataQuality:   dq,
				},
				TsExchange: ts,
			})
		}
	}
}

// --- Discovery & HTTP ---

type clobMarketsResponse struct {
	Count      int64        `json:"count"`
	Limit      int          `json:"limit"`
	NextCursor string       `json:"next_cursor"`
	Data       []clobMarket `json:"data"`
}

type clobMarket struct {
	MarketSlug      string      `json:"market_slug"`
	Question        string      `json:"question"`
	EndDateISO      string      `json:"end_date_iso"`
	EnableOrderBook bool        `json:"enable_order_book"`
	AcceptingOrders bool        `json:"accepting_orders"`
	Closed          bool        `json:"closed"`
	Active          bool        `json:"active"`
	NegRisk         bool        `json:"neg_risk"`
	ConditionID     string      `json:"condition_id"`
	MinOrderSize    float64     `json:"minimum_order_size"`
	MinTickSize     float64     `json:"minimum_tick_size"`
	Tokens          []clobToken `json:"tokens"`
}

type clobToken struct {
	TokenID string  `json:"token_id"`
	Outcome string  `json:"outcome"`
	Price   float64 `json:"price"`
}

func (p *Polymarket) discover(ctx context.Context) error {
	if p.marketSlugRegex == nil && (p.yesTokenID == "" || p.noTokenID == "" || p.marketID == "") {
		// Default heuristic for BTC 15m markets; user can override with POLY_MARKET_SLUG_REGEX.
		p.marketSlugRegex = regexp.MustCompile(`(?i)btc.*15|min.*btc|15.*btc`)
	}

	limit := 200
	cursor := ""
	for page := 0; page < 30; page++ {
		u := fmt.Sprintf("%s/markets?limit=%d", p.baseURL, limit)
		if cursor != "" {
			u += "&next_cursor=" + cursor
		}

		var resp clobMarketsResponse
		if err := p.getJSON(ctx, u, &resp); err != nil {
			return err
		}

		for _, m := range resp.Data {
			if p.marketSlugRegex != nil && !p.marketSlugRegex.MatchString(m.MarketSlug) && !p.marketSlugRegex.MatchString(m.Question) {
				continue
			}
			if !m.EnableOrderBook || !m.AcceptingOrders || m.Closed || !m.Active {
				continue
			}
			yes, no := pickYesNoTokens(m.Tokens)
			if yes == "" || no == "" {
				continue
			}
			end, err := time.Parse(time.RFC3339, m.EndDateISO)
			if err != nil {
				continue
			}

			p.marketID = m.ConditionID
			p.yesTokenID = yes
			p.noTokenID = no
			p.endDate = end
			p.negRisk = m.NegRisk
			p.minOrderSize = m.MinOrderSize
			p.minTickSize = fmtTickSize(m.MinTickSize)
			return nil
		}

		if resp.NextCursor == "" {
			break
		}
		cursor = resp.NextCursor
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(150 * time.Millisecond):
		}
	}

	return fmt.Errorf("no matching market found (set POLY_MARKET_SLUG_REGEX or POLY_YES_TOKEN_ID/POLY_NO_TOKEN_ID/POLY_MARKET_ID)")
}

type clobBook struct {
	AssetID   string      `json:"asset_id"`
	Timestamp string      `json:"timestamp"`
	Bids      []clobLevel `json:"bids"`
	Asks      []clobLevel `json:"asks"`
}

type clobLevel struct {
	Price string `json:"price"`
	Size  string `json:"size"`
}

func (p *Polymarket) getBestBidAsk(ctx context.Context, tokenID string) (bid, ask float64, ts time.Time, err error) {
	u := fmt.Sprintf("%s/book?token_id=%s", p.baseURL, tokenID)
	var book clobBook
	if err := p.getJSON(ctx, u, &book); err != nil {
		return 0, 0, time.Time{}, err
	}

	if len(book.Bids) > 0 {
		bid, _ = strconv.ParseFloat(book.Bids[0].Price, 64)
	}
	if len(book.Asks) > 0 {
		ask, _ = strconv.ParseFloat(book.Asks[0].Price, 64)
	}

	// Timestamp is milliseconds since epoch.
	if book.Timestamp != "" {
		ms, parseErr := strconv.ParseInt(book.Timestamp, 10, 64)
		if parseErr == nil {
			ts = time.UnixMilli(ms).UTC()
		}
	}
	return bid, ask, ts, nil
}

func (p *Polymarket) getJSON(ctx context.Context, url string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("accept", "application/json")
	req.Header.Set("user-agent", "polymarket-btc-bot/0.1")

	res, err := p.http.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()

	if res.StatusCode/100 != 2 {
		return fmt.Errorf("http %d from %s", res.StatusCode, url)
	}
	return json.NewDecoder(res.Body).Decode(out)
}

func pickYesNoTokens(tokens []clobToken) (yesID, noID string) {
	// Heuristic:
	// - Prefer outcome labels YES/NO
	// - Else fall back to first/second token
	for _, t := range tokens {
		if regexp.MustCompile(`(?i)^yes$`).MatchString(t.Outcome) {
			yesID = t.TokenID
		}
		if regexp.MustCompile(`(?i)^no$`).MatchString(t.Outcome) {
			noID = t.TokenID
		}
	}
	if yesID != "" && noID != "" {
		return yesID, noID
	}
	if len(tokens) >= 2 {
		// Common in binary markets: tokens[0] is the positive outcome.
		return tokens[0].TokenID, tokens[1].TokenID
	}
	return "", ""
}

func midpoint(bid, ask float64) float64 {
	if bid <= 0 || ask <= 0 {
		return 0
	}
	return (bid + ask) / 2
}

func clamp01(x float64) float64 {
	if x < 0 {
		return 0
	}
	if x > 1 {
		return 1
	}
	return x
}

func parseDurationDefault(s string, def time.Duration) time.Duration {
	if s == "" {
		return def
	}
	d, err := time.ParseDuration(s)
	if err != nil {
		return def
	}
	if d <= 0 {
		return def
	}
	return d
}

func getenvDefault(k, def string) string {
	v := os.Getenv(k)
	if v == "" {
		return def
	}
	return v
}
