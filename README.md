# Tenet - P2P 加密隧道库

Tenet 是一个可嵌入 Go 应用的去中心化 P2P 加密通信库，适合做多实例间的安全互联。

- 当前版本：`v2.0.4`（2026-02-17）
- 变更记录：`CHANGELOG.md`
- 详细指南：`USER_GUIDE.md`

## 30 秒了解

Tenet 默认帮你处理：

- Noise 端到端加密
- UDP/TCP/KCP 多传输协同
- NAT 穿透与中继回退
- 断线自动重连
- 解密失步后的 fast re-handshake 快速恢复
- 频道级消息隔离（应用层）

## 1 分钟跑起来（CLI）

```bash
# 节点 A
go run ./cmd/basic -l 1231 -s "mysecret" -channel ops

# 节点 B（连接 A）
go run ./cmd/basic -l 1232 -s "mysecret" -channel ops -connect "127.0.0.1:1231"
```

## 最小接入（Go）

```bash
go get github.com/shinyes/tenet
```

```go
package main

import (
	"log"

	"github.com/shinyes/tenet/api"
	tlog "github.com/shinyes/tenet/log"
)

func main() {
	tunnel, err := api.NewTunnel(
		api.WithPassword("my-secret-key"), // 必填：同密码才互联
		api.WithChannelID("ops"),          // 业务频道
		api.WithListenPort(0),             // 0=随机端口
		api.WithLogger(tlog.NewStdLogger()),
	)
	if err != nil {
		log.Fatal(err)
	}

	tunnel.OnReceive(func(peerID string, data []byte) {
		log.Printf("recv from %s: %s", peerID, string(data))
	})

	if err := tunnel.Start(); err != nil {
		log.Fatal(err)
	}
	defer tunnel.GracefulStop()

	// 可选：主动连接一个已知节点
	// _ = tunnel.Connect("127.0.0.1:1231")

	select {}
}
```

## 快速使用路径（推荐顺序）

1. 用 `WithPassword` + `WithChannelID` 初始化 `Tunnel`
2. 注册 `OnReceive`
3. `Start()`
4. `Connect(...)`（或等待被动接入）
5. 使用 `Send(channel, peerID, data)` / `Broadcast(channel, data)`

## 三个必须知道的规则

### 1) 密码不一致一定握手失败

`WithPassword` 是私网边界。

### 2) 频道是业务隔离边界

- 发送必须指定频道
- 接收端未订阅该频道会丢弃消息

### 3) 业务数据只走可靠通道（TCP/KCP）

当前版本不会回退裸 UDP 发送业务数据。

## 快速恢复机制（fast re-handshake）

当出现高频 `cipher: message authentication failed`（常见于会话失步）时，默认流程：

1. 连续解密失败达到阈值后触发 fast re-handshake
2. 直接在当前链路发起重握手（不必先完整 reconnect）
3. 按退避和窗口限次重试
4. 达到失败阈值后才回退 reconnect

常用调优项：

```go
api.WithMaxConsecutiveDecryptFailures(16)
api.WithFastRehandshakeBackoff(500*time.Millisecond, 8*time.Second)
api.WithFastRehandshakeWindow(30*time.Second, 6)
api.WithFastRehandshakeFailThreshold(3)
api.WithFastRehandshakePendingTTL(30*time.Second)
```

## 常用配置速查

| 配置项 | 默认值 | 用途 |
|---|---:|---|
| `WithPassword(pwd)` | - | 必填，同密码互联 |
| `WithChannelID(name)` | 空 | 加入业务频道 |
| `WithListenPort(port)` | `0` | 监听端口，0=随机 |
| `WithEnableHolePunch(bool)` | `true` | NAT 打洞开关 |
| `WithEnableRelay(bool)` | `true` | 中继回退开关 |
| `WithRelayNodes(addrs)` | 空 | 预置中继节点列表 |
| `WithMaxPeers(n)` | `50` | 最大连接数 |
| `WithDialTimeout(d)` | `10s` | 建链超时 |
| `WithEnableRelayAuth(bool)` | `true` | 中继认证开关 |
| `WithEnableReconnect(bool)` | `true` | 自动重连开关 |
| `WithReconnectMaxRetries(n)` | `10` | 最大重试次数（0=无限） |
| `WithReconnectBackoff(initial,max,multiplier)` | `1s / 5m / 2.0` | 重连退避参数 |
| `WithMaxConsecutiveDecryptFailures(n)` | `16` | 连续解密失败阈值 |
| `WithFastRehandshakeBackoff(base,max)` | `500ms / 8s` | fast re-handshake 退避区间 |
| `WithFastRehandshakeWindow(window,maxAttempts)` | `30s / 6` | fast re-handshake 时间窗限次 |
| `WithFastRehandshakeFailThreshold(n)` | `3` | 失败后回退 reconnect 的阈值 |
| `WithFastRehandshakePendingTTL(ttl)` | `30s` | pending 握手状态保留时间 |
| `WithKCPConfig(cfg)` | 默认 | KCP 参数模板 |
| `WithIdentityJSON(json)` | 自动生成 | 指定节点身份 |
| `WithLogger(logger)` | NopLogger | 日志实现（默认静默） |

## 常用配置详解

### 基础项

- `WithPassword(pwd)`  
用途：私网隔离边界。只有密码一致的节点才能完成握手。  
默认值：空字符串（必填，不可省略）。  
建议：生产环境使用高熵随机字符串，避免硬编码在仓库。

- `WithChannelID(name)`  
用途：本地声明“我订阅哪些业务频道”。  
默认值：空（不订阅任何频道）。  
建议：按业务域命名（如 `orders`、`chat`），避免把所有消息都放在同一频道。

- `WithListenPort(port)`  
用途：本地监听端口。`0` 表示系统自动分配。  
默认值：`0`。  
建议：服务端/固定入口使用固定端口；客户端或临时节点可用 `0`。


### 网络与安全（进阶）

- `WithEnableHolePunch(bool)`  
用途：是否启用 NAT 打洞。  
默认值：`true`。  
建议：跨 NAT 通信通常保持开启。

- `WithEnableRelay(bool)`  
用途：直连失败时是否允许回退到中继。  
默认值：`true`。  
建议：公网复杂网络建议开启，优先保障可达性。

- `WithRelayNodes(addrs)`  
用途：指定预置中继节点（`host:port`）。  
默认值：空列表。  
建议：生产环境建议配置至少 1~2 个稳定中继。

- `WithMaxPeers(n)`  
用途：限制单节点最大并发对端连接数。  
默认值：`50`。  
建议：按机器资源与业务规模调整，避免无界增长。

- `WithDialTimeout(d)`  
用途：控制主动建链超时。  
默认值：`10s`。  
建议：低延迟内网可缩短，公网弱网可适度增大。

- `WithEnableRelayAuth(bool)`  
用途：是否启用中继认证。  
默认值：`true`。  
建议：生产环境不要关闭。

### 连接与重连

- `WithEnableReconnect(bool)`  
用途：异常断链后是否自动重连。  
默认值：`true`。  
建议：生产通常保持 `true`。

- `WithReconnectMaxRetries(n)`  
用途：限制重连次数，`0` 为无限重试。  
默认值：`10`。  
建议：服务端常设 `0` 或较大值；一次性任务可设小值，避免无限等待。

- `WithReconnectBackoff(initialDelay, maxDelay, multiplier)`  
用途：设置重连指数退避参数。  
默认值：`1s / 5m / 2.0`。  
建议：公网弱网可适当增大 `initialDelay` 和 `maxDelay`，避免重试风暴。

重连配置校验规则（`WithReconnectConfig` / `WithReconnectBackoff`）：

- `MaxRetries >= 0`（`0` 表示无限重试）
- `InitialDelay > 0`
- `MaxDelay > 0` 且 `MaxDelay >= InitialDelay`
- `BackoffMultiplier >= 1`
- `JitterFactor` 必须在 `[0, 1]`
- `ReconnectTimeout > 0`

若不满足上述条件，`api.NewTunnel(...)` 会返回配置错误。

### 传输、身份与日志

- `WithKCPConfig(cfg)`  
用途：自定义 KCP 参数。  
默认值：使用内置默认 KCP 参数（传 `nil`）。  
建议：先使用默认值，只有在明确瓶颈后再调优。

- `WithIdentityJSON(json)`  
用途：显式指定节点身份。  
默认值：不指定时自动生成临时身份。  
建议：需要稳定节点 ID 时务必显式持久化并加载身份。

- `WithLogger(logger)`  
用途：接入自定义日志实现。  
默认值：`NopLogger`（静默）。  
建议：联调阶段建议开启标准输出日志，生产接入统一日志系统。

### 解密失败与快速恢复

- `WithMaxConsecutiveDecryptFailures(n)`  
用途：连续解密失败阈值，达到后触发 fast re-handshake。  
默认值：`16`。  
调参原则：  
`n` 太小会在短暂抖动时误触发；太大则恢复变慢。

- `WithFastRehandshakeBackoff(base,max)`  
用途：fast re-handshake 的指数退避区间。  
默认值：`500ms / 8s`。  
调参原则：  
低延迟内网可缩短（更快恢复）；公网弱网可适当放大（降低抖动风暴）。

- `WithFastRehandshakeWindow(window,maxAttempts)`  
用途：时间窗内限次，防止高频重握手放大故障。  
默认值：`30s / 6`。  
调参原则：  
窗口越短、次数越小，保护性越强；但恢复尝试也更保守。

- `WithFastRehandshakeFailThreshold(n)`  
用途：连续失败多少次后，放弃 fast re-handshake，回退 reconnect。  
默认值：`3`。  
调参原则：  
弱网可略增大；链路稳定但节点偶发异常时可保持默认。

- `WithFastRehandshakePendingTTL(ttl)`  
用途：待完成握手状态的保留时间。  
默认值：`30s`。  
调参原则：  
跨地域高 RTT 可适当增大；本地网络可保持默认。

### 推荐模板

低延迟内网（追求恢复速度）：

```go
api.WithMaxConsecutiveDecryptFailures(8)
api.WithFastRehandshakeBackoff(200*time.Millisecond, 3*time.Second)
api.WithFastRehandshakeWindow(15*time.Second, 8)
api.WithFastRehandshakeFailThreshold(4)
api.WithFastRehandshakePendingTTL(20*time.Second)
```

公网弱网（追求稳定性）：

```go
api.WithMaxConsecutiveDecryptFailures(24)
api.WithFastRehandshakeBackoff(800*time.Millisecond, 12*time.Second)
api.WithFastRehandshakeWindow(45*time.Second, 5)
api.WithFastRehandshakeFailThreshold(4)
api.WithFastRehandshakePendingTTL(45*time.Second)
```

## 排错速查

| 现象 | 常见原因 | 处理建议 |
|---|---|---|
| `节点未订阅频道` | 发送方本地未加入该频道 | 初始化时加 `WithChannelID(channel)` |
| `reliable data transport unavailable` | 当前只有 UDP 且 KCP 不可用 | 确保 TCP/KCP 可用 |
| `too many tcp sessions, reject` | 入站 TCP 并发会话达到上限 | 降低并发接入速度，或拆分入口节点 |
| 高频 `cipher: message authentication failed` | 消息风暴/乱序导致会话失步 | 先止血应用回环，再调 fast re-handshake 参数 |
| `Broadcast` 返回成功但业务未收到 | 接收端频道过滤 | 确认接收端订阅了同频道 |

## 关键 API

- 生命周期：`Start` / `Stop` / `GracefulStop`
- 连接管理：`Connect` / `Peers` / `PeerCount`
- 消息收发：`Send` / `Broadcast` / `OnReceive`
- 链路观察：`PeerTransport` / `PeerLinkMode`
- 诊断：`GetMetrics` / `ProbeNAT` / `GetNATType`

## 开发与测试

```bash
go test ./...
go test ./... -race
# TCP 并发短连/慢连压测（本地）
go run ./cmd/stress -total 3000 -concurrency 600 -hold 1500ms
```

## 项目结构

- `api/`：对外 `Tunnel` API
- `node/`：连接、收发、重连、恢复逻辑
- `peer/`：对端状态模型
- `transport/`：传输层实现
- `nat/`：打洞、中继、NAT 探测
- `crypto/`：身份、握手、会话加密
- `metrics/`：指标采集

## 许可证

本项目基于 `GNU General Public License v3.0`，见 `LICENSE`。
