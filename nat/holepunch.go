package nat

import (
	"fmt"
	"net"
	"sync"
	"time"
)

// HolePunchConfig 打洞配置
type HolePunchConfig struct {
	// 打洞尝试次数
	Attempts int
	// 每次尝试间隔
	Interval time.Duration
	// 总超时时间
	Timeout time.Duration
}

// DefaultHolePunchConfig 默认打洞配置
func DefaultHolePunchConfig() *HolePunchConfig {
	return &HolePunchConfig{
		Attempts: 10,
		Interval: 100 * time.Millisecond,
		Timeout:  5 * time.Second,
	}
}

// HolePunchResult 打洞结果
type HolePunchResult struct {
	Success  bool
	PeerAddr *net.UDPAddr
	Latency  time.Duration
}

// HolePuncher UDP打洞器
type HolePuncher struct {
	conn   net.PacketConn
	config *HolePunchConfig
	mu     sync.Mutex
}

// NewHolePuncher 创建打洞器
func NewHolePuncher(conn net.PacketConn, config *HolePunchConfig) *HolePuncher {
	if config == nil {
		config = DefaultHolePunchConfig()
	}
	return &HolePuncher{
		conn:   conn,
		config: config,
	}
}

// Punch 执行UDP打洞
// 基本原理：
// 1. 双方同时向对方的公网地址发送UDP包
// 2. 发送的包会在NAT上创建映射（打洞）
// 3. 之后双方就可以通过这个"洞"进行通信
func (hp *HolePuncher) Punch(peerPublicAddr *net.UDPAddr, peerID []byte) (*HolePunchResult, error) {
	hp.mu.Lock()
	defer hp.mu.Unlock()
	if peerPublicAddr == nil {
		return nil, fmt.Errorf("peerPublicAddr is nil")
	}
	if len(peerID) < 16 {
		return nil, fmt.Errorf("peerID长度不足: %d", len(peerID))
	}

	result := &HolePunchResult{
		PeerAddr: peerPublicAddr,
	}

	// 设置超时
	deadline := time.Now().Add(hp.config.Timeout)
	hp.conn.SetDeadline(deadline)
	defer hp.conn.SetDeadline(time.Time{})

	// 构造打洞包
	// 格式: [PUNCH_MAGIC(4)] [PEER_ID(16)] [TIMESTAMP(8)]
	punchPacket := make([]byte, 28)
	// Magic: "PNCH"
	copy(punchPacket[0:4], []byte{0x50, 0x4E, 0x43, 0x48})
	copy(punchPacket[4:20], peerID[:16])
	timestamp := time.Now().UnixNano()
	for i := 0; i < 8; i++ {
		punchPacket[20+i] = byte(timestamp >> (i * 8))
	}

	// 创建接收通道和停止信号
	receiveChan := make(chan *net.UDPAddr, 1)
	stopChan := make(chan struct{})
	defer close(stopChan)

	// 启动接收协程（通过 deadline 机制可以退出）
	go func() {
		buf := make([]byte, 64)
		for {
			select {
			case <-stopChan:
				return
			default:
			}

			n, addr, err := hp.conn.ReadFrom(buf)
			if err != nil {
				// 检查是否应该退出
				select {
				case <-stopChan:
					return
				default:
					// 如果是超时错误，会在下一次迭代中继续
					if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
						return // deadline 到了，函数即将返回
					}
					return
				}
			}
			udpAddr, ok := addr.(*net.UDPAddr)
			if !ok {
				continue
			}

			if udpAddr == nil || !udpAddr.IP.Equal(peerPublicAddr.IP) || udpAddr.Port != peerPublicAddr.Port {
				continue
			}

			// 验证是否是打洞响应
			if n >= 4 && buf[0] == 0x50 && buf[1] == 0x4E && buf[2] == 0x43 && buf[3] == 0x48 {
				select {
				case receiveChan <- udpAddr:
				default:
				}
				return
			}
		}
	}()

	// 发送打洞包
	startTime := time.Now()
	for i := 0; i < hp.config.Attempts; i++ {
		_, err := hp.conn.WriteTo(punchPacket, peerPublicAddr)
		if err != nil {
			return nil, fmt.Errorf("发送打洞包失败: %w", err)
		}

		// 等待响应或继续尝试
		select {
		case addr := <-receiveChan:
			result.Success = true
			result.PeerAddr = addr
			result.Latency = time.Since(startTime)
			return result, nil
		case <-time.After(hp.config.Interval):
			continue
		}
	}

	remaining := hp.config.Timeout - time.Duration(hp.config.Attempts)*hp.config.Interval
	if remaining < 0 {
		remaining = 0
	}

	// 等待最后一次响应
	select {
	case addr := <-receiveChan:
		result.Success = true
		result.PeerAddr = addr
		result.Latency = time.Since(startTime)
		return result, nil
	case <-time.After(remaining):
		return result, fmt.Errorf("打洞超时")
	}
}

// PunchWithRetry 带重试的打洞
func (hp *HolePuncher) PunchWithRetry(peerPublicAddr *net.UDPAddr, peerID []byte, maxRetries int) (*HolePunchResult, error) {
	var lastErr error

	for i := 0; i < maxRetries; i++ {
		result, err := hp.Punch(peerPublicAddr, peerID)
		if err == nil && result.Success {
			return result, nil
		}
		lastErr = err
		time.Sleep(time.Second)
	}

	return nil, fmt.Errorf("打洞失败（重试%d次）: %w", maxRetries, lastErr)
}

// SimultaneousPunch 同时打洞（需要信令协调）
// 双方需要在约定的时间同时开始打洞
type PunchCoordinator struct {
	conn         net.PacketConn
	config       *HolePunchConfig
	puncher      *HolePuncher
	startTime    time.Time
	syncInterval time.Duration
}

// NewPunchCoordinator 创建打洞协调器
func NewPunchCoordinator(conn net.PacketConn) *PunchCoordinator {
	return &PunchCoordinator{
		conn:         conn,
		config:       DefaultHolePunchConfig(),
		puncher:      NewHolePuncher(conn, nil),
		syncInterval: 500 * time.Millisecond,
	}
}

// SchedulePunch 安排同步打洞
func (pc *PunchCoordinator) SchedulePunch(peerPublicAddr *net.UDPAddr, peerID []byte, startAt time.Time) (*HolePunchResult, error) {
	// 等待到约定时间
	waitDuration := time.Until(startAt)
	if waitDuration > 0 {
		time.Sleep(waitDuration)
	}

	// 执行打洞
	return pc.puncher.Punch(peerPublicAddr, peerID)
}
