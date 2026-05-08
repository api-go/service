package service

import (
	"apigo.cc/go/cast"
	"apigo.cc/go/log"
	"apigo.cc/go/standard"
	"encoding/json"
	"io"
	"net/http"
	"reflect"
	"strings"
	"sync/atomic"
	"time"
)

type routeHandler struct {
	webRequestingNum int64
}

func (rh *routeHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	atomic.AddInt64(&rh.webRequestingNum, 1)
	defer atomic.AddInt64(&rh.webRequestingNum, -1)

	startTime := time.Now()
	requestId := r.Header.Get(standard.DiscoverHeaderRequestId)
	if requestId == "" {
		requestId = MakeId(12)
		r.Header.Set(standard.DiscoverHeaderRequestId, requestId)
	}

	request := NewRequest(r)
	request.Id = requestId
	response := NewResponse(w)
	response.Id = requestId
	defer response.checkWriteHeader()

	// 处理 SessionId 和 DeviceId
	handleClientKeys(request, response)

	requestLogger := log.New(requestId)

	// 0. 处理重写 (Rewrite)
	if processRewrite(request, response, requestLogger) {
		return
	}

	// 处理代理 (Proxy)
	if processProxy(request, response, requestLogger) {
		return
	}

	// 1. 路由匹配
	path := r.URL.Path
	host := r.Host

	// 处理静态文件
	if processStatic(path, request, response, requestLogger) {
		return
	}

	s, ws := findService(r.Method, host, path)

	// 2. 参数解析 (Form & Body)
	args := make(map[string]any)
	parseRequestArgs(request, args)

	// 3. 前置过滤器
	var result any
	for _, filter := range inFilters {
		result = filter(&args, request, response, requestLogger)
		if result != nil {
			break
		}
	}

	// 4. 处理业务执行 (WS 或 Web)
	if result == nil {
		if ws != nil {
			doWebsocketService(ws, request, response, requestLogger)
			return
		} else if s != nil {
			// 鉴权
			pass, obj := checkAuth(s, request, response, args, requestLogger)
			if !pass {
				if !response.changed {
					response.WriteHeader(http.StatusForbidden)
				}
				return
			}
			// 执行业务
			result = doWebService(s, request, response, args, nil, requestLogger, obj)
		}
	}

	if s == nil && result == nil {
		response.WriteHeader(http.StatusNotFound)
		return
	}

	// 5. 后置过滤器
	for _, filter := range outFilters {
		newResult, done := filter(args, request, response, result, requestLogger)
		if newResult != nil {
			result = newResult
		}
		if done {
			break
		}
	}

	// 6. 输出结果
	outputResult(response, result)

	// 7. 记录日志
	_ = startTime
}

func findService(method, host, path string) (*webServiceType, *websocketServiceType) {
	webServicesLock.RLock()
	defer webServicesLock.RUnlock()

	// 1. Web Service 匹配
	if s, exists := webServices[method+path]; exists {
		return s, nil
	}
	if s, exists := webServices[path]; exists {
		return s, nil
	}

	// 2. WebSocket 匹配
	websocketServicesLock.RLock()
	defer websocketServicesLock.RUnlock()
	if ws, exists := websocketServices[path]; exists {
		return nil, ws
	}

	// 3. 正则匹配
	for i := len(regexWebServices) - 1; i >= 0; i-- {
		s := regexWebServices[i]
		if s.method != "" && s.method != method {
			continue
		}
		if s.pathMatcher != nil && s.pathMatcher.MatchString(path) {
			return s, nil
		}
	}

	return nil, nil
}

func parseRequestArgs(request *Request, args map[string]any) {
	// Query params
	query := request.URL.Query()
	for k, v := range query {
		if len(v) == 1 {
			args[k] = v[0]
		} else {
			args[k] = v
		}
	}

	// Form params
	if request.Method == http.MethodPost || request.Method == http.MethodPut {
		contentType := request.Header.Get("Content-Type")
		if strings.HasPrefix(contentType, "application/json") {
			body, _ := io.ReadAll(request.Body)
			_ = request.Body.Close()
			if len(body) > 0 {
				_ = json.Unmarshal(body, &args)
			}
		} else {
			_ = request.ParseForm()
			for k, v := range request.Form {
				if len(v) == 1 {
					args[k] = v[0]
				} else {
					args[k] = v
				}
			}
		}
	}
}

func checkAuth(s *webServiceType, request *Request, response *Response, args map[string]any, logger *log.Logger) (bool, any) {
	ac := webAuthCheckers[s.authLevel]
	if ac == nil {
		ac = webAuthChecker
	}
	if ac == nil {
		return true, nil
	}
	return ac(s.authLevel, logger, &request.RequestURI, args, request, response, &s.options)
}

func doWebService(service *webServiceType, request *Request, response *Response, args map[string]any,
	result any, logger *log.Logger, object any) any {
	if result != nil {
		return result
	}

	params := make([]reflect.Value, service.parmsNum)
	for i := 0; i < service.parmsNum; i++ {
		t := service.funcType.In(i)
		switch i {
		case service.requestIndex:
			params[i] = reflect.ValueOf(request)
		case service.httpRequestIndex:
			params[i] = reflect.ValueOf(request.Request)
		case service.responseIndex:
			params[i] = reflect.ValueOf(response)
		case service.responseWriterIndex:
			params[i] = reflect.ValueOf(response.Writer)
		case service.loggerIndex:
			params[i] = reflect.ValueOf(logger)
		case service.inIndex:
			in := reflect.New(service.inType).Interface()
			cast.Convert(in, args)
			// 参数校验
			if service.inType.Kind() == reflect.Struct {
				if ok, _ := VerifyStruct(in, logger); !ok {
					response.WriteHeader(http.StatusBadRequest)
					return "parameter verification failed"
				}
			}
			params[i] = reflect.ValueOf(in).Elem()
		default:
			// 尝试依赖注入
			if obj := GetInject(t); obj != nil {
				params[i] = reflect.ValueOf(obj)
			} else {
				params[i] = reflect.New(t).Elem()
			}
		}
	}

	outs := service.funcValue.Call(params)
	if len(outs) > 0 {
		return outs[0].Interface()
	}
	return ""
}

func outputResult(response *Response, result any) {
	if result == nil {
		return
	}

	var data []byte
	contentType := ""

	switch v := result.(type) {
	case string:
		data = []byte(v)
	case []byte:
		data = v
	default:
		data, _ = cast.ToJSONBytes(result)
		contentType = "application/json; charset=UTF-8"
	}

	if contentType != "" && response.Header().Get("Content-Type") == "" {
		response.Header().Set("Content-Type", contentType)
	}
	_, _ = response.Write(data)
}

func handleClientKeys(request *Request, response *Response) {
	// SessionId
	if usedSessionIdKey != "" {
		sessionId := request.Header.Get(usedSessionIdKey)
		if sessionId == "" && !Config.SessionWithoutCookie {
			if ck, err := request.Cookie(usedSessionIdKey); err == nil {
				sessionId = ck.Value
			}
		}
		if sessionId == "" {
			if sessionIdMaker != nil {
				sessionId = sessionIdMaker()
			} else {
				sessionId = MakeId(14)
			}
			if !Config.SessionWithoutCookie {
				http.SetCookie(response.Writer, &http.Cookie{
					Name:     usedSessionIdKey,
					Value:    sessionId,
					Path:     "/",
					HttpOnly: true,
				})
			}
		}
		request.Header.Set(standard.DiscoverHeaderSessionId, sessionId)
		response.Header().Set(usedSessionIdKey, sessionId)
	}

	// DeviceId
	if usedDeviceIdKey != "" {
		deviceId := request.Header.Get(usedDeviceIdKey)
		if deviceId == "" && !Config.DeviceWithoutCookie {
			if ck, err := request.Cookie(usedDeviceIdKey); err == nil {
				deviceId = ck.Value
			}
		}
		if deviceId == "" {
			deviceId = MakeId(14)
			if !Config.DeviceWithoutCookie {
				http.SetCookie(response.Writer, &http.Cookie{
					Name:     usedDeviceIdKey,
					Value:    deviceId,
					Path:     "/",
					Expires:  time.Now().AddDate(10, 0, 0),
					HttpOnly: true,
				})
			}
		}
		request.Header.Set(standard.DiscoverHeaderDeviceId, deviceId)
		response.Header().Set(usedDeviceIdKey, deviceId)
	}
}
