package client

import (
	"crypto/ecdsa"
	"fmt"

	"polymarket-btc-bot/internal/clob/signing"
	"polymarket-btc-bot/internal/clob/types"
	"github.com/ethereum/go-ethereum/common"
)

// AuthConfig 认证配置
type AuthConfig struct {
	PrivateKey *ecdsa.PrivateKey
	ChainID    types.Chain
	Creds      *types.ApiKeyCreds
}

// CanL2Auth 检查是否可以进行 L2 认证
func (c *Client) CanL2Auth() error {
	if c.authConfig == nil || c.authConfig.Creds == nil {
		return fmt.Errorf("L2 认证不可用: API 凭证未配置")
	}
	return nil
}

// CanL1Auth 检查是否可以进行 L1 认证
func (c *Client) CanL1Auth() error {
	if c.authConfig == nil || c.authConfig.PrivateKey == nil {
		return fmt.Errorf("L1 认证不可用: 私钥未配置")
	}
	return nil
}

// GetAddress 获取账号地址（从私钥计算）
func (c *Client) GetAddress() (common.Address, error) {
	if c.authConfig == nil || c.authConfig.PrivateKey == nil {
		return common.Address{}, fmt.Errorf("私钥未配置，无法获取地址")
	}
	return signing.GetAddressFromPrivateKey(c.authConfig.PrivateKey), nil
}

