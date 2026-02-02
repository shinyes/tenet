package node

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

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

// identityData 用于 JSON 序列化的中间结构
type identityData struct {
	PrivateKey string `json:"private_key"` // Ed25519 私钥（hex）
}

// SaveIdentity 将身份保存到文件
// 文件格式为 JSON，包含 Ed25519 私钥
// 其他密钥可以从私钥派生
func (i *Identity) SaveIdentity(path string) error {
	// 确保目录存在
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return fmt.Errorf("创建目录失败: %w", err)
	}

	data := identityData{
		PrivateKey: hex.EncodeToString(i.PrivateKey),
	}

	jsonData, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return fmt.Errorf("序列化身份失败: %w", err)
	}

	// 使用安全的文件权限（仅所有者可读写）
	if err := os.WriteFile(path, jsonData, 0600); err != nil {
		return fmt.Errorf("写入身份文件失败: %w", err)
	}

	return nil
}

// LoadIdentity 从文件加载身份
// 从存储的 Ed25519 私钥派生所有其他密钥
func LoadIdentity(path string) (*Identity, error) {
	jsonData, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("读取身份文件失败: %w", err)
	}

	var data identityData
	if err := json.Unmarshal(jsonData, &data); err != nil {
		return nil, fmt.Errorf("解析身份文件失败: %w", err)
	}

	privKeyBytes, err := hex.DecodeString(data.PrivateKey)
	if err != nil {
		return nil, fmt.Errorf("解码私钥失败: %w", err)
	}

	if len(privKeyBytes) != ed25519.PrivateKeySize {
		return nil, fmt.Errorf("无效的私钥长度: %d", len(privKeyBytes))
	}

	return IdentityFromPrivateKey(ed25519.PrivateKey(privKeyBytes))
}

// LoadOrCreateIdentity 加载或创建身份
// 如果文件存在则加载，否则创建新身份并保存
func LoadOrCreateIdentity(path string) (*Identity, error) {
	// 尝试加载已有身份
	if _, err := os.Stat(path); err == nil {
		id, err := LoadIdentity(path)
		if err == nil {
			return id, nil
		}
		// 加载失败，创建新的
	}

	// 创建新身份
	id, err := NewIdentity()
	if err != nil {
		return nil, err
	}

	// 保存到文件
	if err := id.SaveIdentity(path); err != nil {
		return nil, err
	}

	return id, nil
}

// IdentityExists 检查身份文件是否存在
func IdentityExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// ExportPublicKey 导出公钥（用于分享给其他节点）
func (i *Identity) ExportPublicKey() string {
	return hex.EncodeToString(i.NoisePublicKey[:])
}

// GetFingerprint 获取身份指纹（用于显示和验证）
func (i *Identity) GetFingerprint() string {
	hash := sha256.Sum256(i.NoisePublicKey[:])
	// 返回前 8 字节的十六进制
	return hex.EncodeToString(hash[:8])
}
