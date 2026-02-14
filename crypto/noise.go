package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/subtle"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/flynn/noise"
)

var ErrSessionClosed = errors.New("session closed")

// NoiseConfig Noise协议配置
var NoiseConfig = noise.Config{
	CipherSuite: noise.NewCipherSuite(noise.DH25519, noise.CipherAESGCM, noise.HashSHA256),
	Pattern:     noise.HandshakeXX, // XX模式：双方都需要验证身份
}

// secureZero 安全地将字节切片清零，防止敏感数据残留在内存中
// 使用 subtle.ConstantTimeCopy 确保操作不会被编译器优化掉
func secureZero(b []byte) {
	zeros := make([]byte, len(b))
	subtle.ConstantTimeCopy(1, b, zeros)
}

// HandshakeState 握手状态
type HandshakeState struct {
	hs          *noise.HandshakeState
	isInitiator bool
	completed   bool
	createdAt   int64 // Unix 时间戳（秒），用于超时清理
	mu          sync.Mutex
}

// NewInitiatorHandshake 创建发起方握手状态
func NewInitiatorHandshake(localPrivateKey, localPublicKey []byte, networkSecret []byte) (*HandshakeState, []byte, error) {
	if len(localPrivateKey) < 32 || len(localPublicKey) < 32 {
		return nil, nil, fmt.Errorf("密钥长度不足: private=%d public=%d", len(localPrivateKey), len(localPublicKey))
	}
	// 使用网络密码作为预共享密钥的一部分
	prologue := append([]byte("p2ptunnel-v1-"), networkSecret...)

	config := NoiseConfig
	config.Initiator = true
	config.Prologue = prologue

	// 设置本地静态密钥
	var staticKey noise.DHKey
	staticKey.Private = make([]byte, 32)
	staticKey.Public = make([]byte, 32)
	copy(staticKey.Private, localPrivateKey[:32])
	copy(staticKey.Public, localPublicKey[:32])
	config.StaticKeypair = staticKey

	hs, err := noise.NewHandshakeState(config)
	if err != nil {
		return nil, nil, fmt.Errorf("创建握手状态失败: %w", err)
	}

	// 生成第一条握手消息
	msg1, _, _, err := hs.WriteMessage(nil, nil)
	if err != nil {
		return nil, nil, fmt.Errorf("生成握手消息1失败: %w", err)
	}

	return &HandshakeState{
		hs:          hs,
		isInitiator: true,
		createdAt:   time.Now().Unix(),
	}, msg1, nil
}

// NewResponderHandshake 创建响应方握手状态
func NewResponderHandshake(localPrivateKey, localPublicKey []byte, networkSecret []byte) (*HandshakeState, error) {
	if len(localPrivateKey) < 32 || len(localPublicKey) < 32 {
		return nil, fmt.Errorf("密钥长度不足: private=%d public=%d", len(localPrivateKey), len(localPublicKey))
	}
	prologue := append([]byte("p2ptunnel-v1-"), networkSecret...)

	config := NoiseConfig
	config.Initiator = false
	config.Prologue = prologue

	var staticKey noise.DHKey
	staticKey.Private = make([]byte, 32)
	staticKey.Public = make([]byte, 32)
	copy(staticKey.Private, localPrivateKey[:32])
	copy(staticKey.Public, localPublicKey[:32])
	config.StaticKeypair = staticKey

	hs, err := noise.NewHandshakeState(config)
	if err != nil {
		return nil, fmt.Errorf("创建握手状态失败: %w", err)
	}

	return &HandshakeState{
		hs:          hs,
		isInitiator: false,
		createdAt:   time.Now().Unix(),
	}, nil
}

// CreatedAt 返回握手状态创建时间（Unix 秒）
func (h *HandshakeState) CreatedAt() int64 {
	return h.createdAt
}

// ProcessMessage 处理握手消息
func (h *HandshakeState) ProcessMessage(msg []byte) ([]byte, *Session, error) {
	h.mu.Lock()
	defer h.mu.Unlock()

	if h.completed {
		return nil, nil, fmt.Errorf("握手已完成")
	}

	// 读取消息
	payload, cs1, cs2, err := h.hs.ReadMessage(nil, msg)
	if err != nil {
		return nil, nil, fmt.Errorf("读取握手消息失败: %w", err)
	}

	// 如果握手未完成，生成响应消息
	if cs1 == nil || cs2 == nil {
		response, cs1New, cs2New, err := h.hs.WriteMessage(nil, payload)
		if err != nil {
			return nil, nil, fmt.Errorf("生成握手响应失败: %w", err)
		}

		// 检查WriteMessage是否完成了握手 (例如Initiator发送Msg3)
		if cs1New != nil && cs2New != nil {
			h.completed = true
			var sendCipher, recvCipher *noise.CipherState
			if h.isInitiator {
				sendCipher = cs1New
				recvCipher = cs2New
			} else {
				sendCipher = cs2New
				recvCipher = cs1New
			}

			session := &Session{
				sendCipher:      sendCipher,
				recvCipher:      recvCipher,
				remoteStaticKey: h.hs.PeerStatic(),
				handshakeHash:   h.hs.ChannelBinding(),
			}
			return response, session, nil
		}

		return response, nil, nil
	}

	// 握手完成
	h.completed = true

	var sendCipher, recvCipher *noise.CipherState
	if h.isInitiator {
		sendCipher = cs1
		recvCipher = cs2
	} else {
		sendCipher = cs2
		recvCipher = cs1
	}

	session := &Session{
		sendCipher:      sendCipher,
		recvCipher:      recvCipher,
		remoteStaticKey: h.hs.PeerStatic(),
		handshakeHash:   h.hs.ChannelBinding(),
	}

	return nil, session, nil
}

// WriteMessage 生成握手消息（用于发起方的后续消息）
func (h *HandshakeState) WriteMessage(payload []byte) ([]byte, *Session, error) {
	h.mu.Lock()
	defer h.mu.Unlock()

	msg, cs1, cs2, err := h.hs.WriteMessage(nil, payload)
	if err != nil {
		return nil, nil, fmt.Errorf("生成握手消息失败: %w", err)
	}

	if cs1 == nil || cs2 == nil {
		return msg, nil, nil
	}

	// 握手完成
	h.completed = true

	var sendCipher, recvCipher *noise.CipherState
	if h.isInitiator {
		sendCipher = cs1
		recvCipher = cs2
	} else {
		sendCipher = cs2
		recvCipher = cs1
	}

	session := &Session{
		sendCipher:      sendCipher,
		recvCipher:      recvCipher,
		remoteStaticKey: h.hs.PeerStatic(),
		handshakeHash:   h.hs.ChannelBinding(),
	}

	return msg, session, nil
}

// Session 加密会话
type Session struct {
	sendCipher      *noise.CipherState
	recvCipher      *noise.CipherState
	remoteStaticKey []byte
	handshakeHash   []byte
	sendNonce       uint64
	recvNonce       uint64
	mu              sync.Mutex
}

// Encrypt 加密数据
func (s *Session) Encrypt(plaintext []byte) ([]byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.sendCipher == nil {
		return nil, ErrSessionClosed
	}

	// Noise CipherState.Encrypt 已经处理了nonce
	ciphertext, err := s.sendCipher.Encrypt(nil, nil, plaintext)
	if err != nil {
		return nil, fmt.Errorf("加密失败: %w", err)
	}

	return ciphertext, nil
}

// Decrypt 解密数据
func (s *Session) Decrypt(ciphertext []byte) ([]byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.recvCipher == nil {
		return nil, ErrSessionClosed
	}

	plaintext, err := s.recvCipher.Decrypt(nil, nil, ciphertext)
	if err != nil {
		return nil, fmt.Errorf("解密失败: %w", err)
	}

	return plaintext, nil
}

// RemotePublicKey 获取对端公钥
func (s *Session) RemotePublicKey() []byte {
	return s.remoteStaticKey
}

// HandshakeHash 获取握手哈希（用于通道绑定）
func (s *Session) HandshakeHash() []byte {
	return s.handshakeHash
}

// Close 安全关闭会话，清除敏感密钥材料
// 调用后会话不可再使用
func (s *Session) Close() {
	s.mu.Lock()
	defer s.mu.Unlock()

	// 清除远程公钥
	if s.remoteStaticKey != nil {
		secureZero(s.remoteStaticKey)
		s.remoteStaticKey = nil
	}

	// 清除握手哈希
	if s.handshakeHash != nil {
		secureZero(s.handshakeHash)
		s.handshakeHash = nil
	}

	// 注意：noise.CipherState 内部的密钥无法直接访问清除
	// 将引用置空以便 GC 回收
	s.sendCipher = nil
	s.recvCipher = nil
}

// AESGCMCipher 备用的纯AES-GCM加密（不使用Noise时）
type AESGCMCipher struct {
	aead cipher.AEAD
}

// NewAESGCMCipher 创建AES-GCM加密器
func NewAESGCMCipher(key []byte) (*AESGCMCipher, error) {
	if len(key) != 32 {
		return nil, fmt.Errorf("密钥长度必须是32字节")
	}

	block, err := aesNewCipher(key)
	if err != nil {
		return nil, err
	}

	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}

	return &AESGCMCipher{aead: aead}, nil
}

// aesNewCipher 创建AES cipher（Go标准库）
func aesNewCipher(key []byte) (cipher.Block, error) {
	return aes.NewCipher(key)
}

// Encrypt 加密
func (c *AESGCMCipher) Encrypt(plaintext []byte) ([]byte, error) {
	nonce := make([]byte, c.aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}

	ciphertext := c.aead.Seal(nonce, nonce, plaintext, nil)
	return ciphertext, nil
}

// Decrypt 解密
func (c *AESGCMCipher) Decrypt(ciphertext []byte) ([]byte, error) {
	if len(ciphertext) < c.aead.NonceSize() {
		return nil, fmt.Errorf("密文太短")
	}

	nonce := ciphertext[:c.aead.NonceSize()]
	ciphertext = ciphertext[c.aead.NonceSize():]

	plaintext, err := c.aead.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return nil, err
	}

	return plaintext, nil
}
