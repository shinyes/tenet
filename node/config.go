package node

import (
	"errors"
	"net"
	"time"

	"github.com/cykyes/tenet/crypto" // Added crypto import
	"github.com/cykyes/tenet/log"
)

// Config 节点配置
type Config struct {
	// 网络密码，用于验证节点是否属于同一网络
	NetworkPassword string

	// UDP 监听端口，0表示自动分配
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

	// 预设中继节点地址（host:port）
	RelayNodes []string

	// 最大连接数
	MaxPeers int

	// 日志记录器，默认静默（NopLogger）
	Logger log.Logger

	// 是否启用中继认证
	EnableRelayAuth bool

	// 中继认证令牌有效期
	RelayAuthTTL time.Duration

	// 身份信息
	Identity *crypto.Identity

	// KCP 配置（nil 则使用默认平衡模式）
	KCPConfig *KCPConfig

	// 是否启用自动重连
	EnableReconnect bool

	// 重连配置（nil 则使用默认配置）
	ReconnectConfig *ReconnectConfig
}

// DefaultConfig 返回默认配置
func DefaultConfig() *Config {
	return &Config{
		NetworkPassword: "",
		ListenPort:      0,

		DialTimeout:       10 * time.Second,
		HeartbeatInterval: 5 * time.Second,
		HeartbeatTimeout:  30 * time.Second,
		EnableHolePunch:   true,
		EnableRelay:       true,
		RelayNodes:        []string{},
		MaxPeers:          50,
		Logger:            log.Nop(),
		EnableRelayAuth:   true,
		RelayAuthTTL:      5 * time.Minute,

		KCPConfig:       nil,  // 使用默认平衡模式
		EnableReconnect: true, // 默认启用自动重连
		ReconnectConfig: nil,  // 使用默认重连配置
	}
}

// Validate 验证配置参数的有效性
func (c *Config) Validate() error {
	var errs []error

	// 检查网络密码（必须配置）
	if c.NetworkPassword == "" {
		errs = append(errs, errors.New("必须配置网络密码 (NetworkPassword)"))
	}

	// 检查端口范围
	if c.ListenPort < 0 || c.ListenPort > 65535 {
		errs = append(errs, errors.New("ListenPort must be between 0 and 65535"))
	}

	// 检查超时配置
	if c.DialTimeout <= 0 {
		errs = append(errs, errors.New("DialTimeout must be positive"))
	}

	if c.HeartbeatInterval <= 0 {
		errs = append(errs, errors.New("HeartbeatInterval must be positive"))
	}

	if c.HeartbeatTimeout <= 0 {
		errs = append(errs, errors.New("HeartbeatTimeout must be positive"))
	}

	// 心跳超时应该大于心跳间隔
	if c.HeartbeatTimeout <= c.HeartbeatInterval {
		errs = append(errs, errors.New("HeartbeatTimeout must be greater than HeartbeatInterval"))
	}

	// 心跳超时应至少为心跳间隔的 2 倍
	if c.HeartbeatTimeout < 2*c.HeartbeatInterval {
		errs = append(errs, errors.New("HeartbeatTimeout should be at least 2x HeartbeatInterval for reliability"))
	}

	// 检查最大连接数
	if c.MaxPeers < 1 {
		errs = append(errs, errors.New("MaxPeers must be at least 1"))
	}

	// 检查中继认证 TTL
	if c.EnableRelayAuth && c.RelayAuthTTL <= 0 {
		errs = append(errs, errors.New("RelayAuthTTL must be positive when EnableRelayAuth is true"))
	}

	// 检查中继节点地址格式
	for _, addr := range c.RelayNodes {
		if _, err := net.ResolveUDPAddr("udp", addr); err != nil {
			errs = append(errs, errors.New("invalid relay address: "+addr))
		}
	}

	// 检查日志器
	if c.Logger == nil {
		c.Logger = log.Nop()
	}

	if len(errs) > 0 {
		return errors.Join(errs...)
	}
	return nil
}

// Option 配置选项函数
type Option func(*Config)

// WithNetworkPassword 设置网络密码
func WithNetworkPassword(password string) Option {
	return func(c *Config) {
		c.NetworkPassword = password
	}
}

// WithListenPort 设置 UDP 监听端口
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

// WithRelayNodes 设置中继节点地址列表
func WithRelayNodes(addrs []string) Option {
	return func(c *Config) {
		c.RelayNodes = addrs
	}
}

// WithLogger 设置日志记录器
func WithLogger(logger log.Logger) Option {
	return func(c *Config) {
		if logger != nil {
			c.Logger = logger
		}
	}
}

// WithEnableRelayAuth 设置是否启用中继认证
func WithEnableRelayAuth(enable bool) Option {
	return func(c *Config) {
		c.EnableRelayAuth = enable
	}
}

// WithRelayAuthTTL 设置中继认证令牌有效期
func WithRelayAuthTTL(ttl time.Duration) Option {
	return func(c *Config) {
		c.RelayAuthTTL = ttl
	}
}

// WithIdentity 设置节点身份
func WithIdentity(identity *crypto.Identity) Option {
	return func(c *Config) {
		c.Identity = identity
	}
}

// WithKCPConfig 设置 KCP 配置
func WithKCPConfig(cfg *KCPConfig) Option {
	return func(c *Config) {
		c.KCPConfig = cfg
	}
}

// WithEnableReconnect 设置是否启用自动重连
func WithEnableReconnect(enable bool) Option {
	return func(c *Config) {
		c.EnableReconnect = enable
	}
}

// WithReconnectConfig 设置重连配置
func WithReconnectConfig(cfg *ReconnectConfig) Option {
	return func(c *Config) {
		c.ReconnectConfig = cfg
	}
}

// WithReconnectMaxRetries 设置最大重连次数（0 表示无限重试）
func WithReconnectMaxRetries(maxRetries int) Option {
	return func(c *Config) {
		if c.ReconnectConfig == nil {
			c.ReconnectConfig = DefaultReconnectConfig()
		}
		c.ReconnectConfig.MaxRetries = maxRetries
	}
}

// WithReconnectBackoff 设置重连退避参数
func WithReconnectBackoff(initialDelay, maxDelay time.Duration, multiplier float64) Option {
	return func(c *Config) {
		if c.ReconnectConfig == nil {
			c.ReconnectConfig = DefaultReconnectConfig()
		}
		c.ReconnectConfig.InitialDelay = initialDelay
		c.ReconnectConfig.MaxDelay = maxDelay
		c.ReconnectConfig.BackoffMultiplier = multiplier
	}
}
