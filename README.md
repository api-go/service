# go/service (核心微服务框架)

极简、自动化的 Web 与 WebSocket 服务框架，实现极致的依赖注入与路由映射。

## 核心特性
- **极致精简**: 剥离非核心组件（如 Starter, Task, 业务 Result 定义），保持底座纯净。
- **路由反射**: 自动解析函数参数，支持 `*Request`, `*Response`, `*log.Logger` 及自定义结构体自动注入。
- **自动校验**: 集成 `verify` 引擎，通过 Struct Tag 实现入参合法性自动检查。
- **功能闭环**: 内置静态文件服务、基础 WebSocket 注册、URL 重写、反向代理（对接 Discover）。
- **统一 ID 体系**: 整合 Redis 集群版 ID 生成器，全局统一生成 `RequestId` 与 `LogTraceID`。

## API 指南

### 1. 服务注册
```go
import "apigo.cc/go/service"

// 注册标准 Web 服务，自动注入 Struct 参数并执行校验
service.Register(0, "/hello", func(in struct{ Name string `verify:"length:2+"` }) string {
    return "Hello " + in.Name
}, "打招呼接口")
```

### 2. WebSocket 支持 (极简模式)
```go
// 业务自行处理消息循环与逻辑
service.RegisterWebsocket(0, "/ws", func(conn *websocket.Conn, logger *log.Logger) {
    defer conn.Close()
    for {
        _, msg, err := conn.ReadMessage()
        if err != nil {
            break
        }
        logger.Info("received", "msg", string(msg))
    }
}, "聊天室")
```

### 3. 生命周期管理
```go
func main() {
    // 异步启动
    as := service.AsyncStart()
    // 执行其他初始化...
    as.Wait() // 阻塞并监听信号优雅退出
}
```

### 4. 增强插件
- **静态文件**: `service.Static("/ui", "./static_dir")`
- **URL 重写**: `service.Rewrite("/old", "/new")`
- **反向代理**: `service.Proxy(0, "/api", "other_app", "/api")`
- **文档生成**: `service.MakeDocument()` 返回全量接口描述

## 基础设施对齐
- **类型转换**: `apigo.cc/go/cast`
- **日志系统**: `apigo.cc/go/log`
- **服务发现**: `apigo.cc/go/discover`
- **分布式 ID**: `apigo.cc/go/id`
- **文件操作**: `apigo.cc/go/file`
