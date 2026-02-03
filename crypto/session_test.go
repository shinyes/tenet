package crypto

import (
	"sync"
	"testing"
)

func TestNewSessionManager(t *testing.T) {
	sm := NewSessionManager()
	if sm == nil {
		t.Fatal("NewSessionManager 返回 nil")
	}
	if sm.Count() != 0 {
		t.Errorf("新 SessionManager 应为空，Count=%d", sm.Count())
	}
}

func TestSessionManagerAddAndGet(t *testing.T) {
	sm := NewSessionManager()

	// 创建测试 session
	session1, _ := createSessionPair(t)

	sm.Add("peer1", session1)

	if sm.Count() != 1 {
		t.Errorf("添加后 Count 期望 1，实际 %d", sm.Count())
	}

	got := sm.Get("peer1")
	if got != session1 {
		t.Error("Get 返回的 session 不匹配")
	}
}

func TestSessionManagerGetNonExistent(t *testing.T) {
	sm := NewSessionManager()

	got := sm.Get("nonexistent")
	if got != nil {
		t.Error("不存在的 peerID 应返回 nil")
	}
}

func TestSessionManagerRemove(t *testing.T) {
	sm := NewSessionManager()
	session1, _ := createSessionPair(t)

	sm.Add("peer1", session1)
	sm.Remove("peer1")

	if sm.Count() != 0 {
		t.Errorf("删除后 Count 应为 0，实际 %d", sm.Count())
	}

	got := sm.Get("peer1")
	if got != nil {
		t.Error("删除后 Get 应返回 nil")
	}
}

func TestSessionManagerHas(t *testing.T) {
	sm := NewSessionManager()
	session1, _ := createSessionPair(t)

	if sm.Has("peer1") {
		t.Error("空 SessionManager 的 Has 应返回 false")
	}

	sm.Add("peer1", session1)

	if !sm.Has("peer1") {
		t.Error("添加后 Has 应返回 true")
	}

	if sm.Has("peer2") {
		t.Error("不存在的 peer Has 应返回 false")
	}
}

func TestSessionManagerCount(t *testing.T) {
	sm := NewSessionManager()
	s1, s2 := createSessionPair(t)
	s3, _ := createSessionPair(t)

	sm.Add("peer1", s1)
	sm.Add("peer2", s2)
	sm.Add("peer3", s3)

	if sm.Count() != 3 {
		t.Errorf("Count 期望 3，实际 %d", sm.Count())
	}
}

func TestSessionManagerAll(t *testing.T) {
	sm := NewSessionManager()
	s1, s2 := createSessionPair(t)

	sm.Add("peer1", s1)
	sm.Add("peer2", s2)

	all := sm.All()
	if len(all) != 2 {
		t.Errorf("All 长度期望 2，实际 %d", len(all))
	}

	if all["peer1"] != s1 || all["peer2"] != s2 {
		t.Error("All 返回的 session 不匹配")
	}

	// 确保返回的是副本，修改不影响原数据
	delete(all, "peer1")
	if sm.Count() != 2 {
		t.Error("修改 All 返回值不应影响 SessionManager")
	}
}

func TestSessionManagerClear(t *testing.T) {
	sm := NewSessionManager()
	s1, s2 := createSessionPair(t)

	sm.Add("peer1", s1)
	sm.Add("peer2", s2)

	sm.Clear()

	if sm.Count() != 0 {
		t.Errorf("Clear 后 Count 应为 0，实际 %d", sm.Count())
	}
}

func TestSessionManagerConcurrent(t *testing.T) {
	sm := NewSessionManager()
	var wg sync.WaitGroup

	// 并发添加
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			s1, _ := createSessionPair(t)
			peerID := string(rune('A' + n%26))
			sm.Add(peerID, s1)
			sm.Get(peerID)
			sm.Has(peerID)
			sm.Count()
		}(i)
	}

	wg.Wait()
	// 不应 panic
}

func TestAESGCMCipher(t *testing.T) {
	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i)
	}

	cipher, err := NewAESGCMCipher(key)
	if err != nil {
		t.Fatalf("NewAESGCMCipher 失败: %v", err)
	}

	plaintext := []byte("Secret message for AES-GCM")
	ciphertext, err := cipher.Encrypt(plaintext)
	if err != nil {
		t.Fatalf("Encrypt 失败: %v", err)
	}

	decrypted, err := cipher.Decrypt(ciphertext)
	if err != nil {
		t.Fatalf("Decrypt 失败: %v", err)
	}

	if string(decrypted) != string(plaintext) {
		t.Errorf("解密结果不匹配: got %s, want %s", decrypted, plaintext)
	}
}

func TestAESGCMCipherInvalidKeyLength(t *testing.T) {
	_, err := NewAESGCMCipher([]byte("short"))
	if err == nil {
		t.Error("密钥长度不是 32 字节应返回错误")
	}
}

func TestAESGCMCipherDecryptTooShort(t *testing.T) {
	key := make([]byte, 32)
	cipher, _ := NewAESGCMCipher(key)

	_, err := cipher.Decrypt([]byte("short"))
	if err == nil {
		t.Error("解密过短数据应返回错误")
	}
}

func TestAESGCMCipherDecryptInvalid(t *testing.T) {
	key := make([]byte, 32)
	cipher, _ := NewAESGCMCipher(key)

	// 加密一条消息
	ciphertext, _ := cipher.Encrypt([]byte("test"))

	// 篡改密文
	ciphertext[len(ciphertext)-1] ^= 0xFF

	_, err := cipher.Decrypt(ciphertext)
	if err == nil {
		t.Error("解密篡改的密文应返回错误")
	}
}

func TestAESGCMCipherMultipleMessages(t *testing.T) {
	key := make([]byte, 32)
	cipher, _ := NewAESGCMCipher(key)

	for i := 0; i < 10; i++ {
		msg := []byte("Message " + string(rune('0'+i)))
		enc, err := cipher.Encrypt(msg)
		if err != nil {
			t.Errorf("第 %d 条消息加密失败: %v", i, err)
		}
		dec, err := cipher.Decrypt(enc)
		if err != nil {
			t.Errorf("第 %d 条消息解密失败: %v", i, err)
		}
		if string(dec) != string(msg) {
			t.Errorf("第 %d 条消息不匹配", i)
		}
	}
}
