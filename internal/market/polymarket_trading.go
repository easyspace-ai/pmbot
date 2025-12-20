package market

import (
	"context"
	"crypto/ecdsa"
	"errors"
	"fmt"
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

	p.log.Info("polymarket trading initialized",
		"address", p.address,
		"funder", p.funder,
		"chain_id", p.chainID,
		"signature_type", p.signatureType,
		"has_api_creds", p.apiCreds != nil,
		"has_clob_client", p.clobClient != nil,
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

	// 创建订单选项
	options := &clobtypes.CreateOrderOptions{
		TickSize: tickSize,
		NegRisk:  &p.negRisk,
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

