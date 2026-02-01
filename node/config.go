package node

import (
	"time"
)

// Config 节点配置
type Config struct {
	// 网络密码，用于验证节点是否属于同一网络
	NetworkPassword string

	// 本地监听端口，0表示自动分配
	ListenPort int

	// 连接超时
	DialTimeout time.Duration

	// 心跳间隔
	HeartbeatInterval time.Duration

	// 心跳超时（多久没收到心跳视为断开）
	HeartbeatTimeout time.Duration

	// 是否启用NAT打洞
	EnableHolePunch bool

	// 是否允许作为中继节点
	EnableRelay bool

	// 最大连接数
	MaxPeers int
}

// DefaultConfig 返回默认配置
func DefaultConfig() *Config {
	return &Config{
		NetworkPassword:   "",
		ListenPort:        0,
		DialTimeout:       10 * time.Second,
		HeartbeatInterval: 5 * time.Second,
		HeartbeatTimeout:  30 * time.Second,
		EnableHolePunch:   true,
		EnableRelay:       true,
		MaxPeers:          50,
	}
}

// Option 配置选项函数
type Option func(*Config)

// WithNetworkPassword 设置网络密码
func WithNetworkPassword(password string) Option {
	return func(c *Config) {
		c.NetworkPassword = password
	}
}

// WithListenPort 设置监听端口
func WithListenPort(port int) Option {
	return func(c *Config) {
		c.ListenPort = port
	}
}

// WithDialTimeout 设置连接超时
func WithDialTimeout(timeout time.Duration) Option {
	return func(c *Config) {
		c.DialTimeout = timeout
	}
}

// WithHeartbeatInterval 设置心跳间隔
func WithHeartbeatInterval(interval time.Duration) Option {
	return func(c *Config) {
		c.HeartbeatInterval = interval
	}
}

// WithEnableHolePunch 设置是否启用NAT打洞
func WithEnableHolePunch(enable bool) Option {
	return func(c *Config) {
		c.EnableHolePunch = enable
	}
}

// WithEnableRelay 设置是否允许中继
func WithEnableRelay(enable bool) Option {
	return func(c *Config) {
		c.EnableRelay = enable
	}
}

// WithMaxPeers 设置最大连接数
func WithMaxPeers(max int) Option {
	return func(c *Config) {
		c.MaxPeers = max
	}
}
