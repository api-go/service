# go/service (核心微服务框架)

极简、自动化的 Web 与 WebSocket 服务框架，实现极致的依赖注入与路由映射。

## 核心特性
- **极致精简**: 剥离非核心组件（如 Starter, Task, 业务 Result 定义），保持底座纯净。
- **运行时重载**: `WebServer.Reload` 可原子调整 HTTP、HTTPS、HTTP/2、H2C 监听，并让新 TLS 握手立即使用更新后的证书；配置验证或端口绑定失败时保留当前服务。
- **零配置启动**: 默认监听随机端口并启用 `h2c` 协议，自动完成 `discover` 注册，实现真正的 "Zero-Config" 微服务。
- **智能探测**: 自动识别应用名称 (`ReadBuildInfo`) 与局域网 IP，消除环境摩擦。
- **路由反射**: 自动解析函数参数，支持 `*Request`, `*Response`, `*log.Logger` 及自定义结构体自动注入。
- **自动校验**: 集成 `verify` 引擎，通过 Struct Tag 实现入参合法性自动检查。
- **功能闭环**: 内置静态文件服务、基础 WebSocket 注册、URL 重写、反向代理（对接 Discover）。
- **可控压缩**: `AutoMinify` 优先提供同目录 `.min.*` 预构建资源；运行时压缩保留声明式 HTML 结构、属性引号和 JavaScript 声明名称。
- **统一 ID 体系**: 整合 Redis 集群版 ID 生成器，全局统一生成 `RequestId` 与 `LogTraceID`。
- **可信客户端 IP**: `Request.ClientIp()` 在请求进入服务前已有 Request ID 时信任 `X-Real-IP`/`X-Forwarded-For`；没有 Request ID 的直连请求默认忽略这些代理头，可通过 `AcceptXRealIpWithoutRequestId` 显式放宽。代理链只采用第一个合法地址。
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
