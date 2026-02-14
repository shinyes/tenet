# Changelog

All notable changes to this project will be documented in this file.

## [1.2.0] - 2026-02-14

### Added
- 新增 fast re-handshake 快速恢复路径：连续解密失败达到阈值后可立即重握手
- 新增 fast re-handshake 观测指标：`FastRehandshakeAttempts`、`FastRehandshakeSuccess`、`FastRehandshakeFailed`
- 新增 fast re-handshake 相关配置项：
  - `WithFastRehandshakeBackoff`
  - `WithFastRehandshakeWindow`
  - `WithFastRehandshakeFailThreshold`
  - `WithFastRehandshakePendingTTL`

### Changed
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
