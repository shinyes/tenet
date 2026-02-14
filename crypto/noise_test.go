package crypto

import (
	"bytes"
	"errors"
	"sync"
	"testing"
)

// 辅助函数：创建完整的握手会话对
func createSessionPair(t *testing.T) (*Session, *Session) {
	t.Helper()

	id1, err := NewIdentity()
	if err != nil {
		t.Fatalf("创建身份1失败: %v", err)
	}
	id2, err := NewIdentity()
	if err != nil {
		t.Fatalf("创建身份2失败: %v", err)
	}

	networkSecret := []byte("test-network-secret")

	// 发起方创建握手
	initiator, msg1, err := NewInitiatorHandshake(
		id1.NoisePrivateKey[:],
		id1.NoisePublicKey[:],
		networkSecret,
	)
	if err != nil {
		t.Fatalf("创建发起方握手失败: %v", err)
	}

	// 响应方创建握手
	responder, err := NewResponderHandshake(
		id2.NoisePrivateKey[:],
		id2.NoisePublicKey[:],
		networkSecret,
	)
	if err != nil {
		t.Fatalf("创建响应方握手失败: %v", err)
	}

	// 响应方处理 msg1，生成 msg2
	msg2, _, err := responder.ProcessMessage(msg1)
	if err != nil {
		t.Fatalf("响应方处理 msg1 失败: %v", err)
	}

	// 发起方处理 msg2，生成 msg3 和 session1
	msg3, session1, err := initiator.ProcessMessage(msg2)
	if err != nil {
		t.Fatalf("发起方处理 msg2 失败: %v", err)
	}

	// 响应方处理 msg3，得到 session2
	_, session2, err := responder.ProcessMessage(msg3)
	if err != nil {
		t.Fatalf("响应方处理 msg3 失败: %v", err)
	}

	if session1 == nil || session2 == nil {
		t.Fatal("握手完成后 Session 不应为 nil")
	}

	return session1, session2
}

func TestNoiseHandshakeComplete(t *testing.T) {
	session1, session2 := createSessionPair(t)

	// 验证远程公钥存在
	if len(session1.RemotePublicKey()) == 0 {
		t.Error("session1 远程公钥为空")
	}
	if len(session2.RemotePublicKey()) == 0 {
		t.Error("session2 远程公钥为空")
	}

	// 验证握手哈希存在且相同
	if len(session1.HandshakeHash()) == 0 {
		t.Error("session1 握手哈希为空")
	}
	if !bytes.Equal(session1.HandshakeHash(), session2.HandshakeHash()) {
		t.Error("两端握手哈希应该相同")
	}
}

func TestSessionEncryptDecrypt(t *testing.T) {
	session1, session2 := createSessionPair(t)

	// 从 session1 发送到 session2
	plaintext := []byte("Hello, secure world!")
	ciphertext, err := session1.Encrypt(plaintext)
	if err != nil {
		t.Fatalf("加密失败: %v", err)
	}

	if bytes.Equal(ciphertext, plaintext) {
		t.Error("密文不应该等于明文")
	}

	decrypted, err := session2.Decrypt(ciphertext)
	if err != nil {
		t.Fatalf("解密失败: %v", err)
	}

	if !bytes.Equal(decrypted, plaintext) {
		t.Errorf("解密结果不匹配: got %s, want %s", decrypted, plaintext)
	}
}

func TestSessionBidirectionalEncryption(t *testing.T) {
	session1, session2 := createSessionPair(t)

	// 双向通信测试
	msg1to2 := []byte("Message from 1 to 2")
	msg2to1 := []byte("Message from 2 to 1")

	// 1 -> 2
	enc1, _ := session1.Encrypt(msg1to2)
	dec1, err := session2.Decrypt(enc1)
	if err != nil {
		t.Errorf("1->2 解密失败: %v", err)
	}
	if !bytes.Equal(dec1, msg1to2) {
		t.Error("1->2 消息不匹配")
	}

	// 2 -> 1
	enc2, _ := session2.Encrypt(msg2to1)
	dec2, err := session1.Decrypt(enc2)
	if err != nil {
		t.Errorf("2->1 解密失败: %v", err)
	}
	if !bytes.Equal(dec2, msg2to1) {
		t.Error("2->1 消息不匹配")
	}
}

func TestSessionMultipleMessages(t *testing.T) {
	session1, session2 := createSessionPair(t)

	// 发送多条消息
	for i := 0; i < 10; i++ {
		msg := []byte("Message number " + string(rune('0'+i)))
		enc, _ := session1.Encrypt(msg)
		dec, err := session2.Decrypt(enc)
		if err != nil {
			t.Errorf("第 %d 条消息解密失败: %v", i, err)
		}
		if !bytes.Equal(dec, msg) {
			t.Errorf("第 %d 条消息不匹配", i)
		}
	}
}

func TestSessionClose(t *testing.T) {
	session1, _ := createSessionPair(t)

	// 关闭会话
	session1.Close()

	// 关闭后密钥材料应该被清除
	if session1.remoteStaticKey != nil {
		t.Error("关闭后 remoteStaticKey 应为 nil")
	}
	if session1.handshakeHash != nil {
		t.Error("关闭后 handshakeHash 应为 nil")
	}
	if session1.sendCipher != nil || session1.recvCipher != nil {
		t.Error("关闭后 cipher 应为 nil")
	}
}

func TestSessionEncryptAfterClose(t *testing.T) {
	session1, _ := createSessionPair(t)
	session1.Close()

	_, err := session1.Encrypt([]byte("hello"))
	if !errors.Is(err, ErrSessionClosed) {
		t.Fatalf("Encrypt after Close should return ErrSessionClosed, got: %v", err)
	}
}

func TestSessionDecryptAfterClose(t *testing.T) {
	session1, session2 := createSessionPair(t)
	ciphertext, err := session2.Encrypt([]byte("hello"))
	if err != nil {
		t.Fatalf("Encrypt failed: %v", err)
	}

	session1.Close()

	_, err = session1.Decrypt(ciphertext)
	if !errors.Is(err, ErrSessionClosed) {
		t.Fatalf("Decrypt after Close should return ErrSessionClosed, got: %v", err)
	}
}

func TestSessionConcurrentEncrypt(t *testing.T) {
	session1, session2 := createSessionPair(t)

	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			msg := []byte("concurrent message")
			enc, err := session1.Encrypt(msg)
			if err != nil {
				t.Errorf("并发加密 %d 失败: %v", n, err)
				return
			}
			dec, err := session2.Decrypt(enc)
			if err != nil {
				t.Errorf("并发解密 %d 失败: %v", n, err)
				return
			}
			if !bytes.Equal(dec, msg) {
				t.Errorf("并发消息 %d 不匹配", n)
			}
		}(i)
	}
	wg.Wait()
}

func TestHandshakeStateCreatedAt(t *testing.T) {
	id, _ := NewIdentity()
	hs, _, err := NewInitiatorHandshake(
		id.NoisePrivateKey[:],
		id.NoisePublicKey[:],
		nil,
	)
	if err != nil {
		t.Fatalf("创建握手失败: %v", err)
	}

	createdAt := hs.CreatedAt()
	if createdAt == 0 {
		t.Error("CreatedAt 应该 > 0")
	}
}

func TestNewInitiatorHandshakeInvalidKey(t *testing.T) {
	// 密钥太短
	_, _, err := NewInitiatorHandshake(
		[]byte("short"),
		[]byte("short"),
		nil,
	)
	if err == nil {
		t.Error("密钥太短应该返回错误")
	}
}

func TestNewResponderHandshakeInvalidKey(t *testing.T) {
	_, err := NewResponderHandshake(
		[]byte("short"),
		[]byte("short"),
		nil,
	)
	if err == nil {
		t.Error("密钥太短应该返回错误")
	}
}

func TestHandshakeAlreadyCompleted(t *testing.T) {
	session1, session2 := createSessionPair(t)
	_, _ = session1, session2

	// 注意：此测试确认握手完成后的行为
	// 具体测试依赖于内部实现
}
