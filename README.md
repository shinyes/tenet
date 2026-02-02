# Tenet (P2P Tunnel Library)

一个去中心化 P2P 加密隧道库，支持 NAT 打洞、Noise 协议加密和自动中继选择。专为嵌入到分布式应用设计，支持 Windows、Linux 和 Android 平台。

## 特性

- 🔒 **端到端加密**：使用 Noise Protocol 框架（与 WireGuard 相同）
- 🕳️ **NAT 穿透**：支持 TCP (Simultaneous Open) 与 UDP 并行打洞，智能选择最佳链路
- 🔍 **NAT 类型探测**：无需外部 STUN 服务器，通过已连接节点探测 NAT 类型
- ⚡ **无缝升级**：UDP 快速握手，后台自动升级至抗 QoS 的 TCP 通道
- 🔄 **自动中继**：打洞失败时自动在可用节点中回退到中继
- 🌐 **节点发现**：连接的节点会互相介绍其他节点
- 🔑 **密码组网**：相同密码的节点自动组成私有网络
- 💓 **心跳保活**：定期心跳检测连接状态，超时自动断开
- 🔃 **断线重连**：节点断开后自动尝试重新连接
- 🛡️ **安全防护**：中继认证防滥用
- 📊 **指标监控**：内置连接、流量、打洞成功率等统计
- 🌍 **跨平台**：支持 Windows、Linux、macOS、Android

## 快速开始

### 安装

```bash
go get github.com/cykyes/tenet
```

### 构建环境

- Go 版本：1.25.5

### 基础调用

```go
package main

import (
    "fmt"
    "log"
    "github.com/cykyes/tenet/api"
    tlog "github.com/cykyes/tenet/log"
)

func main() {
    // 1. 创建节点（可选：启用日志）
    node, _ := api.NewTunnel(
        api.WithPassword("my-secret-key"),
        api.WithListenPort(0), // 0 = 随机端口
        // 启用日志输出（默认静默，适合嵌入其他应用）
        api.WithLogger(tlog.NewStdLogger(tlog.WithLevel(tlog.LevelInfo))),
    )

    // 2. 注册回调
    shortID := func(id string) string {
        if len(id) > 8 {
            return id[:8]
        }
        return id
    }

    node.OnReceive(func(peerID string, data []byte) {
        log.Printf("收到消息 [%s]: %s", shortID(peerID), string(data))
    })

    node.OnPeerConnected(func(peerID string) {
        log.Printf("新连接: %s", shortID(peerID))
        // 连接成功后发送问候
        node.Send(peerID, []byte("Hello Tenet!"))
        
        // 探测 NAT 类型（可选）
        if natType, err := node.ProbeNAT(); err == nil {
            log.Printf("本机 NAT 类型: %s", natType)
        }
    })

    // 3. 启动并连接
    node.Start()
    defer node.GracefulStop() // 优雅关闭

    fmt.Printf("本地地址: %s\n", node.LocalAddr())
    
    // 主动连接其他节点（可选）
    // node.Connect("1.2.3.4:9000")
    
    select {} // 阻塞运行
}
```

### 运行示例

提供了完整的命令行示例程序，位于 `examples/basic`。

**节点 A**（等待连接）:
```bash
go run examples/basic/main.go -l 1231 -secret "mysecret"
```

**节点 B**（主动连接，启用详细日志）:
```bash
go run examples/basic/main.go -l 1232 -secret "mysecret" -p "127.0.0.1:1231" -v
```

**使用中继节点**:
```bash
# 中继服务器（开启 -relay 提供中继服务）
go run examples/basic/main.go -l 9000 -secret "mysecret" -relay

# 普通节点（自动使用可用中继，无需指定 -relay）
go run examples/basic/main.go -l 1232 -secret "mysecret" -p "1.2.3.4:9000"
```
> 只有开启 `-relay` 的节点才会响应中继转发请求。其他节点在直连失败时会自动通过已连接的中继节点回退。

### 编译示例

**Windows**:
```bash
go build -o build\basic.exe .\examples\basic
```

连接建立后，使用 `peers` 命令可查看当前链路状态：
```
> peers
已连接节点:
  1. 8cac1d66... [tcp]
```
`[tcp]` 表示已成功建立 TCP 直连（抗阻塞），`[udp]` 表示使用 UDP 通道。

## API 参考

### Tunnel

| 方法 | 说明 |
|------|------|
| `NewTunnel(opts...)` | 创建隧道实例 |
| `Start()` | 启动隧道服务 (UDP & TCP 监听) |
| `Stop()` | 停止服务 |
| `GracefulStop()` | 优雅关闭（通知对端后再断开） |
| `Connect(addr)` | 连接对等节点 (尝试双栈打洞) |
| `Send(peerID, data)` | 发送数据 |
| `OnReceive(handler)` | 设置接收回调 |
| `OnPeerConnected(handler)` | 节点连接回调 |
| `OnPeerDisconnected(handler)` | 节点断开回调 |
| `LocalID()` | 获取本地节点ID |
| `Peers()` | 获取已连接节点列表 |
| `PeerTransport(peerID)` | 获取节点当前的传输协议 |
| `PeerLinkMode(peerID)` | 获取链路模式 (p2p/relay) |
| `ProbeNAT()` | 探测本机 NAT 类型 |
| `GetNATType()` | 获取已探测的 NAT 类型 |
| `GetMetrics()` | 获取指标快照 |

### 配置选项

| 选项 | 说明 |
|------|------|
| `WithPassword(pwd)` | 网络密码 |
| `WithListenPort(port)` | 监听端口 |
| `WithEnableHolePunch(bool)` | 启用NAT打洞 |
| `WithEnableRelay(bool)` | 启用中继服务（作为中继服务器） |
| `WithMaxPeers(n)` | 最大连接数（超过后拒绝新连接） |
| `WithHeartbeatInterval(d)` | 心跳发送间隔（默认 5 秒） |
| `WithRelayNodes(addrs)` | 预设中继节点种子列表（可选） |
| `WithLogger(logger)` | 设置日志记录器（默认静默） |
| `WithEnableRelayAuth(bool)` | 启用中继认证（默认开启） |

## 项目结构

```
├── api/          # 公共API (Tunnel 接口)
├── node/         # 核心节点管理 (Node, Config)
├── nat/          # NAT穿透 (TCP/UDP打洞, NAT探测, 中继)
├── transport/    # 传输层封装 (跨平台Socket复用)
├── crypto/       # Noise协议加密
├── peer/         # 对等节点管理 (Peer, ConnState, PeerStore)
├── metrics/      # 指标监控 (连接/流量/打洞统计)
├── log/          # 日志接口 (Logger, NopLogger, StdLogger)
└── examples/     # 示例代码
```

## 工作原理

1.  **启动**: 监听本地 UDP 端口，并利用 `SO_REUSEADDR` 监听同名 TCP 端口。
2.  **连接**: 手动输入对方地址发起连接。
3.  **打洞**:
    *   **UDP**: 发送尝试包，实现快速握手。
    *   **TCP**: 发起 Simultaneous Open (同时开放) 连接，尝试穿透 NAT。
4.  **握手**: 使用 Noise Protocol (XX 模式) 进行双向身份验证和密钥交换。
5.  **升级**:
    *   系统优先使用 TCP 连接（QoS 友好）。
    *   如果 UDP 先建立，系统会保持后台 TCP 尝试。
    *   一旦 TCP 握手成功，无缝将该节点的所有流量升级至 TCP 通道。
6.  **通信**: 建立加密会话后，通过当前最优通道发送加密数据包。
7.  **中继**: 打洞失败时，优先从已连接节点中选择可用中继回退。
8.  **节点发现**: 节点连接成功后自动交换已知节点列表，实现网络自动扩展。
9.  **心跳保活**: 每 5 秒发送心跳包，30 秒无响应则视为断开。
10. **断线重连**: 节点断开后 5 秒自动尝试重新连接。

### 节点发现协议

当 A↔B 和 C↔B 已连接时：
1. C 向 B 发送 **DiscoveryRequest (0x04)** 请求已知节点列表
2. B 返回 **DiscoveryResponse (0x05)** 包含 A 的 PeerID 和地址
3. C 自动尝试连接 A，无需手动配置

```
      A ←────────→ B ←────────→ C
      ↑                         │
      └─────── 自动发现 ─────────┘
```

## 协议常量

| 类型 | 值 | 说明 |
|------|-----|------|
| `PacketTypeHandshake` | 0x01 | Noise 握手 |
| `PacketTypeData` | 0x02 | 加密数据 |
| `PacketTypeRelay` | 0x03 | 中继封装 |
| `PacketTypeDiscoveryReq` | 0x04 | 节点发现请求 |
| `PacketTypeDiscoveryResp` | 0x05 | 节点发现响应 |
| `PacketTypeHeartbeat` | 0x06 | 心跳请求 |
| `PacketTypeHeartbeatAck` | 0x07 | 心跳响应 |

## 依赖

- [flynn/noise](https://github.com/flynn/noise) - Noise Protocol框架
- [golang.org/x/crypto](https://pkg.go.dev/golang.org/x/crypto) - 加密原语

## 高级功能

### NAT 类型探测

无需外部 STUN 服务器，通过已连接的节点探测本机 NAT 类型：

```go
// 连接至少一个节点后调用
natType, err := tunnel.ProbeNAT()
if err == nil {
    fmt.Printf("NAT 类型: %s\n", natType) // "Full Cone", "Port Restricted", "Symmetric" 等
}
```

**NAT 类型与打洞策略**：

| 本机 NAT | 对端 NAT | 推荐策略 |
|----------|----------|----------|
| 公网/全锥 | 公网/全锥 | UDP 直连 |
| 锥形 | 锥形 | UDP 打洞 |
| 锥形 | 端口受限 | UDP 打洞 + TCP 备选 |
| 对称型 | 锥形 | TCP 同时打开 |
| 对称型 | 对称型 | 端口预测 + 中继回退 |

### 指标监控

获取节点运行指标：

```go
metrics := tunnel.GetMetrics()
fmt.Printf("活跃连接: %d\n", metrics.ConnectionsActive)
fmt.Printf("发送流量: %d bytes\n", metrics.BytesSent)
fmt.Printf("打洞成功率: %.2f%%\n", metrics.PunchSuccessRate * 100)
```

### 日志配置

默认静默模式（适合嵌入其他应用）：

```go
// 默认不输出日志
tunnel, _ := api.NewTunnel(api.WithPassword("secret"))

// 启用日志
import "github.com/cykyes/tenet/log"

tunnel, _ := api.NewTunnel(
    api.WithPassword("secret"),
    api.WithLogger(log.NewStdLogger(
        log.WithLevel(log.LevelDebug), // Debug/Info/Warn/Error
        log.WithPrefix("[myapp]"),
    )),
)
```

### 跨平台编译

**Linux x64**:
```bash
GOOS=linux GOARCH=amd64 go build -o build/app_linux_amd64 ./examples/basic
```

**Android ARM64**:
```bash
GOOS=android GOARCH=arm64 go build -o build/app_android_arm64 ./examples/basic
```

**Windows x64**:
```bash
GOOS=windows GOARCH=amd64 go build -o build/app_windows_amd64.exe ./examples/basic
```

## 参考
- [Easytier](https://github.com/EasyTier/EasyTier) - 去中心化组网