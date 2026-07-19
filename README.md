# go/service (核心微服务框架)

极简、自动化的 Web 与 WebSocket 服务框架，实现极致的依赖注入与路由映射。

## 核心特性
- **极致精简**: 剥离非核心组件（如 Starter, Task, 业务 Result 定义），保持底座纯净。
- **零配置启动**: 默认监听随机端口并启用 `h2c` 协议，自动完成 `discover` 注册，实现真正的 "Zero-Config" 微服务。
- **智能探测**: 自动识别应用名称 (`ReadBuildInfo`) 与局域网 IP，消除环境摩擦。
- **路由反射**: 自动解析函数参数，支持 `*Request`, `*Response`, `*log.Logger` 及自定义结构体自动注入。
- **自动校验**: 集成 `verify` 引擎，通过 Struct Tag 实现入参合法性自动检查。
- **功能闭环**: 内置静态文件服务、基础 WebSocket 注册、URL 重写、反向代理（对接 Discover）。
- **统一 ID 体系**: 整合 Redis 集群版 ID 生成器，全局统一生成 `RequestId` 与 `LogTraceID`。

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
- **URL 重写**: `service.Rewrite("/old", "/new")`
- **反向代理**: `service.Proxy(0, "/api", "other_app", "/api")`
- **文档生成**: `service.MakeDocument()` 返回全量接口描述
- **依赖注入**: `service.GetInjectT[T]()` 快速获取已注入的对象或组件

## 配置指南 (ServiceConfig)

详细配置项可查阅 `config.go` 中的 `ServiceConfig` 结构。通过 `config.Load` 支持从 `env.yml` 或环境变量加载。

### 1. 基础服务配置
```yaml
service:
  App: "user-service"         # 应用名称 (缺省自动探测)
  Listen: ":8080,http|:443"   # 监听端口, 多监听用 | 隔开, 选项用 , 隔开 (http/h2/h2c)
  Register: "127.0.0.1:6379"  # 发现中心地址 (Redis URL 或配置名)
  Weight: 100                 # 服务发现权重
  ReadTimeout: 30000          # 读取请求超时 (ms)
  WriteTimeout: 30000         # 写入响应超时 (ms)
  MaxHeaderBytes: 1048576     # 最大头部字节数 (1MB)
  StopTimeout: 5000           # 优雅停机等待时间 (ms)
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
