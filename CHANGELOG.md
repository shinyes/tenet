# Changelog

本项目的所有重要变更都会记录在此文件中。

## [1.4.4] - 2026-02-17

### 变更
- 提升 `node/connect_flow.go` 中标识符命名清晰度：将远端地址、握手状态/数据包及连接结果处理相关变量由缩写替换为语义明确的命名。
- 无行为变更；本次发布聚焦可读性与可维护性。

## [1.4.3] - 2026-02-16

### 变更
- 重构 `node` 运行时架构：将启动、连接流程、TCP/UDP I/O、数据路径和发送路径拆分为职责更聚焦的文件。
- 精简 `node.go`，使编排逻辑和对外方法保持简洁。
- 重构 `transport/UDPMux` 路由逻辑，拆分为专用分发辅助函数，并新增显式的原始中继包路由。
- 通过澄清状态与退避流程的关键注释，提升 reconnect 模块可维护性。

### Added
- Added connect-flow tests for timeout, closing/not-started errors, late TCP upgrade behavior, handshake packet format, and pending-handshake registration.
- Added reconnect tests for dedup scheduling, success/failure callbacks, jitter backoff bounds, and cancel idempotency.
- Added mux routing tests covering TENT/NATP/raw relay routing and dedicated KCP handler dispatch.

## [1.4.2] - 2026-02-15

### 变更
- 优化频道订阅热路径：本地订阅校验从线性扫描改为哈希集合 O(1) 查找。
- 统一频道哈希表示为 `[32]byte`，降低频繁哈希与比较带来的开销。
- `JoinChannel` / `LeaveChannel` / `Send` / 频道同步路径统一复用固定哈希表示，减少热路径开销。

### Added
- 新增频道性能基准：`BenchmarkIsChannelSubscribed_Hit` / `BenchmarkIsChannelSubscribed_Miss`。
- 新增线性实现对照基准：`BenchmarkIsChannelSubscribed_LinearHit` / `BenchmarkIsChannelSubscribed_LinearMiss`。

## [1.4.1] - 2026-02-15

### Fixed
- 在 `registerRelayCandidate` 中保护 `relayAddrSet` 的访问，避免在并行握手时发生并发 map 读/写 panic。
- 新增发现源认证检查，确保仅通过认证的对等方能触发发现请求/响应的处理。
- 为 `discoveryConnectSeen` 新增上限和确定性修剪路径，防止在高基数发现输入下出现无限制的短期增长。
- 扩展节点测试以覆盖中继候选者并发、发现认证门控以及发现 seen-map 硬限制行为。

## [1.4.0] - 2026-02-14

### Added
- 新增 fast re-handshake 快速恢复路径：连续解密失败达到阈值后可立即重握手
- 新增 fast re-handshake 观测指标：`FastRehandshakeAttempts`、`FastRehandshakeSuccess`、`FastRehandshakeFailed`
- 新增 fast re-handshake 相关配置项：
  - `WithFastRehandshakeBackoff`
  - `WithFastRehandshakeWindow`
  - `WithFastRehandshakeFailThreshold`
  - `WithFastRehandshakePendingTTL`

### 变更
- 业务数据面仅使用可靠通道（TCP/KCP），不再回退裸 UDP
- 强化重握手流程：增加 pending handshake 冲突保护、退避与窗口限次、失败阈值后回退 reconnect
- 扩展测试覆盖：增加恢复后业务收发、配置校验、指标与退避逻辑相关测试

### Docs
- 重构 README 阅读路径，补充快速上手、排错速查与常用配置详解
- 更新 USER_GUIDE，增加快速恢复机制与配置说明

## [1.1.1] - 2026-02-14

### Fixed
- 修复 `Peer` 传输升级与发送路径并发读写导致的数据竞争问题（`Session`/`Addr`/`LinkMode` 读取改为线程安全访问）
- 修复 `processData` 在 `onReceive == nil` 时提前返回，导致频道更新帧被跳过的问题
- 修复 TCP 发送使用全局写锁导致的跨连接串行化问题，改为按连接粒度加锁

## [1.0.1] - 2026-02-13

### Fixed
- 修复 `AppFrameTypeChannelUser` 常量未定义的问题，重命名为 `AppFrameTypeUserWithChannel`
- 修复日志格式化字符串错误（`%s)` 改为 `%s`）
- 清理测试代码中未使用的变量 `receivedPeer`

## [1.0.0] - 2026-02-12

### Initial Release
- 初始版本发布
- P2P 加密隧道基础功能
- 支持多频道隔离机制
