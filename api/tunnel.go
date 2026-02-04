package api

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/cykyes/tenet/crypto"
	"github.com/cykyes/tenet/log"
	"github.com/cykyes/tenet/node"
)

// Tunnel 是用户使用的主要接口
type Tunnel struct {
	node    *node.Node
	mu      sync.RWMutex
	started bool
	closed  bool
}

// NewTunnel 创建隧道实例
func NewTunnel(opts ...Option) (*Tunnel, error) {
	cfg := &tunnelConfig{
		nodeOpts: make([]node.Option, 0),
	}

	for _, opt := range opts {
		opt(cfg)
	}

	if cfg.err != nil {
		return nil, fmt.Errorf("配置错误: %w", cfg.err)
	}

	// 创建节点
	n, err := node.NewNode(cfg.nodeOpts...)
	if err != nil {
		return nil, fmt.Errorf("创建节点失败: %w", err)
	}

	return &Tunnel{
		node: n,
	}, nil
}

// Start 启动隧道服务
func (t *Tunnel) Start() error {
	t.mu.Lock()
	defer t.mu.Unlock()

	if t.closed {
		return fmt.Errorf("隧道已关闭")
	}
	if t.started {
		return fmt.Errorf("隧道已启动")
	}

	if err := t.node.Start(); err != nil {
		return err
	}
	t.started = true
	return nil
}

// Stop 停止服务
func (t *Tunnel) Stop() error {
	t.mu.Lock()
	defer t.mu.Unlock()

	if !t.started {
		return fmt.Errorf("隧道未启动")
	}

	t.closed = true
	t.started = false
	return t.node.Stop()
}

// Connect 主动连接对等节点
func (t *Tunnel) Connect(addr string) error {
	return t.node.Connect(addr)
}

// Send 发送数据
func (t *Tunnel) Send(peerID string, data []byte) error {
	return t.node.Send(peerID, data)
}

// OnReceive 设置接收回调
func (t *Tunnel) OnReceive(handler func(peerID string, data []byte)) {
	t.node.OnReceive(handler)
}

// OnPeerConnected 对等节点连接回调
func (t *Tunnel) OnPeerConnected(handler func(peerID string)) {
	t.node.OnPeerConnected(handler)
}

// OnPeerDisconnected 对等节点断开回调
func (t *Tunnel) OnPeerDisconnected(handler func(peerID string)) {
	t.node.OnPeerDisconnected(handler)
}

// LocalID 获取本地节点ID
func (t *Tunnel) LocalID() string {
	return t.node.ID()
}

// LocalAddr 获取本地监听地址
func (t *Tunnel) LocalAddr() string {
	if t.node.LocalAddr != nil {
		return t.node.LocalAddr.String()
	}
	return ""
}

// PublicAddr 获取公网地址（如果已探测）
func (t *Tunnel) PublicAddr() string {
	if t.node.PublicAddr != nil {
		return t.node.PublicAddr.String()
	}
	return ""
}

// Peers 获取已连接的对等节点列表
func (t *Tunnel) Peers() []string {
	return t.node.Peers.IDs()
}

// PeerCount 获取已连接节点数量
func (t *Tunnel) PeerCount() int {
	return t.node.Peers.Count()
}

// PeerTransport 获取对等节点的传输协议 (tcp/udp)
func (t *Tunnel) PeerTransport(peerID string) string {
	return t.node.GetPeerTransport(peerID)
}

// PeerLinkMode 获取对等节点的链路模式 (p2p/relay)
func (t *Tunnel) PeerLinkMode(peerID string) string {
	return t.node.GetPeerLinkMode(peerID)
}

// GetMetrics 获取节点指标快照
func (t *Tunnel) GetMetrics() interface{} {
	return t.node.GetMetrics()
}

// GetIdentityJSON 获取当前节点身份的 JSON 数据
func (t *Tunnel) GetIdentityJSON() ([]byte, error) {
	if t.node.Identity == nil {
		return nil, fmt.Errorf("节点身份未初始化")
	}
	return t.node.Identity.ToJSON()
}

// ProbeNAT 探测本机 NAT 类型（需要至少一个已连接的节点）
func (t *Tunnel) ProbeNAT() (string, error) {
	result, err := t.node.ProbeNAT()
	if err != nil {
		return "", err
	}
	return result.NATType.String(), nil
}

// GetNATType 获取已探测的 NAT 类型
func (t *Tunnel) GetNATType() string {
	info := t.node.GetNATInfo()
	if info == nil {
		return "Unknown"
	}
	return info.Type.String()
}

// GracefulStop 优雅关闭隧道
func (t *Tunnel) GracefulStop() error {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.closed = true
	return t.node.GracefulStop(context.Background())
}

// --- 重连相关方法 ---

// OnReconnecting 设置重连中回调
// 当节点开始尝试重连时触发，提供当前尝试次数和下次重试延迟
func (t *Tunnel) OnReconnecting(handler func(peerID string, attempt int, nextRetryIn time.Duration)) {
	t.node.OnReconnecting(handler)
}

// OnReconnected 设置重连成功回调
// 当节点成功重连时触发，提供总尝试次数
func (t *Tunnel) OnReconnected(handler func(peerID string, attempts int)) {
	t.node.OnReconnected(handler)
}

// OnGaveUp 设置放弃重连回调
// 当达到最大重试次数后触发，提供总尝试次数和最后一次错误
func (t *Tunnel) OnGaveUp(handler func(peerID string, attempts int, lastErr error)) {
	t.node.OnGaveUp(handler)
}

// GetReconnectingPeers 获取正在重连的节点列表
func (t *Tunnel) GetReconnectingPeers() []string {
	infos := t.node.GetAllReconnectInfo()
	if infos == nil {
		return nil
	}
	result := make([]string, 0, len(infos))
	for _, info := range infos {
		result = append(result, info.PeerID)
	}
	return result
}

// CancelReconnect 取消指定节点的重连
func (t *Tunnel) CancelReconnect(peerID string) {
	t.node.CancelReconnect(peerID)
}

// CancelAllReconnects 取消所有重连任务
func (t *Tunnel) CancelAllReconnects() {
	t.node.CancelAllReconnects()
}

// --- 配置选项 ---

type tunnelConfig struct {
	nodeOpts []node.Option
	err      error
}

// Option 配置选项
type Option func(*tunnelConfig)

// WithPassword 设置网络密码
func WithPassword(password string) Option {
	return func(c *tunnelConfig) {
		c.nodeOpts = append(c.nodeOpts, node.WithNetworkPassword(password))
	}
}

// WithListenPort 设置 UDP 监听端口
func WithListenPort(port int) Option {
	return func(c *tunnelConfig) {
		c.nodeOpts = append(c.nodeOpts, node.WithListenPort(port))
	}
}

// WithIdentityJSON 设置节点身份（JSON 格式）
func WithIdentityJSON(jsonData []byte) Option {
	return func(c *tunnelConfig) {
		if c.err != nil {
			return
		}
		id, err := crypto.IdentityFromJSON(jsonData)
		if err != nil {
			c.err = fmt.Errorf("身份数据无效: %w", err)
			return
		}
		c.nodeOpts = append(c.nodeOpts, node.WithIdentity(id))
	}
}

// WithEnableHolePunch 设置是否启用NAT打洞
func WithEnableHolePunch(enable bool) Option {
	return func(c *tunnelConfig) {
		c.nodeOpts = append(c.nodeOpts, node.WithEnableHolePunch(enable))
	}
}

// WithEnableRelay 设置是否允许中继
func WithEnableRelay(enable bool) Option {
	return func(c *tunnelConfig) {
		c.nodeOpts = append(c.nodeOpts, node.WithEnableRelay(enable))
	}
}

// WithMaxPeers 设置最大连接数
func WithMaxPeers(max int) Option {
	return func(c *tunnelConfig) {
		c.nodeOpts = append(c.nodeOpts, node.WithMaxPeers(max))
	}
}

// WithRelayNodes 设置中继节点地址列表
func WithRelayNodes(addrs []string) Option {
	return func(c *tunnelConfig) {
		c.nodeOpts = append(c.nodeOpts, node.WithRelayNodes(addrs))
	}
}

// WithDialTimeout 设置连接超时
func WithDialTimeout(timeout time.Duration) Option {
	return func(c *tunnelConfig) {
		c.nodeOpts = append(c.nodeOpts, node.WithDialTimeout(timeout))
	}
}

// WithLogger 设置日志记录器
// 默认为静默模式（NopLogger），不输出任何日志
// 可使用 log.NewStdLogger() 启用控制台日志输出
func WithLogger(logger log.Logger) Option {
	return func(c *tunnelConfig) {
		c.nodeOpts = append(c.nodeOpts, node.WithLogger(logger))
	}
}

// WithEnableRelayAuth 设置是否启用中继认证
// 默认启用，可防止未授权节点滥用中继服务
func WithEnableRelayAuth(enable bool) Option {
	return func(c *tunnelConfig) {
		c.nodeOpts = append(c.nodeOpts, node.WithEnableRelayAuth(enable))
	}
}

// WithKCPConfig 设置 KCP 配置
// 可选模式：
//   - node.DefaultKCPConfig(): 平衡模式（默认）
//   - 自定义配置以调整延迟/带宽权衡
func WithKCPConfig(cfg *node.KCPConfig) Option {
	return func(c *tunnelConfig) {
		c.nodeOpts = append(c.nodeOpts, node.WithKCPConfig(cfg))
	}
}

// WithEnableReconnect 设置是否启用自动重连
// 默认启用，断开连接后自动尝试重连
func WithEnableReconnect(enable bool) Option {
	return func(c *tunnelConfig) {
		c.nodeOpts = append(c.nodeOpts, node.WithEnableReconnect(enable))
	}
}

// WithReconnectConfig 设置重连配置
// 可自定义重试次数、退避策略等参数
func WithReconnectConfig(cfg *node.ReconnectConfig) Option {
	return func(c *tunnelConfig) {
		c.nodeOpts = append(c.nodeOpts, node.WithReconnectConfig(cfg))
	}
}

// WithReconnectMaxRetries 设置最大重连次数
// 0 表示无限重试，默认为 10 次
func WithReconnectMaxRetries(maxRetries int) Option {
	return func(c *tunnelConfig) {
		c.nodeOpts = append(c.nodeOpts, node.WithReconnectMaxRetries(maxRetries))
	}
}

// WithReconnectBackoff 设置重连退避参数
// initialDelay: 初始延迟（默认 1 秒）
// maxDelay: 最大延迟（默认 5 分钟）
// multiplier: 退避乘数（默认 2.0）
func WithReconnectBackoff(initialDelay, maxDelay time.Duration, multiplier float64) Option {
	return func(c *tunnelConfig) {
		c.nodeOpts = append(c.nodeOpts, node.WithReconnectBackoff(initialDelay, maxDelay, multiplier))
	}
}
