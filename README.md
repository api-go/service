# go/service (核心微服务框架)

[English](README.en.md) | 简体中文

极简、自动化的 Web 与 WebSocket 服务框架，实现极致的依赖注入与路由映射。

## 核心特性
- **极致精简**: 剥离非核心组件（如 Starter, Task, 业务 Result 定义），保持底座纯净。
- **运行时重载**: `WebServer.Reload` 可原子调整 HTTP、HTTPS、HTTP/2、H2C 监听，并让新 TLS 握手立即使用更新后的证书；配置验证或端口绑定失败时保留当前服务。
- **零配置启动**: 默认监听随机端口并启用 `h2c` 协议，自动完成 `discover` 注册，实现真正的 "Zero-Config" 微服务。
- **智能探测**: 自动识别应用名称 (`ReadBuildInfo`) 与局域网 IP，消除环境摩擦。
- **路由反射**: 自动解析函数参数，支持 `*Request`, `*Response`, `*log.Logger` 及自定义结构体自动注入。
- **自动校验**: 集成 `verify` 引擎，通过 Struct Tag 实现入参合法性自动检查。
- **功能闭环**: 内置静态文件服务、基础 WebSocket 注册、URL 重写、反向代理（对接 Discover）。
- **代理协议**: 普通请求保留 HTTP/2 / H2C 协议选择；WebSocket 在鉴权后独立协商 HTTP/1.1 或 RFC 8441 HTTP/2，支持 Cookie、Origin、子协议、双向数据和控制帧，并纳入停机管理。Discover `Open` 返回握手响应，代理可保留上游 Set-Cookie 和拒绝状态/正文。
- **可控压缩**: `AutoMinify` 优先提供同目录 `.min.*` 预构建资源；运行时压缩保留声明式 HTML 结构、属性引号和 JavaScript 声明名称。JavaScript 使用 esbuild 的空白压缩，不启用语法折叠或标识符改名，避免改变嵌套条件分支；压缩器版本边界参与缓存键。 HTML 内联普通脚本及 `type="module"` 共用该压缩器。`/*html*/` 优化仅作用于解析器识别的无 tag、无 JS 插值模板；字符串、注释、正则和其他模板保持 JS 语义。HTML 压缩保留注释、文档标签及默认属性值；压缩结果若引入 JS 插值、打断模板或改变 JS 转义，则保留该模板原文。模板预解析不支持的语法跳过 HTML 优化，仍由 esbuild 压缩 JS；压缩失败或缓存不可写时原样提供源码。
  自动压缩缓存按资源作用域、路径及源内容隔离，站点同路径和挂载替换不会复用其他源码的缓存。
- **统一 ID 体系**: 整合 Redis 集群版 ID 生成器，全局统一生成 `RequestId` 与 `LogTraceID`。
- **可信客户端 IP**: 默认保留内部 Request-ID relay 兼容模式；公网入口使用 `ClientIP.Mode: direct` 或 `trusted`，后者按可信代理 CIDR 从右向左解析转发链。Request ID 不代表来源认证。
- **可靠停机**: 服务停止时会先取消所有请求的 `Context()`，SSE 和其他长连接应监听 `request.Context().Done()`；WebSocket 会收到 `1001 Going Away` 并在统一停止期限内完成关闭握手，超时后框架强制关闭全部残余连接。

## API 指南

### 1. 服务注册 (Modern HostContext API)
```go
import "apigo.cc/go/service"

// 推荐：流式注册模式
service.Host("*").POST("/hello", func(in struct{ Name string `verify:"length:2+"` }) string {
    return "Hello " + in.Name
}).Auth(0).Memo("打招呼接口")

// 快捷方法支持 GET, POST, PUT, DELETE, ANY 等
service.Host("api.example.com").GET("/user/{id}", getUserInfo).Auth(1)
```

### 2. 分组注册 (Group)
```go
v1 := service.Host("*").Group("/api/v1")
v1.GET("/profile", getProfile)
v1.POST("/update", updateProfile)
```

### 3. WebSocket 支持 (极简模式)
```go
// 整合进 HostContext 链式调用
service.Host("*").WebSocket("/ws", func(conn *websocket.Conn, logger *log.Logger) {
    defer conn.Close()
    // ...
}).Auth(0).Memo("聊天室")
```

### 4. 会话管理 (Session)
框架内置了会话管理机制，支持内存存储和 Redis 存储。

- **获取会话**: 在服务方法中注入 `*service.Session` 参数。
- **自动鉴权**: 如果未设置自定义 `AuthChecker`，框架将自动检查 Session 中的 `_authLevel` 是否满足接口要求。
- **权限校验**: `session.AuthFuncs("func1", "&func2")` 支持 OR 和 AND 逻辑的权限细粒度校验。
- **客户端标识一致性**: 启用 Cookie 时，请求头携带的 Session ID 或 Device ID 为当前客户端标识；当对应 Cookie 缺失或不一致时，框架会同步写入 HttpOnly Cookie，使 Fetch 请求与页面导航使用同一身份。

```go
service.Host("*").POST("/login", func(s *service.Session, in LoginArgs) string {
    // 业务登录逻辑...
    s.SetAuthLevel(1) // 设置鉴权级别
    s.Set("uid", "123")
    s.Save() // 持久化
    return "ok"
})

service.Host("*").GET("/profile", func(s *service.Session) any {
    return s.Get("uid")
}).Auth(1) // 自动要求 Session AuthLevel >= 1
```

### 5. 生命周期管理
```go
func main() {
    // 异步启动
    as := service.AsyncStart()
    // 执行其他初始化...
    as.Wait() // 阻塞并监听信号优雅退出
}
```

### 4. 增强插件
- **静态文件**: `service.Static("/ui", "./static_dir")` 或 `service.Host("example.com").Static("/ui", "./static_dir")`
- **自定义静态源**: `service.Host("example.com").StaticFS("/", source)` 或 `service.ReplaceStaticFS(host, mounts)`，适用于知识库、对象存储和虚拟文件系统。需要虚拟主机隔离时使用 `service.ReplaceStaticFSExclusive(host, mounts)`；选中该 Host 后，文件未命中不会继续进入后续 Host 的自定义静态源。
- **未命中处理**: `service.SetFallback(handler)` 在静态文件和已注册路由都未命中后执行。
- **URL 重写**: `service.Rewrite("/old", "/new")`
- **反向代理**: `service.Proxy(0, "/api", "other_app", "/api")`
- **文档生成**: `service.MakeDocument()` 返回全量接口描述
- **依赖注入**: `service.GetInjectT[T]()` 快速获取已注入的对象或组件

低代码需要转发二进制或 SSE 流时，使用 `response.WriteBytes(chunk)` 原样写入动态运行时提供的字节数组，并在每个需要立即发送的分块后调用 `response.Flush()`；文本输出使用 `response.WriteString(text)`。`Write`、`WriteBytes` 和 `WriteString` 会根据 `Content-Type` 自动缓冲 AutoReversion 或开发注入需要处理的 HTML/CSS，其他内容仍直接输出。首次调用 `Flush()` 会原样发送尚未处理的缓冲，并让当前响应后续写入保持实时流式行为。

自定义静态源必须返回可 `Seek` 的 `http.File`。生产模式下静态内容直接流式输出并绕过 output filter；`EnableWebDev` 仅会为 HTML 显式缓冲内容，用于注入开发热刷新脚本。

启用 `AutoMinify` 时，静态 HTML、CSS、JavaScript 和模块资源会生成压缩缓存；动态响应不会压缩，避免每次请求重复消耗 CPU。JavaScript 中需要压缩的嵌入式 HTML 模板使用 `/*html*/` 标记，例如 `Util.makeDom(/*html*/\`<div>...</div>\`)`；未标记的 JavaScript 模板字符串保持原样。

启用 `AutoReversion` 时，框架会维护静态资源的双向依赖关系，并每 5 秒扫描一次已发现资源。依赖资源更新后，其修改时间会逐层传播为引用方的逻辑修改时间；静态应答的 `Last-Modified`、304 判断和资源版本号均使用该逻辑时间。正常请求只查询内存中的逻辑时间，不会临时遍历依赖树。

HTML 可以使用 `<script srcs="/ui/frameworks/*.js,/ui/components/**/*.js"></script>` 和 `<link rel="stylesheet" hrefs="/ui/*.css">` 声明本地资源模式。AutoReversion 按模式书写顺序展开，单个模式内按路径稳定排序，自动去重，再为每个实际资源独立追加版本号。静态目录与 `http.FileSystem` 挂载会自动提供模式枚举；动态 HTML 可通过 `SetAssetPatternResolver` 提供枚举器。

## 配置指南 (ServiceConfig)

详细配置项可查阅 `config.go` 中的 `ServiceConfig` 结构。通过 `config.Load` 支持从 `env.yml` 或环境变量加载。

### 1. 基础服务配置
```yaml
service:
  App: "user-service"         # 应用名称 (缺省自动探测)
  Listen: ":8080,http|:8443,https" # 多监听用 | 隔开，协议支持 http/https/h2/h2c
  Register: "127.0.0.1:6379"  # 发现中心地址 (Redis URL 或配置名)
  Weight: 100                 # 服务发现权重
  ReadTimeout: 30000          # 读取请求超时 (ms)
  WriteTimeout: 30000         # 写入响应超时 (ms)
  MaxHeaderBytes: 1048576     # 最大头部字节数 (1MB)
  StopTimeout: 5000           # 优雅停机等待时间 (ms)
```

`Listen` 为空时，显式配置 `App` 和 `Register` 的服务使用随机端口 H2C；其他应用优先监听 `80,http|443,https`，端口不可用时整组降级到 `8080,http|8443,https`。HTTPS 未配置证书时会在 `data/ssl/_auto` 生成并复用 99 年有效的自签名证书。

证书支持加密或明文的内联内容与文件。加载时会先尝试 URLBase64 + AES-GCM 解密，失败后按原始 PEM 解析：

```yaml
service:
  SSL:
    "example.com":
      CertFile: "data/ssl/example.com/cert.pem"
      KeyFile: "data/ssl/example.com/key.pem"
    "*.internal.example.com":
      Cert: "<encrypted-or-plain-pem>"
      Key: "<encrypted-or-plain-pem>"
```

应用可按 Host 动态控制 HTTP 到 HTTPS 的 308 重定向，目标端口自动取当前实际 HTTPS listener（包括默认降级后的 8443）：

```go
service.ReplaceForceHTTPS("example.com", true)
```

### 2. 下游调用配置 (Calls)
```yaml
service:
  Calls:
    order-service:            # 目标应用名
      Timeout: 5000           # 调用超时时间 (ms)
      Http2: true             # 强制启用 H2C
      Token: "secure-token"   # 访问凭证
```

### 3. 声明式路由与代理 (Nginx 模式)
除了代码注册，也支持在配置文件中直接声明：
```yaml
service:
  # 代理规则 (Proxy)
  Proxies:
    "aa.com":                 # 指定 Host (支持 aa.com, :8080, *, "")
      - Path: "/api/*"        # 必须以 /* 结尾启用前缀匹配
        ToApp: "backend-app"  # 转发至服务发现的应用名
        ToPath: "/v1/*"       # 后缀映射
      - Path: "/direct"       # 精确匹配
        ToApp: "http://1.1.1.1:80" # 直接转发至 URL
        ToPath: "/hello"
  
  # 重写规则 (Rewrite)
  Rewrites:
    "*":                      # 全局规则
      - Path: "^/old/(.*)$"   # 带捕获组的正则匹配
        ToPath: "/new/$1"     # 变量替换

  # 静态目录 (Static)
  Statics:
    "www.example.com":
      "/ui": "./dist"         # 将 /ui 路径映射至本地 ./dist 目录
```

## 基础设施对齐
- **类型转换**: `apigo.cc/go/cast`
- **日志系统**: `apigo.cc/go/log`
- **服务发现**: `apigo.cc/go/discover`（`service v1.5.22` 对齐 `discover v1.5.8`）
- **分布式 ID**: `apigo.cc/go/id`
- **文件操作**: `apigo.cc/go/file`

## Gateway ingress APIs (v1.5.46)

- `WebServer.Handler() http.Handler`: 使用同一流水线，不启动 listener，适用于测试或嵌入。
- `WebServer.SetClientIPPolicy(ClientIPPolicy) error`: 原子替换 IP 信任策略；Mode 为 legacy/direct/trusted，Header 为 X-Forwarded-For（默认）、X-Real-IP 或 X-Client-IP。trusted 必须提供 TrustedProxies；非法 hop 终止信任。
- `WebServer.SetRequestGate(RequestGate)`: 请求 Body 未读取时执行，早于 HTTPS 重定向、Rewrite、Proxy、Static。返回 true 表示响应已处理。
- `RelayClientIP(request)`: 外部入口在解析真实 IP 后清理并覆盖代理 IP 头，供内部 relay 使用；不提供上游身份认证。
- `PrepareHostPolicy(HostPolicy)`: 校验并编译代理、重写、静态目录、HTTPS 跳转及 native Gate；返回不可变 PreparedHostPolicy。
- `WebServer.ReplaceHostPolicies(map[string]*PreparedHostPolicy)`: 整体原子替换 managed registry。每次请求固定一个快照；managed Host 不回落到本地规则。移除 managed Host 后恢复本地配置。支持精确、端口、通配域名和全局匹配。
- `WebServer.SetCertificateProvider(CertificateProvider)`: 接入外部证书生命周期；返回 nil,nil 时继续 managed/local 证书，错误终止 TLS 握手。
- Proxy 的非零 AuthLevel 在未配置自定义 AuthChecker 时使用 Session 权限检查，与普通接口保持一致。

```yaml
service:
  ClientIP:
    Mode: trusted
    TrustedProxies: ["10.0.0.0/24", "2001:db8:1::/64"]
    Header: X-Forwarded-For
```

只信任明确配置的上级代理；上级必须正确覆盖/追加所选 Header。默认 legacy 仅适用于由网络边界保护的内部服务。TLS 证书更新作用于新握手，请求策略更新作用于新请求。

### 静态根目录边界（v1.5.42）

Static(path, rootPath)的URL后缀在连接rootPath前按根路径归一化，原始及编码的..不能越出注册目录。公开文件与服务端模板应分别放在www／tpl；应用可直接Static("/", "www")。

## v1.5.46 依赖基线

本版本对齐 Log v1.5.17 与发布时模块索引中的上游已发布依赖；公开 API 保持兼容。

Handler 自动提供 GET `/_discover/health`，默认 200/OK；可用 `discover.SetHealthHandler` 自定义状态和内容。入口在可信 IP 解析后补齐标准请求头，调用来源为 `X-From-App`/`X-From-Node`。

代理保留上游状态码、Location、多条 Set-Cookie 和公开 Host；不会自动跟随上游跳转。

## WebSocket 代理配置

`WebServer.SetProxyHTTPClient(*http.Client)`（http 指 go/http）设置直连普通 HTTP 客户端；WebSocket 的默认 Dialer 继承其私有 CA、mTLS、自定义 TCP 拨号和代理配置。`WebServer.SetProxyWebSocketDialer(*websocket.Dialer)` 独立覆盖 WebSocket，nil 恢复默认；两者均应在开始服务前配置。代理不使用共享 CookieJar，只转发当前入口请求的 Cookie。

`ServiceConfig.WebSocketProxy` 对应配置示例：

```yaml
WebSocketProxy:
  HandshakeTimeout: 30000
  WriteTimeout: 30000
  IdleTimeout: 0
  MaxMessageSize: 0
  Compression: false
```

时间单位为毫秒。握手默认使用 RedirectTimeout（未设置时 30 秒）；写入 0 表示 30 秒，负值取消限制；空闲 0 表示允许长期静默，正值对两侧分别检查，数据及 Ping/Pong 续期；消息大小 0 不限制，正值同时约束两侧。代理转发心跳，不主动生成周期 Ping；启用空闲限制时应用应定期发送心跳。压缩默认关闭，两段连接独立协商。转发保留消息语义，不保证原始帧分片结构。

Discover 使用新 Open 接口保留握手结果。拒绝正文最多保留 1024 字节并归一化长度；连接日志记录握手状态、节点、时长、双向消息/字节、关闭码及结束原因。依赖已发布 HTTP v1.5.14 与 Discover v1.5.17。

## HTTP/2 WebSocket（v1.5.48）

原生 WebSocket 路由和代理入口支持 RFC 8441；Go/x/net 的服务端声明仍需启动环境 `GODEBUG=http2xconnect=1`。上游默认自动选择：WSS 优先 H2，ALPN/SETTINGS 不支持时兼容 H1；普通 WS 直接 H1，显式注入 NewClientH2C 可使用 H2C。认证拒绝、证书错误、超时等不触发协议降级。不再增加 WebSocketProxy.HTTP2 配置。默认代理客户端共享连接池与有界能力缓存，Cookie Jar 关闭；Stop 清理其空闲池。显式 SetProxyWebSocketDialer 保持 H1 网络策略。

本版本依赖已发布 HTTP v1.5.14，使用 GOWORK=off 独立验证。TLS 对照方法与原始数据见 http/testdata/websocket/20261022-tls-compare.md。

## External addressing / 外部地址（v1.5.49）

`ServiceConfig.ExternalAddress` and `WebServer.SetExternalAddressPolicy(http.ExternalAddressPolicy)` configure explicit direct-peer trust independently of ClientIPPolicy. Default trusts no forwarding headers, including legacy Request-ID requests. Configure every receiving proxy hop explicitly; no private/loopback trust is implicit.

Ingress pins HTTP external addressing before health, gates, redirects, proxy and business handling. Invalid trusted metadata returns 400. `Request.ExternalAddress()` exposes the result; `MakeUrl` uses its Origin and actual TLS for direct requests (returns empty on invalid addressing). HTTPS redirects use the same external scheme, avoiding loops after TLS termination.

Fixed URL, discovery and WebSocket transparent proxies overwrite both supported address-header pairs from the snapshot and retain Origin unchanged. RPC callers carry browser context separately from the internal destination and credentials. Never use X-Host/X-Scheme raw values for security decisions. Configure forwarding trust explicitly when upgrading services that previously trusted these headers implicitly. Forwarded lists are unsupported; each proxy writes a single complete pair.
