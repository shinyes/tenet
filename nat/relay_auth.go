package nat

import (
	"crypto/ed25519"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"time"
)

// RelayAuthConfig 中继认证配置
type RelayAuthConfig struct {
	// 是否启用中继认证
	Enabled bool
	// 认证令牌有效期（默认5分钟）
	TokenTTL time.Duration
	// 网络密码（用于生成共享密钥）
	NetworkPassword string
}

// DefaultRelayAuthConfig 返回默认的中继认证配置
func DefaultRelayAuthConfig() *RelayAuthConfig {
	return &RelayAuthConfig{
		Enabled:         true,
		TokenTTL:        5 * time.Minute,
		NetworkPassword: "",
	}
}

// RelayAuthToken 中继认证令牌
type RelayAuthToken struct {
	// 源节点ID（16字节）
	SrcID [16]byte
	// 目标节点ID（16字节）
	DstID [16]byte
	// 时间戳（Unix秒）
	Timestamp int64
	// 签名（Ed25519签名，64字节）
	Signature []byte
}

// RelayAuthenticator 中继认证器
type RelayAuthenticator struct {
	config *RelayAuthConfig
	// 派生的共享密钥（用于HMAC验证，当无法验证Ed25519签名时）
	sharedKey []byte
}

// NewRelayAuthenticator 创建中继认证器
func NewRelayAuthenticator(config *RelayAuthConfig) *RelayAuthenticator {
	if config == nil {
		config = DefaultRelayAuthConfig()
	}

	// 从网络密码派生共享密钥
	var sharedKey []byte
	if config.NetworkPassword != "" {
		h := sha256.Sum256([]byte("tenet-relay-auth:" + config.NetworkPassword))
		sharedKey = h[:]
	}

	return &RelayAuthenticator{
		config:    config,
		sharedKey: sharedKey,
	}
}

// GenerateToken 生成中继认证令牌
// srcID: 源节点ID
// dstID: 目标节点ID
// privateKey: 源节点的Ed25519私钥（用于签名）
func (a *RelayAuthenticator) GenerateToken(srcID, dstID [16]byte, privateKey ed25519.PrivateKey) *RelayAuthToken {
	timestamp := time.Now().Unix()

	// 构造待签名消息
	message := a.buildMessage(srcID, dstID, timestamp)

	// 使用Ed25519签名
	signature := ed25519.Sign(privateKey, message)

	return &RelayAuthToken{
		SrcID:     srcID,
		DstID:     dstID,
		Timestamp: timestamp,
		Signature: signature,
	}
}

// GenerateTokenWithHMAC 使用HMAC生成令牌（当没有Ed25519密钥时）
func (a *RelayAuthenticator) GenerateTokenWithHMAC(srcID, dstID [16]byte) *RelayAuthToken {
	if len(a.sharedKey) == 0 {
		return nil
	}

	timestamp := time.Now().Unix()
	message := a.buildMessage(srcID, dstID, timestamp)

	// 使用HMAC-SHA256
	mac := hmac.New(sha256.New, a.sharedKey)
	mac.Write(message)
	signature := mac.Sum(nil)

	return &RelayAuthToken{
		SrcID:     srcID,
		DstID:     dstID,
		Timestamp: timestamp,
		Signature: signature,
	}
}

// VerifyToken 验证中继认证令牌
// publicKey: 源节点的Ed25519公钥（可选，如果为nil则使用HMAC验证）
func (a *RelayAuthenticator) VerifyToken(token *RelayAuthToken, publicKey ed25519.PublicKey) error {
	if !a.config.Enabled {
		return nil // 未启用认证，直接通过
	}

	// 检查时间戳
	now := time.Now().Unix()
	tokenAge := now - token.Timestamp
	if tokenAge < 0 {
		tokenAge = -tokenAge
	}
	if tokenAge > int64(a.config.TokenTTL.Seconds()) {
		return fmt.Errorf("token expired: age=%ds, ttl=%ds", tokenAge, int64(a.config.TokenTTL.Seconds()))
	}

	// 构造待验证消息
	message := a.buildMessage(token.SrcID, token.DstID, token.Timestamp)

	// 优先使用Ed25519验证
	if publicKey != nil && len(token.Signature) == ed25519.SignatureSize {
		if ed25519.Verify(publicKey, message, token.Signature) {
			return nil
		}
		return fmt.Errorf("invalid Ed25519 signature")
	}

	// 回退到HMAC验证
	if len(a.sharedKey) > 0 && len(token.Signature) == sha256.Size {
		mac := hmac.New(sha256.New, a.sharedKey)
		mac.Write(message)
		expectedMAC := mac.Sum(nil)
		if hmac.Equal(token.Signature, expectedMAC) {
			return nil
		}
		return fmt.Errorf("invalid HMAC signature")
	}

	return fmt.Errorf("no valid verification method available")
}

// buildMessage 构造待签名/验证的消息
func (a *RelayAuthenticator) buildMessage(srcID, dstID [16]byte, timestamp int64) []byte {
	message := make([]byte, 16+16+8)
	copy(message[0:16], srcID[:])
	copy(message[16:32], dstID[:])
	binary.BigEndian.PutUint64(message[32:40], uint64(timestamp))
	return message
}

// EncodeToken 编码令牌为字节数组
func (t *RelayAuthToken) Encode() []byte {
	// 格式: [SrcID(16)] [DstID(16)] [Timestamp(8)] [SigLen(1)] [Signature(N)]
	sigLen := len(t.Signature)
	data := make([]byte, 16+16+8+1+sigLen)
	copy(data[0:16], t.SrcID[:])
	copy(data[16:32], t.DstID[:])
	binary.BigEndian.PutUint64(data[32:40], uint64(t.Timestamp))
	data[40] = byte(sigLen)
	copy(data[41:], t.Signature)
	return data
}

// DecodeToken 从字节数组解码令牌
func DecodeToken(data []byte) (*RelayAuthToken, error) {
	if len(data) < 41 {
		return nil, fmt.Errorf("token too short")
	}

	token := &RelayAuthToken{}
	copy(token.SrcID[:], data[0:16])
	copy(token.DstID[:], data[16:32])
	token.Timestamp = int64(binary.BigEndian.Uint64(data[32:40]))

	sigLen := int(data[40])
	if len(data) < 41+sigLen {
		return nil, fmt.Errorf("token signature truncated")
	}
	token.Signature = make([]byte, sigLen)
	copy(token.Signature, data[41:41+sigLen])

	return token, nil
}

// IsEnabled 返回认证是否启用
func (a *RelayAuthenticator) IsEnabled() bool {
	return a.config.Enabled
}
