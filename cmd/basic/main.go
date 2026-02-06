package main

import (
	"bufio"
	"flag"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"github.com/cykyes/tenet/crypto"
	"github.com/cykyes/tenet/node"
)

// Simple logger wrapper to control verbosity
type cliLogger struct {
	debug bool
}

func (l *cliLogger) Info(format string, args ...interface{}) {
	if l.debug {
		log.Printf("[INFO] "+format, args...)
	}
}

func (l *cliLogger) Debug(format string, args ...interface{}) {
	if l.debug {
		log.Printf("[DEBUG] "+format, args...)
	}
}

func (l *cliLogger) Error(format string, args ...interface{}) {
	log.Printf("[ERROR] "+format, args...)
}

func (l *cliLogger) Warn(format string, args ...interface{}) {
	log.Printf("[WARN] "+format, args...)
}

func main() {
	// 1. 解析命令行参数
	port := flag.Int("l", 0, "监听端口 (0 表示随机)")
	channel := flag.String("channel", "default", "频道 ID (只有相同频道的节点才能通信)")
	connectPtr := flag.String("connect", "", "初始连接的对等节点地址 (逗号分隔)")
	secret := flag.String("s", "tenet-demo-network", "网络密钥")
	debug := flag.Bool("d", false, "启用调试日志")
	maxPeers := flag.Int("max-peers", 50, "最大连接数")
	flag.Parse()

	// 2. 创建节点身份
	id, err := crypto.NewIdentity()
	if err != nil {
		log.Fatalf("创建身份失败: %v", err)
	}

	// 配置日志
	logger := &cliLogger{debug: *debug}

	opts := []node.Option{
		node.WithListenPort(*port),
		node.WithIdentity(id),
		node.WithNetworkPassword(*secret),
		node.WithChannelID(*channel),
		node.WithMaxPeers(*maxPeers),
		node.WithLogger(logger),
		node.WithEnableRelay(false),
		node.WithEnableHolePunch(false),
	}

	n, err := node.NewNode(opts...)
	if err != nil {
		log.Fatalf("创建节点失败: %v", err)
	}

	// 3. 设置回调
	n.OnPeerConnected(func(peerID string) {
		fmt.Printf("\r[系统] 成功连接新节点: %s\n> ", peerID[:8])
	})

	n.OnReceive(func(peerID string, data []byte) {
		fmt.Printf("\r[接收] <%s>: %s\n> ", peerID[:8], string(data))
	})

	// 4. 启动节点
	if err := n.Start(); err != nil {
		log.Fatalf("启动失败: %v", err)
	}
	defer n.Stop()

	// 5. 连接初始节点
	if *connectPtr != "" {
		peers := strings.Split(*connectPtr, ",")
		for _, addr := range peers {
			addr = strings.TrimSpace(addr)
			if addr == "" {
				continue
			}
			go func(a string) {
				if err := n.Connect(a); err != nil {
					// 仅在调试模式显示连接错误，或者连接成功后再通知
					if *debug {
						log.Printf("连接 %s 失败: %v", a, err)
					}
				}
			}(addr)
		}
	}

	// 欢迎信息
	printWelcome(n, *channel)

	// 6. 交互式输入循环
	scanner := bufio.NewScanner(os.Stdin)
	fmt.Print("> ")
	for scanner.Scan() {
		text := strings.TrimSpace(scanner.Text())
		if text == "" {
			fmt.Print("> ")
			continue
		}

		if strings.HasPrefix(text, "/") {
			handleCommand(n, text, *channel)
		} else {
			broadcastMessage(n, text)
		}
		fmt.Print("> ")
	}

	if err := scanner.Err(); err != nil {
		log.Printf("读取输入错误: %v", err)
	}

	fmt.Println("\n正在关闭节点...")
}

func printWelcome(n *node.Node, channel string) {
	fmt.Println("========================================")
	fmt.Println("      Tenet CLI P2P Chat Node")
	fmt.Println("========================================")
	fmt.Printf(" ID      : %s\n", n.ID()[:8])
	fmt.Printf(" Address : %s\n", n.LocalAddr)
	fmt.Printf(" Channels: %v\n", n.Config.Channels)
	fmt.Println("----------------------------------------")
	fmt.Println(" 输入 '/' 查看可用命令 (例如 /help)")
	fmt.Println(" 直接输入文本并回车以广播消息")
	fmt.Println("========================================")
}

func handleCommand(n *node.Node, cmdStr string, channel string) {
	parts := strings.Fields(cmdStr)
	cmd := parts[0]
	args := parts[1:]

	switch cmd {
	case "/help":
		fmt.Println("可用命令:")
		fmt.Println("  /list, /peers     列出已连接的节点")
		fmt.Println("  /connect <addr>   连接到指定地址")
		fmt.Println("  /info, /whoami    显示本节点信息")
		fmt.Println("  /metrics, /stats  显示节点指标")
		fmt.Println("  /channel          显示当前频道列表")
		fmt.Println("  /join <name>      加入频道")
		fmt.Println("  /leave <name>     离开频道")
		fmt.Println("  /send <id> <msg>  发送私信给指定节点")
		fmt.Println("  /quit, /exit      退出程序")
		fmt.Println("  /help             显示此帮助")

	case "/list", "/peers":
		peers := n.Peers.IDs()
		fmt.Printf("当前连接节点 (%d):\n", len(peers))
		for _, pid := range peers {
			p, ok := n.Peers.Get(pid)
			if ok {
				transport, addr, _ := p.GetTransportInfo()
				stats := p.GetStats()
				latency := stats.ConnectLatency
				if latency == 0 {
					latency = time.Since(stats.ConnectedAt) // 粗略估计
				}
				fmt.Printf(" - %s [%s] %s (RTT: %v)\n", pid[:8], transport, addr, latency)
			} else {
				fmt.Printf(" - %s (未知状态)\n", pid[:8])
			}
		}

	case "/connect":
		if len(args) == 0 {
			fmt.Println("用法: /connect <ip:port>")
			return
		}
		addr := args[0]
		fmt.Printf("正在连接 %s ...\n", addr)
		go func() {
			err := n.Connect(addr)
			if err != nil {
				fmt.Printf("\r[错误] 连接 %s 失败: %v\n> ", addr, err)
			} else {
				// 连接成功通常由 OnPeerConnected 回调通知，但这里是同步 Connect 方法
				// 如果 Connect 返回 nil，只代表握手开始或打洞请求发送
				// 实际连接状态由 OnPeerConnected 确认
			}
		}()

	case "/info", "/whoami":
		fmt.Printf("ID: %s\n", n.ID())
		fmt.Printf("Address: %s\n", n.LocalAddr)
		fmt.Printf("Peers: %d\n", n.Peers.Count())

	case "/metrics", "/stats":
		s := n.GetMetrics()
		fmt.Println("--- 节点指标 ---")
		fmt.Printf("运行时间: %v\n", s.Uptime)
		fmt.Printf("当前连接: %d\n", s.ConnectionsActive)
		fmt.Printf("总连接/失败: %d / %d\n", s.ConnectionsTotal, s.ConnectionsFailed)
		fmt.Printf("总握手/失败: %d / %d\n", s.HandshakesTotal, s.HandshakesFailed)
		fmt.Printf("发送流量: %d packets (%d bytes)\n", s.PacketsSent, s.BytesSent)
		fmt.Printf("接收流量: %d packets (%d bytes)\n", s.PacketsRecv, s.BytesReceived)
		if s.PacketsSent > 0 {
			fmt.Printf("平均包大小(Tx): %d bytes\n", s.BytesSent/s.PacketsSent)
		}
		fmt.Println("----------------")

	case "/channel", "/channels":
		fmt.Printf("当前已加入频道 (%d):\n", len(n.Config.Channels))
		for _, ch := range n.Config.Channels {
			fmt.Printf(" - %s\n", ch)
		}

	case "/join":
		if len(args) == 0 {
			fmt.Println("用法: /join <channel_name>")
			return
		}
		name := args[0]
		n.JoinChannel(name)
		fmt.Printf("已加入频道: %s\n", name)

	case "/leave":
		if len(args) == 0 {
			fmt.Println("用法: /leave <channel_name>")
			return
		}
		name := args[0]
		n.LeaveChannel(name)
		fmt.Printf("已离开频道: %s\n", name)

	case "/send", "/msg", "/to":
		if len(args) < 2 {
			fmt.Println("用法: /send <NodeID前缀> <消息内容>")
			return
		}
		targetPrefix := args[0]
		msgContent := strings.Join(args[1:], " ")

		// 查找匹配的 PeerID
		var targetID string
		peers := n.Peers.IDs()
		matchCount := 0

		for _, pid := range peers {
			if strings.HasPrefix(pid, targetPrefix) {
				targetID = pid
				matchCount++
			}
		}

		if matchCount == 0 {
			fmt.Printf("未找到前缀为 %s 的节点\n", targetPrefix)
			return
		}
		if matchCount > 1 {
			fmt.Printf("前缀 %s 匹配到多个节点，请提供更长的前缀\n", targetPrefix)
			return
		}

		// 发送消息
		if err := n.Send(targetID, []byte(msgContent)); err != nil {
			fmt.Printf("发送失败: %v\n", err)
		} else {
			fmt.Printf("[私聊] -> <%s>: %s\n", targetID[:8], msgContent)
		}

	case "/quit", "/exit":
		fmt.Println("再见!")
		os.Exit(0)

	default:
		fmt.Printf("未知命令: %s (输入 /help 查看帮助)\n", cmd)
	}
}

func broadcastMessage(n *node.Node, text string) {
	// 收集所有加入相同频道的节点
	targetPeers := make(map[string]struct{})
	for _, ch := range n.Config.Channels {
		peers := n.GetPeersInChannel(ch)
		for _, pid := range peers {
			targetPeers[pid] = struct{}{}
		}
	}

	if len(targetPeers) == 0 {
		fmt.Println("[系统] 无共同频道的节点，消息未发送")
		return
	}

	count := 0
	msg := []byte(text)
	for pid := range targetPeers {
		if err := n.Send(pid, msg); err == nil {
			count++
		}
	}
	fmt.Printf("[系统] 消息已发送给 %d 个节点 (频道内广播)\n", count)
}
