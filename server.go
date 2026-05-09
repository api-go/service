package service

import (
	"apigo.cc/go/config"
	"apigo.cc/go/discover"
	"apigo.cc/go/log"
	"context"
	"fmt"
	"golang.org/x/net/http2"
	"golang.org/x/net/http2/h2c"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"
)

// GlobalDiscoverer 供服务框架内部使用的发现实例
var GlobalDiscoverer *discover.Discoverer

// AsyncServer 异步服务实例
type AsyncServer struct {
	server      *http.Server
	listener    net.Listener
	Addr        string
	stopChan    chan os.Signal
	startChan   chan bool
	useDiscover bool
	discoverer  *discover.Discoverer
}

// AsyncStart 异步启动服务
func AsyncStart() *AsyncServer {
	as := &AsyncServer{
		startChan: make(chan bool, 1),
		stopChan:  make(chan os.Signal, 1),
	}

	go as.start()

	<-as.startChan
	return as
}

func (as *AsyncServer) start() {
	listenStr := Config.Listen
	as.useDiscover = false

	if listenStr == "" {
		listenStr = ":0,h2c"
		as.useDiscover = true
	}

	// 解析第一个监听配置
	part := strings.Split(listenStr, "|")[0]
	addr, opts, _ := strings.Cut(part, ",")

	protocol := "http"
	for _, opt := range strings.Split(opts, ",") {
		opt = strings.ToLower(strings.TrimSpace(opt))
		if opt == "h2c" || opt == "h2" {
			protocol = opt
		}
	}

	if !strings.Contains(addr, ":") {
		addr = ":" + addr
	}

	// 检查是否需要启动服务发现
	appName := Config.DiscoverApp
	if appName == "" {
		appName = GetDefaultName()
	}
	if appName != "" {
		as.useDiscover = true
	}

	listener, err := net.Listen("tcp", addr)
	if err != nil {
		log.DefaultLogger.Error("failed to listen", "addr", addr, "error", err.Error())
		as.startChan <- false
		return
	}

	as.listener = listener
	as.Addr = listener.Addr().String()
	serverAddr = as.Addr

	// 如果使用了随机端口且没有明确指定不需要服务发现，则开启
	if addr == ":0" || strings.HasSuffix(addr, ":0") {
		as.useDiscover = true
	}

	h2s := &http2.Server{}
	var handler http.Handler = &RouteHandler{}
	if protocol == "h2c" {
		handler = h2c.NewHandler(handler, h2s)
	}

	as.server = &http.Server{
		Handler: handler,
	}

	// 启动服务发现
	if as.useDiscover && appName != "" {
		_, port, _ := net.SplitHostPort(as.Addr)
		ip := GetServerIp()
		discoverAddr := fmt.Sprintf("%s:%s", ip, port)

		// 从 discover.yml/json 加载基础结构，但不包含敏感的 SafeBuf
		// 此处不再强行注入 discover.Config 的 Calls，而是留给具体业务手动注入，或者后续提供安全的反序列化机制
		var discConf discover.Config
		_ = config.Load(&discConf, "discover")
		
		// Logger 继承
		logger := log.DefaultLogger

		// 解析必需的 Registry，支持环境变量 fallback
		registry := os.Getenv("DISCOVER_REGISTRY")
		if registry == "" {
			registry = "127.0.0.1:6379::15" // Default fallback
		}

		as.discoverer = discover.Start(registry, appName, discoverAddr, logger, discConf)
		GlobalDiscoverer = as.discoverer
		if as.discoverer != nil {
			log.DefaultLogger.Info("discover registered", "app", appName, "addr", discoverAddr)
		}
	}

	signal.Notify(as.stopChan, os.Interrupt, syscall.SIGTERM)

	go func() {
		log.DefaultLogger.Info("service starting", "addr", as.Addr, "proto", protocol)
		as.startChan <- true
		if err := as.server.Serve(listener); err != nil && err != http.ErrServerClosed {
			log.DefaultLogger.Error("server error", "error", err.Error())
		}
	}()
}

// Stop 停止服务
func (as *AsyncServer) Stop() {
	log.DefaultLogger.Info("service stopping")
	if as.discoverer != nil {
		as.discoverer.Stop()
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := as.server.Shutdown(ctx); err != nil {
		log.DefaultLogger.Error("server shutdown error", "error", err.Error())
	}
	log.DefaultLogger.Info("service stopped")
}

// Wait 等待服务结束 (信号监听)
func (as *AsyncServer) Wait() {
	<-as.stopChan
	as.Stop()
}

// Start 同步启动服务
func Start() {
	AsyncStart().Wait()
}
