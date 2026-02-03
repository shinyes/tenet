package crypto

import (
	"os"
	"path/filepath"
	"testing"
)

func TestNewIdentity(t *testing.T) {
	id, err := NewIdentity()
	if err != nil {
		t.Fatalf("NewIdentity 失败: %v", err)
	}

	if id == nil {
		t.Fatal("NewIdentity 返回 nil")
	}

	// 验证密钥长度
	if len(id.PrivateKey) != 64 {
		t.Errorf("PrivateKey 长度期望 64，实际 %d", len(id.PrivateKey))
	}
	if len(id.PublicKey) != 32 {
		t.Errorf("PublicKey 长度期望 32，实际 %d", len(id.PublicKey))
	}
	if len(id.NoisePrivateKey) != 32 {
		t.Errorf("NoisePrivateKey 长度期望 32，实际 %d", len(id.NoisePrivateKey))
	}
	if len(id.NoisePublicKey) != 32 {
		t.Errorf("NoisePublicKey 长度期望 32，实际 %d", len(id.NoisePublicKey))
	}

	// 验证 ID
	if id.ID.String() == "" {
		t.Error("ID.String() 返回空字符串")
	}
	if len(id.ID.String()) != 32 { // 16 bytes = 32 hex chars
		t.Errorf("ID.String() 长度期望 32，实际 %d", len(id.ID.String()))
	}
}

func TestNodeIDString(t *testing.T) {
	var id NodeID
	for i := range id {
		id[i] = byte(i)
	}

	str := id.String()
	if len(str) != 32 {
		t.Errorf("String() 长度期望 32，实际 %d", len(str))
	}

	short := id.ShortString()
	if len(short) != 8 {
		t.Errorf("ShortString() 长度期望 8，实际 %d", len(short))
	}
}

func TestIdentitySignVerify(t *testing.T) {
	id, err := NewIdentity()
	if err != nil {
		t.Fatalf("NewIdentity 失败: %v", err)
	}

	message := []byte("Hello, World!")
	signature := id.Sign(message)

	if len(signature) == 0 {
		t.Error("签名为空")
	}

	if !id.Verify(message, signature) {
		t.Error("签名验证失败")
	}

	// 验证篡改后的消息
	tampered := []byte("Hello, World?")
	if id.Verify(tampered, signature) {
		t.Error("篡改消息的签名验证应该失败")
	}
}

func TestIdentitySaveLoad(t *testing.T) {
	// 创建临时目录
	tmpDir := t.TempDir()
	idPath := filepath.Join(tmpDir, "test_identity.json")

	// 创建并保存身份
	id1, err := NewIdentity()
	if err != nil {
		t.Fatalf("NewIdentity 失败: %v", err)
	}

	if err := id1.SaveIdentity(idPath); err != nil {
		t.Fatalf("SaveIdentity 失败: %v", err)
	}

	// 验证文件存在
	if !IdentityExists(idPath) {
		t.Error("身份文件不存在")
	}

	// 加载身份
	id2, err := LoadIdentity(idPath)
	if err != nil {
		t.Fatalf("LoadIdentity 失败: %v", err)
	}

	// 验证加载的身份与原始身份一致
	if id1.ID.String() != id2.ID.String() {
		t.Errorf("ID 不匹配: %s != %s", id1.ID.String(), id2.ID.String())
	}
	if id1.ExportPublicKey() != id2.ExportPublicKey() {
		t.Error("公钥不匹配")
	}
}

func TestLoadOrCreateIdentity(t *testing.T) {
	tmpDir := t.TempDir()
	idPath := filepath.Join(tmpDir, "identity.json")

	// 首次调用应创建新身份
	id1, err := LoadOrCreateIdentity(idPath)
	if err != nil {
		t.Fatalf("LoadOrCreateIdentity 失败: %v", err)
	}

	// 第二次调用应加载相同身份
	id2, err := LoadOrCreateIdentity(idPath)
	if err != nil {
		t.Fatalf("第二次 LoadOrCreateIdentity 失败: %v", err)
	}

	if id1.ID.String() != id2.ID.String() {
		t.Errorf("ID 应该相同: %s != %s", id1.ID.String(), id2.ID.String())
	}
}

func TestIdentityFromPrivateKey(t *testing.T) {
	id1, err := NewIdentity()
	if err != nil {
		t.Fatalf("NewIdentity 失败: %v", err)
	}

	id2, err := IdentityFromPrivateKey(id1.PrivateKey)
	if err != nil {
		t.Fatalf("IdentityFromPrivateKey 失败: %v", err)
	}

	if id1.ID.String() != id2.ID.String() {
		t.Errorf("从私钥恢复的 ID 不匹配: %s != %s", id1.ID.String(), id2.ID.String())
	}
}

func TestIdentityExistsNonExistent(t *testing.T) {
	if IdentityExists("/nonexistent/path/identity.json") {
		t.Error("不存在的文件应该返回 false")
	}
}

func TestGetFingerprint(t *testing.T) {
	id, err := NewIdentity()
	if err != nil {
		t.Fatalf("NewIdentity 失败: %v", err)
	}

	fp := id.GetFingerprint()
	if len(fp) != 16 { // 8 bytes = 16 hex chars
		t.Errorf("指纹长度期望 16，实际 %d", len(fp))
	}
}

func TestLoadInvalidIdentity(t *testing.T) {
	tmpDir := t.TempDir()
	idPath := filepath.Join(tmpDir, "invalid.json")

	// 写入无效 JSON
	if err := os.WriteFile(idPath, []byte("invalid json"), 0600); err != nil {
		t.Fatalf("写入测试文件失败: %v", err)
	}

	_, err := LoadIdentity(idPath)
	if err == nil {
		t.Error("加载无效身份应该失败")
	}
}
