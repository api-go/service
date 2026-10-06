# Service Module Test Report

## v1.5.41 验证
- 验证磁盘与 `http.FileSystem` 静态资源优先使用 `.min.*` 兄弟文件，并保持逻辑 wildcard 与显式 `.min.*` wildcard 的边界。
- 验证声明式 HTML 属性、可省略结束标签边界和 JavaScript 声明名称在压缩后保持稳定。
- `go test -v ./...` 与 `go test -bench=. ./...` 作为发布前完整验证执行；`BenchmarkRouting`: **2604 ns/op**（Darwin / Apple M3 Max）。

## v1.5.39 验证
- `TestWebServerStopCancelsRequestLifecycle` 验证停机信号会取消长连接请求 Context；`TestWebServerStopForceClosesAfterTimeout` 验证不响应取消的 handler 不会令停机永久阻塞。
- `TestWebServerStopNotifiesWebSocketAndWaitsForHandler` 验证 WebSocket 收到标准关闭通知并完成握手；`TestWebServerStopForceClosesUnresponsiveWebSocket` 验证总超时后强制断开拒不退出的连接。
- `TestClientIpRejectsUntrustedProxyHeaders` 验证默认忽略直连客户端伪造的代理 IP。
- `TestClientIpUsesTrustedRealIPAndFirstForwardedAddress` 与 `TestClientIpSkipsMalformedForwardedAddresses` 覆盖 `X-Real-IP`、代理链首地址、非法值及 IPv6。
- `go test -v ./...`、`go test -bench=. ./...` 与 WebSocket 生命周期 race 测试通过；`BenchmarkRouting`: **2486 ns/op**（Darwin / Apple M3 Max）。

## v1.5.38 验证
- 动态监听覆盖运行时新增、移除、复用未变化端口，以及新增端口绑定失败时保留原监听集合。
- `TestWebServerTLSCertificateReloadAppliesToNewConnections` 通过真实 TLS 握手验证证书续期立即作用于新连接，并验证无效证书不会替换上一份有效证书。
- `go test -v ./...`、`go test -race ./...`、`go test -bench=. ./...` 与 `git diff --check` 通过；`BenchmarkRouting`: **2555 ns/op**（Darwin / Apple M3 Max）。

## v1.5.36 验证
- `TestClientKeyHeadersSynchronizeCookies` 验证请求头中的 Session ID 与 Device ID 会覆盖不同值的 HttpOnly Cookie。
- `TestClientKeyHeadersDoNotRewriteMatchingCookies` 验证请求头与 Cookie 相同时不重复写入应答 Cookie。
- `go test -v ./...` 与 `go test -bench=. ./...` 通过；`BenchmarkRouting`: **2471 ns/op**（Darwin / Apple M3 Max）。
- `git diff --check` 通过。

## v1.5.33 验证
- `TestExpandAssetReferencesPreservesPatternOrderAndDeduplicates` 验证 `srcs`、`hrefs` 按模式顺序展开并去重。
- `TestAutoAssetReversionExpandsStaticPatterns` 验证静态 HTML 递归模式展开、稳定顺序与逐文件版本号。
- `TestAutoAssetReversionInvalidatesPatternWhenMatchedFileIsRemoved` 验证匹配文件删除后推进 HTML logical time 并刷新展开结果。
- `go test -v ./...` 与 `go test -bench=. ./...` 作为发布前完整验证执行。
- `git diff --check` 通过。

## v1.5.32 (2026-08-28)
- `TestAutoAssetReversionDynamicHTMLWriteString` 验证动态 HTML 通过 `WriteString` 写入时自动追加资源版本。
- `TestAutoAssetReversionDetectsDynamicHTMLWriteString` 验证未显式设置 `Content-Type` 时从首个完整 HTML 写入检测类型并完成版本改写。
- `TestAutoAssetReversionFlushKeepsDynamicHTMLStreaming` 验证 `Flush` 将响应切换为流式直写并跳过尚未执行的内容改写。
- `TestResponseWriteBuffersFilterableHTML` 验证只有过滤器实际关心的内容进入完整响应缓冲。
- `go test -v ./...` 与 `go test -bench=. ./...` 通过；`BenchmarkRouting`: **2524 ns/op**（Darwin / Apple M3 Max）。

## v1.5.31 (2026-08-26)
- `TestAssetDependencyGraphPropagatesNestedDependencyTime` 验证双向依赖图将末端资源的物理修改时间逐层传播为上层资源的逻辑修改时间，起点和末端直接由依赖列表长度推导。
- `TestAssetDependencyGraphHandlesCycles` 验证循环引用不会导致扫描递归失控。
- `TestAssetDependencyGraphLogicalLookupDoesNotLoadSource` 验证高频逻辑时间查询只读取内存节点，不访问资源源。
- `TestAutoAssetReversionInvalidatesHTMLWhenDependencyChanges` 与 `TestAutoAssetReversionInvalidatesPhysicalHTMLWhenDependencyChanges` 验证 `StaticFS` 和磁盘静态目录使用依赖感知的 200 / 304 语义。
- `TestAutoAssetReversionInvalidatesDirectoryIndexForNestedDependency` 验证目录入口映射到 `index.html` 后，三层依赖变化能更新应答和资源版本。
- `go test -v ./...`、`go test -bench=. ./...` 与 `go test -race ./...` 通过。

## v1.5.30 (2026-08-26)
- `TestMinifyJSCompressesMakeDomHTMLAndPreservesEscapedExpressions` 验证 `/*html*/` 模板中的声明式属性和 `\${...}` 表达式保持有效，同时压缩 HTML 空白。
- `TestMinifyJSOnlyProcessesHTMLMarkedTemplates` 验证未标记 JS 模板不被当作 HTML 压缩，并验证转义反引号。
- `go test ./service -count=1` 通过。

## v1.5.28 (2026-08-25)
- `TestParseListenConfigs` 验证 `|` 分隔的 HTTP、HTTPS、H2C 配置全部解析。
- `TestMultiListenerWithAutoTLS` 验证 HTTP/HTTPS 同时监听、真实 TLS 握手、完整状态地址及 99 年自签名证书持久化。
- 同一测试验证 Host 级 HTTPS 308 跳转使用当前实际 TLS 端口。
- `TestMinifyAsset` 验证 HTML、JavaScript 与模块资源压缩。
- `go test -race ./...` 通过。

## v1.5.24 (2026-08-22)
- `TestStaticFSHostCandidatesAndRange` 验证自定义静态源的四级 Host 优先级与 Range 206 响应。
- `TestStaticFSExclusiveStopsFileFallback` 验证隔离 Host 文件未命中时返回 404，未匹配 Host 仍使用全局默认静态源。
- `TestFallbackRunsOnlyAfterRegisteredRoutesMiss` 验证已注册路由优先于 Fallback。
- `go test ./...` 通过。

## v1.5.23 验证
- `TestResponseWriteBytes` 验证动态字节数组写入时保持 UTF-8 内容不变。
- `TestResponseWriteBypassesOutputFilterBuffer` 验证直接响应即使存在输出过滤器也保持实时写入。
- `TestResponseInternalFilteredWriteOptsIntoOutputFilterBuffer` 验证包内过滤写入仍可由静态 HTML 注入等后置过滤器处理。
- `go test -v ./...` 与 `go test -bench=. ./...` 全部通过。

## v1.5.22 验证
- `go list -m all`：模块图使用 `discover v1.5.8`、`redis v1.5.11` 与稳定版 `redigo v1.9.3`。
- `go test -v ./...` 与 `go test -bench=. ./...` 全部通过。

## v1.5.21 验证
- `go list -m all`：模块图使用 `discover v1.5.7`、`redis v1.5.11` 与稳定版 `redigo v1.9.3`。
- `go test -v ./...` 与 `go test -bench=. ./...` 全部通过。

## 性能测试 (Benchmark)
- 测试日期: 2026-08-26
- 版本: v1.5.31
- 指标: `BenchmarkRouting`: **2699 ns/op**
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
- [x] `TestSensitiveRequestHeadersAreExcludedByDefault`: 请求日志默认排除认证头、代理认证头和 Cookie。
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

## 2026-10-06 ingress 验证（unreleased）
- 外部测试包覆盖直连伪造头、Request-ID 兼容、可信 IPv4/IPv6 CIDR、右侧代理链和非法 hop。
- 覆盖 Gate 早于 Proxy/HTTPS 且不读取 Body、发布期间每请求版本固定、非法路由校验、managed Host 隔离和代理 Session 鉴权。
- `go test -v ./...`、`go test -bench=. ./...`、完整 `go test -race ./...` 与 `git diff --check` 通过；普通 Header 转发、hop-by-hop 清理及客户端 IP 重建也有回归覆盖。
- 本次提交前重新执行完整测试与 benchmark；Apple M3 Max：BenchmarkRouting 2744 ns/op，BenchmarkIngressDirectGate 1889 ns/op、6353 B/op、27 allocs/op。入口 benchmark 包含请求/响应构造，不代表网络吞吐。

## 静态目录边界覆盖（未发布）

TestStaticCannotEscapeRegisteredDirectory覆盖根目录注册的正常文件以及原始／一次／二次编码的越界路径，不允许返回目录外文件。go test -v ./...、go test -race ./...、go test -bench=. ./...均通过。BenchmarkRouting约2820ns/op，BenchmarkIngress约2148ns/op（本机单次结果）。
