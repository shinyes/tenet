# Tenet - P2P 加密隧道库

一个去中心化 P2P 加密隧道库，可作为插件内嵌到其他 Go 程序中，帮助不同程序实例之间建立去中心化加密网络。支持 Windows、Linux 和 Android 平台。

## 特性

- 🔒 **端到端加密**：使用 Noise Protocol 框架（与 WireGuard 相同）
- 🔑 **密码组网**：相同密码的节点自动组成私有网络（密码必须配置）
- 🕳️ **NAT 穿透**：支持 TCP (Simultaneous Open) 与 UDP 并行打洞，智能选择最佳链路
- ⚡ **TCP 优先**：UDP 快速握手后自动升级至抗 QoS 的 TCP 通道
- 🔄 **自动中继**：打洞失败时自动选择延迟最低的节点进行中继传输
- 🌐 **节点发现**：新节点连接后自动介绍给网络中的其他节点
- 🔍 **NAT 类型探测**：无需外部 STUN 服务器，通过已连接节点探测 NAT 类型
- 💓 **心跳保活**：定期心跳检测连接状态，超时自动断开并重连
- 🛡️ **中继认证**：中继转发需要认证，防止滥用
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
    // 1. 创建节点（密码必须配置）
    tunnel, err := api.NewTunnel(
        api.WithPassword("my-secret-key"), // 必须：相同密码的节点才能互联
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
        tunnel.Send(peerID, []byte("Hello Tenet!"))
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
go run examples/basic/main.go -l 1231 -secret "mysecret"
```

**节点 B**（主动连接节点 A）:
```bash
go run examples/basic/main.go -l 1232 -secret "mysecret" -p "127.0.0.1:1231"
```

**启用详细日志**（添加 `-v` 参数）:
```bash
go run examples/basic/main.go -l 1232 -secret "mysecret" -p "127.0.0.1:1231" -v
```

**配置中继节点**:
```bash
# 中继服务器（开启中继服务供其他节点使用）
go run examples/basic/main.go -l 9000 -secret "mysecret"

# 普通节点（指定中继节点地址，打洞失败时自动使用）
go run examples/basic/main.go -l 1232 -secret "mysecret" -relay "1.2.3.4:9000"
```
> 所有节点默认启用中继功能。当两个节点无法直连时，会自动通过已连接的中继节点进行数据转发，并优先选择延迟最低的节点。

### 编译示例

**Windows**:
```bash
go build -o build\basic.exe .\examples\basic
```

连接建立后，使用 `peers` 命令可查看当前链路状态：
```
> peers
已连接节点:
  1. 8cac1d66... [tcp/p2p]
```
- `tcp/udp` 表示传输协议（TCP 抗阻塞，优先使用）
- `p2p/relay` 表示链路模式（直连或中继）

## API 参考

### Tunnel

| 方法 | 说明 |
|------|------|
| `NewTunnel(opts...)` | 创建隧道实例 |
| `Start()` | 启动隧道服务（UDP & TCP 监听） |
| `Stop()` | 停止服务 |
| `GracefulStop()` | 优雅关闭（通知对端后再断开） |
| `Connect(addr)` | 连接对等节点（同时尝试 TCP/UDP 打洞） |
| `Send(peerID, data)` | 发送数据 |
| `OnReceive(handler)` | 设置接收回调 |
| `OnPeerConnected(handler)` | 节点连接回调 |
| `OnPeerDisconnected(handler)` | 节点断开回调 |
| `LocalID()` | 获取本地节点 ID |
| `LocalAddr()` | 获取本地监听地址 |
| `Peers()` | 获取已连接节点列表 |
| `PeerTransport(peerID)` | 获取传输协议（tcp/udp） |
| `PeerLinkMode(peerID)` | 获取链路模式（p2p/relay） |
| `ProbeNAT()` | 探测本机 NAT 类型 |
| `GetMetrics()` | 获取指标快照 |

### 配置选项

| 选项 | 说明 |
|------|------|
| `WithPassword(pwd)` | **必须**：网络密码（相同密码的节点才能互联） |
| `WithListenPort(port)` | 监听端口（0 = 随机分配） |
| `WithEnableHolePunch(bool)` | 启用 NAT 打洞（默认开启） |
| `WithEnableRelay(bool)` | 启用中继服务（默认开启） |
| `WithRelayNodes(addrs)` | 预设中继节点地址列表 |
| `WithMaxPeers(n)` | 最大连接数（默认 50） |
| `WithHeartbeatInterval(d)` | 心跳发送间隔（默认 5 秒） |
| `WithHeartbeatTimeout(d)` | 心跳超时时间（默认 30 秒） |
| `WithLogger(logger)` | 设置日志记录器（默认静默） |
| `WithIdentityPath(path)` | 身份文件路径（持久化节点 ID） |

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

### 连接流程

1. **启动**：同时监听 UDP 和 TCP 端口（使用 `SO_REUSEADDR` 共享端口）
2. **连接**：可主动连接其他节点，也可等待其他节点来连接
3. **打洞**：
   - 同时尝试 **UDP 打洞** 和 **TCP Simultaneous Open**
   - 使用相同端口，最大化穿透成功率
4. **握手**：使用 Noise Protocol (XX 模式) 进行双向身份验证
   - 密码作为 Prologue 参与认证，密码不同则握手失败
5. **传输升级**：
   - UDP 握手成功后，后台继续尝试 TCP
   - TCP 握手成功后，自动升级至 TCP 通道（抗 QoS）
6. **中继回退**：打洞失败时，选择延迟最低的已连接节点进行中继
7. **节点发现**：新节点连接后自动交换已知节点列表
8. **心跳保活**：每 5 秒发送心跳，30 秒无响应则断开并尝试重连

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

| 类型 | 值 | 说明 |
|------|:---:|------|
| Handshake | 0x01 | Noise 握手 |
| Data | 0x02 | 加密数据 |
| Relay | 0x03 | 中继封装 |
| DiscoveryReq | 0x04 | 节点发现请求 |
| DiscoveryResp | 0x05 | 节点发现响应 |
| Heartbeat | 0x06 | 心跳请求 |
| HeartbeatAck | 0x07 | 心跳响应 |

## 依赖

- [flynn/noise](https://github.com/flynn/noise) - Noise Protocol 框架
- [golang.org/x/crypto](https://pkg.go.dev/golang.org/x/crypto) - 加密原语
- [golang.org/x/sys](https://pkg.go.dev/golang.org/x/sys) - 系统调用（Windows SO_REUSEADDR）

## 构建要求

- Go 1.21+

## 许可

MIT License