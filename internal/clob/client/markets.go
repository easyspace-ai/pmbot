package client

import (
	"context"
	"fmt"

	"polymarket-btc-bot/internal/clob/signing"
	"polymarket-btc-bot/internal/clob/types"
)

// GetMarkets 获取市场列表
func (c *Client) GetMarkets(ctx context.Context, slug *string) ([]interface{}, error) {
	queryParams := make(map[string]string)
	if slug != nil {
		queryParams["slug"] = *slug
	}

	resp, err := c.httpClient.get(EndpointGetMarkets, nil, queryParams)
	if err != nil {
		return nil, fmt.Errorf("获取市场列表失败: %w", err)
	}

	var markets []interface{}
	if err := parseResponse(resp, &markets); err != nil {
		return nil, err
	}

	return markets, nil
}

// GetOrderBook 获取订单簿（总是获取最新数据）
func (c *Client) GetOrderBook(ctx context.Context, tokenID string, side *types.Side) (*types.OrderBookSummary, error) {
	queryParams := map[string]string{
		"token_id": tokenID,
	}
	if side != nil {
		queryParams["side"] = string(*side)
	}

	resp, err := c.httpClient.get(EndpointGetOrderBook, nil, queryParams)
	if err != nil {
		return nil, fmt.Errorf("获取订单簿失败: %w", err)
	}

	var book types.OrderBookSummary
	if err := parseResponse(resp, &book); err != nil {
		return nil, err
	}

	// Update cache (only if no side filter, as cache key doesn't include side)
	if side == nil {
		c.orderBookCache.set(tokenID, &book)
	}

	return &book, nil
}

// GetCachedOrderBook 获取缓存的订单簿（如果可用且未过期）
// 如果缓存不可用，返回nil和false
// 用于快速决策场景，需要最新数据时使用GetOrderBook
func (c *Client) GetCachedOrderBook(tokenID string) (*types.OrderBookSummary, bool) {
	return c.orderBookCache.get(tokenID)
}

// GetPrice 获取价格
func (c *Client) GetPrice(ctx context.Context, tokenID string) (*types.MarketPrice, error) {
	queryParams := map[string]string{
		"token_id": tokenID,
	}

	resp, err := c.httpClient.get(EndpointGetPrice, nil, queryParams)
	if err != nil {
		return nil, fmt.Errorf("获取价格失败: %w", err)
	}

	var price types.MarketPrice
	if err := parseResponse(resp, &price); err != nil {
		return nil, err
	}

	return &price, nil
}

// GetBalanceAllowance 获取余额和授权
func (c *Client) GetBalanceAllowance(ctx context.Context, params *types.BalanceAllowanceParams) (*types.BalanceAllowanceResponse, error) {
	if err := c.CanL2Auth(); err != nil {
		return nil, err
	}

	queryParams := map[string]string{
		"asset_type": string(params.AssetType),
	}
	if params.TokenID != nil {
		queryParams["token_id"] = *params.TokenID
	}
	// 添加 signature_type 查询参数
	if params.SignatureType != nil {
		queryParams["signature_type"] = fmt.Sprintf("%d", int(*params.SignatureType))
	}

	// 构建 L2 认证头
	l2HeaderArgs := &types.L2HeaderArgs{
		Method:      "GET",
		RequestPath: EndpointGetBalanceAllowance,
		Body:        nil,
	}

	headers, err := signing.CreateL2Headers(
		c.authConfig.PrivateKey,
		c.authConfig.Creds,
		l2HeaderArgs,
		nil,
	)
	if err != nil {
		return nil, fmt.Errorf("创建 L2 认证头失败: %w", err)
	}

	// 转换为 map
	headerMap := map[string]string{
		"POLY_ADDRESS":    headers.PolyAddress,
		"POLY_SIGNATURE":  headers.PolySignature,
		"POLY_TIMESTAMP":  headers.PolyTimestamp,
		"POLY_API_KEY":    headers.PolyAPIKey,
		"POLY_PASSPHRASE": headers.PolyPassphrase,
	}

	resp, err := c.httpClient.get(EndpointGetBalanceAllowance, headerMap, queryParams)
	if err != nil {
		return nil, fmt.Errorf("获取余额和授权失败: %w", err)
	}

	var balance types.BalanceAllowanceResponse
	if err := parseResponse(resp, &balance); err != nil {
		return nil, err
	}

	return &balance, nil
}

