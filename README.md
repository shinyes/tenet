# Tenet - P2P 加密隧道库

Tenet 是一个可嵌入 Go 应用的去中心化 P2P 加密通信库，目标是让多实例之间在复杂网络环境下建立稳定、可观测、可隔离的安全通道。

## 版本信息

- 当前版本：`v1.1.1`（2026-02-14）
- 变更记录：`CHANGELOG.md`
- 使用手册：`USER_GUIDE.md`

## 特性概览

- 端到端加密：基于 Noise Protocol（XX）
- 频道隔离：应用层按频道订阅严格过滤
- 多传输协同：UDP / TCP / KCP
- NAT 穿透：UDP 打洞 + TCP Simultaneous Open
- 中继回退：直连失败自动选择可用中继
- 自动重连：断线后指数退避重试
- 大消息传输：自动分片 + 重组
- 指标监控：连接、握手、收发统计

## v1.1.1 重点更新

- 修复并发竞态：`Session` / `Addr` / `LinkMode` 并发读写
- 修复逻辑问题：`processData` 在 `onReceive == nil` 时不再跳过频道更新帧
- 优化 TCP 并发：写锁从全局改为“按连接粒度”
- 广播兜底仍可配置：`EnableBroadcastFallback` 默认开启

---

## 安装

```bash
go get github.com/shinyes/tenet
```

## 最小接入示例

```go
package main

import (
	"log"

	"github.com/shinyes/tenet/api"
	tlog "github.com/shinyes/tenet/log"
)

func main() {
	tunnel, err := api.NewTunnel(
		api.WithPassword("my-secret-key"),       // 必填：同密码节点才能互联
		api.WithChannelID("ops"),                // 可多次调用，加入多个频道
		api.WithListenPort(0),                   // 0 表示随机端口
		api.WithEnableBroadcastFallback(true),   // 默认 true
		api.WithLogger(tlog.NewStdLogger(tlog.WithLevel(tlog.LevelInfo))),
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

	select {}
}
```

## 快速体验（CLI）

项目内置示例：`cmd/basic`

```bash
# 节点 A
go run ./cmd/basic -l 1231 -s "mysecret" -channel ops

# 节点 B（连接 A）
go run ./cmd/basic -l 1232 -s "mysecret" -channel ops -connect "127.0.0.1:1231"

# 开启调试日志
go run ./cmd/basic -l 1232 -s "mysecret" -channel ops -connect "127.0.0.1:1231" -d
```

---

## 核心概念

### 1) 网络密码（必填）

- `WithPassword(...)` 决定节点是否属于同一私有网络
- 密码不一致将导致握手失败

### 2) 频道（Channel）

- 发送接口：`Send(channel, peerID, data)` / `Broadcast(channel, data)`
- 接收端会检查频道订阅，未订阅频道的数据会被丢弃
- 频道是**通信隔离边界**，不是连接隔离边界

### 3) 广播兜底（EnableBroadcastFallback）

默认 `true`，广播行为如下：

1. 先查找“已知在该频道”的对端  
2. 若结果为空且开关为 `true`，退化为向所有已连接节点发送  
3. 接收端再按频道订阅过滤

关闭兜底（减少额外发送）：

```go
api.WithEnableBroadcastFallback(false)
```

---

## 连接与传输流程（简化）

1. 启动监听（UDP/TCP）
2. 主动连接或被动接入
3. NAT 打洞（UDP + TCP）
4. Noise 握手建立会话
5. 根据链路状态选择 TCP / KCP / UDP 发送
6. 心跳保活，异常断开后进入重连流程

---

## API 概览

### Tunnel 生命周期

- `Start() error`
- `Stop() error`
- `GracefulStop() error`

### 连接管理

- `Connect(addr string) error`
- `Peers() []string`
- `PeerCount() int`
- `PeerTransport(peerID string) string`（`tcp` / `udp`）
- `PeerLinkMode(peerID string) string`（`p2p` / `relay`）

### 消息收发

- `Send(channelName, peerID string, data []byte) error`
- `Broadcast(channelName string, data []byte) (int, error)`
- `OnReceive(handler func(peerID string, data []byte))`

### 事件回调

- `OnPeerConnected(handler func(peerID string))`
- `OnPeerDisconnected(handler func(peerID string))`

### NAT 与指标

- `ProbeNAT() (string, error)`
- `GetNATType() string`
- `GetMetrics() interface{}`

---

## 配置项说明（常用）

| 配置项 | 默认值 | 说明 |
|---|---:|---|
| `WithPassword(pwd)` | - | 必填，同密码才可互联 |
| `WithChannelID(name)` | 空 | 加入频道，可多次调用 |
| `WithListenPort(port)` | `0` | 监听端口，0=随机 |
| `WithEnableBroadcastFallback(bool)` | `true` | 频道未知时广播兜底 |
| `WithEnableHolePunch(bool)` | `true` | NAT 打洞开关 |
| `WithEnableRelay(bool)` | `true` | 中继开关 |
| `WithRelayNodes(addrs)` | 空 | 预置中继节点 |
| `WithMaxPeers(n)` | `50` | 最大连接数 |
| `WithDialTimeout(d)` | `10s` | 连接超时 |
| `WithEnableReconnect(bool)` | `true` | 自动重连开关 |
| `WithReconnectMaxRetries(n)` | `10` | 最大重试次数（0=无限） |
| `WithEnableRelayAuth(bool)` | `true` | 中继认证开关 |
| `WithKCPConfig(cfg)` | 默认 | KCP 参数 |
| `WithIdentityJSON(json)` | 自动生成 | 指定节点身份 |
| `WithLogger(logger)` | NopLogger | 日志实现 |

---

## 常见问题

### 已连接节点没加入频道，为什么看起来还能广播？

通常是广播兜底触发。发送端会先发出包，但接收端未订阅频道会丢弃，不会触发业务 `OnReceive`。

### `Broadcast` 返回成功数量，但业务端没收到？

返回值是“底层发送成功次数”，不是“业务回调次数”。业务回调仍受频道订阅过滤。

### `Send` 报“节点未订阅频道”？

发送方本地未加入该频道。请先 `WithChannelID(channel)` 或改用已订阅频道。

---

## 开发与测试

```bash
go test ./...
go test ./... -race
```

## 项目结构（简要）

- `api/`：对外 `Tunnel` API
- `node/`：节点核心逻辑（连接、收发、重连、频道）
- `peer/`：对等节点模型与状态
- `transport/`：传输层抽象与实现
- `nat/`：打洞、中继、NAT 探测
- `crypto/`：身份、握手、会话加密
- `metrics/`：指标采集

## 许可证

本项目基于 `GNU General Public License v3.0`，见 `LICENSE`。

