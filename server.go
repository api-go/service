package service

import (
	"apigo.cc/go/config"
	"apigo.cc/go/discover"
	"apigo.cc/go/log"
	"apigo.cc/go/redis"
	"apigo.cc/go/safe"
	"apigo.cc/go/starter"
	"context"
	"fmt"
	"golang.org/x/net/http2"
	"golang.org/x/net/http2/h2c"
	"net"
	"net/http"
	"os"
	"strings"
	"time"
)

// GlobalDiscoverer 供服务框架内部使用的发现实例
var GlobalDiscoverer *discover.Discoverer

// WebServer 实现了 starter.Service 和 starter.Reloader 接口
type WebServer struct {
	server      *http.Server
	listener    net.Listener
	Addr        string
	useDiscover bool
	discoverer  *discover.Discoverer
	logger      *log.Logger
}

// NewWebServer 创建并返回一个新的 WebServer 实例
func NewWebServer() *WebServer {
	return &WebServer{}
}

// Start 启动服务，实现 starter.Service 接口
func (ws *WebServer) Start(ctx context.Context, logger *log.Logger) error {
	if logger == nil {
		logger = log.DefaultLogger
	}
	ws.logger = logger

	// 初始加载配置
	if err := config.Load(&Config, "service"); err != nil {
		logger.Error("failed to load config during start", "error", err.Error())
	}
	ApplyConfig()
	
	listenStr := Config.Listen
	ws.useDiscover = false

	if listenStr == "" {
		listenStr = ":0,h2c"
		ws.useDiscover = true
	}

	// 解析第一个监听配置
	part := strings.Split(listenStr, "|")[0]
	addr, opts, _ := strings.Cut(part, ",")

	protocol := ""
	for _, opt := range strings.Split(opts, ",") {
		opt = strings.ToLower(strings.TrimSpace(opt))
		if opt == "h2c" || opt == "h2" || opt == "http" || opt == "https" {
			protocol = opt
		}
	}
	
	if protocol == "" {
		protocol = "http" // Default to http
	}

	if !strings.Contains(addr, ":") {
		addr = ":" + addr
	}

	// 检查是否需要启动服务发现
	appName := Config.App
	if appName == "" {
		appName = GetDefaultName()
		Config.App = appName
	}
	if appName != "" || Config.Register != "" {
		ws.useDiscover = true
	}

	// 初始化服务器唯一标识 (8位，物理上限 3,844/s)
	serverId = IDMaker.Get8Bytes4KPerSecond()

	// 初始化分布式 ID 生成器
	if Config.IdServer != "" {
		rd := redis.GetRedis(Config.IdServer, log.New(serverId))
		if rd.Error == nil {
			IDMaker = redis.NewIDMaker(rd)
		}
	}

	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("failed to listen on %s: %w", addr, err)
	}

	ws.listener = listener
	ws.Addr = listener.Addr().String()
	serverAddr = ws.Addr

	// 如果使用了随机端口且没有明确指定不需要服务发现，则开启
	if addr == ":0" || strings.HasSuffix(addr, ":0") {
		ws.useDiscover = true
	}

	h2s := &http2.Server{}
	var handler http.Handler = &RouteHandler{}
	if protocol == "h2c" {
		handler = h2c.NewHandler(handler, h2s)
	}

	ws.server = &http.Server{
		Handler:           handler,
		ReadTimeout:       time.Duration(Config.ReadTimeout) * time.Millisecond,
		ReadHeaderTimeout: time.Duration(Config.ReadHeaderTimeout) * time.Millisecond,
		WriteTimeout:      time.Duration(Config.WriteTimeout) * time.Millisecond,
		IdleTimeout:       time.Duration(Config.IdleTimeout) * time.Millisecond,
		MaxHeaderBytes:    Config.MaxHeaderBytes,
	}

	// 启动服务发现
	if ws.useDiscover {
		_, port, _ := net.SplitHostPort(ws.Addr)
		ip := GetServerIp()
		discoverAddr := fmt.Sprintf("%s:%s", ip, port)

		// 转换配置
		discConf := discover.Config{
			Weight:         Config.Weight,
			CallRetryTimes: 10, // Default
			Calls:          make(map[string]discover.CallConfig),
		}

		if discConf.Weight <= 0 {
			discConf.Weight = 100
		}

		for name, call := range Config.Calls {
			dc := discover.CallConfig{
				Http2: call.Http2,
				SSL:   call.SSL,
			}
			if call.Timeout > 0 {
				dc.Timeout = time.Duration(call.Timeout) * time.Millisecond
			} else if Config.RedirectTimeout > 0 {
				dc.Timeout = time.Duration(Config.RedirectTimeout) * time.Millisecond
			}
			if call.Token != "" {
				dc.Token = safe.NewSafeBuf([]byte(call.Token))
			}
			discConf.Calls[name] = dc
		}

		// 解析必需的 Register，支持环境变量 fallback
		registry := Config.Register
		if registry == "" {
			registry = os.Getenv("DISCOVER_REGISTRY")
		}
		if registry == "" {
			registry = "127.0.0.1:6379::15" // Default fallback
		}

		ws.discoverer = discover.Start(registry, appName, discoverAddr, logger, discConf)
		GlobalDiscoverer = ws.discoverer
		if ws.discoverer != nil {
			logger.Info("discover registered", "app", appName, "addr", discoverAddr)
		}
	}

	errChan := make(chan error, 1)

	go func() {
		logger.Info("service starting", "addr", ws.Addr, "proto", protocol)
		if err := ws.server.Serve(listener); err != nil && err != http.ErrServerClosed {
			errChan <- err
		}
		close(errChan)
	}()

	// 短暂等待验证是否闪退
	select {
	case err := <-errChan:
		if err != nil {
			return err
		}
	case <-time.After(100 * time.Millisecond):
	}

	return nil
}

// Stop 停止服务，实现 starter.Service 接口
func (ws *WebServer) Stop(ctx context.Context) error {
	logger := ws.logger
	if logger == nil {
		logger = log.DefaultLogger
	}
	logger.Info("service stopping")
	if ws.discoverer != nil {
		ws.discoverer.Stop()
	}
	if ws.server != nil {
		if err := ws.server.Shutdown(ctx); err != nil {
			return err
		}
	}
	logger.Info("service stopped")
	return nil
}

// Status 检查服务健康状态，实现 starter.Service 接口
func (ws *WebServer) Status() (string, error) {
	if ws.server == nil {
		return "", fmt.Errorf("server is not running")
	}
	return ws.Addr, nil
}

// Reload 实现配置重新加载，实现 starter.Reloader 接口
func (ws *WebServer) Reload() error {
	logger := ws.logger
	if logger == nil {
		logger = log.DefaultLogger
	}
	logger.Info("reloading configurations...")
	
	// 重新加载配置文件中的策略
	if err := config.Load(&Config, "service"); err != nil {
		logger.Error("failed to load config during reload", "error", err.Error())
	}
	ApplyConfig()
	
	// 触发业务挂载的 Hook
	return triggerReload()
}

// AsyncServer 兼容旧版异步服务实例
type AsyncServer struct {
	*WebServer
}

// Stop 兼容旧版的无参数停止方法
func (as *AsyncServer) Stop() {
	stopTimeout := time.Duration(Config.StopTimeout) * time.Millisecond
	if stopTimeout <= 0 {
		stopTimeout = 5 * time.Second
	}
	ctx, cancel := context.WithTimeout(context.Background(), stopTimeout)
	defer cancel()
	_ = as.WebServer.Stop(ctx)
}

// AsyncStart 兼容旧版的异步启动方法
func AsyncStart() *AsyncServer {
	ws := NewWebServer()
	_ = ws.Start(context.Background(), log.DefaultLogger)
	return &AsyncServer{WebServer: ws}
}

// Wait 等待服务结束 (兼容旧版，直接阻塞)
func (as *AsyncServer) Wait() {
	select {}
}

// Start 兼容旧版的同步启动方法 (通过内部注册 starter 实现)
func Start() {
	stopTimeout := time.Duration(Config.StopTimeout) * time.Millisecond
	if stopTimeout <= 0 {
		stopTimeout = 5 * time.Second
	}
	starter.Register("web-server", NewWebServer(), 100, 5*time.Second, stopTimeout)
	starter.Run()
}
