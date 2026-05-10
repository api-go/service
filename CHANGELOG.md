# CHANGELOG - go/service

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
