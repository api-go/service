package service

import (
	"apigo.cc/go/config"
	"apigo.cc/go/discover"
	"apigo.cc/go/log"
	"apigo.cc/go/redis"
	"apigo.cc/go/safe"
	"apigo.cc/go/starter"
	"apigo.cc/go/watch"
	"context"
	"fmt"
	"golang.org/x/net/http2"
	"golang.org/x/net/http2/h2c"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"sync"
	"time"
)

type staticType struct {
	path     string
	rootPath *string
}

type WebServer struct {
	Config      ServiceConfig
	server      *http.Server
	listener    net.Listener
	Addr        string
	useDiscover bool
	discoverer  *discover.Discoverer
	logger      *log.Logger

	// 运行时状态
	serverId   string
	serverAddr string
	running    bool

	// Web 服务注册 (按 Host 隔离)
	webServices      map[string]map[string]*webServiceType
	regexWebServices map[string][]*webServiceType
	webServicesLock  sync.RWMutex
	webServicesList  []*webServiceType

	websocketServices     map[string]map[string]*websocketServiceType
	websocketServicesLock sync.RWMutex
	websocketServicesList []*websocketServiceType

	// 路由策略 (按 Host 隔离)
	hostRewrites map[string][]*rewriteType
	hostProxies  map[string][]*proxyType

	codeProxies    map[string][]*proxyType
	fileProxies    map[string][]*proxyType
	dynamicProxies map[string][]*proxyType

	codeRewrites    map[string][]*rewriteType
	fileRewrites    map[string][]*rewriteType
	dynamicRewrites map[string][]*rewriteType

	hostPoliciesLock sync.RWMutex

	// 静态文件服务
	statics           map[string]*string
	staticsByHost     map[string]map[string]*string
	codeStatics       map[string]map[string]*string
	fileStatics       map[string]map[string]*string
	dynamicStatics    map[string]map[string]*string
	hostStatics       map[string][]*staticType
	staticsByHostLock sync.RWMutex

	// 过滤器与拦截器
	inFilters       []func(*map[string]any, *Request, *Response, *log.Logger) any
	outFilters      []func(map[string]any, *Request, *Response, any, *log.Logger) (any, bool)
	errorHandle     func(any, *Request, *Response) any
	webAuthChecker  func(int, *log.Logger, *string, map[string]any, *Request, *Response, *WebServiceOptions) (pass bool, object any)
	webAuthCheckers map[int]func(int, *log.Logger, *string, map[string]any, *Request, *Response, *WebServiceOptions) (pass bool, object any)

	// 注入点
	injectObjects   map[reflect.Type]any
	injectFunctions map[reflect.Type]func() any

	// 客户端标识
	usedDeviceIdKey  string
	usedClientAppKey string
	usedSessionIdKey string
	sessionIdMaker   func() string

	// 停机钩子
	shutdownHooks     []func()
	shutdownHooksLock sync.Mutex

	// 性能优化：标记是否有输出过滤器
	hasOutFilter bool

	// Web 开发模式配置
	webDevEnabled bool
	webDevConfig  watch.Config
}

// DefaultServer 全局单例服务实例
var DefaultServer = NewWebServer()

// Config 全局配置对象 (指向 DefaultServer.Config)
var Config = &DefaultServer.Config

func init() {
	Config.NoLogHeaders = "X-Request-Id,X-Device-Id,X-Session-Id,Cookie,Device-Id,Session-Id"
	Config.LogInputObjectNum = 10
	Config.LogInputArrayNum = 5
	Config.LogInputFieldSize = 20
	Config.LogOutputObjectNum = 5
	Config.LogOutputArrayNum = 3
	Config.LogOutputFieldSize = 20
	Config.LogOutputMaxSize = 200
}

func NewWebServer() *WebServer {
	ws := &WebServer{
		webServices:           make(map[string]map[string]*webServiceType),
		regexWebServices:      make(map[string][]*webServiceType),
		webServicesList:       make([]*webServiceType, 0),
		websocketServices:     make(map[string]map[string]*websocketServiceType),
		websocketServicesList: make([]*websocketServiceType, 0),
		hostRewrites:          make(map[string][]*rewriteType),
		hostProxies:           make(map[string][]*proxyType),
		codeProxies:           make(map[string][]*proxyType),
		fileProxies:           make(map[string][]*proxyType),
		dynamicProxies:        make(map[string][]*proxyType),
		codeRewrites:          make(map[string][]*rewriteType),
		fileRewrites:          make(map[string][]*rewriteType),
		dynamicRewrites:       make(map[string][]*rewriteType),
		statics:               make(map[string]*string),
		staticsByHost:         make(map[string]map[string]*string),
		codeStatics:           make(map[string]map[string]*string),
		fileStatics:           make(map[string]map[string]*string),
		dynamicStatics:        make(map[string]map[string]*string),
		hostStatics:           make(map[string][]*staticType),
		webAuthCheckers:       make(map[int]func(int, *log.Logger, *string, map[string]any, *Request, *Response, *WebServiceOptions) (pass bool, object any)),
		injectObjects:         make(map[reflect.Type]any),
		injectFunctions:       make(map[reflect.Type]func() any),

			usedSessionIdKey: "Session-ID",
			usedDeviceIdKey:  "Device-ID",
			usedClientAppKey: "App",
	}
	return ws
}

// SetDiscovererForTest 提供给测试用例使用的后门方法，用于模拟断开或重置服务发现
func SetDiscovererForTest(d *discover.Discoverer) {
	DefaultServer.discoverer = d
}

// ApplyConfig 将 ServiceConfig 中的路由策略应用到内部的文件级策略中
func (ws *WebServer) ApplyConfig() {
	ws.hostPoliciesLock.Lock()
	defer ws.hostPoliciesLock.Unlock()

	// 1. Proxies KV 解析
	ws.fileProxies = make(map[string][]*proxyType)
	for host, kv := range ws.Config.Proxies {
		h := host
		if h == "*" {
			h = ""
		}
		rules := make([]*proxyType, 0, len(kv))
		for path, val := range kv {
			if to, ok := val.(string); ok {
				rules = append(rules, parseProxyRule(0, path, "", "", to))
			} else {
				// 对象模式
				m := make(map[string]any)
				if tm, ok := val.(map[string]any); ok {
					m = tm
				}
				rules = append(rules, parseProxyRule(
					int(reflect.ValueOf(m["Auth"]).Int()), // Simplified
					path,
					fmt.Sprint(m["ToApp"]),
					fmt.Sprint(m["ToPath"]),
					fmt.Sprint(m["To"]),
				))
			}
		}
		ws.fileProxies[h] = rules
		ws.rebuildProxiesUnderLock(h)
	}

	// 2. Rewrites KV 解析
	ws.fileRewrites = make(map[string][]*rewriteType)
	for host, kv := range ws.Config.Rewrites {
		h := host
		if h == "*" {
			h = ""
		}
		rules := make([]*rewriteType, 0, len(kv))
		for path, val := range kv {
			if to, ok := val.(string); ok {
				rules = append(rules, parseRewriteRule(path, "", to))
			} else {
				m := make(map[string]any)
				if tm, ok := val.(map[string]any); ok {
					m = tm
				}
				rules = append(rules, parseRewriteRule(
					path,
					fmt.Sprint(m["ToPath"]),
					fmt.Sprint(m["To"]),
				))
			}
		}
		ws.fileRewrites[h] = rules
		ws.rebuildRewritesUnderLock(h)
	}

	ws.staticsByHostLock.Lock()
	defer ws.staticsByHostLock.Unlock()
	ws.fileStatics = make(map[string]map[string]*string)

	for host, config := range ws.Config.Statics {
		h := host
		if h == "*" {
			h = ""
		}
		newStatics := make(map[string]*string, len(config))
		for path, rootPath := range config {
			rp := rootPath
			if !filepath.IsAbs(rp) {
				if absPath, err := filepath.Abs(rp); err == nil {
					rp = absPath
				}
			}
			newStatics[path] = &rp
		}
		ws.fileStatics[h] = newStatics
		ws.rebuildStaticsUnderLock(h)
	}

	// 始终重新构建默认 Host 的静态路由，以合并代码定义的路由
	ws.rebuildStaticsUnderLock("")
}

func (ws *WebServer) rebuildProxiesUnderLock(host string) {
	combined := make([]*proxyType, 0)
	combined = append(combined, ws.codeProxies[host]...)
	combined = append(combined, ws.fileProxies[host]...)
	combined = append(combined, ws.dynamicProxies[host]...)

	sort.Slice(combined, func(i, j int) bool {
		return len(combined[i].fromPath) > len(combined[j].fromPath)
	})
	ws.hostProxies[host] = combined
}

func (ws *WebServer) rebuildRewritesUnderLock(host string) {
	combined := make([]*rewriteType, 0)
	combined = append(combined, ws.codeRewrites[host]...)
	combined = append(combined, ws.fileRewrites[host]...)
	combined = append(combined, ws.dynamicRewrites[host]...)

	sort.Slice(combined, func(i, j int) bool {
		return len(combined[i].fromPath) > len(combined[j].fromPath)
	})
	ws.hostRewrites[host] = combined
}

func (ws *WebServer) rebuildStaticsUnderLock(host string) {
	combined := make(map[string]*string)
	for k, v := range ws.codeStatics[host] {
		combined[k] = v
	}
	for k, v := range ws.fileStatics[host] {
		combined[k] = v
	}
	for k, v := range ws.dynamicStatics[host] {
		combined[k] = v
	}

	if host == "" {
		ws.statics = combined
	} else {
		ws.staticsByHost[host] = combined
	}

	// 构造有序的静态路由列表 (按路径长度降序排列，实现最长匹配)
	sorted := make([]*staticType, 0, len(combined))
	for k, v := range combined {
		sorted = append(sorted, &staticType{path: k, rootPath: v})
	}
	sort.Slice(sorted, func(i, j int) bool {
		return len(sorted[i].path) > len(sorted[j].path)
	})
	ws.hostStatics[host] = sorted
}

// Start 启动服务，实现 starter.Service 接口
func (ws *WebServer) Start(ctx context.Context, logger *log.Logger) error {
	if logger == nil {
		logger = log.DefaultLogger
	}
	ws.logger = logger

	// 初始加载配置
	if err := config.Load(&ws.Config, "service"); err != nil {
		logger.Error("failed to load config during start", "error", err.Error())
	}
	ws.ApplyConfig()

	listenStr := ws.Config.Listen
	ws.useDiscover = false

	if listenStr == "" {
		listenStr = ":0,h2c"
		ws.useDiscover = true
	}

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
		protocol = "http"
	}

	if !strings.Contains(addr, ":") {
		addr = ":" + addr
	}

	if ws.webDevEnabled {
		ws.initWebDev(logger)
	}

	appName := ws.Config.App
	if appName == "" {
		appName = GetDefaultName()
		ws.Config.App = appName
	}
	if appName != "" || ws.Config.Register != "" {
		ws.useDiscover = true
	}

	ws.serverId = IDMaker.Get8Bytes4KPerSecond()

	if ws.Config.IdServer != "" {
		rd := redis.GetRedis(ws.Config.IdServer, log.New(ws.serverId))
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
	ws.serverAddr = ws.Addr

	if addr == ":0" || strings.HasSuffix(addr, ":0") {
		ws.useDiscover = true
	}

	h2s := &http2.Server{}
	var handler http.Handler = &RouteHandler{ws: ws}
	if protocol == "h2c" {
		handler = h2c.NewHandler(handler, h2s)
	}

	ws.server = &http.Server{
		Handler:           handler,
		ReadTimeout:       time.Duration(ws.Config.ReadTimeout) * time.Millisecond,
		ReadHeaderTimeout: time.Duration(ws.Config.ReadHeaderTimeout) * time.Millisecond,
		WriteTimeout:      time.Duration(ws.Config.WriteTimeout) * time.Millisecond,
		IdleTimeout:       time.Duration(ws.Config.IdleTimeout) * time.Millisecond,
		MaxHeaderBytes:    ws.Config.MaxHeaderBytes,
	}

	if ws.useDiscover {
		_, port, _ := net.SplitHostPort(ws.Addr)
		ip := GetServerIp()
		discoverAddr := fmt.Sprintf("%s:%s", ip, port)

		discConf := discover.Config{
			Weight:         ws.Config.Weight,
			CallRetryTimes: 10,
			Calls:          make(map[string]discover.CallConfig),
		}

		if discConf.Weight <= 0 {
			discConf.Weight = 100
		}

		for name, call := range ws.Config.Calls {
			dc := discover.CallConfig{
				Http2: call.Http2,
				SSL:   call.SSL,
			}
			if call.Timeout > 0 {
				dc.Timeout = time.Duration(call.Timeout) * time.Millisecond
			} else if ws.Config.RedirectTimeout > 0 {
				dc.Timeout = time.Duration(ws.Config.RedirectTimeout) * time.Millisecond
			}
			if call.Token != "" {
				dc.Token = safe.NewSafeBuf([]byte(call.Token))
			}
			discConf.Calls[name] = dc
		}

		registry := ws.Config.Register
		if registry == "" {
			registry = os.Getenv("DISCOVER_REGISTRY")
		}

		if registry != "" {
			ws.discoverer = discover.Start(registry, appName, discoverAddr, logger, discConf)
			if ws.discoverer != nil {
				logger.Info("discover registered", "app", appName, "addr", discoverAddr)
			}
		}
	}

	errChan := make(chan error, 1)

	go func() {
		logger.Info("starting listener", "addr", ws.Addr, "proto", protocol)
		ws.running = true
		if err := ws.server.Serve(ws.listener); err != nil && err != http.ErrServerClosed {
			errChan <- err
		}
		close(errChan)
	}()

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
	ws.running = false

	// 执行停机钩子 (反序)
	ws.shutdownHooksLock.Lock()
	for i := len(ws.shutdownHooks) - 1; i >= 0; i-- {
		ws.shutdownHooks[i]()
	}
	ws.shutdownHooks = nil
	ws.shutdownHooksLock.Unlock()

	if ws.discoverer != nil {
		ws.discoverer.Stop()
	}
	if ws.server != nil {
		if err := ws.server.Shutdown(ctx); err != nil {
			return err
		}
	}
	return nil
}

// Status 检查服务健康状态，实现 starter.Service 接口
func (ws *WebServer) Status() (string, error) {
	if ws.server == nil || !ws.running {
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

	if err := config.Load(&ws.Config, "service"); err != nil {
		logger.Error("failed to load config during reload", "error", err.Error())
	}
	ws.ApplyConfig()

	return ws.triggerReload()
}

// AsyncServer 兼容旧版异步服务实例
type AsyncServer struct {
	*WebServer
}

// Stop 兼容旧版的无参数停止方法
func (as *AsyncServer) Stop() {
	stopTimeout := time.Duration(as.Config.StopTimeout) * time.Millisecond
	if stopTimeout <= 0 {
		stopTimeout = 5 * time.Second
	}
	ctx, cancel := context.WithTimeout(context.Background(), stopTimeout)
	defer cancel()
	_ = as.WebServer.Stop(ctx)
}

// AsyncStart 兼容旧版的异步启动方法
func AsyncStart() *AsyncServer {
	_ = DefaultServer.Start(context.Background(), log.DefaultLogger)
	return &AsyncServer{WebServer: DefaultServer}
}

// Wait 等待服务结束 (兼容旧版，直接阻塞)
func (as *AsyncServer) Wait() {
	select {}
}

var startOnce sync.Once

// Start 兼容旧版的同步启动方法 (通过内部注册 starter 实现)
func Start() {
	if DefaultServer.running {
		return
	}
	startOnce.Do(func() {
		stopTimeout := time.Duration(Config.StopTimeout) * time.Millisecond
		if stopTimeout <= 0 {
			stopTimeout = 5 * time.Second
		}
		starter.Register("web-server", DefaultServer, 100, 5*time.Second, stopTimeout)
		if err := starter.Start(); err == nil {
			starter.Wait()
		}
	})
}
