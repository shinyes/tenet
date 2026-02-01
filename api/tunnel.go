package api

import (
	"fmt"
	"sync"
	"time"

	"github.com/cykyes/tenet/node"
)

// Tunnel 是用户使用的主要接口
type Tunnel struct {
	node   *node.Node
	mu     sync.RWMutex
	closed bool
}

// NewTunnel 创建隧道实例
func NewTunnel(opts ...Option) (*Tunnel, error) {
	cfg := &tunnelConfig{
		nodeOpts: make([]node.Option, 0),
	}

	for _, opt := range opts {
		opt(cfg)
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

	return t.node.Start()
}

// Stop 停止服务
func (t *Tunnel) Stop() error {
	t.mu.Lock()
	defer t.mu.Unlock()

	t.closed = true
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

// --- 配置选项 ---

type tunnelConfig struct {
	nodeOpts []node.Option
}

// Option 配置选项
type Option func(*tunnelConfig)

// WithPassword 设置网络密码
func WithPassword(password string) Option {
	return func(c *tunnelConfig) {
		c.nodeOpts = append(c.nodeOpts, node.WithNetworkPassword(password))
	}
}

// WithListenPort 设置监听端口
func WithListenPort(port int) Option {
	return func(c *tunnelConfig) {
		c.nodeOpts = append(c.nodeOpts, node.WithListenPort(port))
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

// WithDialTimeout 设置连接超时
func WithDialTimeout(timeout time.Duration) Option {
	return func(c *tunnelConfig) {
		c.nodeOpts = append(c.nodeOpts, node.WithDialTimeout(timeout))
	}
}
