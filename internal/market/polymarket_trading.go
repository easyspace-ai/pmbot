package market

import (
	"context"
	"crypto/ecdsa"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"

	"polymarket-btc-bot/internal/oms"
	"polymarket-btc-bot/internal/types"
	
	clobclient "polymarket-btc-bot/internal/clob/client"
	clobtypes "polymarket-btc-bot/internal/clob/types"
)

var (
	ErrTradingNotConfigured = errors.New("polymarket trading not configured (set POLY_TRADING_ENABLED=1 and credentials)")
)

type apiCreds struct {
	APIKey        string
	APISecret     string
	APIPassphrase string
}

func newApiCredsFromEnv(k, s, p string) *apiCreds {
	if k == "" || s == "" || p == "" {
		return nil
	}
	return &apiCreds{APIKey: k, APISecret: s, APIPassphrase: p}
}

func (p *Polymarket) initTrading(ctx context.Context) error {
	if p.privateKey == "" {
		return fmt.Errorf("%w: POLY_PRIVATE_KEY is required", ErrTradingNotConfigured)
	}
	priv, addr, err := parsePrivateKey(p.privateKey)
	if err != nil {
		return err
	}
	p.address = addr.Hex()
	if p.funder == "" {
		p.funder = p.address
	}

	// 转换API凭证
	var clobCreds *clobtypes.ApiKeyCreds
	if p.apiCreds != nil {
		clobCreds = &clobtypes.ApiKeyCreds{
			Key:        p.apiCreds.APIKey,
			Secret:     p.apiCreds.APISecret,
			Passphrase: p.apiCreds.APIPassphrase,
		}
	} else {
		// 如果没有提供API凭证，尝试创建或推导
		creds, err := p.createOrDeriveAPIKey(ctx, priv)
		if err != nil {
			return err
		}
		p.apiCreds = creds
		clobCreds = &clobtypes.ApiKeyCreds{
			Key:        creds.APIKey,
			Secret:     creds.APISecret,
			Passphrase: creds.APIPassphrase,
		}
	}

	// 初始化CLOB客户端
	chainID := clobtypes.Chain(p.chainID)
	p.clobClient = clobclient.NewClient(p.baseURL, chainID, priv, clobCreds)

	// 初始化CTF客户端（用于合并仓位）
	rpcURL := getenvDefault("POLY_RPC_URL", "https://polygon-rpc.com")
	ctfClient, err := clobclient.NewCTFClient(rpcURL, chainID, priv)
	if err == nil {
		p.ctfClient = ctfClient
		p.log.Info("CTF client initialized", "rpc_url", rpcURL)
		
		// 启动自动合并循环（如果配置启用）
		if os.Getenv("POLY_AUTO_MERGE") == "1" {
			go p.autoMergeLoop(context.Background())
		}
	} else {
		p.log.Warn("CTF client initialization failed (merge disabled)", "error", err)
	}

	p.log.Info("polymarket trading initialized",
		"address", p.address,
		"funder", p.funder,
		"chain_id", p.chainID,
		"signature_type", p.signatureType,
		"has_api_creds", p.apiCreds != nil,
		"has_clob_client", p.clobClient != nil,
		"has_ctf_client", p.ctfClient != nil,
	)
	return nil
}

func (p *Polymarket) PlaceOrder(ctx context.Context, req oms.PlaceOrderRequest) (oms.PlaceOrderResult, error) {
	if !p.tradingEnabled {
		return oms.PlaceOrderResult{Accepted: false, Reason: "not_configured"}, ErrTradingNotConfigured
	}
	if p.clobClient == nil {
		return oms.PlaceOrderResult{Accepted: false, Reason: "not_configured"}, ErrTradingNotConfigured
	}

	// 转换tokenID
	tokenID := ""
	switch req.Side {
	case types.SideYes:
		tokenID = p.yesTokenID
	case types.SideNo:
		tokenID = p.noTokenID
	default:
		return oms.PlaceOrderResult{Accepted: false, Reason: "bad_side"}, fmt.Errorf("unsupported side %v", req.Side)
	}

	// 检查市场是否关闭（快速检查订单簿）
	// 如果市场关闭，订单簿API会返回404
	if err := p.checkMarketOpen(ctx, tokenID); err != nil {
		return oms.PlaceOrderResult{
			Accepted: false,
			Reason:   "market_closed",
		}, fmt.Errorf("market closed or resolved: %w", err)
	}

	// 转换side
	clobSide := convertSide(req.Side)

	// 转换tickSize
	tickSize := convertTickSize(p.minTickSize)

	// 设置 TimeInForce 为 FOK (Fill Or Kill) 
	// 这对于套利策略至关重要，防止单边成交
	tif := "FOK"

	// 创建订单选项
	options := &clobtypes.CreateOrderOptions{
		TickSize:    tickSize,
		NegRisk:     &p.negRisk,
		TimeInForce: &tif,
	}

	// 使用CLOB客户端下单
	resp, err := p.clobClient.PlaceLimitOrder(ctx, tokenID, clobSide, req.Size, req.Price, options)
	if err != nil {
		// 检查是否是市场关闭错误
		errStr := err.Error()
		if strings.Contains(errStr, "404") || strings.Contains(errStr, "No orderbook exists") || strings.Contains(errStr, "market closed") {
			return oms.PlaceOrderResult{
				Accepted: false,
				Reason:   "market_closed",
			}, fmt.Errorf("market closed or resolved: %w", err)
		}
		return oms.PlaceOrderResult{Accepted: false, Reason: "clob_failed"}, err
	}

	if !resp.Success {
		// 检查错误消息是否表示市场关闭
		if strings.Contains(resp.ErrorMsg, "404") || strings.Contains(resp.ErrorMsg, "No orderbook exists") || strings.Contains(resp.ErrorMsg, "market closed") {
			return oms.PlaceOrderResult{
				Accepted: false,
				Reason:   "market_closed",
			}, fmt.Errorf("market closed or resolved: %s", resp.ErrorMsg)
		}
		return oms.PlaceOrderResult{
			Accepted: false,
			Reason:   resp.ErrorMsg,
		}, fmt.Errorf("order rejected: %s", resp.ErrorMsg)
	}

	return oms.PlaceOrderResult{
		ExchangeOrderID: resp.OrderID,
		Accepted:        true,
	}, nil
}

func (p *Polymarket) CancelOrder(ctx context.Context, req oms.CancelOrderRequest) (oms.CancelOrderResult, error) {
	if !p.tradingEnabled {
		return oms.CancelOrderResult{Ok: false, Reason: "not_configured"}, ErrTradingNotConfigured
	}
	if p.clobClient == nil {
		return oms.CancelOrderResult{Ok: false, Reason: "not_configured"}, ErrTradingNotConfigured
	}

	orderID := req.ExchangeOrderID
	if orderID == "" {
		orderID = req.ClientOrderID
	}
	if orderID == "" {
		return oms.CancelOrderResult{Ok: false, Reason: "missing_order_id"}, fmt.Errorf("missing order id")
	}

	// 使用CLOB客户端取消订单
	resp, err := p.clobClient.CancelOrder(ctx, orderID)
	if err != nil {
		return oms.CancelOrderResult{Ok: false, Reason: "clob_failed"}, err
	}

	if !resp.Success {
		return oms.CancelOrderResult{Ok: false, Reason: resp.ErrorMsg}, fmt.Errorf("cancel rejected: %s", resp.ErrorMsg)
	}

	return oms.CancelOrderResult{Ok: true}, nil
}

// MergePositions 尝试合并仓位
func (p *Polymarket) MergePositions(ctx context.Context, amount float64) (string, error) {
	if p.ctfClient == nil {
		return "", fmt.Errorf("CTF client not initialized")
	}

	// 调用CTF客户端的MergePositions
	// 注意：ConditionId 是 marketID
	params := clobclient.MergePositionsParams{
		ConditionId: p.marketID,
		Amount:      amount,
	}

	tx, err := p.ctfClient.MergePositions(ctx, params)
	if err != nil {
		return "", err
	}

	// 发送交易
	txHash, err := p.ctfClient.SendTransaction(ctx, tx)
	if err != nil {
		return "", err
	}

	p.log.Infof("🚀 发送合并交易: %s, 数量: %.2f", txHash.Hex(), amount)
	return txHash.Hex(), nil
}

// autoMergeLoop 自动合并循环
func (p *Polymarket) autoMergeLoop(ctx context.Context) {
	p.log.Info("🔄 启动自动合并循环")
	ticker := time.NewTicker(5 * time.Second) // 每5秒检查一次
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			p.checkAndMerge(ctx)
		}
	}
}

// checkAndMerge 检查并合并
func (p *Polymarket) checkAndMerge(ctx context.Context) {
	if p.ctfClient == nil || p.marketID == "" {
		return
	}

	// 查询可合并余额
	// 由于我们已经在 CTFClient 中实现了 GetMergeableBalance，这里直接调用
	mergeableAmount, err := p.ctfClient.GetMergeableBalance(ctx, p.marketID)
	if err != nil {
		p.log.Debugf("查询可合并余额失败: %v", err)
		return
	}

	// 最小合并阈值 (5.0 USDC) - 提高阈值以确保 Gas 费占比低
	const minMergeThreshold = 5.0
	
	// 简单的 Gas 保护：如果余额太小，不值得花 Gas
	// 注意：这里没有动态查询 Gas Price，而是使用了保守的阈值
	if mergeableAmount < minMergeThreshold {
		return
	}

	p.log.Infof("💰 发现可合并仓位: %.2f (阈值: %.2f)，尝试合并...", mergeableAmount, minMergeThreshold)
	
	// TODO: 在这里添加动态 Gas 估算逻辑
	// estimateGasCost := p.estimateMergeGasCost(ctx)
	// if mergeableAmount < estimateGasCost * 10 { return }

	txHash, err := p.MergePositions(ctx, mergeableAmount)
	if err != nil {
		p.log.Errorf("合并仓位失败: %v", err)
		return
	}
	
	p.log.Infof("✅ 合并交易已发送: %s", txHash)
	
	// 这里可以发布一个状态重置事件，但这需要 Engine 支持
	// 目前我们假设 Engine 会通过 Balance 查询最终看到变化
}

// --- Auth (L1/L2) ---

func (p *Polymarket) createOrDeriveAPIKey(ctx context.Context, priv *ecdsa.PrivateKey) (*apiCreds, error) {
	// 创建临时CLOB客户端用于API密钥操作
	chainID := clobtypes.Chain(p.chainID)
	tempClient := clobclient.NewClient(p.baseURL, chainID, priv, nil)
	
	// 尝试推导API密钥（更可靠）
	clobCreds, err := tempClient.DeriveAPIKey(ctx, 0)
	if err == nil {
		return &apiCreds{
			APIKey:        clobCreds.Key,
			APISecret:     clobCreds.Secret,
			APIPassphrase: clobCreds.Passphrase,
		}, nil
	}
	
	// 如果推导失败，尝试创建
	clobCreds, err2 := tempClient.CreateAPIKey(ctx)
	if err2 == nil {
		return &apiCreds{
			APIKey:        clobCreds.Key,
			APISecret:     clobCreds.Secret,
			APIPassphrase: clobCreds.Passphrase,
		}, nil
	}
	
	return nil, fmt.Errorf("create api key failed: %v; derive failed: %v", err2, err)
}


// 类型转换函数

// convertSide 将pmbot的Side转换为CLOB的Side
func convertSide(side types.Side) clobtypes.Side {
	switch side {
	case types.SideYes:
		return clobtypes.SideBuy
	case types.SideNo:
		return clobtypes.SideBuy // 对于NO，我们也是买入（买入NO token）
	default:
		return clobtypes.SideBuy
	}
}

// convertTickSize 将字符串tickSize转换为CLOB的TickSize类型
func convertTickSize(tickSize string) clobtypes.TickSize {
	switch tickSize {
	case "0.1":
		return clobtypes.TickSize01
	case "0.01":
		return clobtypes.TickSize001
	case "0.001":
		return clobtypes.TickSize0001
	case "0.0001":
		return clobtypes.TickSize00001
	default:
		return clobtypes.TickSize001 // 默认0.01
	}
}

// checkMarketOpen quickly checks if market is still open by attempting to get order book.
// Returns error if market is closed/resolved (404 or no orderbook).
func (p *Polymarket) checkMarketOpen(ctx context.Context, tokenID string) error {
	// Quick check: try to get order book
	// Use a short timeout to avoid blocking
	checkCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()

	_, err := p.clobClient.GetOrderBook(checkCtx, tokenID, nil)
	if err != nil {
		errStr := err.Error()
		if strings.Contains(errStr, "404") || strings.Contains(errStr, "No orderbook exists") || strings.Contains(errStr, "market closed") {
			return fmt.Errorf("market closed or resolved: %w", err)
		}
		// Other errors (network, timeout) don't necessarily mean market is closed
		// Allow order to proceed
	}
	return nil
}

func parsePrivateKey(hexKey string) (*ecdsa.PrivateKey, common.Address, error) {
	k := strings.TrimSpace(hexKey)
	k = strings.TrimPrefix(k, "0x")
	priv, err := crypto.HexToECDSA(k)
	if err != nil {
		return nil, common.Address{}, err
	}
	return priv, crypto.PubkeyToAddress(priv.PublicKey), nil
}
