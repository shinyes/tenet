package nat

import (
	"crypto/ed25519"
	"testing"
	"time"
)

func TestDefaultRelayAuthConfig(t *testing.T) {
	cfg := DefaultRelayAuthConfig()
	if cfg == nil {
		t.Fatal("DefaultRelayAuthConfig 返回 nil")
	}
	if !cfg.Enabled {
		t.Error("默认应启用认证")
	}
	if cfg.TokenTTL != 5*time.Minute {
		t.Errorf("TokenTTL 期望 5分钟，实际 %v", cfg.TokenTTL)
	}
	if cfg.NetworkPassword != "" {
		t.Error("默认 NetworkPassword 应为空")
	}
}

func TestNewRelayAuthenticator(t *testing.T) {
	auth := NewRelayAuthenticator(nil)
	if auth == nil {
		t.Fatal("NewRelayAuthenticator(nil) 返回 nil")
	}
	if len(auth.sharedKey) != 0 {
		t.Error("无密码时 sharedKey 应为空")
	}
}

func TestNewRelayAuthenticatorWithPassword(t *testing.T) {
	auth := NewRelayAuthenticator(&RelayAuthConfig{
		Enabled:         true,
		TokenTTL:        time.Minute,
		NetworkPassword: "secret-password",
	})
	if auth == nil {
		t.Fatal("NewRelayAuthenticator 返回 nil")
	}
	if len(auth.sharedKey) != 32 {
		t.Errorf("sharedKey 长度期望 32，实际 %d", len(auth.sharedKey))
	}
}

func TestGenerateTokenEd25519(t *testing.T) {
	_, privKey, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatalf("生成密钥对失败: %v", err)
	}

	auth := NewRelayAuthenticator(DefaultRelayAuthConfig())

	var srcID, dstID [16]byte
	copy(srcID[:], []byte("source1234567890"))
	copy(dstID[:], []byte("target1234567890"))

	token := auth.GenerateToken(srcID, dstID, privKey)
	if token == nil {
		t.Fatal("GenerateToken 返回 nil")
	}
	if token.SrcID != srcID {
		t.Error("SrcID 不匹配")
	}
	if token.DstID != dstID {
		t.Error("DstID 不匹配")
	}
	if len(token.Signature) != ed25519.SignatureSize {
		t.Errorf("签名长度期望 %d，实际 %d", ed25519.SignatureSize, len(token.Signature))
	}
}

func TestVerifyTokenEd25519(t *testing.T) {
	pubKey, privKey, _ := ed25519.GenerateKey(nil)
	auth := NewRelayAuthenticator(DefaultRelayAuthConfig())

	var srcID, dstID [16]byte
	copy(srcID[:], []byte("source"))
	copy(dstID[:], []byte("target"))

	token := auth.GenerateToken(srcID, dstID, privKey)
	err := auth.VerifyToken(token, pubKey)
	if err != nil {
		t.Errorf("VerifyToken 失败: %v", err)
	}
}

func TestVerifyTokenInvalidSignature(t *testing.T) {
	pubKey, privKey, _ := ed25519.GenerateKey(nil)
	auth := NewRelayAuthenticator(DefaultRelayAuthConfig())

	var srcID, dstID [16]byte
	token := auth.GenerateToken(srcID, dstID, privKey)

	// 篡改签名
	token.Signature[0] ^= 0xFF

	err := auth.VerifyToken(token, pubKey)
	if err == nil {
		t.Error("篡改签名后验证应该失败")
	}
}

func TestGenerateTokenWithHMAC(t *testing.T) {
	auth := NewRelayAuthenticator(&RelayAuthConfig{
		Enabled:         true,
		TokenTTL:        time.Minute,
		NetworkPassword: "shared-secret",
	})

	var srcID, dstID [16]byte
	token := auth.GenerateTokenWithHMAC(srcID, dstID)
	if token == nil {
		t.Fatal("GenerateTokenWithHMAC 返回 nil")
	}
	if len(token.Signature) != 32 { // SHA256
		t.Errorf("HMAC 签名长度期望 32，实际 %d", len(token.Signature))
	}
}

func TestVerifyTokenHMAC(t *testing.T) {
	auth := NewRelayAuthenticator(&RelayAuthConfig{
		Enabled:         true,
		TokenTTL:        time.Minute,
		NetworkPassword: "shared-secret",
	})

	var srcID, dstID [16]byte
	token := auth.GenerateTokenWithHMAC(srcID, dstID)

	// 无公钥时使用 HMAC 验证
	err := auth.VerifyToken(token, nil)
	if err != nil {
		t.Errorf("HMAC 验证失败: %v", err)
	}
}

func TestGenerateTokenWithHMACNoPassword(t *testing.T) {
	auth := NewRelayAuthenticator(DefaultRelayAuthConfig())

	var srcID, dstID [16]byte
	token := auth.GenerateTokenWithHMAC(srcID, dstID)
	if token != nil {
		t.Error("无密码时 GenerateTokenWithHMAC 应返回 nil")
	}
}

func TestTokenEncodeDecode(t *testing.T) {
	var srcID, dstID [16]byte
	copy(srcID[:], "source_node_id!!")
	copy(dstID[:], "target_node_id!!")

	original := &RelayAuthToken{
		SrcID:     srcID,
		DstID:     dstID,
		Timestamp: time.Now().Unix(),
		Signature: []byte("test-signature-data"),
	}

	encoded := original.Encode()
	decoded, err := DecodeToken(encoded)
	if err != nil {
		t.Fatalf("DecodeToken 失败: %v", err)
	}

	if decoded.SrcID != srcID {
		t.Error("解码后 SrcID 不匹配")
	}
	if decoded.DstID != dstID {
		t.Error("解码后 DstID 不匹配")
	}
	if decoded.Timestamp != original.Timestamp {
		t.Error("解码后 Timestamp 不匹配")
	}
	if string(decoded.Signature) != string(original.Signature) {
		t.Error("解码后 Signature 不匹配")
	}
}

func TestDecodeTokenTooShort(t *testing.T) {
	_, err := DecodeToken([]byte("short"))
	if err == nil {
		t.Error("解码过短数据应该失败")
	}
}

func TestDecodeTokenTruncatedSignature(t *testing.T) {
	data := make([]byte, 42)
	data[40] = 100 // 声明签名长度为 100，但实际只有 1 字节

	_, err := DecodeToken(data)
	if err == nil {
		t.Error("解码截断的签名应该失败")
	}
}

func TestExpiredToken(t *testing.T) {
	// 由于时间戳使用秒级精度，我们通过篡改令牌的时间戳来测试过期逻辑
	auth := NewRelayAuthenticator(&RelayAuthConfig{
		Enabled:  true,
		TokenTTL: 5 * time.Second,
	})

	pubKey, privKey, _ := ed25519.GenerateKey(nil)
	var srcID, dstID [16]byte
	token := auth.GenerateToken(srcID, dstID, privKey)

	// 将时间戳改为 10 秒前
	token.Timestamp = time.Now().Unix() - 10

	err := auth.VerifyToken(token, pubKey)
	if err == nil {
		t.Error("过期令牌应该验证失败")
	}
}

func TestVerifyTokenDisabled(t *testing.T) {
	auth := NewRelayAuthenticator(&RelayAuthConfig{
		Enabled: false,
	})

	// 任何令牌都应该通过
	err := auth.VerifyToken(&RelayAuthToken{}, nil)
	if err != nil {
		t.Errorf("禁用认证时应该直接通过: %v", err)
	}
}

func TestIsEnabled(t *testing.T) {
	authEnabled := NewRelayAuthenticator(&RelayAuthConfig{Enabled: true})
	authDisabled := NewRelayAuthenticator(&RelayAuthConfig{Enabled: false})

	if !authEnabled.IsEnabled() {
		t.Error("启用的认证器 IsEnabled 应返回 true")
	}
	if authDisabled.IsEnabled() {
		t.Error("禁用的认证器 IsEnabled 应返回 false")
	}
}
