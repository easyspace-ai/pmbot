package market

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"
	"github.com/sirupsen/logrus"

	"polymarket-btc-bot/internal/bus"
	"polymarket-btc-bot/internal/oms"
	"polymarket-btc-bot/internal/types"
)

const (
	reconnectCoolDownPeriod = 15 * time.Second
	pingInterval            = 10 * time.Second
	readTimeout             = 30 * time.Second
	writeTimeout            = 10 * time.Second
	
	// 健壮性增强常量（参考 btc15）
	stalenessthresholdSecs = 60.0  // 订单簿过期阈值（秒）
	snapshotTimeoutSecs    = 10    // 首次快照等待超时（秒）
	maxReconnectAttempts   = 5     // 最大重连次数
	reconnectDelaySecs     = 2     // 重连延迟（秒）
)

// WebSocketAdapter 是使用 WebSocket 实时数据的市场适配器
// 它保持与 PMBot 单线程架构的兼容性，通过事件总线发布事件
type WebSocketAdapter struct {
	log *logrus.Logger

	// 连接管理
	conn       *websocket.Conn
	connCtx    context.Context
	connCancel context.CancelFunc
	connMu     sync.Mutex

	// 重连管理
	reconnectC chan struct{}
	closeC     chan struct{}

	// 市场信息
	yesTokenID string
	noTokenID  string
	marketID   string
	marketSlug string
	endDate    time.Time
	cycleTimestamp int64 // 当前周期的开始时间戳（Unix 秒）
	
	// 周期切换管理
	cycleCheckStop chan struct{}
	cycleCheckWg   sync.WaitGroup
	switchingCycle  atomic.Bool // 防止重复切换

	// 市场微结构
	minTickSize  string
	minOrderSize float64
	negRisk      bool

	// 代理配置
	proxyURL string

	// 事件总线（用于发布事件到单线程引擎）
	bus *bus.Bus

	// 健康检查
	lastPong      time.Time
	healthCheckMu sync.RWMutex

	// 健壮性增强状态
	firstSnapshotReceived atomic.Bool
	snapshotReceivedAt    time.Time
	snapshotMu            sync.RWMutex
	
	lastUpdateTime        time.Time
	updateMu              sync.RWMutex
	
	reconnectAttempts     atomic.Int32
	connectionStart       time.Time
	connectionStartMu     sync.RWMutex
}

// NewWebSocketAdapter 创建新的 WebSocket 适配器
func NewWebSocketAdapter(log *logrus.Logger, proxyURL string) *WebSocketAdapter {
	return &WebSocketAdapter{
		log:        log,
		reconnectC: make(chan struct{}, 1),
		closeC:     make(chan struct{}),
		proxyURL:   proxyURL,
		lastPong:   time.Now(),
	}
}

// Start 启动 WebSocket 适配器
func (w *WebSocketAdapter) Start(ctx context.Context, b *bus.Bus) error {
	w.bus = b

	// 如果市场信息未设置，需要先发现市场（使用当前周期）
	if w.marketSlug == "" || w.yesTokenID == "" || w.noTokenID == "" {
		// 使用 Polymarket 适配器来获取市场信息（新的实现不再需要 slug regex）
		polymarket := NewPolymarketFromEnv(w.log)
		if err := polymarket.discover(ctx); err != nil {
			return fmt.Errorf("发现市场失败: %w", err)
		}
		
		// 从 Polymarket 适配器获取市场信息
		w.marketID = polymarket.marketID
		w.yesTokenID = polymarket.yesTokenID
		w.noTokenID = polymarket.noTokenID
		w.marketSlug = polymarket.marketSlug
		w.endDate = polymarket.endDate
		w.minTickSize = polymarket.minTickSize
		w.minOrderSize = polymarket.minOrderSize
		w.negRisk = polymarket.negRisk
		
		// 从 slug 提取周期时间戳
		w.cycleTimestamp = ExtractTimestampFromSlug(w.marketSlug)
		if w.cycleTimestamp == 0 {
			w.cycleTimestamp = GetCurrent15MinTimestamp() // 如果提取失败，使用当前周期时间戳
		}
		
		w.log.Infof("市场信息已设置: slug=%s, cycle_timestamp=%d, end_date=%s",
			w.marketSlug, w.cycleTimestamp, w.endDate.Format(time.RFC3339))
	} else {
		// 如果市场信息已设置，从 slug 提取周期时间戳
		w.cycleTimestamp = ExtractTimestampFromSlug(w.marketSlug)
		if w.cycleTimestamp == 0 {
			w.cycleTimestamp = GetCurrent15MinTimestamp()
		}
	}

	// 启动周期检查循环
	w.cycleCheckStop = make(chan struct{})
	w.cycleCheckWg.Add(1)
	go w.cycleCheckLoop(ctx)

	// 发布市场快照事件
	if w.cycleTimestamp > 0 {
		_ = b.Publish(ctx, types.Event{
			Type: types.EventMarketSnapshot,
			Payload: types.MarketSnapshot{
				MarketID:     w.marketID,
				MarketSlug:   w.marketSlug,
				CycleStart:   time.Unix(w.cycleTimestamp, 0),
				EndDate:      w.endDate,
				YesTokenID:   w.yesTokenID,
				NoTokenID:    w.noTokenID,
				MinTickSize:  w.minTickSizeFloat(),
				MinOrderSize: w.minOrderSize,
				NegRisk:      w.negRisk,
			},
		})
	}

	w.log.WithFields(map[string]interface{}{
		"market_id":    w.marketID,
		"market_slug":   w.marketSlug,
		"yes_token_id": w.yesTokenID,
		"no_token_id":  w.noTokenID,
		"end_date":     w.endDate.Format(time.RFC3339),
		"proxy_url":    w.proxyURL,
	}).Info("WebSocket 适配器准备就绪")

	// 启动重连器
	go w.reconnector(ctx)

	// 立即尝试连接
	return w.DialAndConnect(ctx)
}

// DialAndConnect 拨号并连接
func (w *WebSocketAdapter) DialAndConnect(ctx context.Context) error {
	conn, err := w.Dial(ctx)
	if err != nil {
		w.reconnectAttempts.Add(1)
		return err
	}

	// 重置状态
	w.firstSnapshotReceived.Store(false)
	w.snapshotMu.Lock()
	w.snapshotReceivedAt = time.Time{}
	w.snapshotMu.Unlock()
	w.connectionStartMu.Lock()
	w.connectionStart = time.Now()
	w.connectionStartMu.Unlock()
	w.updateMu.Lock()
	w.lastUpdateTime = time.Time{}
	w.updateMu.Unlock()

	// 原子替换连接
	connCtx, connCancel := w.SetConn(ctx, conn)

	// 启动读取和 ping goroutine
	go w.Read(connCtx, conn, connCancel)
	go w.ping(connCtx, conn, connCancel)

	// 订阅市场
	if err := w.subscribe(); err != nil {
		conn.Close()
		return err
	}

	// 等待首次快照（非阻塞，如果超时则继续运行）
	// 使用带超时的等待，但不因为超时就失败
	snapshotChan := make(chan bool, 1)
	go func() {
		received := w.waitForFirstSnapshot(ctx)
		snapshotChan <- received
	}()

	// 等待首次快照，设置超时
	select {
	case received := <-snapshotChan:
		if received {
			// 验证订单簿完整性
			if !w.validateOrderbook() {
				w.log.Warnf("订单簿验证失败，但继续运行")
			} else {
				w.log.Infof("订单簿验证通过")
			}
		} else {
			// 超时但继续运行（可能市场暂时没有活动）
			w.log.Warnf("等待首次快照超时，但继续运行（可能市场暂时没有活动，后续收到数据会自动恢复）")
		}
	case <-time.After(time.Duration(snapshotTimeoutSecs) * time.Second):
		// 超时，但继续运行
		w.log.Warnf("等待首次快照超时（%d秒），但继续运行（可能市场暂时没有活动）", snapshotTimeoutSecs)
	}

	w.log.Infof("WebSocket 已连接: %s", w.marketSlug)
	
	// 重置重连计数（连接成功）
	w.reconnectAttempts.Store(0)
	
	return nil
}

// SetConn 原子替换连接
func (w *WebSocketAdapter) SetConn(ctx context.Context, conn *websocket.Conn) (context.Context, context.CancelFunc) {
	w.connMu.Lock()
	defer w.connMu.Unlock()

	// 取消旧连接
	if w.connCancel != nil {
		w.connCancel()
	}

	// 创建新连接的 context
	connCtx, connCancel := context.WithCancel(ctx)
	w.conn = conn
	w.connCtx = connCtx
	w.connCancel = connCancel

	return connCtx, connCancel
}

// Dial 拨号 WebSocket 连接
func (w *WebSocketAdapter) Dial(ctx context.Context) (*websocket.Conn, error) {
	wsURL := "wss://ws-subscriptions-clob.polymarket.com/ws/market"

	dialer := websocket.Dialer{
		HandshakeTimeout: 30 * time.Second,
	}

	// 配置代理
	if w.proxyURL != "" {
		proxyURL, err := url.Parse(w.proxyURL)
		if err == nil {
			dialer.Proxy = http.ProxyURL(proxyURL)
			w.log.Infof("使用代理连接 WebSocket: %s", w.proxyURL)
		}
	}

	conn, _, err := dialer.Dial(wsURL, nil)
	if err != nil {
		return nil, err
	}

	// 设置 ping/pong handler
	conn.SetPingHandler(nil)
	conn.SetPongHandler(func(string) error {
		w.healthCheckMu.Lock()
		w.lastPong = time.Now()
		w.healthCheckMu.Unlock()
		if err := conn.SetReadDeadline(time.Now().Add(readTimeout * 2)); err != nil {
			w.log.Errorf("设置读取超时失败: %v", err)
		}
		return nil
	})

	return conn, nil
}

// Reconnect 触发重连
func (w *WebSocketAdapter) Reconnect() {
	select {
	case w.reconnectC <- struct{}{}:
	default:
		// channel 已满，忽略
	}
}

// reconnector 重连器 goroutine
func (w *WebSocketAdapter) reconnector(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case <-w.closeC:
			return
		case <-w.reconnectC:
			attempts := w.reconnectAttempts.Load()
			if attempts >= maxReconnectAttempts {
				w.log.Errorf("超过最大重连次数 (%d)，停止重连", maxReconnectAttempts)
				return
			}

			w.log.Warnf("收到重连信号，冷却 %s... (尝试 %d/%d)", reconnectCoolDownPeriod, attempts+1, maxReconnectAttempts)
			time.Sleep(reconnectCoolDownPeriod)

			w.log.Warnf("重新连接...")
			if err := w.DialAndConnect(ctx); err != nil {
				w.reconnectAttempts.Add(1)
				w.log.Warnf("重连失败: %v，将再次尝试...", err)
				w.Reconnect() // 重新发送信号
			} else {
				// 连接成功，重置重连计数
				w.reconnectAttempts.Store(0)
			}
		}
	}
}

// Read 读取消息循环
func (w *WebSocketAdapter) Read(ctx context.Context, conn *websocket.Conn, cancel context.CancelFunc) {
	defer cancel()

	for {
		select {
		case <-ctx.Done():
			return
		case <-w.closeC:
			return
		default:
		}

		// 检查订单簿是否过期
		if w.checkOrderbookStaleness() {
			w.log.Warnf("订单簿过期，触发重连")
			conn.Close()
			w.Reconnect()
			return
		}

		// 设置读取超时
		if err := conn.SetReadDeadline(time.Now().Add(readTimeout)); err != nil {
			w.log.Errorf("设置读取超时失败: %v", err)
			return
		}

		// 读取消息
		_, message, err := conn.ReadMessage()
		if err != nil {
			if websocket.IsCloseError(err, websocket.CloseNormalClosure, websocket.CloseGoingAway) {
				w.log.Debugf("WebSocket 正常关闭")
				return
			}

			if netErr, ok := err.(interface{ Timeout() bool }); ok && netErr.Timeout() {
				continue
			}

			errStr := err.Error()
			if errStr == "use of closed network connection" {
				w.log.Debugf("WebSocket 连接已关闭")
				return
			}

			// 网络错误，触发重连
			w.log.Warnf("WebSocket 读取错误: %v，触发重连", err)
			conn.Close()
			w.Reconnect()
			return
		}

		// 更新最后更新时间
		w.updateMu.Lock()
		w.lastUpdateTime = time.Now()
		w.updateMu.Unlock()

		// 处理消息（发布到事件总线）
		w.handleMessage(ctx, message)
	}
}

// ping ping 循环
func (w *WebSocketAdapter) ping(ctx context.Context, conn *websocket.Conn, cancel context.CancelFunc) {
	defer cancel()

	ticker := time.NewTicker(pingInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-w.closeC:
			return
		case <-ticker.C:
			if err := conn.WriteControl(websocket.PingMessage, nil, time.Now().Add(writeTimeout)); err != nil {
				w.log.Warnf("发送 PING 失败: %v，触发重连", err)
				w.Reconnect()
				return
			}
		}
	}
}

// subscribe 订阅市场
func (w *WebSocketAdapter) subscribe() error {
	subscribeMsg := map[string]interface{}{
		"assets_ids": []string{w.yesTokenID, w.noTokenID},
		"type":       "market",
	}

	w.log.Infof("📡 订阅市场资产: YES=%s, NO=%s", w.yesTokenID, w.noTokenID)

	w.connMu.Lock()
	conn := w.conn
	w.connMu.Unlock()

	if conn == nil {
		return fmt.Errorf("连接未建立")
	}

	if err := conn.WriteJSON(subscribeMsg); err != nil {
		return err
	}
	w.log.Infof("✅ 订阅消息已发送")
	return nil
}

// handleMessage 处理消息并发布到事件总线
func (w *WebSocketAdapter) handleMessage(ctx context.Context, message []byte) {
	var msgType struct {
		EventType string `json:"event_type"`
	}
	if err := json.Unmarshal(message, &msgType); err != nil {
		w.log.Debugf("解析消息类型失败: %v", err)
		return
	}

	switch msgType.EventType {
	case "book":
		// 订单簿快照消息（首次订阅时会收到）
		// 标记收到首次快照
		if !w.firstSnapshotReceived.Load() {
			w.firstSnapshotReceived.Store(true)
			w.snapshotMu.Lock()
			w.snapshotReceivedAt = time.Now()
			w.snapshotMu.Unlock()
			w.log.Infof("✅ 收到订单簿快照（首次快照）")
		}
		// 处理订单簿消息（可选，通常价格变化会通过 price_change 发送）
		w.log.Debugf("收到订单簿快照消息")
	case "price_change":
		// 标记收到首次快照（价格变化表示数据流已开始）
		if !w.firstSnapshotReceived.Load() {
			w.firstSnapshotReceived.Store(true)
			w.snapshotMu.Lock()
			w.snapshotReceivedAt = time.Now()
			w.snapshotMu.Unlock()
			w.log.Infof("✅ 收到价格变化（首次快照）")
		}
		w.handlePriceChange(ctx, message)
	case "subscribed":
		w.log.Infof("✅ WebSocket 收到订阅成功消息")
	case "pong":
		w.healthCheckMu.Lock()
		w.lastPong = time.Now()
		w.healthCheckMu.Unlock()
	case "last_trade_price":
		// 最后成交价消息（也可以作为首次数据）
		if !w.firstSnapshotReceived.Load() {
			w.firstSnapshotReceived.Store(true)
			w.snapshotMu.Lock()
			w.snapshotReceivedAt = time.Now()
			w.snapshotMu.Unlock()
			w.log.Infof("✅ 收到最后成交价（首次快照）")
		}
		w.log.Debugf("收到最后成交价消息")
	case "tick_size_change":
		w.log.Debugf("收到 tick size 变化消息")
	default:
		w.log.Debugf("收到未知消息类型: %s", msgType.EventType)
	}
}

// handlePriceChange 处理价格变化并发布到事件总线
func (w *WebSocketAdapter) handlePriceChange(ctx context.Context, message []byte) {
	var msg map[string]interface{}
	if err := json.Unmarshal(message, &msg); err != nil {
		w.log.Warnf("解析价格变化消息失败: %v", err)
		return
	}

	priceChanges, ok := msg["price_changes"].([]interface{})
	if !ok {
		return
	}

	// 提取价格信息
	var bestBid, bestAsk float64
	var ts time.Time

	for _, pc := range priceChanges {
		change, ok := pc.(map[string]interface{})
		if !ok {
			continue
		}

		assetID, _ := change["asset_id"].(string)
		if assetID != w.yesTokenID {
			continue
		}

		// 获取价格
		if bestAskStr, ok := change["best_ask"].(string); ok && bestAskStr != "" {
			bestAsk, _ = strconv.ParseFloat(bestAskStr, 64)
		}
		if bestBidStr, ok := change["best_bid"].(string); ok && bestBidStr != "" {
			bestBid, _ = strconv.ParseFloat(bestBidStr, 64)
		}

		// 获取时间戳
		if tsStr, ok := change["timestamp"].(string); ok {
			if ms, err := strconv.ParseInt(tsStr, 10, 64); err == nil {
				ts = time.UnixMilli(ms).UTC()
			}
		}
	}

	if bestBid <= 0 && bestAsk <= 0 {
		return
	}

	// 计算 pYes（使用包内辅助函数）
	pYes := calculateMidpoint(bestBid, bestAsk)
	if pYes <= 0 {
		if bestAsk > 0 {
			pYes = bestAsk
		} else if bestBid > 0 {
			pYes = bestBid
		} else {
			pYes = 0.5
		}
	}

	rem := time.Until(w.endDate)
	if rem < 0 {
		rem = 0
	}

	// 发布价格变化事件到总线（单线程引擎会处理）
	_ = w.bus.Publish(ctx, types.Event{
		Type: types.EventMarketTick,
		Payload: types.MarketTick{
			MarketID:      w.marketID,
			PYes:          calculateClamp01(pYes),
			BestBid:       calculateClamp01(bestBid),
			BestAsk:       calculateClamp01(bestAsk),
			TimeRemaining: rem,
			DataQuality:   0.95, // WebSocket 数据质量高
		},
		TsExchange: ts,
	})
}

// cycleCheckLoop 周期检查循环（每秒检查是否需要切换周期）
func (w *WebSocketAdapter) cycleCheckLoop(ctx context.Context) {
	defer w.cycleCheckWg.Done()
	
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()
	
	for {
		select {
		case <-ctx.Done():
			return
		case <-w.closeC:
			return
		case <-w.cycleCheckStop:
			return
		case <-ticker.C:
			w.checkAndSwitchCycle(ctx)
		}
	}
}

// checkAndSwitchCycle 检查并切换周期
func (w *WebSocketAdapter) checkAndSwitchCycle(ctx context.Context) {
	// 防止重复切换
	if w.switchingCycle.Load() {
		return
	}
	
	// 检查周期是否已过期
	if !IsCycleExpired(w.cycleTimestamp) {
		return
	}
	
	// 标记正在切换
	if !w.switchingCycle.CompareAndSwap(false, true) {
		return
	}
	defer w.switchingCycle.Store(false)
	
	w.log.Infof("🔄 检测到周期结束，切换到下一个周期 (当前周期: %s, timestamp=%d)",
		w.marketSlug, w.cycleTimestamp)
	
	// 关闭当前连接
	w.connMu.Lock()
	if w.connCancel != nil {
		w.connCancel()
	}
	if w.conn != nil {
		w.conn.Close()
		w.conn = nil
	}
	w.connMu.Unlock()
	
	// 获取下一个周期的市场
	nextTs := GetNextCycleTimestamp(w.cycleTimestamp)
	nextSlug := Generate15MinSlug(nextTs)
	
	w.log.Infof("获取下一个周期市场: %s (timestamp=%d)", nextSlug, nextTs)
	
	// 使用 Polymarket 适配器获取下一个周期的市场信息
	// 新的 discover 方法会根据当前周期时间戳自动查找
	// 由于周期已经切换，GetCurrent15MinTimestamp() 应该返回下一个周期的时间戳
	polymarket := NewPolymarketFromEnv(w.log)
	
	// 使用 discover 方法获取市场信息
	discoverCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	
	if err := polymarket.discover(discoverCtx); err != nil {
		w.log.Errorf("获取下一个周期市场失败: %v，将重试", err)
		// 如果获取失败，可能市场还没创建，等待一段时间后重试
		go func() {
			time.Sleep(5 * time.Second)
			w.switchingCycle.Store(false) // 重置标志，允许重试
		}()
		return
	}
	
	// 验证获取到的市场是否匹配下一个周期
	if polymarket.marketSlug != nextSlug {
		// 如果 slug 不匹配，检查时间戳是否匹配（可能市场 slug 格式有变化）
		actualTs := ExtractTimestampFromSlug(polymarket.marketSlug)
		if actualTs != nextTs {
			w.log.Warnf("获取到的市场时间戳不匹配: 期望=%d (%s), 实际=%d (%s)，但继续使用", 
				nextTs, nextSlug, actualTs, polymarket.marketSlug)
		}
	}
	
	// 更新市场信息
	w.marketID = polymarket.marketID
	w.yesTokenID = polymarket.yesTokenID
	w.noTokenID = polymarket.noTokenID
	w.marketSlug = polymarket.marketSlug
	w.endDate = polymarket.endDate
	w.minTickSize = polymarket.minTickSize
	w.minOrderSize = polymarket.minOrderSize
	w.negRisk = polymarket.negRisk
	w.cycleTimestamp = nextTs
	
	w.log.Infof("✅ 周期切换成功: %s (timestamp=%d, end_date=%s)",
		w.marketSlug, w.cycleTimestamp, w.endDate.Format(time.RFC3339))
	
	// 发布市场快照事件
	if w.bus != nil {
		_ = w.bus.Publish(ctx, types.Event{
			Type: types.EventMarketSnapshot,
			Payload: types.MarketSnapshot{
				MarketID:     w.marketID,
				MarketSlug:   w.marketSlug,
				CycleStart:   time.Unix(w.cycleTimestamp, 0),
				EndDate:      w.endDate,
				YesTokenID:   w.yesTokenID,
				NoTokenID:    w.noTokenID,
				MinTickSize:  w.minTickSizeFloat(),
				MinOrderSize: w.minOrderSize,
				NegRisk:      w.negRisk,
			},
		})
	}
	
	// 重新连接 WebSocket
	w.log.Infof("重新连接 WebSocket 到新周期市场...")
	if err := w.DialAndConnect(ctx); err != nil {
		w.log.Errorf("重新连接失败: %v，将触发重连器", err)
		w.Reconnect()
	}
}

// discoverMarket 发现市场（复用原有逻辑）
func (w *WebSocketAdapter) discoverMarket(ctx context.Context, httpClient *http.Client) error {
	// 这里可以复用原有的市场发现逻辑
	// 为了简化，假设市场信息已经通过配置提供
	// 如果需要，可以从 HTTP API 发现市场
	return nil
}

// SetMarketInfo 设置市场信息（用于配置模式）
func (w *WebSocketAdapter) SetMarketInfo(marketID, marketSlug, yesTokenID, noTokenID string, endDate time.Time, minTickSize string, minOrderSize float64, negRisk bool) {
	w.marketID = marketID
	w.marketSlug = marketSlug
	w.yesTokenID = yesTokenID
	w.noTokenID = noTokenID
	w.endDate = endDate
	w.minTickSize = minTickSize
	w.minOrderSize = minOrderSize
	w.negRisk = negRisk
}

// PlaceOrder 实现 Adapter 接口（委托给 HTTP 客户端）
// WebSocket 适配器主要用于接收市场数据，订单操作仍使用 HTTP API
func (w *WebSocketAdapter) PlaceOrder(ctx context.Context, req oms.PlaceOrderRequest) (oms.PlaceOrderResult, error) {
	// WebSocket 适配器主要用于市场数据，订单操作需要 HTTP 客户端
	// 这里返回错误，提示需要使用支持交易的适配器
	return oms.PlaceOrderResult{
		Accepted: false,
		Reason:   "WebSocket adapter does not support order placement, use HTTP adapter for trading",
	}, fmt.Errorf("WebSocket adapter does not support order placement")
}

// CancelOrder 实现 Adapter 接口（委托给 HTTP 客户端）
func (w *WebSocketAdapter) CancelOrder(ctx context.Context, req oms.CancelOrderRequest) (oms.CancelOrderResult, error) {
	// WebSocket 适配器主要用于市场数据，订单操作需要 HTTP 客户端
	return oms.CancelOrderResult{
		Ok:     false,
		Reason: "WebSocket adapter does not support order cancellation, use HTTP adapter for trading",
	}, fmt.Errorf("WebSocket adapter does not support order cancellation")
}

// Close 关闭连接
func (w *WebSocketAdapter) Close() error {
	// 停止周期检查循环
	if w.cycleCheckStop != nil {
		select {
		case <-w.cycleCheckStop:
			// 已经关闭
		default:
			close(w.cycleCheckStop)
		}
		w.cycleCheckWg.Wait()
	}
	
	select {
	case <-w.closeC:
		return nil
	default:
		close(w.closeC)
	}

	w.connMu.Lock()
	if w.connCancel != nil {
		w.connCancel()
	}
	if w.conn != nil {
		w.conn.Close()
		w.conn = nil
	}
	w.connMu.Unlock()

	return nil
}

// 辅助函数（避免与包内其他函数冲突）
func calculateMidpoint(bid, ask float64) float64 {
	if bid <= 0 || ask <= 0 {
		return 0
	}
	return (bid + ask) / 2
}

func calculateClamp01(x float64) float64 {
	if x < 0 {
		return 0
	}
	if x > 1 {
		return 1
	}
	return x
}

func (w *WebSocketAdapter) minTickSizeFloat() float64 {
	// 解析 minTickSize 字符串为 float64
	if w.minTickSize == "" {
		return 0.01
	}
	f, _ := strconv.ParseFloat(w.minTickSize, 64)
	return f
}

// waitForFirstSnapshot 等待首次快照
func (w *WebSocketAdapter) waitForFirstSnapshot(ctx context.Context) bool {
	start := time.Now()
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return false
		case <-w.closeC:
			return false
		case <-ticker.C:
			if w.firstSnapshotReceived.Load() {
				elapsed := time.Since(start)
				w.log.Infof("✅ 收到首次快照（等待时间: %v）", elapsed)
				return true
			}
			elapsed := time.Since(start)
			if elapsed > time.Duration(snapshotTimeoutSecs)*time.Second {
				w.log.Warnf("等待首次快照超时（%d秒），但继续运行（可能市场暂时没有活动）", snapshotTimeoutSecs)
				// 不返回 false，而是继续运行（可能市场暂时没有活动）
				// 后续如果有数据会自然标记为已收到快照
				return false
			}
		}
	}
}

// validateOrderbook 验证订单簿完整性
func (w *WebSocketAdapter) validateOrderbook() bool {
	// 简单验证：检查是否收到了快照
	if !w.firstSnapshotReceived.Load() {
		w.log.Error("订单簿验证失败：未收到首次快照")
		return false
	}
	w.log.Info("订单簿验证通过")
	return true
}

// checkOrderbookStaleness 检查订单簿是否过期
func (w *WebSocketAdapter) checkOrderbookStaleness() bool {
	w.updateMu.RLock()
	lastUpdate := w.lastUpdateTime
	w.updateMu.RUnlock()

	// 如果从未收到更新，不认为过期（可能是刚连接）
	if lastUpdate.IsZero() {
		return false
	}

	w.connectionStartMu.RLock()
	connectionStart := w.connectionStart
	w.connectionStartMu.RUnlock()

	// 检查连接时间
	connectionAge := time.Since(connectionStart).Seconds()
	staleness := time.Since(lastUpdate).Seconds()

	// 只有在有活动的情况下才检查过期（连接超过5秒且有更新）
	if connectionAge > 5.0 && staleness > stalenessthresholdSecs {
		w.log.Warnf("订单簿过期：最后更新 %v 秒前（阈值: %.1f秒）", staleness, stalenessthresholdSecs)
		return true
	}

	return false
}

