# Tenet 用户指南

本文面向使用 `github.com/shinyes/tenet` 的业务开发者，重点说明“怎么接入、怎么排查、怎么配置广播兜底”。

## 1. 最小接入流程

1. 创建 Tunnel（至少配置密码）
2. 注册回调（接收、连接、断开）
3. 启动节点
4. 与其他节点建立连接
5. 使用 `Send` / `Broadcast` 发送业务数据

示例：

```go
tunnel, err := api.NewTunnel(
	api.WithPassword("my-secret-key"),
	api.WithChannelID("ops"),
	api.WithListenPort(0),
)
if err != nil {
	return err
}

tunnel.OnReceive(func(peerID string, data []byte) {
	// 处理业务消息
})

if err := tunnel.Start(); err != nil {
	return err
}
defer tunnel.GracefulStop()
```

## 2. 频道机制（务必理解）

- 发送时必须显式指定频道：`Send(channel, peerID, data)` / `Broadcast(channel, data)`
- 发送端约束：本地节点必须已订阅该频道，否则 `Send` 会报错
- 接收端约束：只处理本地已订阅频道的数据，不符合会直接丢弃

这意味着：

- 即使网络层已连通，不在同频道也无法完成业务通信
- 频道是应用层隔离边界，不是连接层隔离边界

## 3. 广播兜底开关（`EnableBroadcastFallback`）

### 默认行为（推荐起步）

默认 `true`。当 `Broadcast(channel, ...)` 找不到“已知在该频道的对端”时，会临时退化为向所有已连接节点发送，再由接收端按频道过滤。

用途：

- 缓解连接刚建立、频道同步尚未完成时的“同频道广播数量为 0”

### 关闭方式

```go
tunnel, _ := api.NewTunnel(
	api.WithPassword("my-secret-key"),
	api.WithChannelID("ops"),
	api.WithEnableBroadcastFallback(false),
)
```

适用场景：

- 你更关注“减少无效发送”，并且可接受频道同步窗口内的广播丢失

## 4. 解密失败与快速恢复（重点）

### 典型症状

- 日志中高频出现 `cipher: message authentication failed`
- 某一侧开始连续解密失败，且短时间内无法自行恢复

### 当前库层恢复路径

1. 连续解密失败达到阈值（`WithMaxConsecutiveDecryptFailures`）后，触发 fast re-handshake  
2. 优先在当前链路立即重握手（不必先完整 reconnect）  
3. 若重握手失败，按退避和窗口限次继续尝试  
4. 仅当连续失败达到阈值后，才回退到 reconnect

### 数据通道约束

- 业务数据面仅走可靠有序通道（TCP/KCP）
- 不会回退到裸 UDP；若可靠通道不可用，会返回 `reliable data transport unavailable`

### 推荐配置

```go
api.WithMaxConsecutiveDecryptFailures(16)
api.WithFastRehandshakeBackoff(500*time.Millisecond, 8*time.Second)
api.WithFastRehandshakeWindow(30*time.Second, 6)
api.WithFastRehandshakeFailThreshold(3)
api.WithFastRehandshakePendingTTL(30*time.Second)
```

### 观测指标（`GetMetrics`）

- `FastRehandshakeAttempts`
- `FastRehandshakeSuccess`
- `FastRehandshakeFailed`

## 5. 常见问题

### Q1：已连接节点没加入频道，为什么看起来还能“转发”？

A：通常是广播兜底触发了“先发给所有连接节点”。但接收端会按频道校验，未订阅频道的数据不会进入 `OnReceive`。

### Q2：`Broadcast` 返回发送成功数量，但业务没有收到？

A：返回值表示“底层发送成功次数”，不等于“业务回调触发次数”。未订阅频道的节点会在接收端被过滤。

### Q3：`Send` 报“节点未订阅频道”？

A：发送方本地没有加入该频道。请先在初始化时加 `WithChannelID(channel)`，或切换为已订阅频道发送。

### Q4：如何判断当前链路走 TCP、UDP 还是中继？

A：使用 `PeerTransport(peerID)` 和 `PeerLinkMode(peerID)`。

### Q5：持续出现 `cipher: message authentication failed`，第一步该做什么？

A：先排查应用层是否存在消息风暴/回环；再确认双方 TCP/KCP 可用。之后通过 fast re-handshake 参数调优恢复节奏，并结合指标判断恢复效果。

### Q6：`reliable data transport unavailable` 是什么问题？

A：表示当前只剩 UDP 且 KCP 不可用。业务数据面不会降级到裸 UDP，需要修复 TCP/KCP 可用性。

## 6. 版本升级说明（v1.1.1）

- 兼容性：无破坏性 API 变更
- 修复项：
  - 并发竞态（会话、地址、链路模式读取）
  - `onReceive == nil` 时频道更新帧误跳过
  - TCP 全局写锁导致跨连接串行化
  - 连续解密失败后 fast re-handshake 快速恢复路径
  - fast re-handshake 增加冲突保护、退避与窗口限次、失败阈值回退 reconnect
  - 新增 fast re-handshake 指标与配置项

建议在生产环境开启 `-race` 完成一次回归验证：

```bash
go test ./... -race
```

## 7. 参考文档

- 项目说明：`README.md`
- 版本记录：`CHANGELOG.md`
