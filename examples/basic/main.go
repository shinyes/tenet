package main

import (
	"bufio"
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/cykyes/tenet/api"
)

func main() {
	// 命令行参数
	port := flag.Int("l", 0, "监听端口（0表示自动分配）")
	password := flag.String("secret", "", "网络密码（相同密码的节点可以互联）")
	connect := flag.String("p", "", "要连接的节点地址（host:port）")
	relays := flag.String("relay", "", "中继节点列表（逗号分隔 host:port）")
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
	if *relays != "" {
		parts := strings.Split(*relays, ",")
		clean := make([]string, 0, len(parts))
		for _, v := range parts {
			v = strings.TrimSpace(v)
			if v != "" {
				clean = append(clean, v)
			}
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
		sid := peerID
		if len(sid) > 16 {
			sid = sid[:16]
		}
		fmt.Printf("\n收到消息 [来自 %s]: %s\n> ", sid, string(data))
	})

	tunnel.OnPeerConnected(func(peerID string) {
		sid := peerID
		if len(sid) > 16 {
			sid = sid[:16]
		}
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
				fmt.Printf("发送失败: %v\n", err)
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

		case "quit", "exit":
			fmt.Println("再见！")
			return

		default:
			fmt.Printf("未知命令: %s\n", cmd)
		}
	}
}
