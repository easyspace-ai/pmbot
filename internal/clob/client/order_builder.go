package client

import (
	"context"
	"fmt"
	"math"
	"math/big"
	"strconv"
	"strings"
	"time"

	"github.com/ethereum/go-ethereum/crypto"
	"polymarket-btc-bot/internal/clob/signing"
	"polymarket-btc-bot/internal/clob/types"
)

// RoundConfig 舍入配置
type RoundConfig struct {
	Price  int // 价格小数位数
	Size   int // 数量小数位数
	Amount int // 金额小数位数
}

// RoundingConfig 根据 tick size 返回舍入配置
var RoundingConfig = map[types.TickSize]RoundConfig{
	types.TickSize01: {
		Price:  1,
		Size:   2,
		Amount: 3,
	},
	types.TickSize001: {
		Price:  2,
		Size:   2,
		Amount: 4,
	},
	types.TickSize0001: {
		Price:  3,
		Size:   2,
		Amount: 5,
	},
	types.TickSize00001: {
		Price:  4,
		Size:   2,
		Amount: 6,
	},
}

// OrderBuilder 订单构建器
type OrderBuilder struct {
	client        *Client
	signatureType types.SignatureType
	funderAddress string
}

// NewOrderBuilder 创建新的订单构建器
func NewOrderBuilder(client *Client, signatureType types.SignatureType, funderAddress string) *OrderBuilder {
	return &OrderBuilder{
		client:        client,
		signatureType: signatureType,
		funderAddress: funderAddress,
	}
}

// BuildOrder 构建并签名订单
func (ob *OrderBuilder) BuildOrder(ctx context.Context, userOrder *types.UserOrder, options *types.CreateOrderOptions) (*types.SignedOrder, error) {
	// 获取合约配置
	contractConfig, err := GetContractConfig(ob.client.GetChainID())
	if err != nil {
		return nil, fmt.Errorf("获取合约配置失败: %w", err)
	}

	// 获取舍入配置
	roundConfig, ok := RoundingConfig[options.TickSize]
	if !ok {
		return nil, fmt.Errorf("不支持的 tick size: %s", options.TickSize)
	}

	// 获取签名者地址
	signerAddress := crypto.PubkeyToAddress(ob.client.authConfig.PrivateKey.PublicKey)

	// 确定 maker 地址（如果提供了 funderAddress，使用它；否则使用 signer）
	maker := signerAddress.Hex()
	if ob.funderAddress != "" {
		maker = ob.funderAddress
	}

	// 计算 maker/taker 金额
	rawMakerAmt, rawTakerAmt, err := getOrderRawAmounts(
		userOrder.Side,
		userOrder.Size,
		userOrder.Price,
		roundConfig,
	)
	if err != nil {
		return nil, fmt.Errorf("计算金额失败: %w", err)
	}

	// 转换为 wei 单位（USDC 精度为 6）
	makerAmount := parseUnits(rawMakerAmt, CollateralTokenDecimals)
	takerAmount := parseUnits(rawTakerAmt, CollateralTokenDecimals)

	// 确定 taker 地址
	taker := "0x0000000000000000000000000000000000000000"
	if userOrder.Taker != nil && *userOrder.Taker != "" {
		taker = *userOrder.Taker
	}

	// 确定 feeRateBps
	feeRateBps := big.NewInt(0)
	if userOrder.FeeRateBps != nil {
		feeRateBps = big.NewInt(int64(*userOrder.FeeRateBps))
	}

	// 确定 nonce
	nonce := big.NewInt(0)
	if userOrder.Nonce != nil {
		nonce = big.NewInt(int64(*userOrder.Nonce))
	}

	// 确定 expiration
	expiration := big.NewInt(0)
	if userOrder.Expiration != nil {
		expiration = big.NewInt(*userOrder.Expiration)
	}

	// 生成 salt（使用当前时间戳纳秒）
	salt := time.Now().UnixNano()

	// 解析 tokenID
	tokenID := new(big.Int)
	tokenID, ok = tokenID.SetString(userOrder.TokenID, 10)
	if !ok {
		return nil, fmt.Errorf("无效的 tokenID: %s", userOrder.TokenID)
	}

	// 确定交易所合约地址
	exchangeAddress := contractConfig.Exchange
	if options.NegRisk != nil && *options.NegRisk {
		exchangeAddress = contractConfig.NegRiskExchange
	}

	// 构建订单数据
	orderData := &signing.OrderData{
		Salt:          salt,
		Maker:         maker,
		Signer:        signerAddress.Hex(),
		Taker:         taker,
		TokenID:       tokenID,
		MakerAmount:   makerAmount,
		TakerAmount:   takerAmount,
		Expiration:    expiration,
		Nonce:         nonce,
		FeeRateBps:    feeRateBps,
		Side:          userOrder.Side,
		SignatureType: ob.signatureType,
	}

	// 签名订单
	signature, err := signing.BuildOrderSignature(
		ob.client.authConfig.PrivateKey,
		ob.client.GetChainID(),
		exchangeAddress,
		orderData,
	)
	if err != nil {
		return nil, fmt.Errorf("签名订单失败: %w", err)
	}

	// 构建 SignedOrder
	signedOrder := &types.SignedOrder{
		Salt:          salt,
		Maker:         maker,
		Signer:        signerAddress.Hex(),
		Taker:         taker,
		TokenID:       userOrder.TokenID,
		MakerAmount:   makerAmount.String(),
		TakerAmount:   takerAmount.String(),
		Expiration:    expiration.String(),
		Nonce:         nonce.String(),
		FeeRateBps:    feeRateBps.String(),
		Side:          userOrder.Side,
		SignatureType: int(ob.signatureType),
		Signature:     signature,
	}

	return signedOrder, nil
}

// decimalPlaces 返回数字的小数位数
func decimalPlaces(num float64) int {
	if num == math.Trunc(num) {
		return 0
	}
	str := strconv.FormatFloat(num, 'f', -1, 64)
	parts := strings.Split(str, ".")
	if len(parts) < 2 {
		return 0
	}
	return len(parts[1])
}

// roundNormal 四舍五入到指定小数位数
func roundNormal(num float64, decimals int) float64 {
	if decimalPlaces(num) <= decimals {
		return num
	}
	multiplier := math.Pow(10, float64(decimals))
	return math.Round(num*multiplier) / multiplier
}

// roundDown 向下舍入到指定小数位数
func roundDown(num float64, decimals int) float64 {
	if decimalPlaces(num) <= decimals {
		return num
	}
	multiplier := math.Pow(10, float64(decimals))
	return math.Floor(num*multiplier) / multiplier
}

// roundUp 向上舍入到指定小数位数
func roundUp(num float64, decimals int) float64 {
	if decimalPlaces(num) <= decimals {
		return num
	}
	multiplier := math.Pow(10, float64(decimals))
	return math.Ceil(num*multiplier) / multiplier
}

// getOrderRawAmounts 计算订单的 maker/taker 金额
func getOrderRawAmounts(
	side types.Side,
	size float64,
	price float64,
	roundConfig RoundConfig,
) (rawMakerAmt float64, rawTakerAmt float64, err error) {
	rawPrice := roundNormal(price, roundConfig.Price)

	if side == types.SideBuy {
		// 买入：taker 获得 tokens，maker 支付 USDC
		rawTakerAmt = roundDown(size, roundConfig.Size)

		rawMakerAmt = rawTakerAmt * rawPrice
		if decimalPlaces(rawMakerAmt) > roundConfig.Amount {
			rawMakerAmt = roundUp(rawMakerAmt, roundConfig.Amount+4)
			if decimalPlaces(rawMakerAmt) > roundConfig.Amount {
				rawMakerAmt = roundDown(rawMakerAmt, roundConfig.Amount)
			}
		}
	} else {
		// 卖出：maker 获得 tokens，taker 支付 USDC
		rawMakerAmt = roundDown(size, roundConfig.Size)

		rawTakerAmt = rawMakerAmt * rawPrice
		if decimalPlaces(rawTakerAmt) > roundConfig.Amount {
			rawTakerAmt = roundUp(rawTakerAmt, roundConfig.Amount+4)
			if decimalPlaces(rawTakerAmt) > roundConfig.Amount {
				rawTakerAmt = roundDown(rawTakerAmt, roundConfig.Amount)
			}
		}
	}

	return rawMakerAmt, rawTakerAmt, nil
}

// parseUnits 将金额转换为 wei 单位（类似 ethers.js 的 parseUnits）
func parseUnits(value float64, decimals int) *big.Int {
	multiplier := new(big.Float).SetFloat64(math.Pow(10, float64(decimals)))
	valueBig := new(big.Float).SetFloat64(value)
	result := new(big.Float).Mul(valueBig, multiplier)
	
	// 转换为 big.Int（向下取整）
	resultInt, _ := result.Int(nil)
	return resultInt
}

