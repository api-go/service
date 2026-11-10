package service

import (
	"github.com/api-go/log"
	"github.com/api-go/watch"
	"errors"
	"math"
	"reflect"
	"regexp"
	"strings"
	"sync"
)

// webServiceType 内部存储的服务元数据
type webServiceType struct {
	authLevel           int
	method              string
	host                string
	path                string
	pathMatcher         *regexp.Regexp
	pathArgs            []string
	paramsNum           int
	inType              reflect.Type
	inIndex             int
	headersType         reflect.Type
	headersIndex        int
	requestIndex        int
	httpRequestIndex    int
	responseIndex       int
	responseWriterIndex int
	loggerIndex         int
	callerIndex         int
	funcType            reflect.Type
	funcValue           reflect.Value
	options             WebServiceOptions
	data                map[string]any
	memo                string
}

// WebServiceOptions 服务注册选项
type WebServiceOptions struct {
	Priority int
	NoDoc    bool
	NoBody   bool
	NoLog200 bool
	Ext      map[string]any
}

type websocketServiceType struct {
	authLevel int
	host      string
	path      string
	memo      string
	funcType  reflect.Type
	funcValue reflect.Value
	options   WebServiceOptions
}

// SetClientKeys 设置客户端标识相关的 Key 映射
func SetClientKeys(deviceIdKey, clientAppKey, sessionIdKey string) {
	DefaultServer.SetClientKeys(deviceIdKey, clientAppKey, sessionIdKey)
}

func (ws *WebServer) SetClientKeys(deviceIdKey, clientAppKey, sessionIdKey string) {
	ws.usedDeviceIdKey = deviceIdKey
	ws.usedClientAppKey = clientAppKey
	ws.usedSessionIdKey = sessionIdKey
}

// SetSessionIdMaker 设置自定义会话 ID 生成器
func SetSessionIdMaker(maker func() string) {
	DefaultServer.SetSessionIdMaker(maker)
}

func (ws *WebServer) SetSessionIdMaker(maker func() string) {
	ws.sessionIdMaker = maker
}

// SetAuthChecker 设置全局鉴权器
func SetAuthChecker(authChecker func(authLevel int, logger *log.Logger, url *string, in map[string]any, request *Request, response *Response, options *WebServiceOptions) (pass bool, object any)) {
	DefaultServer.SetAuthChecker(authChecker)
}

func (ws *WebServer) SetAuthChecker(authChecker func(authLevel int, logger *log.Logger, url *string, in map[string]any, request *Request, response *Response, options *WebServiceOptions) (pass bool, object any)) {
	ws.webAuthChecker = authChecker
}

// AddAuthChecker 为指定级别添加鉴权器
func AddAuthChecker(authLevels []int, authChecker func(authLevel int, logger *log.Logger, url *string, in map[string]any, request *Request, response *Response, options *WebServiceOptions) (pass bool, object any)) {
	DefaultServer.AddAuthChecker(authLevels, authChecker)
}

func (ws *WebServer) AddAuthChecker(authLevels []int, authChecker func(authLevel int, logger *log.Logger, url *string, in map[string]any, request *Request, response *Response, options *WebServiceOptions) (pass bool, object any)) {
	for _, al := range authLevels {
		ws.webAuthCheckers[al] = authChecker
	}
}

// SetInFilter 设置前置过滤器
func SetInFilter(filter func(in *map[string]any, request *Request, response *Response, logger *log.Logger) (out any)) {
	DefaultServer.SetInFilter(filter)
}

func (ws *WebServer) SetInFilter(filter func(in *map[string]any, request *Request, response *Response, logger *log.Logger) (out any)) {
	ws.inFilters = append(ws.inFilters, filter)
}

// AddShutdownHook 增加停机钩子
func AddShutdownHook(hook func()) {
	DefaultServer.AddShutdownHook(hook)
}

func (ws *WebServer) AddShutdownHook(hook func()) {
	ws.shutdownHooksLock.Lock()
	defer ws.shutdownHooksLock.Unlock()
	ws.shutdownHooks = append(ws.shutdownHooks, hook)
}

// SetOutFilter 设置后置过滤器
func SetOutFilter(filter func(in map[string]any, request *Request, response *Response, out any, logger *log.Logger) (newOut any, isOver bool)) {
	DefaultServer.SetOutFilter(filter)
}

func (ws *WebServer) SetOutFilter(filter func(in map[string]any, request *Request, response *Response, out any, logger *log.Logger) (newOut any, isOver bool)) {
	ws.webServicesLock.Lock()
	defer ws.webServicesLock.Unlock()
	ws.outFilters = append(ws.outFilters, filter)
	ws.hasOutFilter = true
}

// HostContext 提供流式服务注册能力
type HostContext struct {
	ws   *WebServer
	host string
}

// Host 指定服务运行的 Host (支持 "example.com", ":8080", "example.com:8080", "*")
func Host(host string) *HostContext {
	return DefaultServer.Host(host)
}

func (ws *WebServer) Host(host string) *HostContext {
	if host == "" {
		host = "*"
	}
	return &HostContext{ws: ws, host: host}
}

// Register 注册一个 Web 服务 (使用默认 Host "*")
func Register(method, path string, serviceFunc any) *webServiceType {
	return DefaultServer.Register(method, path, serviceFunc)
}

func (ws *WebServer) Register(method, path string, serviceFunc any) *webServiceType {
	return ws.Host("*").Register(method, path, serviceFunc)
}

// SetFallback sets the handler used after static files and registered routes miss.
func SetFallback(serviceFunc any) *webServiceType {
	return DefaultServer.SetFallback(serviceFunc)
}

func (ws *WebServer) SetFallback(serviceFunc any) *webServiceType {
	s, err := makeCachedService(serviceFunc)
	if err != nil {
		return &webServiceType{}
	}
	s.host = "*"
	s.method = "*"
	s.path = "*"
	ws.webServicesLock.Lock()
	ws.fallbackService = s
	ws.webServicesLock.Unlock()
	return s
}

// RegisterWebsocket 注册一个 WebSocket 服务 (使用默认 Host "*")
func RegisterWebsocket(path string, serviceFunc any) *websocketServiceType {
	return DefaultServer.RegisterWebsocket(path, serviceFunc)
}

func (ws *WebServer) RegisterWebsocket(path string, serviceFunc any) *websocketServiceType {
	return ws.Host("*").WebSocket(path, serviceFunc)
}

// Proxy 注册一个代理转发 (使用默认 Host "*")
func Proxy(authLevel int, path string, to string) {
	DefaultServer.Proxy(authLevel, path, to)
}

func (ws *WebServer) Proxy(authLevel int, path string, to string) {
	ws.Host("*").Proxy(authLevel, path, to)
}

// Restful 注册一个符合 RESTful 规范的服务结构体 (使用默认 Host "*")
func Restful(authLevel int, path string, serviceStruct any) {
	DefaultServer.Restful(authLevel, path, serviceStruct)
}

func (ws *WebServer) Restful(authLevel int, path string, serviceStruct any) {
	ws.Host("*").Restful(authLevel, path, serviceStruct)
}

func (hc *HostContext) Register(method, path string, serviceFunc any) *webServiceType {
	s, err := makeCachedService(serviceFunc)
	if err != nil {
		return &webServiceType{} // 返回空对象避免链式调用崩溃
	}

	s.host = hc.host
	s.method = strings.ToUpper(method)
	s.path = path

	// 解析路径参数 {name}
	finder, err := regexp.Compile("{(.*?)}")
	if err == nil {
		keyName := regexp.QuoteMeta(path)
		finds := finder.FindAllStringSubmatch(path, 20)
		for _, found := range finds {
			keyName = strings.Replace(keyName, regexp.QuoteMeta(found[0]), "(.*?)", 1)
			hc.ws.webServicesLock.Lock() // Fixed: use lock from ws
			s.pathArgs = append(s.pathArgs, found[1])
			hc.ws.webServicesLock.Unlock()
		}
		if len(s.pathArgs) > 0 {
			s.pathMatcher, _ = regexp.Compile("^" + keyName + "$")
		}
	}

	hc.ws.webServicesLock.Lock()
	defer hc.ws.webServicesLock.Unlock()

	if s.pathMatcher == nil {
		if hc.ws.webServices[s.host] == nil {
			hc.ws.webServices[s.host] = make(map[string]*webServiceType)
		}
		hc.ws.webServices[s.host][s.method+s.path] = s
	} else {
		hc.ws.regexWebServices[s.host] = append(hc.ws.regexWebServices[s.host], s)
	}
	hc.ws.webServicesList = append(hc.ws.webServicesList, s)
	return s
}

func (hc *HostContext) GET(path string, serviceFunc any) *webServiceType {
	return hc.Register("GET", path, serviceFunc)
}

func (hc *HostContext) POST(path string, serviceFunc any) *webServiceType {
	return hc.Register("POST", path, serviceFunc)
}

func (hc *HostContext) PUT(path string, serviceFunc any) *webServiceType {
	return hc.Register("PUT", path, serviceFunc)
}

func (hc *HostContext) DELETE(path string, serviceFunc any) *webServiceType {
	return hc.Register("DELETE", path, serviceFunc)
}

func (hc *HostContext) PATCH(path string, serviceFunc any) *webServiceType {
	return hc.Register("PATCH", path, serviceFunc)
}

func (hc *HostContext) HEAD(path string, serviceFunc any) *webServiceType {
	return hc.Register("HEAD", path, serviceFunc)
}

func (hc *HostContext) OPTIONS(path string, serviceFunc any) *webServiceType {
	return hc.Register("OPTIONS", path, serviceFunc)
}

func (hc *HostContext) ANY(path string, serviceFunc any) *webServiceType {
	return hc.Register("*", path, serviceFunc)
}

// GroupContext 提供路径分组注册能力
type GroupContext struct {
	hc     *HostContext
	prefix string
}

// Group 创建路径分组
func (hc *HostContext) Group(prefix string) *GroupContext {
	if prefix == "/" {
		prefix = ""
	}
	return &GroupContext{hc: hc, prefix: prefix}
}

func (gc *GroupContext) GET(path string, serviceFunc any) *webServiceType {
	return gc.hc.Register("GET", gc.prefix+path, serviceFunc)
}

func (gc *GroupContext) POST(path string, serviceFunc any) *webServiceType {
	return gc.hc.Register("POST", gc.prefix+path, serviceFunc)
}

func (gc *GroupContext) PUT(path string, serviceFunc any) *webServiceType {
	return gc.hc.Register("PUT", gc.prefix+path, serviceFunc)
}

func (gc *GroupContext) DELETE(path string, serviceFunc any) *webServiceType {
	return gc.hc.Register("DELETE", gc.prefix+path, serviceFunc)
}

func (gc *GroupContext) ANY(path string, serviceFunc any) *webServiceType {
	return gc.hc.Register("*", gc.prefix+path, serviceFunc)
}

func (gc *GroupContext) WebSocket(path string, serviceFunc any) *websocketServiceType {
	return gc.hc.WebSocket(gc.prefix+path, serviceFunc)
}

func (gc *GroupContext) Rewrite(path string, to string) *GroupContext {
	gc.hc.Rewrite(gc.prefix+path, to)
	return gc
}
func (gc *GroupContext) Proxy(authLevel int, path string, to string) *GroupContext {
	gc.hc.Proxy(authLevel, gc.prefix+path, to)
	return gc
}
func (hc *HostContext) WebSocket(path string, serviceFunc any) *websocketServiceType {
	funcType := reflect.TypeOf(serviceFunc)
	if funcType.Kind() != reflect.Func {
		return &websocketServiceType{}
	}

	ws := &websocketServiceType{
		host:      hc.host,
		path:      path,
		funcType:  funcType,
		funcValue: reflect.ValueOf(serviceFunc),
	}

	hc.ws.websocketServicesLock.Lock()
	defer hc.ws.websocketServicesLock.Unlock()
	if hc.ws.websocketServices[hc.host] == nil {
		hc.ws.websocketServices[hc.host] = make(map[string]*websocketServiceType)
	}
	hc.ws.websocketServices[hc.host][path] = ws
	hc.ws.websocketServicesList = append(hc.ws.websocketServicesList, ws)
	return ws
}

// Restful 自动根据方法名注册 RESTful 服务
func (hc *HostContext) Restful(authLevel int, path string, serviceStruct any) {
	v := reflect.ValueOf(serviceStruct)
	t := v.Type()
	if t.Kind() == reflect.Ptr {
		t = t.Elem()
	}

	for i := 0; i < v.NumMethod(); i++ {
		methodName := v.Type().Method(i).Name
		var httpMethod string
		switch {
		case strings.HasPrefix(methodName, "Get"):
			httpMethod = "GET"
		case strings.HasPrefix(methodName, "Post"):
			httpMethod = "POST"
		case strings.HasPrefix(methodName, "Put"):
			httpMethod = "PUT"
		case strings.HasPrefix(methodName, "Delete"):
			httpMethod = "DELETE"
		case strings.HasPrefix(methodName, "Patch"):
			httpMethod = "PATCH"
		default:
			continue
		}

		subPath := strings.ToLower(methodName[len(httpMethod):])
		if subPath == "" {
			hc.Register(httpMethod, path, v.Method(i).Interface()).Auth(authLevel)
		} else {
			fullPath := path
			if !strings.HasSuffix(fullPath, "/") {
				fullPath += "/"
			}
			hc.Register(httpMethod, fullPath+subPath, v.Method(i).Interface()).Auth(authLevel)
		}
	}
}

// webServiceType 链式配置方法
func (s *webServiceType) Auth(level int) *webServiceType {
	s.authLevel = level
	return s
}

func (s *webServiceType) Memo(memo string) *webServiceType {
	s.memo = memo
	return s
}

func (s *webServiceType) Priority(p int) *webServiceType {
	s.options.Priority = p
	return s
}

func (s *webServiceType) NoDoc() *webServiceType {
	s.options.NoDoc = true
	return s
}

func (s *webServiceType) NoBody() *webServiceType {
	s.options.NoBody = true
	return s
}

func (s *webServiceType) NoLog200() *webServiceType {
	s.options.NoLog200 = true
	return s
}

func (s *webServiceType) Ext(key string, val any) *webServiceType {
	if s.options.Ext == nil {
		s.options.Ext = make(map[string]any)
	}
	s.options.Ext[key] = val
	return s
}

// websocketServiceType 链式配置方法
func (s *websocketServiceType) Auth(level int) *websocketServiceType {
	s.authLevel = level
	return s
}

func (s *websocketServiceType) Memo(memo string) *websocketServiceType {
	s.memo = memo
	return s
}

func makeCachedService(matchedService any) (*webServiceType, error) {
	funcType := reflect.TypeOf(matchedService)
	if funcType.Kind() != reflect.Func {
		return nil, errors.New("handler must be a function")
	}

	targetService := &webServiceType{
		paramsNum:           funcType.NumIn(),
		inIndex:             -1,
		headersIndex:        -1,
		requestIndex:        -1,
		httpRequestIndex:    -1,
		responseIndex:       -1,
		responseWriterIndex: -1,
		loggerIndex:         -1,
		callerIndex:         -1,
		funcType:            funcType,
		funcValue:           reflect.ValueOf(matchedService),
	}

	for i := 0; i < targetService.paramsNum; i++ {
		t := funcType.In(i)
		tStr := t.String()
		switch tStr {
		case "*service.Request":
			targetService.requestIndex = i
		case "*http.Request":
			targetService.httpRequestIndex = i
		case "*service.Response":
			targetService.responseIndex = i
		case "http.ResponseWriter":
			targetService.responseWriterIndex = i
		case "*log.Logger":
			targetService.loggerIndex = i
		default:
			if t.Kind() == reflect.Struct || (t.Kind() == reflect.Map && t.Elem().Kind() == reflect.Interface) {
				if targetService.inType == nil {
					targetService.inIndex = i
					targetService.inType = t
				} else if targetService.headersType == nil {
					targetService.headersIndex = i
					targetService.headersType = t
				}
			}
		}
	}

	return targetService, nil
}

// GetInject 获取注入对象
func GetInject(dataType reflect.Type) any {
	return DefaultServer.GetInject(dataType)
}

func (ws *WebServer) GetInject(dataType reflect.Type) any {
	if obj, exists := ws.injectObjects[dataType]; exists {
		return obj
	}
	if factory, exists := ws.injectFunctions[dataType]; exists {
		return factory()
	}
	return nil
}

// GetInjectT 获取注入对象 (泛型版)
func GetInjectT[T any]() T {
	var zero T
	t := reflect.TypeOf((*T)(nil)).Elem()
	obj := DefaultServer.GetInject(t)
	if obj == nil {
		return zero
	}
	return obj.(T)
}

var webDevOnce sync.Once

// EnableWebDev 开启 Web 开发模式，支持自动刷新
func EnableWebDev(config watch.Config) {
	DefaultServer.webDevEnabled = true
	DefaultServer.webDevConfig = config
}

func (ws *WebServer) initWebDev(logger *log.Logger) {
	webDevOnce.Do(func() {
		logger.Warning("Web Development Mode Enabled. This should NOT be used in production environment.")
		onWatchConn := map[string]*WebSocketConn{}
		onWatchLock := sync.Mutex{}

		// 1. 注册 WebSocket 服务
		ws.RegisterWebsocket("/_watch", func(request *Request, conn *WebSocketConn, logger *log.Logger) {
			onWatchLock.Lock()
			onWatchConn[request.Id] = conn
			onWatchLock.Unlock()

			// 保持连接，处理消息 (如 ping)
			for {
				if _, err := conn.ReadString(); err != nil {
					break
				}
			}

			onWatchLock.Lock()
			delete(onWatchConn, request.Id)
			onWatchLock.Unlock()
		})

		// 2. 启动文件监听
		watcher, err := watch.Start(ws.webDevConfig, func(e *watch.Event) {
			onWatchLock.Lock()
			defer onWatchLock.Unlock()
			for _, conn := range onWatchConn {
				_ = conn.Send("reload")
			}
		})

		if err != nil {
			logger.Error("failed to start watch for EnableWebDev", "error", err.Error())
			return
		}

		// 3. 注册停机钩子
		ws.AddShutdownHook(func() {
			watcher.Stop()
			onWatchLock.Lock()
			for _, conn := range onWatchConn {
				_ = conn.Close()
			}
			onWatchLock.Unlock()
		})

		// 4. 注册输出过滤器进行注入
		ws.SetOutFilter(func(in map[string]any, request *Request, response *Response, out any, logger *log.Logger) (newOut any, isOver bool) {
			contentType := response.Header().Get("Content-Type")
			var outStr string

			if out != nil {
				switch v := out.(type) {
				case string:
					outStr = v
				case []byte:
					outStr = string(v)
				}
			}

			if outStr == "" && response.changed {
				outStr = string(response.GetBody())
			}

			if outStr == "" {
				return nil, false
			}

			isHtml := strings.HasPrefix(contentType, "text/html")
			if !isHtml && (contentType == "" || strings.HasPrefix(contentType, "text/plain")) {
				// 检测内容前 100 字节是否包含 <html
				checkLen := int(math.Min(float64(len(outStr)), 100))
				if strings.Contains(strings.ToLower(outStr[0:checkLen]), "<html") {
					isHtml = true
				}
			}

			if isHtml && request.URL.Query().Get("dev") == "1" {
				if strings.Contains(outStr, "let _watchWS = null") {
					return nil, false
				}
				// 注入自动刷新的代码
				injectCode := `<script>
	if (new URLSearchParams(location.search).get('dev') === '1') {
	let _watchWS = null
	let _watchWSConnection = false
	let _watchWSIsFirst = true
    function connect() {
		_watchWSConnection = true
        let ws = new WebSocket(location.protocol.replace('http', 'ws') + '//' + location.host + '/_watch')
        ws.onopen = () => {
			_watchWS = ws
			_watchWSConnection = false
			if( !_watchWSIsFirst ) location.reload()
			_watchWSIsFirst = false
		}
        ws.onmessage = () => {
			location.reload()
		}
        ws.onclose = () => {
			_watchWS = null
			_watchWSConnection = false
		}
    }
	setInterval(()=>{
		if(_watchWS!= null){
			try{
				_watchWS.send("ping")
			}catch(err){
				_watchWS = null
				_watchWSConnection = false
			}
		} else if(!_watchWSConnection){
			connect()
		}
	}, 1000)
    connect()
	}
</script>`
				// 仅替换最后一个 </html> 避免多个标签时的重复注入
				lastIndex := strings.LastIndex(outStr, "</html>")
				if lastIndex != -1 {
					outStr = outStr[:lastIndex] + injectCode + outStr[lastIndex:]
				} else {
					outStr = outStr + injectCode
				}

				// 无论如何，只要我们提供了新的输出，就清空原始 Body，防止 handler 重复写入
				response.ClearBody()
				return []byte(outStr), false
			}
			return nil, false
		})
	})
}
