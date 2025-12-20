package client

import (
	"crypto/ecdsa"
	"net/url"
	"strings"
	"time"

	"polymarket-btc-bot/internal/clob/types"
	"polymarket-btc-bot/internal/clob/ratelimit"
)

// Client CLOB 客户端
type Client struct {
	host         string
	chainID      types.Chain
	authConfig   *AuthConfig
	httpClient   *httpClient
	tickSizes    types.TickSizes
	negRisk      types.NegRisk
	feeRates     types.FeeRates
	rateLimiter  *ratelimit.RateLimitManager
	orderBookCache *orderBookCache // Order book cache for faster access
}

// NewClient 创建新的 CLOB 客户端
func NewClient(
	host string,
	chainID types.Chain,
	privateKey *ecdsa.PrivateKey,
	creds *types.ApiKeyCreds,
) *Client {
	authConfig := &AuthConfig{
		PrivateKey: privateKey,
		ChainID:    chainID,
		Creds:      creds,
	}

	// 解析代理 URL（默认不使用代理）
	var proxyURL *url.URL
	useProxy := false
	// 代理配置通过环境变量或外部传入，这里默认不使用

	httpClient := newHTTPClient(host, authConfig, useProxy, proxyURL)

	return &Client{
		host:          strings.TrimSuffix(host, "/"),
		chainID:       chainID,
		authConfig:    authConfig,
		httpClient:    httpClient,
		tickSizes:     make(types.TickSizes),
		negRisk:       make(types.NegRisk),
		feeRates:      make(types.FeeRates),
		rateLimiter:   ratelimit.NewRateLimitManager(),
		orderBookCache: newOrderBookCache(500*time.Millisecond, 100), // 500ms TTL, 100 entries max
	}
}

// GetHost 获取主机地址
func (c *Client) GetHost() string {
	return c.host
}

// GetChainID 获取链 ID
func (c *Client) GetChainID() types.Chain {
	return c.chainID
}

