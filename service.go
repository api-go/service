package service

import (
	"apigo.cc/go/log"
	"errors"
	"reflect"
	"regexp"
	"strings"
	"sync"
)

// Map 通用 Map 类型
type Map = map[string]any

// Arr 通用切片类型
type Arr = []any

// WebServiceOptions 服务注册选项
type WebServiceOptions struct {
	Priority int
	NoDoc    bool
	NoBody   bool
	NoLog200 bool
	Host     string
	Ext      Map
	// Limiters []*Limiter // TODO: Integrate Limiter
}

// webServiceType 内部存储的服务元数据
type webServiceType struct {
	authLevel           int
	method              string
	path                string
	pathMatcher         *regexp.Regexp
	pathArgs            []string
	parmsNum            int
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
	data                Map
	memo                string
}

var (
	serverId        string
	serverAddr      string
	serverProto     = "http"
	serverProtoName = "http"
	running         = false

	webServices      = make(map[string]*webServiceType)
	regexWebServices = make([]*webServiceType, 0)
	webServicesLock  = sync.RWMutex{}
	webServicesList  = make([]*webServiceType, 0)

	websocketServices      = make(map[string]*websocketServiceType)
	websocketServicesLock  = sync.RWMutex{}
	websocketServicesList  = make([]*webServiceType, 0)

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

// Register 注册服务（通用方法）
func Register(authLevel int, path string, serviceFunc any, memo string) {
	Restful(authLevel, "", path, serviceFunc, memo)
}

// Restful 注册指定方法的服务
func Restful(authLevel int, method, path string, serviceFunc any, memo string) {
	RestfulWithOptions(authLevel, method, path, serviceFunc, memo, WebServiceOptions{})
}

// RestfulWithOptions 注册带选项的服务
func RestfulWithOptions(authLevel int, method, path string, serviceFunc any, memo string, options WebServiceOptions) {
	s, err := makeCachedService(serviceFunc)
	if err != nil {
		// TODO: Log error properly when logger is ready
		return
	}

	s.authLevel = authLevel
	s.options = options
	s.method = method
	s.path = path
	s.memo = memo

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

	// 简单路径匹配
	if s.pathMatcher == nil {
		webServices[method+path] = s // TODO: Include Host in key
	} else {
		regexWebServices = append(regexWebServices, s)
	}
	webServicesList = append(webServicesList, s)
}

func makeCachedService(matchedService any) (*webServiceType, error) {
	funcType := reflect.TypeOf(matchedService)
	if funcType.Kind() != reflect.Func {
		return nil, errors.New("handler must be a function")
	}

	targetService := &webServiceType{
		parmsNum:            funcType.NumIn(),
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

	for i := 0; i < targetService.parmsNum; i++ {
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
