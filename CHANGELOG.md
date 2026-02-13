# Changelog

All notable changes to this project will be documented in this file.

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
