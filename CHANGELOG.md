# Changelog

## [v1.5.42] - 2026-10-06
- Static路径后缀按注册根目录归一化，防止原始／编码的..路径越出目录；正常静态资源映射保持不变。
- 新增入口 Gate、严格可信代理 CIDR 策略及内部 IP relay 清理接口。
- 新增不可变 Host 请求策略整体发布、每请求快照与外部证书提供器。
- 非零代理 AuthLevel 默认执行 Session 权限检查。
- 保留默认内部 Request-ID relay 兼容行为。
- 依赖 Discover v1.5.15，修正心跳停止和重启生命周期。


## v1.5.41 (2026-09-13)
- **静态资源压缩**: 自动压缩优先使用同目录的 `.min.*` 预构建资源，同时保持逻辑资源模式默认隐藏该物理旁路文件。
- **模板安全**: HTML 压缩保留显式结束标签与属性引号，JavaScript 压缩保留声明名称，避免改变声明式 UI 绑定语义。
- **依赖对齐**: 升级 `config`、`http`、`redis` 与 `starter` 至模块索引当前版本。

## v1.5.40 (2026-09-12)
- **日志安全**: 请求头日志脱敏补充受保护请求头的大小写与别名覆盖，并增加回归测试。

## v1.5.39 (2026-09-09)
- **凭证日志安全**: 请求日志默认排除 `Authorization`、`Proxy-Authorization` 与 `Cookie`，避免 Agent Token 和会话凭证进入日志。
- **长连接停机**: HTTP 请求继承独立的服务生命周期 Context；`Stop` 会先取消 SSE 等长连接，优雅停机超时后强制关闭残余连接，避免服务管理器无限重试。
- **WebSocket 停机**: 自动登记升级后的连接；停止服务时发送 `1001 Going Away` 关闭通知并等待处理器收尾，总超时后强制断开，停止期间拒绝新连接。
- **真实 IP 防伪**: `Request.ClientIp()` 统一优先读取 `X-Real-IP`，兼容旧 `X-Client-Ip`，正确解析 `X-Forwarded-For` 首个合法地址；默认仅在请求进入服务前已有 Request ID 时信任代理头，`AcceptXRealIpWithoutRequestId` 可显式允许没有 Request ID 的入口接受代理头。

## v1.5.38 (2026-09-06)
- **动态监听**: Reload 会协调 HTTP、HTTPS、HTTP/2 和 H2C 监听集合；新增端口全部绑定成功后才切换，移除端口在后台优雅排空现有请求。
- **证书热更新**: Reload 完整验证并原子替换 TLS 证书快照，既有 HTTPS 监听无需重启即可在后续握手中使用新证书。
- **失败回滚**: 配置读取、证书解析或新端口绑定失败时恢复完整 Service 配置及上一份有效证书快照，运行中的监听不受影响。

## v1.5.36 (2026-09-05)
- **依赖对齐**: 补齐 `discover`、`http`、`redis`、`starter`、`watch` 等级联版本。

## v1.5.35 (2026-09-05)
- **依赖对齐**: 升级全部 `apigo.cc/go/*` 依赖至当前已发布版本。

## v1.5.34 (2026-09-04)
- **客户端标识一致性**: 启用 Cookie 时，将请求头中有效的 Session ID 与 Device ID 同步到缺失或不一致的 HttpOnly Cookie，避免 Fetch 请求与页面导航选择不同身份。
- **回归测试**: 覆盖客户端标识 Cookie 的同步与相同值不重复写入场景。

## v1.5.33 (2026-09-03)
- **资源模式展开**: AutoReversion 支持在 HTML 中将 `script[srcs]` 与 `link[hrefs]` 展开为有序、去重且独立版本化的本地资源引用。
- **资源失效追踪**: 匹配文件新增或删除后推进引用 HTML 的逻辑修改时间并刷新展开结果。
- **依赖对齐**: Redis 依赖升级至 `v1.5.14`。

## v1.5.32 (2026-08-28)
- **动态文本响应**: `Response.Write`、`WriteString` 与 `WriteBytes` 根据 `Content-Type` 自动缓冲需要 AutoReversion 或开发注入处理的 HTML/CSS，不再要求业务代码显式选择过滤写入 API。
- **流式切换**: `Response.Flush` 会原样发送待处理缓冲并将当前响应切换为实时直写，SSE 与二进制分块继续保持低延迟。
- **内部 API**: 过滤缓冲与物理写入收回为包内实现，业务侧统一使用 `Write`、`WriteString`、`WriteBytes` 与 `Flush`。
- **响应一致性**: 动态 HTML/CSS 支持自动资源版本改写；修改内容时移除失效的 `Content-Length`。
- **依赖对齐**: 升级 `discover` 至 `v1.5.10`、`redis` 至 `v1.5.13`。

## v1.5.31 (2026-08-26)
- **资源依赖图**: AutoReversion 使用双向依赖关系维护资源的物理修改时间和逻辑修改时间，支持 HTML、CSS 与 JavaScript 资源的多层依赖传播。
- **条件请求**: 静态文件和 `StaticFS` 的 `Last-Modified` 与 304 判断统一使用逻辑修改时间，末端依赖变化时上层页面不再错误返回 304。
- **请求性能**: 依赖变化由 5 秒定时扫描器处理；正常请求只查询内存节点，不遍历或读取依赖文件。
- **目录入口**: 目录请求映射到 `index.html` / `index.htm` 后使用实际资源路径建图，保证相对引用和自动版本号正确解析。

## v1.5.30 (2026-08-26)
- **JavaScript 资源压缩**: 启用 `.js` 与 `.mjs` 压缩，并支持通过 `/*html*/` 标记压缩嵌入式 HTML 模板。
- **模板安全性**: 保留 `\${...}` 表达式、未标记 JavaScript 模板字符串和转义反引号。
- **回归测试**: 增加 AutoForm/List/DataGrid 风格声明式模板的压缩验证。

## v1.5.28 (2026-08-25)
- **多监听协议**: `Listen` 中以 `|` 分隔的地址全部启动并纳入统一停止与状态管理，不再只使用第一项。
- **零配置入口**: 非服务发现应用默认尝试 80/443，端口无权限或被占用时整组降级到 8080/8443；显式 App + Registry 继续使用随机端口 H2C。
- **HTTPS**: 支持 SNI 域名证书、文件或内联 PEM、AES-GCM 密文兼容加载，以及纯 Go 生成并加密持久化的 99 年自签名兜底证书。
- **Host HTTPS 策略**: 新增 `ReplaceForceHTTPS`，按 Host 动态启用 308 跳转并使用实际 HTTPS listener 端口。
- **JavaScript 资源**: 自动压缩跳过 `.js` 与 `.mjs`，避免压缩器改变现代模块语义；HTML、CSS 等资源继续按原策略处理。
- **缓存清理**: 忽略本地生成的 `data/wwwcache/index.html.min`。
- **依赖对齐**: 升级 `log`、`redis`、`shell` 与 `watch` 到当前稳定补丁版本。

## v1.5.24 (2026-08-22)
- **动态静态源**: 新增 `StaticFS` 与 `ReplaceStaticFS`，可以按 Host 原子替换自定义 `http.FileSystem`；增加 `ReplaceStaticFSExclusive`，支持选中虚拟主机后隔离后续 Host 静态源。
- **Host 一致性**: Static 与 Service 共用 `host:port → host → :port → *` 候选顺序。
- **流式静态响应**: 自定义静态源通过 `http.ServeContent` 支持 Range、HEAD 和大文件流式读取；非开发模式静态内容默认绕过 output filter。
- **路由兜底**: 新增 `SetFallback`，在静态文件和注册路由均未命中时进入统一处理链。

## v1.5.23 (2026-08-17)
- **流式响应**: `Response.Write` 保持直接写入和实时 Flush，不再因服务注册了输出过滤器而被统一缓冲。
- **显式过滤**: 新增 `Response.WriteFiltered`，静态文件等确实需要输出过滤器处理的内容可显式进入缓冲流程。
- **低代码字节桥接**: 新增 `Response.WriteBytes`，可将动态运行时的字节数组无损写入 SSE 或二进制响应。

## v1.5.22 (2026-07-19)
- **服务发现升级**: 升级 `discover` 至 `v1.5.8`，继承 Redis 类型化注册、心跳、注销和节点拉取流程。
- **依赖对齐**: 随模块图同步升级 `cast`、`config`、`crypto`、`encoding`、`file`、`http`、`id`、`log`、`rand`、`safe`、`shell` 的 patch 版本。
- **兼容性**: 本次仅升级基础依赖，公开 API 与运行配置保持不变。

## v1.5.21 (2026-07-18)
- **依赖校验修复**: 升级 `discover` 至 `v1.5.7`、`redis` 至 `v1.5.11`，并使用稳定版 `redigo v1.9.3`。
- **依赖对齐**: 升级 `cast` 至 `v1.5.4`、`crypto` 至 `v1.5.4`、`encoding` 至 `v1.5.5`、`starter` 至 `v1.5.6`。
- **Discover API 对齐**: 服务注册与代理调用改用新版包级 Discover API；调用配置转换为统一的协议、超时和 Token 字符串格式。
- **Header 命名对齐**: 内部引用统一采用 `RequestId`、`SessionId`、`DeviceId`、`UserId`、`ClientIp` Go 命名。

## v1.5.20 (2026-06-22)
- **Client Key 应答头条件化**:
  - `Device-Id` / `Session-Id` 仅当请求头未携带时才写入应答头，避免客户端已持有 ID 时重复返回。
  - 静态文件和 WebSocket 升级应答中不再写入 `Device-Id` / `Session-Id` 头，仅通过 Cookie 维护身份（浏览器 WebSocket API 不支持自定义请求头）。
- **Client App 头命名规范化**:
  - 客户端上报键名从 `AppName`/`AppVersion` 改为 `App-Name`/`App-Version`（与 `Device-Id`/`Session-Id` 一致使用破折号）。
  - **Breaking**: 旧版客户端需同步修改请求头名称。
- **配置字段重命名**:
  - `NoLogHeaders` → `NoLogRequestHeaders`（请求头排除列表）。
  - `NoLogOutputFields` → `NoLogResponseFields`（响应体字段排除）。
  - 新增 `NoLogResponseHeaders`（响应头排除列表，用户可追加自定义字段）。
  - **Breaking**: 使用了旧字段名的配置需同步修改。
- **动态排除列表**:
  - `NoLogRequestHeaders` 默认值动态构建（包含内部标准头 + 当前配置的 client key 名），用户只需追加自己关心的额外字段。
  - 移除 `init()` 中的硬编码默认值。
- **Cookie 头智能过滤**:
  - `sanitizeLogHeaders` 对 `Cookie` 头不再整体排除，而是解析 Cookie 内容，仅剔除 key 命中排除列表的键值对（如 `Device-Id`/`Session-Id`），保留业务 Cookie。
  - 排除列表同时适用于 Header 名匹配和 Cookie key 匹配。
- **日志应答头排除**:
  - 响应头日志捕获改为使用 `effectiveNoLogResponseHeaders()`，替代之前的硬编码空字符串。

## v1.5.19 (2026-06-22)
- **依赖更新**:
  - 升级依赖 `id` 至 `v1.5.6`，`redis` 至 `v1.5.10`。

## v1.5.18 (2026-06-21)
- **依赖更新**:
  - 升级依赖 `id` 至 `v1.5.5`，`redis` 至 `v1.5.9`（同步修复旧版本 ID 未打乱的 Bug）。

## v1.5.17 (2026-06-21)
- **Session 增强**:
  - `Save` 改为可变参数 `Save(args ...map[string]any) error`，支持传入 map 批量设置后保存。
  - 新增 `Load(keys []string) map[string]any`，支持批量读取，空参数返回全部数据。
  - `Remove` 改为可变参数 `Remove(keys ...string)`，支持一次删除多个 key。
- **日志脱敏引擎**:
  - 新增 `sanitizeLogData` 递归脱敏函数，基于 size 预算、字符串截断、数组/对象元素限制构建日志安全对象，不影响原始数据。
  - 新增 `sanitizeLogHeaders` 统一处理请求/响应头过滤。
  - 新增 `sanitizeRespBody` 自动 JSON 解析后走对象脱敏，非 JSON 按字符串截断。
- **日志配置新增**:
  - `NoLogInput`/`NoLogOutput`/`NoLogAllHeaders` — 高并发加速开关。
  - `LogInputObjectNum`/`LogOutputObjectNum` — 对象最多记录 key 数（默认 10/5）。
  - 各配置默认值: fieldSize=20, arrayNum(In=5/Out=3), maxSize=200, NoLogHeaders 默认过滤内部头。
- **响应体捕获修复**:
  - `response.Write` 改为始终调用 `keepBody` 缓冲 body（限制大小 LogOutputMaxSize），修复 200 响应无日志输出的问题。
  - `outputResult` 先调 `keepBody` 再分路径写出，修复 dev 模式（hasOutFilter）下 PhysicalWrite 绕过 body 缓冲的问题。
- **客户端 Key 默认值**: `Session-ID`/`Device-ID`/`App`，配合 HTTP header canonicalize 显示为 `Session-Id`/`Device-Id`。
- **调试清理**: 移除 handler.go 中 `fmt.Println`/`shell.BMagenta` 调试残留。
- **日志格式优化**: RequestHeaders 颜色 blue，ResponseDataLength key 为 Size，ResponseData/ResponseHeaders 颜色 yellow。
- **依赖更新**: 升级 `js` 至 `v1.5.6`。

## v1.5.15 (2026-06-21)
- **错误堆栈重构**:
  - 重构 `js_export.go`，将匿名占位工厂函数改写为包级具名函数。
  - 使用 `jsmod.MakeError` 包裹 `Upgrade` 返回的错误，并在 `Response`、`Session`、`WebSocketConn` 的相关出错方法中包裹返回的错误。
- **修复**:
  - 修复 `RouteHandler.ServeHTTP` 在 `RouteHandler.ws` 为空时可能触发的 nil 指针引用 panic，使其安全降级使用 `DefaultServer`。
- **依赖对齐**:
  - 升级依赖 `jsmod` 至 `v1.5.3`, `timer` (`v1.5.0`), `cast` 至 `v1.5.3`, `rand` 至 `v1.5.3`, `encoding` 至 `v1.5.4`, `shell` 至 `v1.5.3`, `safe` 至 `v1.5.2`, `id` 至 `v1.5.4`, `crypto` 至 `v1.5.3`, `file` 至 `v1.5.5`, `config` 至 `v1.5.3`, `log` 至 `v1.5.8`, `http` 至 `v1.5.3`, `redis` 至 `v1.5.6`, `starter` 至 `v1.5.5`, `discover` 至 `v1.5.3`。

## v1.5.14 (2026-06-11)
- **依赖对齐**:
  - 对齐基础设施依赖到 v1.5.x。

## v1.5.13 (2026-06-10)
- **优化**:
  - 对齐 JS 导出为 PascalCase 格式。

## v1.5.12 (2026-06-07)
- **基础设施对齐: 切换至 starter v1.5.3 编排模式**:
  - 弃用已废弃的 `starter.Run()`，全面转向 `starter.Start() / starter.Wait()`。

## v1.5.11 (2026-06-06)
- **修复: 路由与静态文件匹配鲁棒性增强**:
  - **路径参数提取**: 修复了正则匹配路由（如 `{name}`）无法正确提取并注入路径参数到业务函数的问题。
  - **静态文件匹配**: 引入 `hostStatics` 有序路由表，实现“最长前缀匹配”策略，解决在复杂或重叠的静态目录配置下的匹配歧义问题。
  - **URL 兼容性**: 针对包含空格、中文字符及特殊符号的复杂 URL，在路由与静态文件匹配阶段统一进行 Robust 处理，彻底解决 404 隐患。
  - **Host 匹配增强**: 验证并明确了 Host 匹配的灵活性，支持 `hostname:port`, `hostname`, `:port` 的自动降级匹配。

## v1.5.10 (2026-06-05)
- **修复: Static 服务 URL 解码**:
  - 修复了 \`service.Static()\` 在处理包含空格或特殊字符（如 \`%20\`）的请求路径时，因未解码导致文件匹配失败的问题。
  - 在路由匹配阶段提前进行 URL Path 解码，提升整体路径匹配的健壮性。

## v1.5.9 (2026-06-05)
- **优化: 低代码环境深度对齐**:
  - **JS 友好型 Header**: 引入 \`service.Header\` 包装类，提供大小写不敏感的 \`Get/Set/Add/Del\` 方法，提升脚本开发体验。
  - **Cookie 遮蔽**: 在 \`Request\` 中实现方法遮蔽，确保 JS 侧看到的 Cookie 参数均为简化的 \`Service_Cookie\`，彻底解决穿透问题。
  - **API 统一**: 将 \`Request.Headers()\` 重命名为 \`Request.Header()\`，与 \`Response.Header()\` 保持命名对齐。
- **重构**: 给内部字段（如 \`*http.Request\`, \`ResponseWriter\`）增加 \`js:"-"\` 标签，精准管控对 JS 暴露的 API 边界。
- **修复**: 解决了因 Header 包装导致的 Go 内部代码（\`handler.go\`, \`static.go\`）编译错误。
- **新特性: EnableWebDev 支持**:
  - 引入了 `service.EnableWebDev(config watch.Config)`，支持自动刷新页面的开发模式。
  - **WebSocket 同步**: 自动注册 `/_watch` 服务，与文件监听器协同工作。
  - **智能 HTML 注入**: 采用 `OutFilter` 在 HTML 响应末尾精准注入 WebSocket 重连脚本，支持静态文件与动态服务。
  - **性能优化**: 仅在开启开发模式时启用响应缓冲，生产环境无任何性能损失。
- **基础设施**: 增加包级 `AddShutdownHook` 支持，提供更优雅的资源回收机制。
- **依赖同步**: 升级至 `log v1.5.5`，对齐不带堆栈的 Warning 规范。

## v1.5.5 (2026-06-05)
- **依赖同步**: 全量对齐至 `@go` 基础设施最新版本（`log v1.5.4`, `starter v1.5.2`, `db v1.5.2`）。

## v1.5.4 (2026-06-05)
- **优化: 生命周期日志剥离**:
  - 彻底移除了 `WebServer` 内部冗余的 `starting / stopping / stopped` 控制台日志输出。
  - **权责对齐**: 服务的生命周期审计现已全量交由 `apigo.cc/go/starter (v1.5.2)` 接管。
  - **专注输出**: `service` 模块现在仅负责输出关键的监听信息（如 `starting listener addr:[::]:8001 proto:http`），并自动继承 `starter` 分配的长 TraceID 建立链路关联。

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
