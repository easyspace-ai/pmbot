package market

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"polymarket-btc-bot/internal/bus"
	"polymarket-btc-bot/internal/oms"
	"polymarket-btc-bot/internal/types"

	"polymarket-btc-bot/internal/clob/signing"
	clobtypes "polymarket-btc-bot/internal/clob/types"
)

// CLOB /data/orders is an authenticated endpoint (L2).
// We poll it to:
// - drive the OMS state machine with authoritative status
// - feed Position Truth Engine with fills (via /data/trades, added next)

type clobOrdersResp struct {
	Count      int64           `json:"count"`
	Limit      int             `json:"limit"`
	NextCursor string          `json:"next_cursor"`
	Data       []clobOrderItem `json:"data"`
}

type clobOrderItem struct {
	ID        string `json:"id"`
	Status    string `json:"status"` // e.g. "OPEN", "CANCELED", "MATCHED", "UNMATCHED" (varies)
	Market    string `json:"market"`
	AssetID   string `json:"asset_id"`
	Side      string `json:"side"`  // "BUY" or "SELL"
	Price     string `json:"price"` // stringified decimal
	Size      string `json:"size"`  // stringified decimal
	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"updated_at"`
}

func (p *Polymarket) pollOrdersLoop(ctx context.Context, b *bus.Bus) {
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	// Default cursor in official clients
	if p.ordersCursor == "" {
		p.ordersCursor = "MA=="
	}

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if p.apiCreds == nil {
				continue
			}
			// Pull orders for this market; cursor-based paging.
			if err := p.pullOrdersOnce(ctx, b); err != nil {
				// degrade data quality by emitting risk event; do not kill immediately
				_ = b.Publish(ctx, types.Event{
					Type: types.EventRisk,
					Payload: types.RiskState{
						KillSwitch: false,
						Freeze:     false,
						Reason:     fmt.Sprintf("orders_poll_error:%v", err),
						Ts:         time.Now().UTC(),
					},
				})
			}
		}
	}
}

func (p *Polymarket) pullOrdersOnce(ctx context.Context, b *bus.Bus) error {
	// Build query exactly like py-clob-client: /data/orders?market=...&next_cursor=...
	url := fmt.Sprintf("%s/data/orders?market=%s&next_cursor=%s", p.baseURL, p.marketID, p.ordersCursor)

	// 构建L2认证头
	if p.apiCreds == nil {
		return fmt.Errorf("API credentials not initialized")
	}

	clobCreds := &clobtypes.ApiKeyCreds{
		Key:        p.apiCreds.APIKey,
		Secret:     p.apiCreds.APISecret,
		Passphrase: p.apiCreds.APIPassphrase,
	}

	priv, _, err := parsePrivateKeyHelper(p.privateKey)
	if err != nil {
		return err
	}

	l2HeaderArgs := &clobtypes.L2HeaderArgs{
		Method:      "GET",
		RequestPath: "/data/orders",
		Body:        nil,
	}

	headers, err := signing.CreateL2Headers(priv, clobCreds, l2HeaderArgs, nil)
	if err != nil {
		return err
	}

	headerMap := map[string]string{
		"POLY_ADDRESS":    headers.PolyAddress,
		"POLY_SIGNATURE":  headers.PolySignature,
		"POLY_TIMESTAMP":  headers.PolyTimestamp,
		"POLY_API_KEY":    headers.PolyAPIKey,
		"POLY_PASSPHRASE": headers.PolyPassphrase,
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}

	for k, v := range headerMap {
		req.Header.Set(k, v)
	}
	req.Header.Set("accept", "application/json")
	req.Header.Set("user-agent", "polymarket-btc-bot/0.1")

	res, err := p.http.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()

	body, err := io.ReadAll(res.Body)
	if err != nil {
		return err
	}

	if res.StatusCode/100 != 2 {
		return fmt.Errorf("http %d: %s", res.StatusCode, truncHelper(body, 256))
	}

	var resp clobOrdersResp
	if err := json.Unmarshal(body, &resp); err != nil {
		return err
	}

	if resp.NextCursor != "" {
		p.ordersCursor = resp.NextCursor
	}

	// Emit order updates. We don't yet map clientOrderID because Polymarket order IDs are server-generated;
	// OMS uses ExchangeOrderID for cancel anyway. We'll attach it in ExchangeOrderID.
	now := time.Now().UTC()
	for _, it := range resp.Data {
		upd := oms.OrderUpdate{
			ClientOrderID:   "", // unknown; could be mapped by local registry later
			ExchangeOrderID: it.ID,
			State:           mapOrderStatus(it.Status),
			FilledSize:      -1,
			Ts:              now,
			Reason:          it.Status,
		}
		_ = b.Publish(ctx, types.Event{Type: types.EventOrderUpdate, Payload: upd})
	}

	return nil
}

func mapOrderStatus(s string) oms.OrderState {
	switch s {
	case "OPEN":
		return oms.StateAck
	case "CANCELED", "CANCELLED":
		return oms.StateCanceled
	case "MATCHED", "FILLED":
		return oms.StateFilled
	case "REJECTED":
		return oms.StateRejected
	default:
		return oms.StateUnknown
	}
}
