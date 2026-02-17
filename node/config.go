package node

import (
	"errors"
	"net"
	"time"

	"github.com/shinyes/tenet/crypto"
	"github.com/shinyes/tenet/log"
)

// Config 节点配置
type Config struct {
	// 网络密码：用于校验节点是否属于同一网络（必填）
	NetworkPassword string

	// UDP 监听端口，0 表示自动分配
	ListenPort int

	// 连接超时
	DialTimeout time.Duration

	// 心跳发送间隔
	HeartbeatInterval time.Duration

	// 心跳超时（超过该时间未收到心跳，视为断开）
	HeartbeatTimeout time.Duration

	// 是否启用 NAT 打洞
	EnableHolePunch bool

	// 是否启用中继能力
	EnableRelay bool

	// 预置中继节点地址（host:port）
	RelayNodes []string

	// 最大连接数
	MaxPeers int

	// 日志器（默认 Nop）
	Logger log.Logger

	// 是否启用中继认证
	EnableRelayAuth bool

	// 中继认证令牌有效期
	RelayAuthTTL time.Duration

	// 节点身份
	Identity *crypto.Identity

	// KCP 配置（nil 表示使用默认配置）
	KCPConfig *KCPConfig

	// 是否启用自动重连
	EnableReconnect bool

	// 重连配置（nil 表示使用默认配置）
	ReconnectConfig *ReconnectConfig

	// 连续解密失败阈值；达到后会触发断线重连，避免会话永久失步
	MaxConsecutiveDecryptFailures int

	FastRehandshakeBaseBackoff       time.Duration
	FastRehandshakeMaxBackoff        time.Duration
	FastRehandshakeWindow            time.Duration
	FastRehandshakeMaxAttemptsWindow int
	FastRehandshakeFailThreshold     int
	FastRehandshakePendingTTL        time.Duration

	// 本地订阅的频道名称列表
	Channels []string
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

		KCPConfig:                        nil,
		EnableReconnect:                  true,
		ReconnectConfig:                  nil,
		MaxConsecutiveDecryptFailures:    16,
		FastRehandshakeBaseBackoff:       500 * time.Millisecond,
		FastRehandshakeMaxBackoff:        8 * time.Second,
		FastRehandshakeWindow:            30 * time.Second,
		FastRehandshakeMaxAttemptsWindow: 6,
		FastRehandshakeFailThreshold:     3,
		FastRehandshakePendingTTL:        30 * time.Second,
	}
}

// Validate 校验配置参数
func (c *Config) Validate() error {
	var errs []error

	// 网络密码必填
	if c.NetworkPassword == "" {
		errs = append(errs, errors.New("必须配置网络密码 (NetworkPassword)"))
	}

	// 端口范围
	if c.ListenPort < 0 || c.ListenPort > 65535 {
		errs = append(errs, errors.New("ListenPort must be between 0 and 65535"))
	}

	// 超时配置
	if c.DialTimeout <= 0 {
		errs = append(errs, errors.New("DialTimeout must be positive"))
	}
	if c.HeartbeatInterval <= 0 {
		errs = append(errs, errors.New("HeartbeatInterval must be positive"))
	}
	if c.HeartbeatTimeout <= 0 {
		errs = append(errs, errors.New("HeartbeatTimeout must be positive"))
	}
	if c.HeartbeatTimeout <= c.HeartbeatInterval {
		errs = append(errs, errors.New("HeartbeatTimeout must be greater than HeartbeatInterval"))
	}
	if c.HeartbeatTimeout < 2*c.HeartbeatInterval {
		errs = append(errs, errors.New("HeartbeatTimeout should be at least 2x HeartbeatInterval for reliability"))
	}

	// 连接数
	if c.MaxPeers < 1 {
		errs = append(errs, errors.New("MaxPeers must be at least 1"))
	}

	// 中继认证 TTL
	if c.EnableRelayAuth && c.RelayAuthTTL <= 0 {
		errs = append(errs, errors.New("RelayAuthTTL must be positive when EnableRelayAuth is true"))
	}
	if c.MaxConsecutiveDecryptFailures <= 0 {
		errs = append(errs, errors.New("MaxConsecutiveDecryptFailures must be positive"))
	}
	if c.FastRehandshakeBaseBackoff <= 0 {
		errs = append(errs, errors.New("FastRehandshakeBaseBackoff must be positive"))
	}
	if c.FastRehandshakeMaxBackoff <= 0 {
		errs = append(errs, errors.New("FastRehandshakeMaxBackoff must be positive"))
	}
	if c.FastRehandshakeMaxBackoff < c.FastRehandshakeBaseBackoff {
		errs = append(errs, errors.New("FastRehandshakeMaxBackoff must be >= FastRehandshakeBaseBackoff"))
	}
	if c.FastRehandshakeWindow <= 0 {
		errs = append(errs, errors.New("FastRehandshakeWindow must be positive"))
	}
	if c.FastRehandshakeMaxAttemptsWindow <= 0 {
		errs = append(errs, errors.New("FastRehandshakeMaxAttemptsWindow must be positive"))
	}
	if c.FastRehandshakeFailThreshold <= 0 {
		errs = append(errs, errors.New("FastRehandshakeFailThreshold must be positive"))
	}
	if c.FastRehandshakePendingTTL <= 0 {
		errs = append(errs, errors.New("FastRehandshakePendingTTL must be positive"))
	}
	if c.ReconnectConfig != nil {
		rc := c.ReconnectConfig
		if rc.MaxRetries < 0 {
			errs = append(errs, errors.New("ReconnectConfig.MaxRetries must be >= 0"))
		}
		if rc.InitialDelay <= 0 {
			errs = append(errs, errors.New("ReconnectConfig.InitialDelay must be positive"))
		}
		if rc.MaxDelay <= 0 {
			errs = append(errs, errors.New("ReconnectConfig.MaxDelay must be positive"))
		}
		if rc.InitialDelay > 0 && rc.MaxDelay > 0 && rc.MaxDelay < rc.InitialDelay {
			errs = append(errs, errors.New("ReconnectConfig.MaxDelay must be >= ReconnectConfig.InitialDelay"))
		}
		if rc.BackoffMultiplier < 1 {
			errs = append(errs, errors.New("ReconnectConfig.BackoffMultiplier must be >= 1"))
		}
		if rc.JitterFactor < 0 || rc.JitterFactor > 1 {
			errs = append(errs, errors.New("ReconnectConfig.JitterFactor must be in [0,1]"))
		}
		if rc.ReconnectTimeout <= 0 {
			errs = append(errs, errors.New("ReconnectConfig.ReconnectTimeout must be positive"))
		}
	}

	// 中继地址格式
	for _, addr := range c.RelayNodes {
		if _, err := net.ResolveUDPAddr("udp", addr); err != nil {
			errs = append(errs, errors.New("invalid relay address: "+addr))
		}
	}

	// 日志器兜底
	if c.Logger == nil {
		c.Logger = log.Nop()
	}

	if len(errs) > 0 {
		return errors.Join(errs...)
	}
	return nil
}

// Option 配置项函数
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

// WithEnableHolePunch 设置是否启用 NAT 打洞
func WithEnableHolePunch(enable bool) Option {
	return func(c *Config) {
		c.EnableHolePunch = enable
	}
}

// WithEnableRelay 设置是否启用中继
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

// WithLogger 设置日志器
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

// WithMaxConsecutiveDecryptFailures 设置连续解密失败阈值
func WithMaxConsecutiveDecryptFailures(max int) Option {
	return func(c *Config) {
		c.MaxConsecutiveDecryptFailures = max
	}
}

// WithFastRehandshakeBackoff sets fast re-handshake backoff parameters.
func WithFastRehandshakeBackoff(baseDelay, maxDelay time.Duration) Option {
	return func(c *Config) {
		c.FastRehandshakeBaseBackoff = baseDelay
		c.FastRehandshakeMaxBackoff = maxDelay
	}
}

// WithFastRehandshakeWindow sets fast re-handshake attempt window and max attempts.
func WithFastRehandshakeWindow(window time.Duration, maxAttempts int) Option {
	return func(c *Config) {
		c.FastRehandshakeWindow = window
		c.FastRehandshakeMaxAttemptsWindow = maxAttempts
	}
}

// WithFastRehandshakeFailThreshold sets the failure threshold before reconnect fallback.
func WithFastRehandshakeFailThreshold(threshold int) Option {
	return func(c *Config) {
		c.FastRehandshakeFailThreshold = threshold
	}
}

// WithFastRehandshakePendingTTL sets pending handshake TTL used by fast re-handshake.
func WithFastRehandshakePendingTTL(ttl time.Duration) Option {
	return func(c *Config) {
		c.FastRehandshakePendingTTL = ttl
	}
}

// WithChannelID 添加频道（支持多次调用）
func WithChannelID(channelName string) Option {
	return func(c *Config) {
		for _, ch := range c.Channels {
			if ch == channelName {
				return
			}
		}
		c.Channels = append(c.Channels, channelName)
	}
}
