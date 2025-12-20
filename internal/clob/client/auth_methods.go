package client

import (
	"context"
	"fmt"

	"polymarket-btc-bot/internal/clob/signing"
	"polymarket-btc-bot/internal/clob/types"
)

// CreateOrDeriveAPIKey 创建或推导 API 密钥（L1 方法）
func (c *Client) CreateOrDeriveAPIKey(ctx context.Context, nonce *int64) (*types.ApiKeyCreds, error) {
	if err := c.CanL1Auth(); err != nil {
		return nil, err
	}

	// 构建 L1 认证头
	var n int64 = 0
	if nonce != nil {
		n = *nonce
	}

	headers, err := signing.CreateL1Headers(
		c.authConfig.PrivateKey,
		c.authConfig.ChainID,
		&n,
		nil,
	)
	if err != nil {
		return nil, fmt.Errorf("创建 L1 认证头失败: %w", err)
	}

	// 转换为 map
	headerMap := map[string]string{
		"POLY_ADDRESS":   headers.PolyAddress,
		"POLY_SIGNATURE":  headers.PolySignature,
		"POLY_TIMESTAMP":  headers.PolyTimestamp,
		"POLY_NONCE":     headers.PolyNonce,
	}

	// 直接推导现有 API 密钥（跳过创建步骤，避免 400 错误）
	endpoint := EndpointDeriveAPIKey
	resp, err := c.httpClient.get(endpoint, headerMap, nil)
	if err != nil {
		return nil, fmt.Errorf("推导 API 密钥失败: %w", err)
	}

	var apiKeyRaw types.ApiKeyRaw
	if err := parseResponse(resp, &apiKeyRaw); err != nil {
		return nil, fmt.Errorf("解析 API 密钥响应失败: %w", err)
	}

	// 转换为 ApiKeyCreds
	creds := &types.ApiKeyCreds{
		Key:       apiKeyRaw.ApiKey,
		Secret:    apiKeyRaw.Secret,
		Passphrase: apiKeyRaw.Passphrase,
	}

	return creds, nil
}

// DeriveAPIKey 推导现有 API 密钥
func (c *Client) DeriveAPIKey(ctx context.Context, nonce int64) (*types.ApiKeyCreds, error) {
	return c.CreateOrDeriveAPIKey(ctx, &nonce)
}

// CreateAPIKey 创建新的 API 密钥
func (c *Client) CreateAPIKey(ctx context.Context) (*types.ApiKeyCreds, error) {
	return c.CreateOrDeriveAPIKey(ctx, nil)
}

