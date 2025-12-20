package client

import (
	"fmt"
	"strconv"

	"polymarket-btc-bot/internal/clob/types"
)

// CalculateOptimalFill 根据订单簿计算最优成交价格和数量
// 用于市价单：计算在指定 USDC 金额下能买入/卖出多少 tokens，以及平均价格
//
// 参数:
//   - book: 订单簿数据
//   - side: 买入或卖出
//   - amountUSDC: USDC 金额（买入时）或 token 数量（卖出时）
//
// 返回:
//   - totalSize: 能成交的 token 数量
//   - avgPrice: 平均成交价格
//   - filledUSDC: 实际使用的 USDC 金额（买入时）或获得的 USDC 金额（卖出时）
func CalculateOptimalFill(book *types.OrderBookSummary, side types.Side, amountUSDC float64) (totalSize float64, avgPrice float64, filledUSDC float64) {
	var levels []types.OrderSummary
	if side == types.SideBuy {
		levels = book.Asks // 买入时看卖单（asks）
	} else {
		levels = book.Bids // 卖出时看买单（bids）
	}

	if len(levels) == 0 {
		return 0, 0, 0
	}

	remainingUSDC := amountUSDC
	totalCost := 0.0

	for _, level := range levels {
		price, _ := strconv.ParseFloat(level.Price, 64)
		size, _ := strconv.ParseFloat(level.Size, 64)

		levelValue := size * price
		if levelValue <= remainingUSDC {
			// 这一层全部成交
			totalSize += size
			totalCost += levelValue
			remainingUSDC -= levelValue
		} else {
			// 部分成交
			fillSize := remainingUSDC / price
			totalSize += fillSize
			totalCost += remainingUSDC
			remainingUSDC = 0
			break
		}

		if remainingUSDC <= 0 {
			break
		}
	}

	if totalSize > 0 {
		avgPrice = totalCost / totalSize
	}
	filledUSDC = amountUSDC - remainingUSDC

	return totalSize, avgPrice, filledUSDC
}

// RoundToTickSize 将价格舍入到 tick size
func RoundToTickSize(price float64, tickSize types.TickSize) float64 {
	var tick float64
	switch tickSize {
	case types.TickSize01:
		tick = 0.1
	case types.TickSize001:
		tick = 0.01
	case types.TickSize0001:
		tick = 0.001
	case types.TickSize00001:
		tick = 0.0001
	default:
		tick = 0.01 // 默认
	}

	return float64(int(price/tick+0.5)) * tick
}

// ValidateFOKPrecision 验证 FOK/FAK 订单的精度要求
// FOK/FAK 要求：
//   - Price: 2位小数（tick size 0.01）
//   - Size: 4位小数
//   - Maker amount (USDC for buy): 2位小数
//   - Taker amount (tokens): 4位小数
func ValidateFOKPrecision(size float64, price float64, side types.Side) error {
	// 验证价格精度（2位小数）
	if price*100 != float64(int(price*100+0.5)) {
		return fmt.Errorf("FOK/FAK 订单价格必须是 2 位小数，当前: %.6f", price)
	}

	// 验证数量精度（4位小数）
	if size*10000 != float64(int(size*10000+0.5)) {
		return fmt.Errorf("FOK/FAK 订单数量必须是 4 位小数，当前: %.6f", size)
	}

	// 验证 USDC 金额精度（买入时）
	if side == types.SideBuy {
		usdcValue := size * price
		if usdcValue*100 != float64(int(usdcValue*100+0.5)) {
			return fmt.Errorf("FOK/FAK 订单 USDC 金额必须是 2 位小数，当前: %.6f", usdcValue)
		}
	}

	return nil
}

