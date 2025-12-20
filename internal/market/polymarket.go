package market

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"time"

	"github.com/sirupsen/logrus"

	"polymarket-btc-bot/internal/bus"
	"polymarket-btc-bot/internal/types"
	
	clobclient "polymarket-btc-bot/internal/clob/client"
	clobtypes "polymarket-btc-bot/internal/clob/types"
)

// Polymarket is a minimal real-data adapter:
// - Market discovery via CLOB /markets
// - Price via polling CLOB /book?token_id=...
//
// Order placement/cancel is scaffolded but may require auth/signing details.
type Polymarket struct {
	log *logrus.Logger

	http *http.Client

	baseURL string

	// Market selection
	marketSlugRegex *regexp.Regexp
	// If set, skip discovery and use these directly
	yesTokenID string
	noTokenID  string
	marketID   string
	marketSlug string

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
	clobClient     *clobclient.Client // CLOB客户端
	ctfClient      *clobclient.CTFClient // CTF合约客户端

	// L2 polling cursors/dedup
	ordersCursor string
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

func NewPolymarketFromEnv(log *logrus.Logger) *Polymarket {
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

func NewPolymarket(log *logrus.Logger, cfg PolymarketConfig) *Polymarket {
	if log == nil {
		log = logrus.New()
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

	// Announce the current cycle context.
	_ = b.Publish(ctx, types.Event{
		Type: types.EventMarketSnapshot,
		Payload: types.MarketSnapshot{
			MarketID:      p.marketID,
			MarketSlug:    p.marketSlug,
			CycleStart:    cycleStartFromSlug(p.marketSlug),
			EndDate:       p.endDate,
			YesTokenID:    p.yesTokenID,
			NoTokenID:     p.noTokenID,
			MinTickSize:   p.minTickSizeFloat(),
			MinOrderSize:  p.minOrderSize,
			NegRisk:       p.negRisk,
		},
	})

	p.log.WithFields(map[string]interface{}{
		"base":           p.baseURL,
		"market_id":      p.marketID,
		"market_slug":    p.marketSlug,
		"yes_token_id":   p.yesTokenID,
		"no_token_id":    p.noTokenID,
		"end_date":       p.endDate.Format(time.RFC3339),
		"poll_interval":  p.pollInterval.String(),
		"trading_enabled": p.tradingEnabled,
	}).Info("polymarket adapter ready")

	if p.tradingEnabled {
		if err := p.initTrading(ctx); err != nil {
			return err
		}

		// Start authenticated polling for order updates / fills.
		go p.pollOrdersLoop(ctx, b)
		go p.pollTradesLoop(ctx, b)
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
			// Rotate market when it ends (15m markets roll frequently).
			if !p.endDate.IsZero() && time.Now().UTC().After(p.endDate) {
				_ = b.Publish(ctx, types.Event{
					Type: types.EventRisk,
					Payload: types.RiskState{
						KillSwitch: false,
						Freeze:     true,
						Reason:     "market_expired_rotate",
						Ts:         time.Now().UTC(),
					},
				})

				if err := p.discover(ctx); err != nil {
					_ = b.Publish(ctx, types.Event{
						Type: types.EventRisk,
						Payload: types.RiskState{
							KillSwitch: true,
							Freeze:     true,
							Reason:     "discover_failed:" + err.Error(),
							Ts:         time.Now().UTC(),
						},
					})
					continue
				}

				// Reset L2 cursors after rotation.
				p.ordersCursor = "MA=="
				lastGood = time.Time{}

				// Announce the new cycle so Engine can reset per-cycle state.
				_ = b.Publish(ctx, types.Event{
					Type: types.EventMarketSnapshot,
					Payload: types.MarketSnapshot{
						MarketID:      p.marketID,
						MarketSlug:    p.marketSlug,
						CycleStart:    cycleStartFromSlug(p.marketSlug),
						EndDate:       p.endDate,
						YesTokenID:    p.yesTokenID,
						NoTokenID:     p.noTokenID,
						MinTickSize:   p.minTickSizeFloat(),
						MinOrderSize:  p.minOrderSize,
						NegRisk:       p.negRisk,
					},
				})
			}

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

// fetchMarketBySlugFromGamma 从 Gamma API 根据 slug 获取市场信息
func (p *Polymarket) fetchMarketBySlugFromGamma(ctx context.Context, slug string) (*gammaMarket, error) {
	gammaURL := "https://gamma-api.polymarket.com/markets"
	u := fmt.Sprintf("%s?slug=%s&closed=false", gammaURL, url.QueryEscape(slug))
	
	var markets []gammaMarket
	if err := p.getJSON(ctx, u, &markets); err != nil {
		return nil, fmt.Errorf("从 Gamma API 获取市场失败: %w", err)
	}
	
	if len(markets) == 0 {
		return nil, fmt.Errorf("未找到市场: %s", slug)
	}
	
	return &markets[0], nil
}

// fetchMarketFromCLOB 从 CLOB API 根据 conditionID 获取完整市场信息（包括 token IDs）
func (p *Polymarket) fetchMarketFromCLOB(ctx context.Context, conditionID string) (*clobMarket, error) {
	u := fmt.Sprintf("%s/markets/%s", p.baseURL, conditionID)
	
	var market clobMarket
	if err := p.getJSON(ctx, u, &market); err != nil {
		return nil, fmt.Errorf("从 CLOB API 获取市场失败: %w", err)
	}
	
	return &market, nil
}

type gammaMarket struct {
	ID           string `json:"id"`
	Question     string `json:"question"`
	ConditionID  string `json:"conditionId"`
	Slug         string `json:"slug"`
	ClobTokenIDs string `json:"clobTokenIds"` // JSON 字符串数组，如 ["token1", "token2"]
	EndDate      string `json:"endDate"`
	StartDate    string `json:"startDate"`
	Category     string `json:"category"`
	Active       bool   `json:"active"`
	Closed       bool   `json:"closed"`
}

func (p *Polymarket) discover(ctx context.Context) error {
	// 如果已经提供了市场信息，直接使用
	if p.yesTokenID != "" && p.noTokenID != "" && p.marketID != "" {
		p.log.Infof("使用配置的市场信息: market_id=%s", p.marketID)
		return nil
	}
	
	// 按照 gobet 的方式：根据当前周期时间戳生成 slug，然后查询
	currentTs := GetCurrent15MinTimestamp()
	currentSlug := Generate15MinSlug(currentTs)
	
	p.log.Infof("根据当前周期发现市场: slug=%s (timestamp=%d)", currentSlug, currentTs)
	
	// 步骤1: 从 Gamma API 获取市场基本信息（包括 conditionID）
	gammaMarket, err := p.fetchMarketBySlugFromGamma(ctx, currentSlug)
	if err != nil {
		// 如果当前周期市场不存在，尝试下一个周期（可能市场还没创建）
		nextTs := GetNextCycleTimestamp(currentTs)
		nextSlug := Generate15MinSlug(nextTs)
		p.log.Warnf("当前周期市场不存在，尝试下一个周期: %s", nextSlug)
		
		gammaMarket, err = p.fetchMarketBySlugFromGamma(ctx, nextSlug)
		if err != nil {
			return fmt.Errorf("无法找到当前或下一个周期的市场: %w", err)
		}
		// 使用下一个周期的 slug
		currentSlug = nextSlug
		currentTs = nextTs
	}
	
	if gammaMarket.ConditionID == "" {
		return fmt.Errorf("Gamma API 返回的市场缺少 conditionID: %s", currentSlug)
	}
	
	// 步骤2: 从 CLOB API 获取完整市场信息（包括 token IDs）
	clobMarket, err := p.fetchMarketFromCLOB(ctx, gammaMarket.ConditionID)
	if err != nil {
		return fmt.Errorf("从 CLOB API 获取市场信息失败: %w", err)
	}
	
	// 解析 token IDs
	yes, no := pickYesNoTokens(clobMarket.Tokens)
	if yes == "" || no == "" {
		return fmt.Errorf("无法从市场数据中提取 token IDs: conditionID=%s", gammaMarket.ConditionID)
	}
	
	// 解析结束时间
	endDate, err := time.Parse(time.RFC3339, clobMarket.EndDateISO)
	if err != nil {
		return fmt.Errorf("解析结束时间失败: %w", err)
	}
	
	// 设置市场信息
	p.marketID = gammaMarket.ConditionID
	p.yesTokenID = yes
	p.noTokenID = no
	p.marketSlug = currentSlug
	p.endDate = endDate
	p.negRisk = clobMarket.NegRisk
	p.minOrderSize = clobMarket.MinOrderSize
	p.minTickSize = fmtTickSize(clobMarket.MinTickSize)
	
	p.log.Infof("市场发现成功: slug=%s, condition_id=%s, end_date=%s, minutes_remaining=%.1f",
		currentSlug, gammaMarket.ConditionID, endDate.Format(time.RFC3339), time.Until(endDate).Minutes())
	
	return nil
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
	// 使用CLOB客户端获取订单簿
	// 优先使用缓存（快速），如果缓存不可用则获取最新数据
	if p.clobClient != nil {
		var book *clobtypes.OrderBookSummary
		
		// 尝试从缓存获取（快速，~50ms）
		cachedBook, ok := p.clobClient.GetCachedOrderBook(tokenID)
		if ok {
			book = cachedBook
		} else {
			// 缓存不可用，获取最新数据（较慢，~200ms）
			book, err = p.clobClient.GetOrderBook(ctx, tokenID, nil)
			if err != nil {
				// 如果CLOB客户端失败，回退到原有方法
				goto fallback
			}
		}
		
		if book != nil {
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
	}
	
fallback:
	
	// 回退到原有的HTTP方法
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
