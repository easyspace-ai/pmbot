package rtds

import (
	"encoding/json"
	"fmt"
)

// Subscribe subscribes to one or more topics
func (c *Client) Subscribe(subscriptions []Subscription) error {
	if !c.IsConnected() {
		return fmt.Errorf("client is not connected")
	}

	req := SubscriptionRequest{
		Action:        ActionSubscribe,
		Subscriptions: subscriptions,
	}

	if err := c.SendMessage(req); err != nil {
		return err
	}

	// Track active subscriptions for reconnection
	c.subscriptionsMutex.Lock()
	for _, sub := range subscriptions {
		// Check if subscription already exists
		exists := false
		for i, existing := range c.activeSubscriptions {
			if existing.Topic == sub.Topic && existing.Type == sub.Type && existing.Filters == sub.Filters {
				// Update existing subscription (in case auth changed)
				c.activeSubscriptions[i] = sub
				exists = true
				break
			}
		}
		if !exists {
			c.activeSubscriptions = append(c.activeSubscriptions, sub)
		}
	}
	c.subscriptionsMutex.Unlock()

	return nil
}

// Unsubscribe unsubscribes from one or more topics
func (c *Client) Unsubscribe(subscriptions []Subscription) error {
	if !c.IsConnected() {
		return fmt.Errorf("client is not connected")
	}

	req := SubscriptionRequest{
		Action:        ActionUnsubscribe,
		Subscriptions: subscriptions,
	}

	if err := c.SendMessage(req); err != nil {
		return err
	}

	// Remove from active subscriptions
	c.subscriptionsMutex.Lock()
	for _, sub := range subscriptions {
		for i := len(c.activeSubscriptions) - 1; i >= 0; i-- {
			existing := c.activeSubscriptions[i]
			if existing.Topic == sub.Topic && existing.Type == sub.Type && existing.Filters == sub.Filters {
				// Remove subscription
				c.activeSubscriptions = append(c.activeSubscriptions[:i], c.activeSubscriptions[i+1:]...)
				break
			}
		}
	}
	c.subscriptionsMutex.Unlock()

	return nil
}

// SubscribeToCryptoPrices subscribes to cryptocurrency price updates
func (c *Client) SubscribeToCryptoPrices(source string, symbols ...string) error {
	topic := "crypto_prices"
	if source == "chainlink" {
		topic = "crypto_prices_chainlink"
	}

	if len(symbols) == 0 {
		return fmt.Errorf("at least one symbol is required")
	}

	// For both Binance and Chainlink, subscribe to each symbol separately
	// This matches the RTDS API format better
	var subscriptions []Subscription
	for _, symbol := range symbols {
		filterMap := map[string]string{"symbol": symbol}
		filterBytes, _ := json.Marshal(filterMap)
		filters := string(filterBytes)

		sub := Subscription{
			Topic:   topic,
			Type:    "update",
			Filters: filters,
		}
		subscriptions = append(subscriptions, sub)
	}

	return c.Subscribe(subscriptions)
}

// SubscribeToComments subscribes to comment events
func (c *Client) SubscribeToComments(eventID *int, entityType string, commentTypes ...string) error {
	filters := ""
	if eventID != nil {
		filterMap := map[string]interface{}{
			"parentEntityID":   *eventID,
			"parentEntityType": entityType,
		}
		filterBytes, _ := json.Marshal(filterMap)
		filters = string(filterBytes)
	}

	types := commentTypes
	if len(types) == 0 {
		types = []string{"*"} // Subscribe to all comment types
	}

	var subscriptions []Subscription
	for _, t := range types {
		sub := Subscription{
			Topic:   "comments",
			Type:    t,
			Filters: filters,
		}
		subscriptions = append(subscriptions, sub)
	}

	return c.Subscribe(subscriptions)
}

// SubscribeToActivity subscribes to activity events (trades, orders_matched)
func (c *Client) SubscribeToActivity(eventSlug, marketSlug string, activityTypes ...string) error {
	filters := ""
	if eventSlug != "" {
		filterMap := map[string]string{"event_slug": eventSlug}
		filterBytes, _ := json.Marshal(filterMap)
		filters = string(filterBytes)
	} else if marketSlug != "" {
		filterMap := map[string]string{"market_slug": marketSlug}
		filterBytes, _ := json.Marshal(filterMap)
		filters = string(filterBytes)
	}

	types := activityTypes
	if len(types) == 0 {
		types = []string{"trades", "orders_matched"}
	}

	var subscriptions []Subscription
	for _, t := range types {
		sub := Subscription{
			Topic:   "activity",
			Type:    t,
			Filters: filters,
		}
		subscriptions = append(subscriptions, sub)
	}

	return c.Subscribe(subscriptions)
}

// SubscribeToClobMarket subscribes to CLOB market data
func (c *Client) SubscribeToClobMarket(marketIDs []string, dataTypes ...string) error {
	if len(marketIDs) == 0 {
		return fmt.Errorf("at least one market ID is required")
	}

	// Filters are mandatory for clob_market
	filters := ""
	if len(marketIDs) > 0 {
		filterBytes, _ := json.Marshal(marketIDs)
		filters = string(filterBytes)
	}

	types := dataTypes
	if len(types) == 0 {
		types = []string{"agg_orderbook", "last_trade_price", "tick_size_change"}
	}

	var subscriptions []Subscription
	for _, t := range types {
		sub := Subscription{
			Topic:   "clob_market",
			Type:    t,
			Filters: filters,
		}
		subscriptions = append(subscriptions, sub)
	}

	return c.Subscribe(subscriptions)
}

// SubscribeToClobUser subscribes to CLOB user data (requires authentication)
func (c *Client) SubscribeToClobUser(auth *ClobAuth, dataTypes ...string) error {
	if auth == nil {
		return fmt.Errorf("CLOB authentication is required")
	}

	types := dataTypes
	if len(types) == 0 {
		types = []string{"*"} // Subscribe to all user data types
	}

	var subscriptions []Subscription
	for _, t := range types {
		sub := Subscription{
			Topic:    "clob_user",
			Type:     t,
			ClobAuth: auth,
		}
		subscriptions = append(subscriptions, sub)
	}

	return c.Subscribe(subscriptions)
}

// SubscribeToRFQ subscribes to RFQ (Request for Quote) events
func (c *Client) SubscribeToRFQ(rfqTypes ...string) error {
	types := rfqTypes
	if len(types) == 0 {
		types = []string{
			"request_created",
			"request_edited",
			"request_canceled",
			"request_expired",
			"quote_created",
			"quote_edited",
			"quote_canceled",
			"quote_expired",
		}
	}

	var subscriptions []Subscription
	for _, t := range types {
		sub := Subscription{
			Topic: "rfq",
			Type:  t,
		}
		subscriptions = append(subscriptions, sub)
	}

	return c.Subscribe(subscriptions)
}
