package service

import (
	"apigo.cc/go/cast"
	"apigo.cc/go/discover"
	"apigo.cc/go/log"
	"apigo.cc/go/timer"
	"io"
	"net/http"
	"reflect"
	"strings"
	"sync/atomic"
	"time"
)

type RouteHandler struct {
	webRequestingNum int64
}

func (rh *RouteHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	atomic.AddInt64(&rh.webRequestingNum, 1)
	defer atomic.AddInt64(&rh.webRequestingNum, -1)

	tracker := timer.Start()
	requestId := r.Header.Get(discover.HeaderRequestID)
	if requestId == "" {
		requestId = MakeId(12)
		r.Header.Set(discover.HeaderRequestID, requestId)
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

	authLevel := 0
	priority := 0
	if s != nil {
		authLevel = s.authLevel
		priority = s.options.Priority
	}

	// 4. 处理业务执行 (WS 或 Web)
	if result == nil {
		if ws != nil {
			authLevel = ws.authLevel
			priority = ws.options.Priority
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
	if s == nil || !s.options.NoLog200 || response.Code != 200 {
		scheme := "http"
		if r.TLS != nil {
			scheme = "https"
		}
		usedTime := float32(tracker.Stop().Seconds())

		// 获取一些 Header 信息
		reqHeaders := make(map[string]string)
		for k, v := range r.Header {
			reqHeaders[k] = strings.Join(v, ", ")
		}
		respHeaders := make(map[string]string)
		for k, v := range response.Header() {
			respHeaders[k] = strings.Join(v, ", ")
		}

		// 限制记录的 Body 长度
		respData := ""
		if response.Code != 200 {
			if len(response.body) < 1024 {
				respData = string(response.body)
			} else {
				respData = string(response.body[:1024]) + "..."
			}
		}

		logRequest(
			requestLogger,
			r.Method, path, host, scheme, r.Proto,
			request.ClientIp(), serverId, "", "", // app, node 暂无
			r.Header.Get(discover.HeaderFromApp), r.Header.Get(discover.HeaderFromNode),
			"", request.DeviceId(), request.SessionId(), requestId,
			request.Header.Get(discover.HeaderClientAppName), request.Header.Get(discover.HeaderClientAppVersion),
			authLevel, priority,
			reqHeaders, args,
			response.Code, usedTime,
			respHeaders, respData, uint(len(response.body)),
		)
	}
}

func findService(method, host, path string) (*webServiceType, *websocketServiceType) {
	webServicesLock.RLock()
	defer webServicesLock.RUnlock()

	// 1. 准备 Host 候选列表: "host:port", "host", ":port", "*"
	hostOnly, port, _ := strings.Cut(host, ":")
	hosts := []string{host}
	if port != "" {
		hosts = append(hosts, hostOnly, ":"+port)
	}
	hosts = append(hosts, "*")

	// 2. 匹配 Web Service
	for _, h := range hosts {
		if services, exists := webServices[h]; exists {
			if s, ok := services[method+path]; ok {
				return s, nil
			}
			if s, ok := services["*"+path]; ok {
				return s, nil
			}
		}
	}

	// 3. 匹配 WebSocket
	websocketServicesLock.RLock()
	defer websocketServicesLock.RUnlock()
	for _, h := range hosts {
		if services, exists := websocketServices[h]; exists {
			if ws, ok := services[path]; ok {
				return nil, ws
			}
		}
	}

	// 4. 正则匹配
	for _, h := range hosts {
		if services, exists := regexWebServices[h]; exists {
			for i := len(services) - 1; i >= 0; i-- {
				s := services[i]
				if s.method != "*" && s.method != method {
					continue
				}
				if s.pathMatcher != nil && s.pathMatcher.MatchString(path) {
					return s, nil
				}
			}
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
				_ = cast.UnmarshalJSON(body, &args)
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

	params := make([]reflect.Value, service.paramsNum)
	for i := 0; i < service.paramsNum; i++ {
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
		request.Header.Set(discover.HeaderSessionID, sessionId)
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
		request.Header.Set(discover.HeaderDeviceID, deviceId)
		response.Header().Set(usedDeviceIdKey, deviceId)
	}
}
