package market

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"polymarket-btc-bot/internal/bus"
	"polymarket-btc-bot/internal/oms"
	"polymarket-btc-bot/internal/types"
)

// CLOB /data/trades is authenticated (L2). We poll it and emit Fill events.
// The exact schema can vary; we parse a conservative subset.

type clobTradesResp struct {
	Count      int64            `json:"count"`
	Limit      int              `json:"limit"`
	NextCursor string           `json:"next_cursor"`
	Data       []clobTradeItem  `json:"data"`
}

type clobTradeItem struct {
	ID        string `json:"id"`
	Market    string `json:"market"`
	AssetID   string `json:"asset_id"`
	Price     string `json:"price"`
	Size      string `json:"size"`
	Side      string `json:"side"` // "BUY"/"SELL" from taker or maker; best-effort
	OrderID   string `json:"order_id"`
	CreatedAt string `json:"created_at"`
}

func (p *Polymarket) pollTradesLoop(ctx context.Context, b *bus.Bus) {
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	cursor := "MA=="
	seen := make(map[string]struct{}, 4096)
	lastMarket := p.marketID

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if p.apiCreds == nil {
				continue
			}
			// If market rotated, reset cursor+dedup for the new cycle.
			if p.marketID != "" && p.marketID != lastMarket {
				cursor = "MA=="
				seen = make(map[string]struct{}, 4096)
				lastMarket = p.marketID
			}
			next, err := p.pullTradesOnce(ctx, b, cursor, seen)
			if err != nil {
				_ = b.Publish(ctx, types.Event{
					Type: types.EventRisk,
					Payload: types.RiskState{
						KillSwitch: false,
						Freeze:     false,
						Reason:     fmt.Sprintf("trades_poll_error:%v", err),
						Ts:         time.Now().UTC(),
					},
				})
				continue
			}
			if next != "" {
				cursor = next
			}
		}
	}
}

func (p *Polymarket) pullTradesOnce(ctx context.Context, b *bus.Bus, cursor string, seen map[string]struct{}) (string, error) {
	url := fmt.Sprintf("%s/data/trades?market=%s&next_cursor=%s", p.baseURL, p.marketID, cursor)
	headers := p.l2Headers("GET", "/data/trades", nil)
	body, status, err := p.do(ctx, http.MethodGet, url, headers, nil)
	if err != nil {
		return "", err
	}
	if status/100 != 2 {
		return "", fmt.Errorf("http %d: %s", status, trunc(body, 256))
	}

	var resp clobTradesResp
	if err := json.Unmarshal(body, &resp); err != nil {
		return "", err
	}

	now := time.Now().UTC()
	for _, it := range resp.Data {
		if it.ID != "" {
			if _, ok := seen[it.ID]; ok {
				continue
			}
			seen[it.ID] = struct{}{}
		}
		price, _ := strconv.ParseFloat(it.Price, 64)
		size, _ := strconv.ParseFloat(it.Size, 64)

		// Map to YES/NO based on asset_id when possible.
		side := types.SideUnknown
		if it.AssetID == p.yesTokenID {
			side = types.SideYes
		} else if it.AssetID == p.noTokenID {
			side = types.SideNo
		}

		fill := oms.Fill{
			ClientOrderID:   "",
			ExchangeOrderID: it.OrderID,
			MarketID:        p.marketID,
			Side:            side,
			Price:           price,
			Size:            size,
			Ts:              now,
		}
		_ = b.Publish(ctx, types.Event{Type: types.EventFill, Payload: fill})
	}

	return resp.NextCursor, nil
}

