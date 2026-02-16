package node

import (
	"context"
	"math"
	"sync"
	"time"
)

// ReconnectConfig 重连配置
// ReconnectConfig controls retry/backoff behavior for reconnect attempts.
type ReconnectConfig struct {
	// 是否启用自动重连
	Enabled bool

	// 最大重试次数，0 表示无限重试
	MaxRetries int

	// 初始重连延迟
	InitialDelay time.Duration

	// 最大重连延迟
	MaxDelay time.Duration

	// 退避乘数（每次失败后延迟乘以此值）
	BackoffMultiplier float64

	// 抖动比例（0-1，添加随机性避免雷鸣效应）
	JitterFactor float64

	// 重连超时（单次重连尝试的超时时间）
	ReconnectTimeout time.Duration
}

// DefaultReconnectConfig 返回默认重连配置
// DefaultReconnectConfig returns production-safe reconnect defaults.
func DefaultReconnectConfig() *ReconnectConfig {
	return &ReconnectConfig{
		Enabled:           true,
		MaxRetries:        10,               // 最多重试 10 次
		InitialDelay:      time.Second,      // 初始延迟 1 秒
		MaxDelay:          5 * time.Minute,  // 最大延迟 5 分钟
		BackoffMultiplier: 2.0,              // 每次翻倍
		JitterFactor:      0.2,              // 20% 抖动
		ReconnectTimeout:  30 * time.Second, // 单次重连超时 30 秒
	}
}

// ReconnectState 重连状态
// ReconnectState describes current reconnect state machine status.
type ReconnectState int

const (
	// ReconnectStateIdle 空闲（未在重连）
	ReconnectStateIdle ReconnectState = iota
	// ReconnectStateWaiting 等待重连（在退避延迟中）
	ReconnectStateWaiting
	// ReconnectStateConnecting 正在重连
	ReconnectStateConnecting
	// ReconnectStateSuccess 重连成功
	ReconnectStateSuccess
	// ReconnectStateFailed 重连失败（达到最大重试次数）
	ReconnectStateFailed
)

func (s ReconnectState) String() string {
	switch s {
	case ReconnectStateIdle:
		return "Idle"
	case ReconnectStateWaiting:
		return "Waiting"
	case ReconnectStateConnecting:
		return "Connecting"
	case ReconnectStateSuccess:
		return "Success"
	case ReconnectStateFailed:
		return "Failed"
	default:
		return "Unknown"
	}
}

// ReconnectInfo 重连信息
// ReconnectInfo is an external snapshot of one reconnect task.
type ReconnectInfo struct {
	PeerID       string         // 目标节点 ID
	Addr         string         // 目标地址
	State        ReconnectState // 当前状态
	Attempt      int            // 当前尝试次数
	MaxRetries   int            // 最大重试次数
	NextRetry    time.Time      // 下次重试时间
	LastError    error          // 最后一次错误
	DisconnectAt time.Time      // 断开时间
}

// reconnectEntry 重连条目（内部使用）
type reconnectEntry struct {
	peerID       string
	addr         string
	state        ReconnectState
	attempt      int
	maxRetries   int
	nextRetry    time.Time
	lastError    error
	disconnectAt time.Time
	cancelFunc   context.CancelFunc
}

// ReconnectManager 重连管理器
// ReconnectManager schedules and runs reconnect tasks per peer.
type ReconnectManager struct {
	config *ReconnectConfig
	node   *Node

	entries map[string]*reconnectEntry // peerID -> entry
	mu      sync.RWMutex

	// 回调
	onReconnecting func(peerID string, attempt int, nextRetryIn time.Duration)
	onReconnected  func(peerID string, attempts int)
	onGaveUp       func(peerID string, attempts int, lastErr error)

	closing chan struct{}
	wg      sync.WaitGroup
}

// NewReconnectManager 创建重连管理器
// NewReconnectManager creates a reconnect manager bound to a node instance.
func NewReconnectManager(node *Node, config *ReconnectConfig) *ReconnectManager {
	if config == nil {
		config = DefaultReconnectConfig()
	}
	return &ReconnectManager{
		config:  config,
		node:    node,
		entries: make(map[string]*reconnectEntry),
		closing: make(chan struct{}),
	}
}

// SetOnReconnecting 设置重连中回调
func (rm *ReconnectManager) SetOnReconnecting(handler func(peerID string, attempt int, nextRetryIn time.Duration)) {
	rm.mu.Lock()
	defer rm.mu.Unlock()
	rm.onReconnecting = handler
}

// SetOnReconnected 设置重连成功回调
func (rm *ReconnectManager) SetOnReconnected(handler func(peerID string, attempts int)) {
	rm.mu.Lock()
	defer rm.mu.Unlock()
	rm.onReconnected = handler
}

// SetOnGaveUp 设置放弃重连回调
func (rm *ReconnectManager) SetOnGaveUp(handler func(peerID string, attempts int, lastErr error)) {
	rm.mu.Lock()
	defer rm.mu.Unlock()
	rm.onGaveUp = handler
}

// ScheduleReconnect 调度重连任务
// ScheduleReconnect starts or reuses reconnect workflow for one peer.
func (rm *ReconnectManager) ScheduleReconnect(peerID, addr string) {
	if !rm.config.Enabled || addr == "" {
		return
	}

	rm.mu.Lock()

	// 检查是否已在重连中
	if entry, exists := rm.entries[peerID]; exists {
		if entry.state == ReconnectStateConnecting || entry.state == ReconnectStateWaiting {
			rm.mu.Unlock()
			return
		}
	}

	// 创建新的重连条目
	ctx, cancel := context.WithCancel(context.Background())
	entry := &reconnectEntry{
		peerID:       peerID,
		addr:         addr,
		state:        ReconnectStateWaiting,
		attempt:      0,
		maxRetries:   rm.config.MaxRetries,
		disconnectAt: time.Now(),
		cancelFunc:   cancel,
	}
	rm.entries[peerID] = entry
	rm.mu.Unlock()

	// 启动重连协程
	rm.wg.Add(1)
	go rm.reconnectLoop(ctx, entry)
}

// CancelReconnect 取消指定节点的重连
func (rm *ReconnectManager) CancelReconnect(peerID string) {
	rm.mu.Lock()
	defer rm.mu.Unlock()

	if entry, exists := rm.entries[peerID]; exists {
		if entry.cancelFunc != nil {
			entry.cancelFunc()
		}
		delete(rm.entries, peerID)
	}
}

// CancelAll 取消所有重连任务
func (rm *ReconnectManager) CancelAll() {
	rm.mu.Lock()
	for peerID, entry := range rm.entries {
		if entry.cancelFunc != nil {
			entry.cancelFunc()
		}
		delete(rm.entries, peerID)
	}
	rm.mu.Unlock()
}

// GetReconnectInfo 获取指定节点的重连信息
func (rm *ReconnectManager) GetReconnectInfo(peerID string) *ReconnectInfo {
	rm.mu.RLock()
	defer rm.mu.RUnlock()

	entry, exists := rm.entries[peerID]
	if !exists {
		return nil
	}

	return &ReconnectInfo{
		PeerID:       entry.peerID,
		Addr:         entry.addr,
		State:        entry.state,
		Attempt:      entry.attempt,
		MaxRetries:   entry.maxRetries,
		NextRetry:    entry.nextRetry,
		LastError:    entry.lastError,
		DisconnectAt: entry.disconnectAt,
	}
}

// GetAllReconnectInfo 获取所有重连信息
func (rm *ReconnectManager) GetAllReconnectInfo() []*ReconnectInfo {
	rm.mu.RLock()
	defer rm.mu.RUnlock()

	result := make([]*ReconnectInfo, 0, len(rm.entries))
	for _, entry := range rm.entries {
		result = append(result, &ReconnectInfo{
			PeerID:       entry.peerID,
			Addr:         entry.addr,
			State:        entry.state,
			Attempt:      entry.attempt,
			MaxRetries:   entry.maxRetries,
			NextRetry:    entry.nextRetry,
			LastError:    entry.lastError,
			DisconnectAt: entry.disconnectAt,
		})
	}
	return result
}

// Close 关闭重连管理器
func (rm *ReconnectManager) Close() {
	close(rm.closing)
	rm.CancelAll()
	rm.wg.Wait()
}

// reconnectLoop 重连循环
// reconnectLoop executes retry with timeout/backoff until success or give-up.
func (rm *ReconnectManager) reconnectLoop(ctx context.Context, entry *reconnectEntry) {
	defer rm.wg.Done()

	for {
		select {
		case <-ctx.Done():
			return
		case <-rm.closing:
			return
		default:
		}

		// 检查是否已经重新连接
		if rm.isPeerConnected(entry.peerID) {
			rm.handleReconnectSuccess(entry)
			return
		}

		entry.attempt++

		// 检查是否超过最大重试次数
		if rm.config.MaxRetries > 0 && entry.attempt > rm.config.MaxRetries {
			rm.handleReconnectFailed(entry)
			return
		}

		// 计算退避延迟
		delay := rm.calculateBackoff(entry.attempt)
		entry.nextRetry = time.Now().Add(delay)

		rm.mu.Lock()
		entry.state = ReconnectStateWaiting
		rm.mu.Unlock()

		// 触发重连中回调
		rm.mu.RLock()
		callback := rm.onReconnecting
		rm.mu.RUnlock()
		if callback != nil {
			go callback(entry.peerID, entry.attempt, delay)
		}

		rm.node.Config.Logger.Info("节点 %s 将在 %v 后进行第 %d 次重连尝试",
			entry.peerID[:min(8, len(entry.peerID))], delay, entry.attempt)

		// 等待延迟
		select {
		case <-ctx.Done():
			return
		case <-rm.closing:
			return
		case <-time.After(delay):
		}

		// 再次检查是否已连接
		if rm.isPeerConnected(entry.peerID) {
			rm.handleReconnectSuccess(entry)
			return
		}

		// 执行重连
		rm.mu.Lock()
		entry.state = ReconnectStateConnecting
		rm.mu.Unlock()

		rm.node.Config.Logger.Info("正在重连节点 %s (尝试 %d/%d)",
			entry.peerID[:min(8, len(entry.peerID))], entry.attempt, rm.config.MaxRetries)

		// 使用带超时的 context 进行连接
		attemptCtx, cancel := context.WithTimeout(ctx, rm.config.ReconnectTimeout)
		err := rm.node.ConnectContext(attemptCtx, entry.addr)
		cancel()

		if err != nil {
			entry.lastError = err
			rm.node.Config.Logger.Warn("重连节点 %s 失败: %v",
				entry.peerID[:min(8, len(entry.peerID))], err)

			// 更新指标
			if rm.node.metrics != nil {
				rm.node.metrics.IncReconnectAttempts()
				rm.node.metrics.IncReconnectFailed()
			}
			continue
		}

		// 更新指标
		if rm.node.metrics != nil {
			rm.node.metrics.IncReconnectAttempts()
		}

		// 等待一小段时间确认连接成功
		select {
		case <-ctx.Done():
			return
		case <-rm.closing:
			return
		case <-time.After(2 * time.Second):
		}

		// 检查是否真的连接成功
		if rm.isPeerConnected(entry.peerID) {
			rm.handleReconnectSuccess(entry)
			return
		}
		// 如果仍未连接，继续重试
	}
}

// isPeerConnected 检查节点是否已连接
func (rm *ReconnectManager) isPeerConnected(peerID string) bool {
	for _, id := range rm.node.Peers.IDs() {
		if id == peerID {
			return true
		}
	}
	return false
}

// calculateBackoff 计算退避延迟
// calculateBackoff computes exponential backoff with optional jitter and caps.
func (rm *ReconnectManager) calculateBackoff(attempt int) time.Duration {
	// 指数退避：delay = initialDelay * (multiplier ^ (attempt - 1))
	delay := float64(rm.config.InitialDelay) * math.Pow(rm.config.BackoffMultiplier, float64(attempt-1))

	// 添加抖动
	if rm.config.JitterFactor > 0 {
		jitter := delay * rm.config.JitterFactor * (2*randFloat() - 1) // -jitter ~ +jitter
		delay += jitter
	}

	// 限制最大延迟
	if delay > float64(rm.config.MaxDelay) {
		delay = float64(rm.config.MaxDelay)
	}

	// 确保延迟不小于 0
	if delay < 0 {
		delay = float64(rm.config.InitialDelay)
	}

	return time.Duration(delay)
}

// handleReconnectSuccess 处理重连成功
// handleReconnectSuccess finalizes entry and emits success callback/metrics.
func (rm *ReconnectManager) handleReconnectSuccess(entry *reconnectEntry) {
	rm.mu.Lock()
	entry.state = ReconnectStateSuccess
	attempts := entry.attempt
	peerID := entry.peerID
	delete(rm.entries, peerID)
	callback := rm.onReconnected
	rm.mu.Unlock()

	rm.node.Config.Logger.Info("节点 %s 重连成功 (共尝试 %d 次)",
		peerID[:min(8, len(peerID))], attempts)

	// 更新指标
	if rm.node.metrics != nil {
		rm.node.metrics.IncReconnectSuccess()
	}

	if callback != nil {
		go callback(peerID, attempts)
	}
}

// handleReconnectFailed 处理重连失败（放弃）
// handleReconnectFailed finalizes entry and emits give-up callback.
func (rm *ReconnectManager) handleReconnectFailed(entry *reconnectEntry) {
	rm.mu.Lock()
	entry.state = ReconnectStateFailed
	attempts := entry.attempt - 1 // 实际尝试次数
	peerID := entry.peerID
	lastErr := entry.lastError
	delete(rm.entries, peerID)
	callback := rm.onGaveUp
	rm.mu.Unlock()

	rm.node.Config.Logger.Warn("节点 %s 重连失败，已放弃 (共尝试 %d 次，最后错误: %v)",
		peerID[:min(8, len(peerID))], attempts, lastErr)

	if callback != nil {
		go callback(peerID, attempts, lastErr)
	}
}

// randFloat 生成 [0, 1) 范围的随机浮点数
func randFloat() float64 {
	// 使用时间纳秒作为简单随机源
	return float64(time.Now().UnixNano()%1000) / 1000.0
}

// min 返回两个整数中的较小值
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
