package service

import (
	"apigo.cc/go/config"
	"apigo.cc/go/discover"
	"apigo.cc/go/log"
	"apigo.cc/go/redis"
	"apigo.cc/go/starter"
	"apigo.cc/go/watch"
	"context"
	"crypto/tls"
	"errors"
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
	"sync/atomic"
	"time"
)

type staticType struct {
	path     string
	rootPath *string
}

type assetPatternState struct {
	fingerprint string
	modTime     int64
}

type WebServer struct {
	clientIPPolicy      atomic.Pointer[compiledClientIPPolicy]
	requestGate         atomic.Pointer[requestGateEntry]
	managedPolicies     atomic.Pointer[hostPolicyRegistry]
	certificateProvider atomic.Pointer[certificateProviderEntry]
	Config              ServiceConfig
	server              *http.Server
	listener            net.Listener
	servers             []*http.Server
	listeners           []net.Listener
	Addr                string
	Addrs               []string
	listenConfigs       []listenConfig
	useDiscover         bool
	discoverStarted     bool
	logger              *log.Logger
	lifecycleMu         sync.Mutex
	lifecycleCtx        context.Context
	lifecycleCancel     context.CancelFunc
	tlsMu               sync.RWMutex
	tlsCertificates     map[string]*tls.Certificate
	tlsFallback         *tls.Certificate

	// 运行时状态
	serverId   string
	serverAddr string
	running    bool

	// Web 服务注册 (按 Host 隔离)
	webServices      map[string]map[string]*webServiceType
	regexWebServices map[string][]*webServiceType
	webServicesLock  sync.RWMutex
	webServicesList  []*webServiceType
	fallbackService  *webServiceType

	websocketServices     map[string]map[string]*websocketServiceType
	websocketServicesLock sync.RWMutex
	websocketServicesList []*websocketServiceType
	websocketConns        map[*WebSocketConn]struct{}
	websocketConnsLock    sync.Mutex
	websocketConnsChanged chan struct{}
	websocketStopping     bool

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
	forceHTTPSHosts  map[string]bool

	// 静态文件服务
	statics           map[string]*string
	staticsByHost     map[string]map[string]*string
	codeStatics       map[string]map[string]*string
	fileStatics       map[string]map[string]*string
	dynamicStatics    map[string]map[string]*string
	hostStatics       map[string][]*staticType
	staticsByHostLock sync.RWMutex
	staticFSByHost    map[string]map[string]http.FileSystem
	staticFSExclusive map[string]bool

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
	webDevEnabled        bool
	webDevConfig         watch.Config
	assetResolver        AssetVersionResolver
	assetPatternResolver AssetPatternResolver
	assetPatternMu       sync.Mutex
	assetPatternStates   map[string]assetPatternState
	assetGraph           *assetDependencyGraph
}

type listenConfig struct {
	addr     string
	protocol string
}

// DefaultServer 全局单例服务实例
var DefaultServer = NewWebServer()

// Config 全局配置对象 (指向 DefaultServer.Config)
var Config = &DefaultServer.Config

func init() {
	// NoLogRequestHeaders / NoLogResponseHeaders 的默认值在日志捕获时动态构建
	Config.LogInputObjectNum = 10
	Config.LogInputArrayNum = 5
	Config.LogInputFieldSize = 20
	Config.LogOutputObjectNum = 5
	Config.LogOutputArrayNum = 3
	Config.LogOutputFieldSize = 20
	Config.LogOutputMaxSize = 200
	Config.AutoReversionCachePath = "data/wwwcache"
}

func NewWebServer() *WebServer {
	ws := &WebServer{
		webServices:           make(map[string]map[string]*webServiceType),
		regexWebServices:      make(map[string][]*webServiceType),
		webServicesList:       make([]*webServiceType, 0),
		websocketServices:     make(map[string]map[string]*websocketServiceType),
		websocketServicesList: make([]*websocketServiceType, 0),
		websocketConns:        make(map[*WebSocketConn]struct{}),
		websocketConnsChanged: make(chan struct{}),
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
		staticFSByHost:        make(map[string]map[string]http.FileSystem),
		staticFSExclusive:     make(map[string]bool),
		webAuthCheckers:       make(map[int]func(int, *log.Logger, *string, map[string]any, *Request, *Response, *WebServiceOptions) (pass bool, object any)),
		injectObjects:         make(map[reflect.Type]any),
		injectFunctions:       make(map[reflect.Type]func() any),
		assetGraph:            newAssetDependencyGraph(),
		assetPatternStates:    make(map[string]assetPatternState),

		usedSessionIdKey: "Session-ID",
		usedDeviceIdKey:  "Device-ID",
		usedClientAppKey: "App",
	}
	return ws
}

func makeDiscoverCallConfig(call CallConfig, fallbackTimeout int) string {
	protocol := "http"
	if call.SSL {
		protocol = "https"
	} else if call.Http2 {
		protocol = "h2c"
	}
	parts := []string{protocol}
	timeout := call.Timeout
	if timeout <= 0 {
		timeout = fallbackTimeout
	}
	if timeout > 0 {
		parts = append(parts, fmt.Sprintf("%dms", timeout))
	}
	if call.Token != "" {
		parts = append(parts, call.Token)
	}
	return strings.Join(parts, ":")
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
	ws.lifecycleMu.Lock()
	defer ws.lifecycleMu.Unlock()
	if ws.lifecycleCancel != nil {
		ws.lifecycleCancel()
	}
	// The starter context passed to Start is bounded by the startup timeout and
	// is cancelled as soon as Start returns. Requests instead share a dedicated
	// service-lifetime context which Stop cancels before graceful shutdown.
	ws.lifecycleCtx, ws.lifecycleCancel = context.WithCancel(context.Background())
	ws.websocketConnsLock.Lock()
	ws.websocketStopping = false
	ws.websocketConnsLock.Unlock()
	if logger == nil {
		logger = log.DefaultLogger
	}
	ws.logger = logger
	if ws.Config.AutoMinify {
		cachePath := ws.Config.AutoReversionCachePath
		if cachePath == "" {
			cachePath = "data/wwwcache"
		}
		if err := CleanupAssetCache(cachePath, 30*24*time.Hour, time.Now()); err != nil {
			logger.Warning("failed to cleanup auto minify cache", "path", cachePath, "error", err.Error())
		}
	}

	// 初始加载配置
	if err := config.Load(&ws.Config, "service"); err != nil {
		logger.Error("failed to load config during start", "error", err.Error())
	}
	if ws.Config.ClientIP.Mode != "" {
		if err := ws.SetClientIPPolicy(ws.Config.ClientIP); err != nil {
			return err
		}
	}
	ws.ApplyConfig()
	if ws.Config.AutoReversion {
		ws.assetGraph.start()
	}

	listenStr := strings.TrimSpace(ws.Config.Listen)
	defaultListen := listenStr == ""
	ws.useDiscover = false

	if ws.webDevEnabled {
		ws.initWebDev(logger)
	}

	explicitApp := strings.TrimSpace(ws.Config.App)
	registry := strings.TrimSpace(ws.Config.Register)
	if registry == "" {
		registry = strings.TrimSpace(os.Getenv("DISCOVER_REGISTRY"))
	}
	if defaultListen {
		if explicitApp != "" && registry != "" {
			listenStr = ":0,h2c"
			ws.useDiscover = true
		} else {
			listenStr = ":80,http|:443,https"
		}
	}

	appName := explicitApp
	if appName == "" {
		appName = GetDefaultName()
		ws.Config.App = appName
	}
	if registry != "" {
		ws.useDiscover = true
	}

	ws.serverId = IDMaker.Get8Bytes4KPerSecond()

	if ws.Config.IdServer != "" {
		rd := redis.GetRedis(ws.Config.IdServer, log.New(ws.serverId))
		if rd.Error == nil {
			IDMaker = redis.NewIDMaker(rd)
		}
	}

	configs, err := parseListenConfigs(listenStr)
	if err != nil {
		return err
	}
	if err = ws.openListeners(configs); err != nil && defaultListen && !ws.useDiscover {
		logger.Warning("default ports unavailable, falling back", "from", listenStr, "to", ":8080,http|:8443,https", "error", err.Error())
		configs, _ = parseListenConfigs(":8080,http|:8443,https")
		err = ws.openListeners(configs)
	}
	if err != nil {
		return err
	}
	ws.listener = ws.listeners[0]
	ws.server = ws.servers[0]
	ws.Addr = ws.Addrs[0]
	ws.serverAddr = ws.Addr
	for _, config := range configs {
		if config.addr == ":0" || strings.HasSuffix(config.addr, ":0") {
			ws.useDiscover = true
		}
	}

	if ws.useDiscover {
		_, port, _ := net.SplitHostPort(ws.Addr)
		ip := GetServerIp()
		discoverAddr := fmt.Sprintf("%s:%s", ip, port)

		weight := ws.Config.Weight
		if weight <= 0 {
			weight = 100
		}

		calls := make(map[string]string, len(ws.Config.Calls))
		for name, call := range ws.Config.Calls {
			calls[name] = makeDiscoverCallConfig(call, ws.Config.RedirectTimeout)
		}

		if registry != "" {
			discover.Init()
			discover.Config.Registry = registry
			discover.Config.App = appName
			discover.Config.Weight = weight
			discover.Config.CallRetryTimes = 10
			discover.Config.Calls = calls
			discover.SetLogger(logger)
			ws.discoverStarted = discover.Start(discoverAddr)
			if ws.discoverStarted {
				logger.Info("discover registered", "app", appName, "addr", discoverAddr)
			}
		}
	}

	errChan := make(chan error, len(ws.servers))
	ws.running = true
	for index, server := range ws.servers {
		listener := ws.listeners[index]
		config := configs[index]
		logger.Info("starting listener", "addr", ws.Addrs[index], "proto", config.protocol)
		go func() {
			if serveErr := server.Serve(listener); serveErr != nil && serveErr != http.ErrServerClosed {
				errChan <- serveErr
			}
		}()
	}

	select {
	case err := <-errChan:
		if err != nil {
			return err
		}
	case <-time.After(100 * time.Millisecond):
	}

	return nil
}

func parseListenConfigs(value string) ([]listenConfig, error) {
	parts := strings.Split(value, "|")
	configs := make([]listenConfig, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		addr, options, _ := strings.Cut(part, ",")
		addr = strings.TrimSpace(addr)
		if !strings.Contains(addr, ":") {
			addr = ":" + addr
		}
		protocol := "http"
		for _, option := range strings.Split(options, ",") {
			switch option = strings.ToLower(strings.TrimSpace(option)); option {
			case "http", "https", "h2", "h2c":
				protocol = option
			}
		}
		configs = append(configs, listenConfig{addr: addr, protocol: protocol})
	}
	if len(configs) == 0 {
		return nil, fmt.Errorf("service listen config is empty")
	}
	return configs, nil
}

func (ws *WebServer) openListeners(configs []listenConfig) error {
	ws.closeListeners()
	listeners, servers, addrs, err := ws.createListeners(configs)
	if err != nil {
		return err
	}
	ws.listeners = listeners
	ws.servers = servers
	ws.Addrs = addrs
	ws.listenConfigs = append([]listenConfig(nil), configs...)
	return nil
}

func (ws *WebServer) createListeners(configs []listenConfig) ([]net.Listener, []*http.Server, []string, error) {
	listeners := make([]net.Listener, 0, len(configs))
	servers := make([]*http.Server, 0, len(configs))
	addrs := make([]string, 0, len(configs))
	closeCreated := func() {
		for _, listener := range listeners {
			_ = listener.Close()
		}
	}
	var tlsConf *tls.Config
	for _, config := range configs {
		listener, err := net.Listen("tcp", config.addr)
		if err != nil {
			closeCreated()
			return nil, nil, nil, fmt.Errorf("failed to listen on %s: %w", config.addr, err)
		}
		if config.protocol == "https" || config.protocol == "h2" {
			if tlsConf == nil {
				tlsConf, err = ws.tlsConfig()
				if err != nil {
					_ = listener.Close()
					closeCreated()
					return nil, nil, nil, err
				}
			}
			listener = tls.NewListener(listener, tlsConf)
		}
		var handler http.Handler = &RouteHandler{ws: ws}
		if config.protocol == "h2c" {
			handler = h2c.NewHandler(handler, &http2.Server{})
		}
		server := &http.Server{Handler: handler, ReadTimeout: time.Duration(ws.Config.ReadTimeout) * time.Millisecond, ReadHeaderTimeout: time.Duration(ws.Config.ReadHeaderTimeout) * time.Millisecond, WriteTimeout: time.Duration(ws.Config.WriteTimeout) * time.Millisecond, IdleTimeout: time.Duration(ws.Config.IdleTimeout) * time.Millisecond, MaxHeaderBytes: ws.Config.MaxHeaderBytes}
		if ws.lifecycleCtx != nil {
			lifecycleCtx := ws.lifecycleCtx
			server.BaseContext = func(net.Listener) context.Context { return lifecycleCtx }
		}
		listeners = append(listeners, listener)
		servers = append(servers, server)
		addrs = append(addrs, listener.Addr().String())
	}
	return listeners, servers, addrs, nil
}

func listenConfigKey(config listenConfig) string {
	return config.protocol + "\x00" + config.addr
}

func cloneServiceConfig(config ServiceConfig) ServiceConfig {
	cloned := config
	cloned.ClientIP.TrustedProxies = append([]string(nil), config.ClientIP.TrustedProxies...)
	cloned.Calls = make(map[string]CallConfig, len(config.Calls))
	for name, call := range config.Calls {
		cloned.Calls[name] = call
	}
	cloned.SSL = make(map[string]*CertSet, len(config.SSL))
	for host, certificate := range config.SSL {
		if certificate == nil {
			cloned.SSL[host] = nil
			continue
		}
		copied := *certificate
		cloned.SSL[host] = &copied
	}
	cloned.AccessTokens = make(map[string]*int, len(config.AccessTokens))
	for token, level := range config.AccessTokens {
		if level == nil {
			cloned.AccessTokens[token] = nil
			continue
		}
		copied := *level
		cloned.AccessTokens[token] = &copied
	}
	cloned.Proxies = cloneNestedAnyMap(config.Proxies)
	cloned.Rewrites = cloneNestedAnyMap(config.Rewrites)
	cloned.Statics = make(map[string]map[string]string, len(config.Statics))
	for host, values := range config.Statics {
		copied := make(map[string]string, len(values))
		for key, value := range values {
			copied[key] = value
		}
		cloned.Statics[host] = copied
	}
	cloned.IndexFiles = append([]string(nil), config.IndexFiles...)
	return cloned
}

func cloneNestedAnyMap(values map[string]map[string]any) map[string]map[string]any {
	cloned := make(map[string]map[string]any, len(values))
	for group, entries := range values {
		copied := make(map[string]any, len(entries))
		for key, value := range entries {
			copied[key] = value
		}
		cloned[group] = copied
	}
	return cloned
}

func (ws *WebServer) reconcileListeners(configs []listenConfig) error {
	if !ws.running {
		return nil
	}

	existing := make(map[string]int, len(ws.listenConfigs))
	for index, config := range ws.listenConfigs {
		existing[listenConfigKey(config)] = index
	}

	var additions []listenConfig
	for _, config := range configs {
		if _, ok := existing[listenConfigKey(config)]; !ok {
			additions = append(additions, config)
		}
	}
	newListeners, newServers, newAddrs, err := ws.createListeners(additions)
	if err != nil {
		return err
	}

	for index, server := range newServers {
		listener := newListeners[index]
		config := additions[index]
		ws.logger.Info("starting listener", "addr", newAddrs[index], "proto", config.protocol)
		go func() {
			if serveErr := server.Serve(listener); serveErr != nil && serveErr != http.ErrServerClosed {
				ws.logger.Error("listener stopped", "addr", listener.Addr().String(), "error", serveErr.Error())
			}
		}()
	}

	added := make(map[string]int, len(additions))
	for index, config := range additions {
		added[listenConfigKey(config)] = index
	}
	nextListeners := make([]net.Listener, 0, len(configs))
	nextServers := make([]*http.Server, 0, len(configs))
	nextAddrs := make([]string, 0, len(configs))
	retained := make(map[int]bool, len(configs))
	for _, config := range configs {
		key := listenConfigKey(config)
		if index, ok := existing[key]; ok {
			retained[index] = true
			nextListeners = append(nextListeners, ws.listeners[index])
			nextServers = append(nextServers, ws.servers[index])
			nextAddrs = append(nextAddrs, ws.Addrs[index])
			continue
		}
		index := added[key]
		nextListeners = append(nextListeners, newListeners[index])
		nextServers = append(nextServers, newServers[index])
		nextAddrs = append(nextAddrs, newAddrs[index])
	}

	removed := make([]*http.Server, 0)
	for index, server := range ws.servers {
		if !retained[index] {
			removed = append(removed, server)
		}
	}
	ws.listeners = nextListeners
	ws.servers = nextServers
	ws.Addrs = nextAddrs
	ws.listenConfigs = append([]listenConfig(nil), configs...)
	ws.listener, ws.server, ws.Addr, ws.serverAddr = ws.listeners[0], ws.servers[0], ws.Addrs[0], ws.Addrs[0]

	if len(removed) > 0 {
		timeout := time.Duration(ws.Config.StopTimeout) * time.Millisecond
		if timeout <= 0 {
			timeout = 5 * time.Second
		}
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), timeout)
			defer cancel()
			for _, server := range removed {
				if shutdownErr := server.Shutdown(ctx); shutdownErr != nil {
					ws.logger.Warning("listener drain failed", "error", shutdownErr.Error())
				}
			}
		}()
	}
	return nil
}

func (ws *WebServer) closeListeners() {
	for _, listener := range ws.listeners {
		_ = listener.Close()
	}
	ws.listeners = nil
	ws.servers = nil
	ws.Addrs = nil
	ws.listenConfigs = nil
}

// Stop 停止服务，实现 starter.Service 接口
func (ws *WebServer) Stop(ctx context.Context) error {
	ws.lifecycleMu.Lock()
	defer ws.lifecycleMu.Unlock()

	ws.running = false
	ws.assetGraph.stop()
	if ws.lifecycleCancel != nil {
		ws.lifecycleCancel()
		ws.lifecycleCancel = nil
		ws.lifecycleCtx = nil
	}
	ws.beginWebSocketShutdown(ctx)

	// 执行停机钩子 (反序)
	ws.shutdownHooksLock.Lock()
	for i := len(ws.shutdownHooks) - 1; i >= 0; i-- {
		ws.shutdownHooks[i]()
	}
	ws.shutdownHooks = nil
	ws.shutdownHooksLock.Unlock()

	if ws.discoverStarted {
		discover.Stop()
		ws.discoverStarted = false
	}
	var shutdownErrors []error
	for _, server := range ws.servers {
		if err := server.Shutdown(ctx); err != nil {
			// Shutdown deliberately leaves active connections open. Once its
			// deadline is reached, force-close them so callers can complete the
			// service lifecycle instead of retrying Stop forever.
			closeErr := server.Close()
			if closeErr != nil && !errors.Is(closeErr, http.ErrServerClosed) {
				shutdownErrors = append(shutdownErrors, errors.Join(err, closeErr))
			} else if !errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded) {
				shutdownErrors = append(shutdownErrors, err)
			}
		}
	}
	if !ws.waitForWebSockets(ctx) {
		ws.closeWebSockets()
	}
	if err := errors.Join(shutdownErrors...); err != nil {
		return err
	}
	ws.closeListeners()
	return nil
}

// Status 检查服务健康状态，实现 starter.Service 接口
func (ws *WebServer) Status() (string, error) {
	if len(ws.servers) == 0 || !ws.running {
		return "", fmt.Errorf("server is not running")
	}
	return strings.Join(ws.Addrs, "|"), nil
}

// Reload 实现配置重新加载，实现 starter.Reloader 接口
func (ws *WebServer) Reload() error {
	ws.lifecycleMu.Lock()
	defer ws.lifecycleMu.Unlock()

	logger := ws.logger
	if logger == nil {
		logger = log.DefaultLogger
	}
	logger.Info("reloading configurations...")

	previousConfig := cloneServiceConfig(ws.Config)
	previousCertificates, previousFallback := ws.tlsCertificates, ws.tlsFallback
	nextConfig := cloneServiceConfig(previousConfig)
	if err := config.Load(&nextConfig, "service"); err != nil {
		return fmt.Errorf("load service config: %w", err)
	}
	var nextClientIP *compiledClientIPPolicy
	if nextConfig.ClientIP.Mode != "" {
		var err error
		nextClientIP, err = compileClientIPPolicy(nextConfig.ClientIP)
		if err != nil {
			return err
		}
	}
	ws.Config = nextConfig
	listenStr := strings.TrimSpace(ws.Config.Listen)
	configs := append([]listenConfig(nil), ws.listenConfigs...)
	if listenStr != "" {
		var err error
		configs, err = parseListenConfigs(listenStr)
		if err != nil {
			ws.Config = previousConfig
			return err
		}
	}
	usesTLS := len(ws.Config.SSL) > 0
	for _, listenerConfig := range configs {
		usesTLS = usesTLS || listenerConfig.protocol == "https" || listenerConfig.protocol == "h2"
	}
	if usesTLS {
		if err := ws.reloadTLSCertificates(); err != nil {
			ws.Config = previousConfig
			return err
		}
	}
	if err := ws.reconcileListeners(configs); err != nil {
		ws.Config = previousConfig
		ws.tlsMu.Lock()
		ws.tlsCertificates, ws.tlsFallback = previousCertificates, previousFallback
		ws.tlsMu.Unlock()
		return err
	}
	if nextClientIP != nil {
		ws.clientIPPolicy.Store(nextClientIP)
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
