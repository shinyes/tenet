package node

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"

	"golang.org/x/crypto/curve25519"
)

// NodeID 节点唯一标识符（基于公钥的SHA256前16字节）
type NodeID [16]byte

// String 返回NodeID的十六进制表示
func (id NodeID) String() string {
	return hex.EncodeToString(id[:])
}

// ShortString 返回NodeID的短表示（前8个字符）
func (id NodeID) ShortString() string {
	return hex.EncodeToString(id[:4])
}

// Identity 节点身份，包含密钥对
type Identity struct {
	// Ed25519密钥对用于身份签名
	PrivateKey ed25519.PrivateKey
	PublicKey  ed25519.PublicKey

	// Curve25519密钥用于Noise协议密钥交换
	NoisePrivateKey [32]byte
	NoisePublicKey  [32]byte

	// 节点ID
	ID NodeID
}

// NewIdentity 生成新的节点身份
func NewIdentity() (*Identity, error) {
	// 生成Ed25519密钥对
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("生成Ed25519密钥失败: %w", err)
	}

	// 从Ed25519私钥派生Curve25519密钥（用于Noise）
	// Ed25519私钥的前32字节可以用作Curve25519私钥的种子
	var noisePriv [32]byte
	copy(noisePriv[:], priv[:32])

	// 计算Curve25519公钥
	var noisePub [32]byte
	curve25519.ScalarBaseMult(&noisePub, &noisePriv)

	// 计算节点ID（Noise公钥的SHA256前16字节）
	// 这样握手完成后可以直接从RemoteStaticKey推导出ID
	idHash := sha256.Sum256(noisePub[:])
	var id NodeID
	copy(id[:], idHash[:16])

	return &Identity{
		PrivateKey:      priv,
		PublicKey:       pub,
		NoisePrivateKey: noisePriv,
		NoisePublicKey:  noisePub,
		ID:              id,
	}, nil
}

// IdentityFromPrivateKey 从私钥恢复身份
func IdentityFromPrivateKey(privKey ed25519.PrivateKey) (*Identity, error) {
	if len(privKey) != ed25519.PrivateKeySize {
		return nil, fmt.Errorf("无效的私钥长度")
	}

	pub := privKey.Public().(ed25519.PublicKey)

	var noisePriv [32]byte
	copy(noisePriv[:], privKey[:32])

	var noisePub [32]byte
	curve25519.ScalarBaseMult(&noisePub, &noisePriv)

	idHash := sha256.Sum256(noisePub[:])
	var id NodeID
	copy(id[:], idHash[:16])

	return &Identity{
		PrivateKey:      privKey,
		PublicKey:       pub,
		NoisePrivateKey: noisePriv,
		NoisePublicKey:  noisePub,
		ID:              id,
	}, nil
}

// Sign 使用私钥签名消息
func (i *Identity) Sign(message []byte) []byte {
	return ed25519.Sign(i.PrivateKey, message)
}

// Verify 验证签名
func (i *Identity) Verify(message, signature []byte) bool {
	return ed25519.Verify(i.PublicKey, message, signature)
}
