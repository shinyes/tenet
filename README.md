# Tenet - P2P 加密隧道库

一个去中心化 P2P 加密隧道库，可作为插件内嵌到其他 Go 程序中，帮助不同程序实例之间建立去中心化加密网络。支持 Windows、Linux、macOS 和 Android 平台。

## 特性

- 📦 **自动分包**：支持任意大小数据发送（透明分片与重组）
- 🆔 **灵活身份**：支持 JSON 格式的可移植身份凭证
- 🔒 **端到端加密**：使用 Noise Protocol 框架（与 WireGuard 相同）
- 🔑 **密码组网**：相同密码的节点自动组成私有网络（密码必须配置）
- 🔒 **多频道隐私**：支持多频道订阅，频道名哈希隐藏，实现“暗网”式隔离
- 🕳️ **NAT 穿透**：支持 TCP (Simultaneous Open) 与 UDP 并行打洞，智能选择最佳链路
- ⚡ **TCP 优先**：UDP 快速握手后自动升级至抗 QoS 的 TCP 通道
- 🔄 **自动中继**：打洞失败时自动选择延迟最低的节点进行中继传输
- 🛣️ **多路复用**：KCP、NAT 打洞与 TENT 协议共享单一 UDP 端口 (Single Socket)
- 🌐 **IPv6 双栈**：支持 IPv4/IPv6 双栈监听与连接
- 🔍 **节点发现**：新节点连接后自动介绍给网络中的其他节点
- 🔍 **NAT 类型探测**：无需外部 STUN 服务器，通过已连接节点探测 NAT 类型
- 💓 **心跳保活**：定期心跳检测连接状态，超时自动断开
- 🔁 **自动重连**：断线后自动重连，支持指数退避和回调通知
- 🛡️ **中继认证**：中继转发需要认证（Ed25519/HMAC），防止滥用
- 📊 **指标监控**：内置连接、流量、打洞成功率、重连等统计
- 🚀 **KCP 可靠传输**：内置 KCP 协议层，提供低延迟可靠 UDP 传输
- 🌍 **跨平台**：支持 Windows、Linux、macOS、Android

## 快速开始

### 安装

```bash
go get github.com/shinyes/tenet
```

### 构建环境

- Go 版本：1.25.5

### 基础调用

```go
package main

import (
    "fmt"
    "log"
    "github.com/shinyes/tenet/api"
    tlog "github.com/shinyes/tenet/log"
)

func main() {
    // 1. 创建节点（密码必须配置）
    tunnel, err := api.NewTunnel(
        api.WithPassword("my-secret-key"), // 必须：相同密码的节点才能互联
        api.WithChannelID("ops"),         // 加入频道（可多次调用加入多个频道）
        api.WithListenPort(0),             // 0 = 随机端口
        // 可选：启用日志输出（默认静默，适合嵌入其他应用）
        api.WithLogger(tlog.NewStdLogger(tlog.WithLevel(tlog.LevelInfo))),
    )
    if err != nil {
        log.Fatalf("创建隧道失败: %v", err)
    }

    // 2. 注册回调
    shortID := func(id string) string {
        if len(id) > 8 {
            return id[:8]
        }
        return id
    }

    tunnel.OnReceive(func(peerID string, data []byte) {
        log.Printf("收到消息 [%s]: %s", shortID(peerID), string(data))
    })

    tunnel.OnPeerConnected(func(peerID string) {
        log.Printf("新连接: %s", shortID(peerID))
        // 连接成功后发送问候
        // 发送消息到指定频道的节点
        tunnel.Send("my-channel", peerID, []byte("Hello Tenet!"))

        // 向频道内所有节点广播消息
        count, err := tunnel.Broadcast("my-channel", []byte("Broadcast message"))
        fmt.Printf("消息已发送给 %d 个节点\n", count)
    })

    tunnel.OnPeerDisconnected(func(peerID string) {
        log.Printf("节点断开: %s", shortID(peerID))
    })

    // 3. 启动
    if err := tunnel.Start(); err != nil {
        log.Fatalf("启动失败: %v", err)
    }
    defer tunnel.GracefulStop() // 优雅关闭

    fmt.Printf("本地节点 ID: %s\n", tunnel.LocalID())
    fmt.Printf("本地监听地址: %s\n", tunnel.LocalAddr())
    
    // 主动连接其他节点（可选，也可等待其他节点来连接）
    // tunnel.Connect("1.2.3.4:9000")
    
    select {} // 阻塞运行
}
```

### 运行示例

提供了完整的命令行示例程序，位于 `examples/basic`。

**节点 A**（等待连接）:
```bash
go run examples/basic/main.go -l 1231 -s "mysecret" -channel ops
```

**节点 B**（主动连接节点 A）:
```bash
go run examples/basic/main.go -l 1232 -s "mysecret" -channel ops -connect "127.0.0.1:1231"
```

**启用详细日志**（添加 `-d` 参数）:
```bash
go run examples/basic/main.go -l 1232 -s "mysecret" -channel dev -connect "127.0.0.1:1231" -d
```

**配置中继节点**:
```bash
# 中继服务器（开启中继服务供其他节点使用）
go run examples/basic/main.go -l 9000 -s "mysecret"

# 普通节点（指定中继节点地址，打洞失败时自动使用）
go run examples/basic/main.go -l 1232 -s "mysecret" -channel ops -relay "1.2.3.4:9000"
```
> 所有节点默认启用中继功能。当两个节点无法直连时，会自动通过已连接的中继节点进行数据转发，并优先选择延迟最低的节点。

### 自动分包 (Large Data Transfer)
- **切片传输**: 大于 60KB 的数据会自动被切分为多个分片。
- **重组缓冲区**: 接收端自动重组，单次传输支持上限 50MB（默认，可在 `MaxReassemblySize` 调整）。
- **通道锁定**: 传输过程中自动锁定可靠通道（TCP/KCP），防止链路切换导致的乱序和解密失败。

## API 参考

### Tunnel

| 方法 | 说明 |
|------|------|
| `NewTunnel(opts...)` | 创建隧道实例 |
| `Start()` | 启动隧道服务（UDP & TCP 监听） |
| `Stop()` | 停止服务 |
| `GracefulStop()` | 优雅关闭（通知对端后再断开） |
| `Connect(addr)` | 连接对等节点（同时尝试 TCP/UDP 打洞） |
| `Send(channel, peerID, data)` | 发送数据到指定频道的节点（支持大文件自动分片） |
| `Broadcast(channel, data)` | 向指定频道内所有节点广播数据（返回成功发送的节点数） |
| `OnReceive(handler)` | 设置接收回调 |
| `OnPeerConnected(handler)` | 节点连接回调 |
| `OnPeerDisconnected(handler)` | 节点断开回调 |
| `LocalID()` | 获取本地节点 ID |
| `LocalAddr()` | 获取本地监听地址 |
| `Peers()` | 获取已连接节点列表 |
| `PeerTransport(peerID)` | 获取传输协议（tcp/udp） |
| `PeerLinkMode(peerID)` | 获取链路模式（p2p/relay） |
| `ProbeNAT()` | 探测本机 NAT 类型 |
| `GetNATType()` | 获取已探测的 NAT 类型 |
| `GetMetrics()` | 获取指标快照 |

### 配置选项

| 选项 | 说明 |
|------|------|
| `WithPassword(pwd)` | **必须**：网络密码（相同密码的节点才能互联） |
| `WithChannelID(name)` | 加入指定频道（可多次调用以加入多个频道，不同频道完全隔离） |
| `WithListenPort(port)` | 监听端口 (UDP/TCP 复用, 0 = 随机分配) |
| `WithEnableHolePunch(bool)` | 启用 NAT 打洞（默认开启） |
| `WithEnableRelay(bool)` | 启用中继服务（默认开启） |
| `WithRelayNodes(addrs)` | 预设中继节点地址（可选，已连接节点会自动注册为候选中继） |
| `WithMaxPeers(n)` | 最大连接数（默认 50） |
| `WithHeartbeatInterval(d)` | 心跳发送间隔（默认 5 秒） |
| `WithHeartbeatTimeout(d)` | 心跳超时时间（默认 30 秒） |
| `WithLogger(logger)` | 设置日志记录器（默认静默） |
| `WithIdentityJSON(json)` | 设置节点身份（可从 GetIdentityJSON 获取） |
| `WithEnableReconnect(bool)` | 启用自动重连（默认开启） |
| `WithReconnectMaxRetries(n)` | 最大重连次数（默认 10，0=无限） |
| `WithReconnectBackoff(init, max, mult)` | 重连退避参数 |

### 重连回调

| 方法 | 说明 |
|------|------|
| `OnReconnecting(handler)` | 开始重连尝试时触发 |
| `OnReconnected(handler)` | 重连成功时触发 |
| `OnGaveUp(handler)` | 达到最大重试次数放弃时触发 |
| `GetReconnectingPeers()` | 获取正在重连的节点列表 |
| `CancelReconnect(peerID)` | 取消指定节点的重连 |

## 项目结构

```
tenet/
├── api/                  # 公共 API（用户主要使用的接口）
│   └── tunnel.go         # Tunnel 接口及配置选项
│
├── node/                 # 核心节点实现
│   ├── node.go           # Node 主结构和核心逻辑
│   ├── config.go         # 配置定义和验证
│   ├── handshake.go      # 握手处理
│   ├── heartbeat.go      # 心跳检测与断线处理
│   ├── discovery.go      # 节点发现协议
│   ├── reconnect.go      # 自动重连机制（指数退避）
│   ├── relay_handler.go  # 中继转发处理
│   ├── kcp.go            # KCP 可靠传输管理器
│   └── kcp_transport.go  # KCP 传输层集成
│
├── crypto/               # 加密模块
│   ├── identity.go       # 节点身份（Ed25519/Curve25519 密钥）
│   ├── noise.go          # Noise Protocol (XX 模式)
│   └── session.go        # 加密会话管理
│
├── nat/                  # NAT 穿透模块
│   ├── holepunch.go      # UDP 打洞
│   ├── tcp_punch.go      # TCP Simultaneous Open
│   ├── probe.go          # NAT 类型探测
│   ├── relay.go          # 中继节点管理与评分
│   ├── relay_auth.go     # 中继认证（Ed25519/HMAC）
│   └── nat.go            # NAT 类型定义
│
├── peer/                 # 对等节点管理
│   └── peer.go           # Peer 结构和 PeerStore
│
├── transport/            # 传输层抽象
│   ├── transport.go      # Transport 接口
│   └── socket*.go        # 跨平台 Socket 配置（SO_REUSEADDR）
│
├── metrics/              # 指标监控
│   └── metrics.go        # 统计收集器（原子操作，线程安全）
│
├── log/                  # 日志接口
│   └── logger.go         # Logger 接口（NopLogger/StdLogger）
│
├── internal/             # 内部实现（不对外暴露）
│   ├── pool/             # 高效缓冲池（sync.Pool）
│   └── protocol/         # 协议编解码工具
│
├── examples/             # 示例代码
│   ├── basic/            # 命令行基础示例
│   └── large_transfer/   # 大文件分片传输测试示例
│
├── build/                # 构建产物
└── doc.go                # 包文档
```

## 工作原理

### 频道隔离

Tenet 实现了严格的**频道隔离**机制，确保不同频道之间消息完全隔离：

1. **频道订阅**：节点启动时或运行中可加入任意频道（使用 `WithChannelID` 或 `JoinChannel`）
2. **频道同步**：连接建立后，节点自动同步本地频道列表给对端
3. **消息发送**：发送消息时必须指定频道，帧内携带 32 字节 SHA256 频道 Hash
4. **接收过滤**：接收端严格验证频道 Hash，只有本地订阅的频道消息才能被接收
5. **完全隔离**：不同频道的节点可以建立物理连接，但无法通信

```
频道 A [ops]:    Node1 <──────> Node2
频道 B [dev]:             Node3

✅ Node1 与 Node2 可通信（同频道 ops）
✅ Node2 与 Node3 可建立物理连接（同一网络）
❌ Node1 与 Node3 无法通信（不同频道）
```

### 连接流程

1. **启动**：
   - 监听单一端口 (IPv4/IPv6 双栈)
   - 启用 UDP 多路复用 (UDPMux)，同时处理 KCP、打洞和控制协议
   - TCP 自动绑定到相同端口
2. **连接**：可主动连接其他节点，也可等待其他节点来连接
3. **打洞**：
   - 同时尝试 **UDP 打洞** 和 **TCP Simultaneous Open**
   - 使用相同端口，最大化穿透成功率
4. **握手**：使用 Noise Protocol (XX 模式) 进行双向身份验证
   - 密码作为 Prologue 参与认证，密码不同则握手失败
5. **传输升级**：
   - KCP 提供快速可靠的 UDP 传输
   - TCP 握手成功后，自动升级至 TCP 通道（抗 QoS）
   - **平滑切换**: 引入 `PreviousSession` 机制，升级瞬间的“在途”旧包仍可被正确解密。
6. **通道锁定**: 发送大包时自动锁定最优传输通道，防止中继或升级导致的 Nonce 乱序。
7. **中继回退**：打洞失败时，选择延迟最低的已连接节点进行中继
8. **节点发现**：新节点连接后自动交换已知节点列表
9. **心跳保活**：每 5 秒发送心跳，30 秒无响应则断开并尝试重连

### 节点发现

当 A↔B 和 B↔C 已连接时：
1. C 向 B 发送 **DiscoveryRequest** 请求已知节点列表
2. B 返回 **DiscoveryResponse** 包含 A 的 PeerID 和地址
3. C 自动尝试连接 A，无需手动配置

```
      A ←────────→ B ←────────→ C
      ↑                         │
      └─────── 自动发现 ─────────┘
```

## 平台支持

| 平台 | 架构 | 状态 |
|------|------|:----:|
| Windows | x64, ARM64 | ✅ |
| Linux | x64, ARM64 | ✅ |
| macOS | x64, ARM64 | ✅ |
| Android | ARM64, ARM | ✅ |

### 跨平台编译

```bash
# Linux x64
GOOS=linux GOARCH=amd64 go build -o build/app_linux_amd64 ./examples/basic

# Android ARM64
GOOS=android GOARCH=arm64 go build -o build/app_android_arm64 ./examples/basic

# Windows x64
GOOS=windows GOARCH=amd64 go build -o build/app_windows_amd64.exe ./examples/basic
```

## 协议格式

### 网络层帧类型

| 类型 | 值 | 说明 |
|------|:---:|------|
| Handshake | 0x01 | Noise 握手 |
| Data | 0x02 | 加密数据 |
| Relay | 0x03 | 中继封装 |
| DiscoveryReq | 0x04 | 节点发现请求 |
| DiscoveryResp | 0x05 | 节点发现响应 |
| Heartbeat | 0x06 | 心跳请求 |
| HeartbeatAck | 0x07 | 心跳响应 |

### 应用层帧类型

| 类型 | 值 | 说明 |
|------|:---:|------|
| ChannelUser | 0x00 | 频道用户数据 [ChannelHash(32)] [UserData] |
| ChannelUpdate | 0x01 | 频道更新控制帧 [OpCode(1)] [ChannelHash(32)] |

## 依赖

- [flynn/noise](https://github.com/flynn/noise) - Noise Protocol 框架
- [xtaci/kcp-go](https://github.com/xtaci/kcp-go) - KCP 可靠 UDP 传输
- [golang.org/x/crypto](https://pkg.go.dev/golang.org/x/crypto) - 加密原语
- [golang.org/x/sys](https://pkg.go.dev/golang.org/x/sys) - 系统调用（SO_REUSEADDR）

## 构建要求

- Go 1.25

## 开发

```bash
# 运行所有测试
go test ./... -v

# 测试覆盖率
go test ./... -cover

# 构建示例
go build -o build/basic ./examples/basic
```

## 开源协议

本项目采用 [GNU General Public License v3.0](LICENSE) 开源协议。
