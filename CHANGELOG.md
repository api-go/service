# CHANGELOG - go/service

## v1.5.3 (2026-06-04)
- **新特性**:
  - `Static` 静态文件服务增加默认索引文件识别：当请求目录时，若未配置 `IndexFiles`，会自动尝试匹配 `index.html` 或 `index.htm`。
- **优化**:
  - 依赖升级至 `apigo.cc/go/log v1.5.3`，实现对第三方库原始日志的自动劫持与 `serverId` 注入。
  - 清理了启动阶段多余的诊断日志，确保“零配置”启动时的控制台纯净度。
- **稳定性**:
  - 修复了 `Start()` 函数的幂等性问题，防止在手动调用与 `starter` 框架调度冲突时产生重复绑定端口的错误。

## v1.5.2 (2026-06-04)
- **架构重构**: 彻底移除全局状态泄露，重构为严格的单例模式。
...
- **修复**: 在 `WebServer.Start` 中显式调用 `config.Load(&Config, "service")`，确保启动时自动从 `env.yaml` 加载 `service:` 块。
- **修复**: 优化 `WebServer.Reload` 的配置加载逻辑，确保与启动加载逻辑保持一致。

## v1.5.0 (2026-05-10)
- **基础设施对齐**: 全局对齐至 v1.5.0。

## v1.3.7 (2026-06-02)
- **API 统一**:
  - `HostContext` 新增 `Static` 方法，支持 `Host("example.com").Static("/ui", "./dir")` 这种更具一致性的链式调用风格。
  - 全局 `Static` 统一收拢为底层 `Host("*").Static(...)` 实现。

## v1.3.6 (2026-05-31)
- **基础设施增强**:
  - 新增 `WebSocketConn` 标准包装器，提供统一的 `Send`, `ReadString`, `ReadBytes`, `ReadJSON` 接口。
  - 新增 `Upgrade` 函数，支持在自定义处理器中手动升级 HTTP 为 WebSocket。
  - 集成 `Session` 会话管理，支持分布式 Redis 或本地内存存储。
- **安全加固**: 彻底移除 `UploadFile.Save` 方法，规避低代码环境下的文件落盘风险。
- **JSMOD 类型对齐**:
  - 提供 `newRequest`, `newResponse`, `newWebSocket`, `newSession`, `newFile` 占位工厂，支持 AI 环境下的类型自动发现 (DTS)。
  - 导出 `upgrade` 方法支持动态服务分发场景。

## v1.3.5 (2026-05-31)
- **Logging Refactor (Callback Pattern)**: 引入 `LogRequest` 闭环式回调封装，自动处理日志级别检查、对象池获取及元数据填充，消除 20+ 参数带来的维护压力。
- **Graceful Shutdown**: `ServiceConfig` 新增 `StopTimeout` 字段，支持通过配置灵活管控服务优雅退出的超时时间（默认 5s）。
- **Panic Recovery**: 增强 `handler.go` 中的 `recover` 逻辑，在发生 Panic 时自动记录 `requestId` 和 `path`，大幅提升故障定位效率。
- **Infrastructure Alignment**: 全量回退并对齐 Go 版本至 `1.25.0`；同步更新 `handler.go` 以适配新的日志调用模式。

## v1.0.5 (2026-05-10)
- **Wait**: This version was a temporary placeholder. See v1.3.1 for the actual release.

## v1.0.4 (2026-05-10)
- **Log Optimization**: Implemented `NoLogGets`, `NoLogHeaders`, `LogInputArrayNum`, `LogInputFieldSize`, and other fine-grained logging filters.
- **Static Log**: Added automatic logging for static file access.
- **Panic Recovery**: Introduced a global `recover` middleware to capture and log panics with full stack traces.
- **Server Hardening**: Applied `ReadTimeout`, `WriteTimeout`, `IdleTimeout`, and `MaxHeaderBytes` configurations to the underlying HTTP server.
- **Service Alignment**: Populated `Config.App` and improved `serverId` generation for better traceability.
- **Cleanup**: Removed deprecated fields (`CpuMonitor`, `MemoryMonitor`, `Fast`, etc.) and unused logging methods (`logTask`, `logMonitor`).

## v1.0.3 (2026-05-09)
### Added
- **Zero-Config Microservices**: 实现智能启动逻辑。当 `Listen` 为空时，自动开启随机端口并使用 `h2c` 协议。
- **Auto-Discover**: 自动集成 `go/discover`。当使用随机端口或配置了应用名时，自动完成服务注册。
- **Smart Detection**: 内置应用名自动识别 (`debug.ReadBuildInfo`) 与服务 IP 自动探测 (UDP 伪拨号法)。
- **H2C Server**: 原生支持 HTTP/2 Cleartext (h2c) 服务端协议，提升微服务间通信性能。

### Changed
- **Infrastructure Alignment**: `go.mod` 引入 `golang.org/x/net` 以支持 H2C，升级 `go/discover` 至 `v1.0.9`。
- **IO Security**: 持续优化 `go/file` 在静态文件服务中的应用。

## v1.0.2 (2026-05-09)
### Changed
- **Infrastructure Alignment**: `go.mod` 升级 `go/config` 至 `v1.0.7`，`go/http` 至 `v1.0.10`。
- **IO Security**: 移除所有业务逻辑中的原生 `os` 调用，强制使用 `go/file`。
- **Virtualization**: `Static`, `SendFile`, `UploadFile.Save` 全面支持内存文件系统，提升测试与高频读写性能。
- **Performance**: 优化了 `static.go` 的 304 检查逻辑，`BenchmarkRouting` 性能提升至 ~2984 ns/op。

## v1.0.1 (2026-05-08)
### Added
- 集成 `apigo.cc/go/log` 并实现完整的 `Request` 日志记录，支持 `NoLog200` 选项。
- 集成 `apigo.cc/go/timer` 用于高精度请求耗时统计。
- 在 `service.go` 中添加 `GetInjectT` 泛型函数，提升依赖注入体验。
- `Response` 结构体新增 `body` 捕获（仅在非 200 状态下且小于 4KB 时捕获），用于错误日志记录。

### Changed
- **Infrastructure Alignment**: `go.mod` 补全所有基础设施依赖，并添加 `replace` 指令对齐本地版本。
- **Naming Alignment**: 修复 `parmsNum` 为 `paramsNum`；移除私有函数 `_verifyValue` 的下划线前缀。
- **Performance**: 优化了 `ServeHTTP` 的执行链路，`BenchmarkRouting` 性能提升至 ~3047 ns/op。
- **Modernization**: `parseRequestArgs` 中将 `json.Unmarshal` 替换为 `cast.UnmarshalJSON`。
- **Robustness**: `UploadFile.Save` 采用 `file.EnsureParentDir` 保证 IO 安全。

## v1.0.0 (2026-05-01)
- 初始版本发布，支持 Host 隔离路由与自动参数注入。
