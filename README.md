# go/service (核心微服务框架)

极简、自动化的 Web 与 WebSocket 服务框架，实现极致的依赖注入与路由映射。

## 核心特性
- **路由反射**: 自动解析函数参数，支持 `*Request`, `*Response`, `*log.Logger` 及自定义结构体自动注入。
- **自动校验**: 集成 `verify` 引擎，通过 Struct Tag 实现入参合法性自动检查。
- **功能闭环**: 内置静态文件服务、WebSocket (带 Action 路由)、URL 重写、反向代理（对接 Discover）。
- **零摩擦启动**: 支持命令行指令管理 (start/stop/help) 及异步平滑启停。

## API 指南

### 1. 服务注册
```go
import "apigo.cc/go/service"

// 注册标准 Web 服务
service.Register(0, "/hello", func(in struct{ Name string }) string {
    return "Hello " + in.Name
}, "打招呼接口")

// 注册 Restful 服务
service.Restful(0, "POST", "/user/{id}", func(args map[string]any) service.Result {
    res := service.Result{}
    res.OK()
    return res
}, "更新用户")
```

### 2. WebSocket 支持
```go
ar := service.RegisterWebsocket(0, "/ws", onOpen, onClose, "聊天室")
ar.RegisterAction(0, "chat", func(in ChatMessage, sess *MySession) {
    // 处理消息
}, "发送消息")
```

### 3. 增强插件
- **静态文件**: `service.Static("/ui", "./static_dir")`
- **URL 重写**: `service.Rewrite("/old", "/new")`
- **反向代理**: `service.Proxy(0, "/api", "other_app", "/api")`

### 4. 生命周期管理
```go
func main() {
    service.CheckCmd() // 处理 start/stop/help 指令
    service.Start()    // 阻塞启动
}
```

## 基础设施对齐
- **类型转换**: `apigo.cc/go/cast`
- **日志系统**: `apigo.cc/go/log`
- **服务发现**: `apigo.cc/go/discover`
- **分布式 ID**: `apigo.cc/go/id`
- **文件操作**: `apigo.cc/go/file`
