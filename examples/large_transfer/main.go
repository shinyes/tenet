package main

import (
	"crypto/rand"
	"crypto/sha256"
	"fmt"
	"log"
	"time"

	"github.com/cykyes/tenet/api"
)

// GenerateRandomData 生成指定大小的随机数据
func GenerateRandomData(size int) []byte {
	data := make([]byte, size)
	rand.Read(data)
	return data
}

func main() {
	// --- Alice (发送者) ---
	alice, err := api.NewTunnel(
		api.WithListenPort(20001),
		api.WithPassword("transfer-secret"),
	)
	if err != nil {
		log.Fatalf("Alice 创建失败: %v", err)
	}
	defer alice.Stop()

	// --- Bob (接收者) ---
	bob, err := api.NewTunnel(
		api.WithListenPort(20002),
		api.WithPassword("transfer-secret"),
	)
	if err != nil {
		log.Fatalf("Bob 创建失败: %v", err)
	}
	defer bob.Stop()

	// 准备大数据 (5MB)
	dataSize := 5 * 1024 * 1024 // 5MB

	log.Printf("Alice: 正在生成 %d MB 随机数据...", dataSize/1024/1024)
	largeData := GenerateRandomData(dataSize)
	originalHash := sha256.Sum256(largeData)
	log.Printf("Alice: 原始数据哈希: %x", originalHash)

	// 信号通道
	receivedChan := make(chan []byte, 1)
	aliceConnected := make(chan string, 1)

	// 设置 Bob 的接收回调
	bob.OnReceive(func(peerID string, data []byte) {
		log.Printf("Bob: 收到来自 %s 的完整数据包，长度: %d 字节", peerID[:8], len(data))
		receivedChan <- data
	})

	// 设置 Alice 的连接回调
	alice.OnPeerConnected(func(id string) {
		aliceConnected <- id
	})

	log.Println("系统: 启动节点...")
	if err := alice.Start(); err != nil {
		log.Fatalf("Alice 启动失败: %v", err)
	}
	if err := bob.Start(); err != nil {
		log.Fatalf("Bob 启动失败: %v", err)
	}

	// 等待启动稳定
	time.Sleep(500 * time.Millisecond)

	// Alice 连接 Bob
	bobAddr := fmt.Sprintf("127.0.0.1:%d", 20002)
	log.Printf("Alice: 连接 Bob (%s)...", bobAddr)
	// 因为 Bob 也有 UDP 监听，我们可以直接连 UDP 或 TCP
	if err := alice.Connect(bobAddr); err != nil {
		log.Fatalf("连接失败: %v", err)
	}

	// 等待 Alice 确认连接
	log.Println("Alice: 等待握手完成...")
	var bobID string
	select {
	case id := <-aliceConnected:
		bobID = id
		log.Printf("Alice: 已连接到 Bob (ID: %s)", bobID[:8])
	case <-time.After(5 * time.Second):
		log.Fatal("Alice: 连接超时")
	}

	// 发送数据

	log.Println("Alice: 开始发送大数据...")
	startTime := time.Now()
	if err := alice.Send(bobID, largeData); err != nil {
		log.Fatalf("Alice: 发送失败: %v", err)
	}
	log.Println("Alice: 发送调用完成（数据已进入发送队列）")

	// 等待接收
	log.Println("Bob: 等待数据接收...")
	select {
	case recvData := <-receivedChan:
		duration := time.Since(startTime)
		recvHash := sha256.Sum256(recvData)
		log.Printf("Bob: 接收哈希: %x", recvHash)
		log.Printf("传输耗时: %v", duration)
		speed := float64(dataSize) / 1024 / 1024 / duration.Seconds()
		log.Printf("传输速度: %.2f MB/s", speed)

		// if bytes.Equal(alice.GetIdentityJSON(), bob.GetIdentityJSON()) {
		// 	// Just verify hashes match, identities are different
		// }

		if recvHash == originalHash {
			log.Println("验证成功: 数据完整无误！✅")
		} else {
			log.Fatal("验证失败: 数据损坏！❌")
		}

	case <-time.After(30 * time.Second):
		log.Fatal("Bob: 接收超时")
	}
}
