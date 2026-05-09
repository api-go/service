package service

import (
	"apigo.cc/go/discover"
	"apigo.cc/go/log"
	"apigo.cc/go/safe"
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
	appName := Config.App
	if appName == "" {
		appName = GetDefaultName()
	}
	if appName != "" || Config.Register != "" {
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
	if as.useDiscover {
		_, port, _ := net.SplitHostPort(as.Addr)
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

		as.discoverer = discover.Start(registry, appName, discoverAddr, log.DefaultLogger, discConf)
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
