# Service Module Test Report

## v1.5.23 验证
- `TestResponseWriteBytes` 验证动态字节数组写入时保持 UTF-8 内容不变。
- `TestResponseWriteBypassesOutputFilterBuffer` 验证直接响应即使存在输出过滤器也保持实时写入。
- `TestResponseWriteFilteredOptsIntoOutputFilterBuffer` 验证显式过滤写入仍可由静态 HTML 注入等后置过滤器处理。
- `go test -v ./...` 与 `go test -bench=. ./...` 全部通过。

## v1.5.22 验证
- `go list -m all`：模块图使用 `discover v1.5.8`、`redis v1.5.11` 与稳定版 `redigo v1.9.3`。
- `go test -v ./...` 与 `go test -bench=. ./...` 全部通过。

## v1.5.21 验证
- `go list -m all`：模块图使用 `discover v1.5.7`、`redis v1.5.11` 与稳定版 `redigo v1.9.3`。
- `go test -v ./...` 与 `go test -bench=. ./...` 全部通过。

## 性能测试 (Benchmark)
- 测试日期: 2026-08-17
- 版本: v1.5.23
- 指标: `BenchmarkRouting`: **2312 ns/op**
- 环境: Darwin / Apple M3 Max

## 单元测试覆盖 (Unit Test)
- [x] `TestServeHTTP`: 基础请求与响应
- [x] `TestServeHTTP_404`: 404 处理
- [x] `TestServeHTTP_VerifyFailed`: 参数校验失败处理
- [x] `TestServeHTTP_Panic`: **(New)** 验证全局 Panic 恢复与日志记录
- [x] `TestRewrite`: 路径重写
- [x] `TestProxyDirect`: 代理转发 (Mock)
- [x] `TestAsyncServer`: 异步启动与生命周期 (已验证 Server Timeout 配置生效)
- [x] `TestServiceRegister`: 基础路由注册
- [x] `TestRegexServiceRegister`: 正则路由注册
- [x] `TestStaticService`: 静态文件服务 (已支持内存文件与自动日志记录)
- [x] `TestVerifyStruct`: 基础结构校验
- [x] `TestNestedVerify`: 嵌套结构校验
- [x] `TestCustomVerify`: 自定义校验函数
- [x] `TestWebSocketService`: WebSocket 注册
- [x] `TestGetDefaultName`: 自动应用名识别
- [x] `TestGetServerIp`: 自动 IP 探测
- [x] `TestSmartStartup`: 零配置智能启动与 Discover 注册
- [x] `TestSanitizeScalars` ~ `TestSanitizeMixedSlice`: 日志脱敏 10 个测试（标量/对象/数组/嵌套/预算/Unicode）
- [x] `TestSessionLogic`: Session Save/Load/Remove 及 AuthFuncs
- [x] `TestSessionInjection`: Session HTTP 注入流程
- [x] Logging Filters: NoLogInput/NoLogOutput/NoLogAllHeaders/NoLogGets/NoLogRequestHeaders/NoLogResponseHeaders/NoLogResponseFields
- [x] Client Keys: Device-Id/Session-Id 应答头条件化（请求有则不应答）、静态文件/WebSocket 仅 Cookie
- [x] Cookie 头智能过滤: 排除列表中 key 从 Cookie 内容中剔除，保留业务 Cookie
- [x] Response Body: 200 响应和 dev 模式下 keepBody 捕获
- [x] Response Dynamic Bytes: 低代码动态数组通过 `WriteBytes` 写入时保持 UTF-8 字节不变

## 基础设施对齐验证
- [x] 成功集成 `apigo.cc/go/cast` 用于参数解析与类型强转。
- [x] 成功集成 `apigo.cc/go/timer` 用于高性能耗时追踪。
- [x] 成功集成 `apigo.cc/go/log` 并实现完整的 Request 日志记录，支持头过滤与内容截断。
- [x] 强制集成 `apigo.cc/go/file` 替代原生 `os`，全面支持内存虚拟文件系统。
- [x] 成功集成 `apigo.cc/go/id` 与 `go/redis` 实现分布式有序 ID。
- [x] 成功集成 `apigo.cc/go/discover` 并支持 H2C 协议的零配置自动注册。
- [x] **Safety**: 已集成 `recover` 机制，保障服务在高并发业务 Panic 时的稳定性。
