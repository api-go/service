# Service Module Test Report

## v1.5.47：JavaScript 与嵌入式 HTML 验证

- TestJavaScriptMinificationPreservesNestedEmptyBranches 通过真实 StaticFS HTTP 响应，分别执行源代码及压缩代码，验证禁止属性、空值允许属性、可信 URI 和自定义元素分支。
- 保留已有 HTML 模板、声明名称、缓存作用域和预构建 .min 文件测试。
- `TestJavaScriptMinificationSemanticMatrix` 在 `.js` / `.mjs` HTTP 冷缓存和热缓存中执行源码与响应，验证空分支、ASI、正则、闭包、finally、类、Unicode、BigInt、普通/嵌套/插值/tagged 模板及字符串中的伪 HTML 标记。
- `TestJavaScriptMinificationInlineScript` 验证默认、`text/javascript`、`module` 三种 HTML 内联脚本均实际压缩且保留属性过滤分支语义。
- `TestJavaScriptMinificationCacheLifecycle` 覆盖磁盘与 StaticFS 的并发冷缓存、跨 WebServer 实例复用、同时间戳源码替换、非法 JS 原样回退及修复后恢复。
- `TestJavaScriptMinificationIgnoresPreviousCompressorCache` 写入旧版真实缓存键的错误响应，验证升级压缩器后自动隔离；`TestJavaScriptMinificationCacheWriteFailure` 验证缓存不可写时原样返回源码。
- 2026-10-16 Service 最终树验证：`GOWORK=off go test -v ./...`、`GOWORK=off go test -race ./...`、`GOWORK=off go test -run '^$' -bench=. ./...`、`GOWORK=off go vet ./...` 通过。测试不依赖 Master、浏览器或第三方服务。
- `TestJavaScriptMinificationEmbeddedHTMLSafety` 覆盖实体生成插值/反引号/反斜杠、声明式转义表达式、转义反引号、`pre`/`textarea`、行内文字空格、带空格属性、HTML 注释，以及确实缩小模板缩进。冷/热 HTTP 响应均执行断言。
- 2026-10-16 发布验证：`GOWORK=off go test -v ./...`、完整 race、完整 tests+benchmark、vet 及 `git diff --check` 通过；Redis 使用已有本地服务。Apple M3 Max：Routing 3343 ns/op；IngressDirectGate 2611 ns/op、6431 B/op、32 allocs/op；ProxyWebSocket 36926 ns/op、4972 B/op、19 allocs/op。
- 导出 API 无变更；`ai/tools/getExports` 不可用，以 `GOWORK=off go doc` 与导出源码核对。

## v1.5.46 缓存隔离验证
- TestAutoMinifyIsolatesHostsAndReplacedMounts 覆盖两个主机同路径及替换挂载。

## v1.5.45 验证
- 压缩消息按解压后大小限制；超限消息不会作为完整消息送达上游。独立 Dialer 配置快照与未请求的子协议拒绝有覆盖。
- 2026-10-14 本次 Open 联动：GOWORK=off 全量与基准通过，代理专项 race（2 次）和停机专项（15 次）通过；Apple M3 Max，1 KiB WebSocket 回显 39268 ns/op、4993 B/op、19 allocs/op。API 使用 go doc -all 与源码核对。
- 覆盖 HTTP2 HTTPS 上游与 WSS 私有 CA、长拒绝正文与逐跳头过滤、分片大消息/压缩、空闲/消息/慢接收方写入限制、CookieJar 隔离、上游主动关闭和无效握手零上游调用。
- TestDiscoverProxyWebSocketRejectPreservesResponse 验证 Discover 拒绝握手的 401、正文和 Cookie；成功握手 Cookie 同时覆盖直连与 Discover。
- WebSocket 代理覆盖直连与 Redis Discover、鉴权拒绝、Cookie / Origin / Host、路径查询、子协议、文本/二进制、Ping/Pong、关闭码及两侧连接回收。
- 验证同一 TLS 监听同时提供 HTTP/2 普通请求与 HTTP/1.1 WebSocket；Discover 普通代理仍使用 H2C，直连拒绝握手保留状态、正文及 Cookie。
- 导出 API 使用 `GOWORK=off go doc` 与源码交叉核对；新增代理配置与传输配置 API。
- 本地验证（2026-10-14）：`GOWORK=off go test -v ./...`、`go test -bench=. ./...` 通过；WebSocket / HTTP2 代理专项 `go test -race ... -count=5` 通过。Apple M3 Max：Routing 3261 ns/op，IngressDirectGate 2645 ns/op、6433 B/op、32 allocs/op。

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

## v1.5.42 发布验证（2026-10-06）
- 外部测试包覆盖直连伪造头、Request-ID 兼容、可信 IPv4/IPv6 CIDR、右侧代理链和非法 hop。
- 覆盖 Gate 早于 Proxy/HTTPS 且不读取 Body、发布期间每请求版本固定、非法路由校验、managed Host 隔离和代理 Session 鉴权。
- `go test -v ./...`、`go test -bench=. ./...`、完整 `go test -race ./...` 与 `git diff --check` 通过；普通 Header 转发、hop-by-hop 清理及客户端 IP 重建也有回归覆盖。
- 使用已发布 Discover v1.5.15，在 GOWORK=off 下重新执行完整测试与 benchmark；Apple M3 Max：BenchmarkRouting 2807 ns/op，BenchmarkIngressDirectGate 2301 ns/op、6353 B/op、27 allocs/op。入口 benchmark 包含请求/响应构造，不代表网络吞吐。

## v1.5.42 静态目录边界覆盖

TestStaticCannotEscapeRegisteredDirectory覆盖根目录注册的正常文件以及原始／一次／二次编码的越界路径，不允许返回目录外文件。go test -v ./...、go test -race ./...、go test -bench=. ./...均通过。BenchmarkRouting约2807ns/op，BenchmarkIngressDirectGate约2301ns/op（本机单次结果）。

## v1.5.43 发布验证（2026-10-10）

- GOWORK=off go test -v ./... 与 go test -bench=. ./... 通过，依赖均为已发布 tag。
- API 以 GOWORK=off go doc 与导出源码交叉核对（getExports 工具不可用）。
- GOWORK=off go test -race ./... 通过。

```text
BenchmarkRouting-16              	  327060	      3396 ns/op
BenchmarkIngressDirectGate-16    	  513038	      2449 ns/op	    6434 B/op	      32 allocs/op
```

## v1.5.44 验证

- 外部测试 TestProxyPreservesOriginalHost 校验公开 Host 与伪造请求头覆盖。
- TestProxyPreservesStatusAndCookies 覆盖 200、204、302、303、401、403、503，正文、Location 与两条 Set-Cookie。
- 发布检查使用 GOWORK=off 的已发布依赖；全量测试、基准测试和代理 race 检查通过。
- API 核对采用 go doc 与导出源码交叉检查，公开 API 未改变。

本次基准（Apple M3 Max）：Routing 3400 ns/op；IngressDirectGate 2592 ns/op、6434 B/op、32 allocs/op。

## v1.5.46 发布验证（2026-10-15）

- `GOWORK=off go test -v ./...` 与 `GOWORK=off go test -bench=. ./...` 通过。
- getExports 工具不可用，以 `GOWORK=off go doc` 与导出源码核对 API。

```text
BenchmarkRouting-16              	  381876	      3125 ns/op
BenchmarkIngressDirectGate-16    	  530913	      2295 ns/op	    6433 B/op	      32 allocs/op
BenchmarkProxyWebSocket-16       	   31534	     36531 ns/op	  28.03 MB/s	    5000 B/op	      19 allocs/op
```

## v1.5.48 HTTP/2 WebSocket 发布验证（2026-10-23）

`TestProxyUsesHTTP2UpstreamAndWSSWithPrivateCA` 在默认启动配置严格验证 WSS 回退 HTTP/1.1，在启动时设置 `GODEBUG=http2xconnect=1` 后严格验证 RFC 8441 HTTP/2；两种模式均验证普通请求使用 HTTP/2、私有 CA 信任、握手响应头及真实消息接收，不跳过该测试。

2026-10-22 本地工作区验证：默认模式与启动 `GODEBUG=http2xconnect=1` 模式各自的 targeted、全量 test、vet、race、benchmark 均通过。Apple M3 Max 本次 Routing / IngressDirectGate / ProxyWebSocket：默认 4202 / 2401 / 28317 ns/op，H2 模式 4160 / 2406 / 29086 ns/op；仅为本机样本，不代表协议性能对比。

依赖已发布 HTTP v1.5.14；独立发布验证设置 GOWORK=off，不使用相邻模块源码。

覆盖 H1→H1/H2、H2→H1/H2、原生路由、文本/二进制、认证拒绝、正常关闭、多值 Cookie、Host/认证头透传、真实 H2C 监听关闭及 1001 通知。上游默认智能选择，无新增 WebSocketProxy.HTTP2 配置；默认代理池复用能力缓存与 H2 连接，禁止跨用户 Cookie Jar。API 使用 go doc -all 与导出源码核对。

2026-10-22 / Apple M3 Max 本轮基准：Routing 3307 ns/op，IngressDirectGate 2527 ns/op，ProxyWebSocket 43271 ns/op。HTTP 客户端新增同负载 H1/H2 TLS 对照数据见相邻 http/testdata/websocket/20261022-tls-compare.md。

2026-10-23：Redis 既有服务 PONG，未启停用户 Redis；`GOWORK=off` 默认和启动 `GODEBUG=http2xconnect=1` 两种模式的全量 test-v/count1、vet、race/count1、benchmark 均通过。`ai/tools/getExports` 不可用，使用 `GOWORK=off go doc -all .` 与导出源码核对，公开 Go API 未新增或移除；原 `Upgrade`、代理与生命周期接口保持兼容。

Apple M3 Max 本次 Routing / IngressDirectGate / ProxyWebSocket：默认 4015 / 2434 / 36905 ns/op，H2 启动模式 3991 / 2467 / 35626 ns/op；此基准不构成 H1/H2 对照结论。

## External ingress coverage (2026-10-23)
- Direct HTTP/TLS MakeUrl, legacy-ID spoof rejection, explicit peer trust, malformed trusted metadata, concurrent multi-hop forwarding, Origin retention and no HTTPS redirect loop.
- Full go test -v ./... and go test -bench=. ./...; API review via go doc/exported sources (getExports unavailable).

- Additional H2C discovery proxy test verifies HTTPS metadata and unchanged Origin through internal cleartext transport. Temporary replace validated against the real DL Gateway before release. Release baseline: HTTP v1.5.15 / Discover v1.5.18, GOWORK=off, no replace.

- Released-dependency benchmark (Apple M3 Max): routing 159.8 ns/op; ingress direct gate 3553 ns/op, 6865 B/op, 43 allocs/op; WebSocket proxy 38836 ns/op, 26.37 MB/s.
