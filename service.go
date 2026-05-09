package service

import (
	"apigo.cc/go/log"
	"errors"
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
	paramsNum            int
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

var (
	serverId        string
	serverAddr      string
	serverProto     = "http"
	serverProtoName = "http"
	running         = false

	// webServices 按 Host 隔离: map[host]map[method+path]*webServiceType
	webServices      = make(map[string]map[string]*webServiceType)
	// regexWebServices 按 Host 隔离: map[host][]*webServiceType
	regexWebServices = make(map[string][]*webServiceType)
	webServicesLock  = sync.RWMutex{}
	webServicesList  = make([]*webServiceType, 0)

	websocketServices     = make(map[string]map[string]*websocketServiceType)
	websocketServicesLock = sync.RWMutex{}
	websocketServicesList = make([]*websocketServiceType, 0)

	// Rewrite 与 Proxy 按 Host 隔离
	hostRewrites     = make(map[string][]*rewriteType)
	hostProxies      = make(map[string][]*proxyType)
	hostPoliciesLock = sync.RWMutex{}

	// 过滤器与拦截器
	inFilters       = make([]func(*map[string]any, *Request, *Response, *log.Logger) any, 0)
	outFilters      = make([]func(map[string]any, *Request, *Response, any, *log.Logger) (any, bool), 0)
	errorHandle     func(any, *Request, *Response) any
	webAuthChecker  func(int, *log.Logger, *string, map[string]any, *Request, *Response, *WebServiceOptions) (pass bool, object any)
	webAuthCheckers = make(map[int]func(int, *log.Logger, *string, map[string]any, *Request, *Response, *WebServiceOptions) (pass bool, object any))

	// 注入点
	injectObjects   = make(map[reflect.Type]any)
	injectFunctions = make(map[reflect.Type]func() any)

	usedDeviceIdKey  string
	usedClientAppKey string
	usedSessionIdKey string
	sessionIdMaker   func() string
)

// SetClientKeys 设置客户端标识相关的 Key 映射
func SetClientKeys(deviceIdKey, clientAppKey, sessionIdKey string) {
	usedDeviceIdKey = deviceIdKey
	usedClientAppKey = clientAppKey
	usedSessionIdKey = sessionIdKey
}

// SetSessionIdMaker 设置自定义会话 ID 生成器
func SetSessionIdMaker(maker func() string) {
	sessionIdMaker = maker
}

// SetAuthChecker 设置全局鉴权器
func SetAuthChecker(authChecker func(authLevel int, logger *log.Logger, url *string, in map[string]any, request *Request, response *Response, options *WebServiceOptions) (pass bool, object any)) {
	webAuthChecker = authChecker
}

// AddAuthChecker 为指定级别添加鉴权器
func AddAuthChecker(authLevels []int, authChecker func(authLevel int, logger *log.Logger, url *string, in map[string]any, request *Request, response *Response, options *WebServiceOptions) (pass bool, object any)) {
	for _, al := range authLevels {
		webAuthCheckers[al] = authChecker
	}
}

// SetInFilter 设置前置过滤器
func SetInFilter(filter func(in *map[string]any, request *Request, response *Response, logger *log.Logger) (out any)) {
	inFilters = append(inFilters, filter)
}

// SetOutFilter 设置后置过滤器
func SetOutFilter(filter func(in map[string]any, request *Request, response *Response, out any, logger *log.Logger) (newOut any, isOver bool)) {
	outFilters = append(outFilters, filter)
}

// HostContext 提供流式服务注册能力
type HostContext struct {
	host string
}

// Host 指定服务运行的 Host (支持 "example.com", ":8080", "example.com:8080", "*")
func Host(host string) *HostContext {
	if host == "" {
		host = "*"
	}
	return &HostContext{host: host}
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
			s.pathArgs = append(s.pathArgs, found[1])
		}
		if len(s.pathArgs) > 0 {
			s.pathMatcher, _ = regexp.Compile("^" + keyName + "$")
		}
	}

	webServicesLock.Lock()
	defer webServicesLock.Unlock()

	if s.pathMatcher == nil {
		if webServices[s.host] == nil {
			webServices[s.host] = make(map[string]*webServiceType)
		}
		webServices[s.host][s.method+s.path] = s
	} else {
		regexWebServices[s.host] = append(regexWebServices[s.host], s)
	}
	webServicesList = append(webServicesList, s)
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

func (gc *GroupContext) Rewrite(path string, toPath string) *GroupContext {
	gc.hc.Rewrite(gc.prefix+path, toPath)
	return gc
}

func (gc *GroupContext) Proxy(authLevel int, path string, toApp, toPath string) *GroupContext {
	gc.hc.Proxy(authLevel, gc.prefix+path, toApp, toPath)
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

	websocketServicesLock.Lock()
	defer websocketServicesLock.Unlock()
	if websocketServices[hc.host] == nil {
		websocketServices[hc.host] = make(map[string]*websocketServiceType)
	}
	websocketServices[hc.host][path] = ws
	websocketServicesList = append(websocketServicesList, ws)
	return ws
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
		paramsNum:            funcType.NumIn(),
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
	if obj, exists := injectObjects[dataType]; exists {
		return obj
	}
	if factory, exists := injectFunctions[dataType]; exists {
		return factory()
	}
	return nil
}

// GetInjectT 获取注入对象 (泛型版)
func GetInjectT[T any]() T {
	var zero T
	t := reflect.TypeOf((*T)(nil)).Elem()
	obj := GetInject(t)
	if obj == nil {
		return zero
	}
	return obj.(T)
}
