package client

import (
	"fmt"
	"polymarket-btc-bot/internal/clob/types"
)

// ContractConfig 合约配置
type ContractConfig struct {
	Exchange         string // 标准交易所合约地址
	NegRiskAdapter   string // 负风险适配器地址
	NegRiskExchange  string // 负风险交易所合约地址
	Collateral       string // 抵押品代币地址
	ConditionalTokens string // 条件代币合约地址
}

const (
	// CollateralTokenDecimals 抵押品代币精度（USDC = 6）
	CollateralTokenDecimals = 6
	
	// ConditionalTokenDecimals 条件代币精度（= 6）
	ConditionalTokenDecimals = 6
)

// PolygonMainnetContracts Polygon 主网合约地址
var PolygonMainnetContracts = ContractConfig{
	Exchange:         "0x4bFb41d5B3570DeFd03C39a9A4D8dE6Bd8B8982E",
	NegRiskAdapter:   "0xd91E80cF2E7be2e162c6513ceD06f1dD0dA35296",
	NegRiskExchange:  "0xC5d563A36AE78145C45a50134d48A1215220f80a",
	Collateral:       "0x2791Bca1f2de4661ED88A30C99A7a9449Aa84174", // USDC
	ConditionalTokens: "0x4D97DCd97eC945f40cF65F87097ACe5EA0476045",
}

// AmoyTestnetContracts Amoy 测试网合约地址
var AmoyTestnetContracts = ContractConfig{
	Exchange:         "0xdFE02Eb6733538f8Ea35D585af8DE5958AD99E40",
	NegRiskAdapter:   "0xd91E80cF2E7be2e162c6513ceD06f1dD0dA35296",
	NegRiskExchange:  "0xC5d563A36AE78145C45a50134d48A1215220f80a",
	Collateral:       "0x9c4e1703476e875070ee25b56a58b008cfb8fa78",
	ConditionalTokens: "0x69308FB512518e39F9b16112fA8d994F4e2Bf8bB",
}

// GetContractConfig 根据链 ID 获取合约配置
func GetContractConfig(chainID types.Chain) (*ContractConfig, error) {
	switch chainID {
	case types.ChainPolygon:
		return &PolygonMainnetContracts, nil
	case types.ChainAmoy:
		return &AmoyTestnetContracts, nil
	default:
		return nil, fmt.Errorf("不支持的链 ID: %d", chainID)
	}
}

