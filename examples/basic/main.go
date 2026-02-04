package main

import (
	"bufio"
	"flag"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/cykyes/tenet/api"
	"github.com/cykyes/tenet/log"
	"github.com/cykyes/tenet/metrics"
)

// ping 相关变量
var (
	pingMu      sync.Mutex
	pingPending = make(map[string]time.Time)     // peerID -> 发送时间
	pingResults = make(map[string]time.Duration) // peerID -> 最近 RTT
)

// formatBytes 格式化字节数为人类可读格式
func formatBytes(bytes int64) string {
	const (
		KB = 1024
		MB = KB * 1024
		GB = MB * 1024
	)
	switch {
	case bytes >= GB:
		return fmt.Sprintf("%.2f GB", float64(bytes)/GB)
	case bytes >= MB:
		return fmt.Sprintf("%.2f MB", float64(bytes)/MB)
	case bytes >= KB:
		return fmt.Sprintf("%.2f KB", float64(bytes)/KB)
	default:
		return fmt.Sprintf("%d B", bytes)
	}
}

func main() {
	// 命令行参数
	port := flag.Int("l", 0, "监听端口（0表示自动分配）")
	password := flag.String("secret", "", "网络密码（相同密码的节点可以互联）")
	connect := flag.String("p", "", "要连接的节点地址（host:port）")
	relays := flag.String("relay", "", "中继节点列表（逗号分隔 host:port）")
	identityFile := flag.String("id", "identity.json", "身份文件路径")
	verbose := flag.Bool("v", false, "启用详细日志输出")
	flag.Parse()

	fmt.Println("=== P2P 加密隧道示例 ===")
	fmt.Println()

	// 创建隧道
	opts := []api.Option{
		api.WithPassword(*password),
		api.WithListenPort(*port),
		api.WithEnableRelay(true),
		api.WithEnableHolePunch(true),
	}

	// 如果启用详细日志，注入 StdLogger
	if *verbose {
		logger := log.NewStdLogger(
			log.WithLevel(log.LevelDebug),
			log.WithPrefix("[tenet]"),
		)
		opts = append(opts, api.WithLogger(logger))
		fmt.Println("详细日志已启用")
	}
	if *relays != "" {
		parts := strings.Split(*relays, ",")
		clean := make([]string, 0, len(parts))
		for _, v := range parts {
			v = strings.TrimSpace(v)
			if v != "" {
				clean = append(clean, v)
			}
		}
		// 加载身份
		if *identityFile != "" {
			data, err := os.ReadFile(*identityFile)
			if err == nil {
				fmt.Printf("加载身份文件: %s\n", *identityFile)
				opts = append(opts, api.WithIdentityJSON(data))
			} else if !os.IsNotExist(err) {
				fmt.Printf("读取身份文件失败: %v\n", err)
			}
		} else {
			fmt.Println("未指定身份文件，将生成临时身份")
		}

		if len(clean) > 0 {
			opts = append(opts, api.WithRelayNodes(clean))
		}
	}
	tunnel, err := api.NewTunnel(opts...)
	if err != nil {
		fmt.Printf("创建隧道失败: %v\n", err)
		os.Exit(1)
	}

	// 设置回调
	tunnel.OnReceive(func(peerID string, data []byte) {
		msg := string(data)
		sid := peerID
		if len(sid) > 16 {
			sid = sid[:16]
		}

		// 处理 ping 请求
		if strings.HasPrefix(msg, "PING:") {
			// 收到 ping 请求，回复 pong
			tunnel.Send(peerID, []byte("PONG:"+msg[5:]))
			return
		}

		// 处理 pong 响应
		if strings.HasPrefix(msg, "PONG:") {
			pingMu.Lock()
			if startTime, ok := pingPending[peerID]; ok {
				rtt := time.Since(startTime)
				pingResults[peerID] = rtt
				delete(pingPending, peerID)
				pingMu.Unlock()
				fmt.Printf("\n来自 %s 的 pong: RTT = %v\n> ", sid, rtt)
			} else {
				pingMu.Unlock()
			}
			return
		}

		fmt.Printf("\n收到消息 [来自 %s]: %s\n> ", sid, msg)
	})

	tunnel.OnPeerConnected(func(peerID string) {
		sid := peerID
		fmt.Printf("\n节点已连接: %s\n> ", sid)
	})

	tunnel.OnPeerDisconnected(func(peerID string) {
		sid := peerID
		if len(sid) > 16 {
			sid = sid[:16]
		}
		fmt.Printf("\n节点已断开: %s\n> ", sid)
	})

	// 启动隧道
	if err := tunnel.Start(); err != nil {
		fmt.Printf("启动隧道失败: %v\n", err)
		os.Exit(1)
	}
	defer tunnel.Stop()

	// 显示本地信息
	fmt.Printf("本地节点ID: %s\n", tunnel.LocalID())
	fmt.Printf("本地地址: %s\n", tunnel.LocalAddr())
	if pubAddr := tunnel.PublicAddr(); pubAddr != "" {
		fmt.Printf("公网地址: %s\n", pubAddr)
	}
	if *password != "" {
		fmt.Printf("网络密码: %s\n", *password)
	}
	// 保存身份（如果是新生成的）
	if *identityFile != "" {
		if _, err := os.Stat(*identityFile); os.IsNotExist(err) {
			jsonBytes, err := tunnel.GetIdentityJSON()
			if err == nil {
				if err := os.WriteFile(*identityFile, jsonBytes, 0600); err == nil {
					fmt.Printf("身份已保存至: %s\n", *identityFile)
				} else {
					fmt.Printf("保存身份失败: %v\n", err)
				}
			}
		}
	}

	fmt.Println()

	// 如果指定了连接目标，尝试连接
	if *connect != "" {
		fmt.Printf("正在连接到: %s\n", *connect)
		if err := tunnel.Connect(*connect); err != nil {
			fmt.Printf("连接失败: %v\n", err)
		} else {
			fmt.Println("连接请求已发送...")
		}
	}

	// 命令行交互
	fmt.Println()
	fmt.Println("命令:")
	fmt.Println("  send <peer_id> <message>  - 发送消息")
	fmt.Println("  connect <addr>            - 连接节点")
	fmt.Println("  peers                     - 显示已连接节点")
	fmt.Println("  ping <peer_id>            - 测试节点延迟")
	fmt.Println("  stats                     - 显示统计信息")
	fmt.Println("  info                      - 显示本地信息")
	fmt.Println("  quit                      - 退出")
	fmt.Println()

	scanner := bufio.NewScanner(os.Stdin)
	for {
		fmt.Print("> ")
		if !scanner.Scan() {
			break
		}

		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}

		parts := strings.SplitN(line, " ", 3)
		cmd := parts[0]

		switch cmd {
		case "send":
			if len(parts) < 3 {
				fmt.Println("用法: send <peer_id> <message>")
				continue
			}
			peerID := parts[1]
			message := parts[2]
			if err := tunnel.Send(peerID, []byte(message)); err != nil {
				errMsg := err.Error()
				switch {
				case strings.Contains(errMsg, "不能向本节点发送数据"):
					fmt.Println("发送失败: 不能向自己发送消息")
				case strings.Contains(errMsg, "未找到对等节点"):
					fmt.Printf("发送失败: 未找到节点 %s，请先连接该节点\n", peerID)
				case strings.Contains(errMsg, "对等节点会话未建立"):
					fmt.Printf("发送失败: 与节点 %s 的加密会话尚未建立，请稍后重试\n", peerID)
				case strings.Contains(errMsg, "UDP 地址无效"):
					fmt.Println("发送失败: 对等节点的地址无效")
				default:
					fmt.Printf("发送失败: %v\n", err)
				}
			} else {
				fmt.Println("已发送")
			}

		case "connect":
			if len(parts) < 2 {
				fmt.Println("用法: connect <addr>")
				continue
			}
			addr := parts[1]
			if err := tunnel.Connect(addr); err != nil {
				fmt.Printf("连接失败: %v\n", err)
			} else {
				fmt.Println("连接请求已发送...")
			}

		case "peers":
			peers := tunnel.Peers()
			if len(peers) == 0 {
				fmt.Println("暂无已连接节点")
			} else {
				fmt.Println("已连接节点:")
				for i, p := range peers {
					transport := tunnel.PeerTransport(p)
					linkMode := tunnel.PeerLinkMode(p)
					fmt.Printf("  %d. %s [%s/%s]\n", i+1, p, transport, linkMode)
				}
			}

		case "info":
			fmt.Printf("节点ID: %s\n", tunnel.LocalID())
			fmt.Printf("本地地址: %s\n", tunnel.LocalAddr())
			fmt.Printf("公网地址: %s\n", tunnel.PublicAddr())
			fmt.Printf("已连接: %d 个节点\n", tunnel.PeerCount())

		case "ping":
			if len(parts) < 2 {
				fmt.Println("用法: ping <peer_id>")
				continue
			}
			peerID := parts[1]

			// 记录发送时间
			pingMu.Lock()
			pingPending[peerID] = time.Now()
			pingMu.Unlock()

			// 发送 ping 消息
			timestamp := fmt.Sprintf("%d", time.Now().UnixNano())
			if err := tunnel.Send(peerID, []byte("PING:"+timestamp)); err != nil {
				pingMu.Lock()
				delete(pingPending, peerID)
				pingMu.Unlock()
				fmt.Printf("ping 失败: %v\n", err)
			} else {
				sid := peerID
				if len(sid) > 16 {
					sid = sid[:16]
				}
				fmt.Printf("正在 ping %s ...\n", sid)
			}

		case "stats":
			m := tunnel.GetMetrics()
			if snapshot, ok := m.(metrics.Snapshot); ok {
				fmt.Println("=== 节点统计 ===")
				fmt.Printf("运行时间: %v\n", snapshot.Uptime.Round(time.Second))
				fmt.Println()
				fmt.Println("连接统计:")
				fmt.Printf("  活跃连接: %d\n", snapshot.ConnectionsActive)
				fmt.Printf("  握手次数: %d (失败: %d)\n", snapshot.HandshakesTotal, snapshot.HandshakesFailed)
				if snapshot.AvgHandshakeLatency > 0 {
					fmt.Printf("  平均握手延迟: %v\n", snapshot.AvgHandshakeLatency)
				}
				fmt.Println()
				fmt.Println("流量统计:")
				fmt.Printf("  发送: %s (%d 包)\n", formatBytes(snapshot.BytesSent), snapshot.PacketsSent)
				fmt.Printf("  接收: %s (%d 包)\n", formatBytes(snapshot.BytesReceived), snapshot.PacketsRecv)
				fmt.Println()
				fmt.Println("NAT 打洞:")
				fmt.Printf("  UDP: %d/%d (成功/尝试)\n", snapshot.PunchSuccessUDP, snapshot.PunchAttemptsUDP)
				fmt.Printf("  TCP: %d/%d (成功/尝试)\n", snapshot.PunchSuccessTCP, snapshot.PunchAttemptsTCP)
				if snapshot.PunchSuccessRate > 0 {
					fmt.Printf("  成功率: %.1f%%\n", snapshot.PunchSuccessRate*100)
				}
				fmt.Println()
				fmt.Println("中继统计:")
				fmt.Printf("  中继连接: %d\n", snapshot.RelayConnects)
				fmt.Printf("  中继流量: %s (%d 包)\n", formatBytes(snapshot.RelayBytes), snapshot.RelayPackets)
				if snapshot.RelayAuthFailed > 0 {
					fmt.Printf("  认证失败: %d\n", snapshot.RelayAuthFailed)
				}
				if snapshot.ErrorsTotal > 0 {
					fmt.Printf("\n错误总数: %d\n", snapshot.ErrorsTotal)
				}
			} else {
				fmt.Println("无法获取统计信息")
			}

		case "quit", "exit":
			fmt.Println("再见！")
			return

		default:
			fmt.Printf("未知命令: %s\n", cmd)
		}
	}
}
