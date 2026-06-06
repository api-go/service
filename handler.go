package service

import (
	"apigo.cc/go/cast"
	"apigo.cc/go/discover"
	"apigo.cc/go/log"
	"apigo.cc/go/timer"
	"io"
	"net/http"
	"net/url"
	"reflect"
	"runtime/debug"
	"strings"
	"sync/atomic"
	"time"
)

type RouteHandler struct {
	ws               *WebServer
	webRequestingNum int64
}

func (rh *RouteHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	ws := rh.ws
	atomic.AddInt64(&rh.webRequestingNum, 1)
	defer atomic.AddInt64(&rh.webRequestingNum, -1)

	tracker := timer.Start()
	requestId := r.Header.Get(discover.HeaderRequestID)
	if requestId == "" {
		requestId = IDMaker.Get10Bytes14MPerSecond()
		r.Header.Set(discover.HeaderRequestID, requestId)
	}

	request := NewRequest(r)
	request.Id = requestId
	response := NewResponse(w, ws)
	response.Id = requestId
	requestLogger := log.New(requestId)

	// 0. 延迟处理日志与状态检查
	var s *webServiceType
	var wsc *websocketServiceType
	var authLevel int
	var priority int
	var args = make(map[string]any)
	var result any

	defer func() {
		// 捕捉 Panic
		if err := recover(); err != nil {
			requestLogger.Error("panic recovered", "requestId", requestId, "path", r.URL.Path, "error", err, "stack", string(debug.Stack()))
			if !response.changed {
				response.WriteHeader(http.StatusInternalServerError)
				outputResult(response, "internal server error")
			}
		}

		response.checkWriteHeader()

		// 记录日志
		if (s == nil || !s.options.NoLog200 || response.Code != 200) &&
			!(ws.Config.NoLogGets && r.Method == http.MethodGet && response.Code == 200) {

			scheme := "http"
			if r.TLS != nil {
				scheme = "https"
			}
			usedTime := float32(tracker.Stop().Seconds())

			// 过滤请求头
			reqHeaders := make(map[string]string)
			noLogHeaders := strings.Split(ws.Config.NoLogHeaders, ",")
			for k, v := range r.Header {
				skip := false
				for _, nl := range noLogHeaders {
					if nl != "" && strings.EqualFold(k, strings.TrimSpace(nl)) {
						skip = true
						break
					}
				}
				if !skip {
					reqHeaders[k] = strings.Join(v, ", ")
				}
			}

			// 过滤响应头
			respHeaders := make(map[string]string)
			for k, v := range response.Header().H {
				respHeaders[k] = strings.Join(v, ", ")
			}

			// 处理响应内容截断
			var respData any
			if response.Code != 200 {
				if len(response.body) < 1024 {
					respData = string(response.body)
				} else {
					respData = string(response.body[:1024]) + "..."
				}
			} else if ws.Config.NoLogOutputFields != "" {
				// 简单的字段过滤逻辑 (如果是 JSON 对象)
				// 这里可以根据 Config.NoLogOutputFields, LogOutputArrayNum, LogOutputFieldSize 进行更复杂的处理
				// 暂按字符串截断处理
				if len(response.body) > 0 {
					respData = "[content hidden or truncated]"
				}
			}

			LogRequest(requestLogger, func(entry *RequestLog) {
				entry.Method = r.Method
				entry.Path = r.URL.Path
				entry.Host = hostOnly(r.Host)
				entry.Scheme = scheme
				entry.Proto = r.Proto
				entry.ClientIp = request.ClientIp()
				entry.ServerId = ws.serverId
				entry.App = ws.Config.App
				entry.FromApp = r.Header.Get(discover.HeaderFromApp)
				entry.FromNode = r.Header.Get(discover.HeaderFromNode)
				entry.DeviceId = request.DeviceId()
				entry.SessionId = request.SessionId()
				entry.ClientAppName = r.Header.Get(discover.HeaderClientAppName)
				entry.ClientAppVersion = r.Header.Get(discover.HeaderClientAppVersion)
				entry.AuthLevel = authLevel
				entry.Priority = priority
				entry.RequestHeaders = reqHeaders
				entry.RequestData = args
				entry.ResponseCode = response.Code
				entry.UsedTime = usedTime
				entry.ResponseHeaders = respHeaders
				entry.ResponseData = respData
				entry.ResponseDataLength = uint(len(response.body))
			})
		}
	}()

	// 处理 SessionId 和 DeviceId
	ws.handleClientKeys(request, response)

	// 1. 处理重写 (Rewrite)
	if ws.processRewrite(request, response, requestLogger) {
		return
	}

	// 2. 处理代理 (Proxy)
	if ws.processProxy(request, response, requestLogger) {
		return
	}

	// 3. 路由匹配
	path, _ := url.PathUnescape(r.URL.Path)
	host := r.Host

	// 处理静态文件
	if ws.processStatic(path, request, response, requestLogger) {
		goto filter
	}

	s, wsc = ws.findService(r.Method, host, path, args)

	// 4. 参数解析 (Form & Body)
	parseRequestArgs(request, args)

	// 5. 前置过滤器
	for _, filter := range ws.inFilters {
		result = filter(&args, request, response, requestLogger)
		if result != nil {
			break
		}
	}

	if s != nil {
		authLevel = s.authLevel
		priority = s.options.Priority
	}

	// 6. 处理业务执行 (WS 或 Web)
	if result == nil {
		if wsc != nil {
			authLevel = wsc.authLevel
			priority = wsc.options.Priority
			// 鉴权
			pass, obj := ws.checkAuth(wsc.authLevel, &wsc.options, request, response, args, requestLogger)
			if !pass {
				if !response.changed {
					response.WriteHeader(http.StatusForbidden)
				}
				return
			}
			ws.doWebsocketService(wsc, request, response, requestLogger, obj)
			return
		} else if s != nil {
			// 鉴权
			pass, obj := ws.checkAuth(s.authLevel, &s.options, request, response, args, requestLogger)
			if !pass {
				if !response.changed {
					response.WriteHeader(http.StatusForbidden)
				}
				return
			}
			// 执行业务
			result = ws.doWebService(s, request, response, args, nil, requestLogger, obj)
		}
	}

	if s == nil && result == nil && !response.changed {
		response.WriteHeader(http.StatusNotFound)
		result = "404 page not found"
	}

filter:
	// 7. 后置过滤器 (即使 response.changed 也要执行，比如静态文件的 HTML 注入)
	for _, filter := range ws.outFilters {
		newResult, done := filter(args, request, response, result, requestLogger)
		if newResult != nil {
			result = newResult
			// 如果 response.changed 为 true，说明已经有内容写出了。
			// 如果过滤器返回了非 nil 的 result，我们通常认为它想替换或追加内容。
			// 特别是对于静态文件，如果我们清空了 body 并返回了新内容，result 就不再是 nil。
		}
		if done {
			break
		}
	}

	// 8. 输出结果
	if ws.hasOutFilter {
		// 过滤器模式：所有内容都应该从 result 或 response.body 中写出
		if result != nil {
			outputResult(response, result)
		} else if response.changed {
			response.PhysicalWrite(response.body)
		}
	} else {
		// 普通模式：result (业务返回值) 需要写出，而 response.changed (比如静态文件) 已经由 Response.Write 写过了
		if result != nil {
			outputResult(response, result)
		}
	}
}

func hostOnly(host string) string {
	h, _, _ := strings.Cut(host, ":")
	return h
}

func (ws *WebServer) findService(method, host, path string, args map[string]any) (*webServiceType, *websocketServiceType) {
	ws.webServicesLock.RLock()
	defer ws.webServicesLock.RUnlock()

	// 1. 准备 Host 候选列表: "host:port", "host", ":port", "*"
	hostOnly, port, _ := strings.Cut(host, ":")
	hosts := []string{host}
	if port != "" {
		hosts = append(hosts, hostOnly, ":"+port)
	}
	hosts = append(hosts, "*")

	// 2. 匹配 Web Service
	for _, h := range hosts {
		if services, exists := ws.webServices[h]; exists {
			if s, ok := services[method+path]; ok {
				return s, nil
			}
			if s, ok := services["*"+path]; ok {
				return s, nil
			}
		}
	}

	// 3. 匹配 WebSocket
	ws.websocketServicesLock.RLock()
	defer ws.websocketServicesLock.RUnlock()
	for _, h := range hosts {
		if services, exists := ws.websocketServices[h]; exists {
			if ws, ok := services[path]; ok {
				return nil, ws
			}
		}
	}

	// 4. 正则匹配
	for _, h := range hosts {
		if services, exists := ws.regexWebServices[h]; exists {
			for i := len(services) - 1; i >= 0; i-- {
				s := services[i]
				if s.method != "*" && s.method != method {
					continue
				}
				if s.pathMatcher != nil && s.pathMatcher.MatchString(path) {
					matches := s.pathMatcher.FindStringSubmatch(path)
					if len(matches) > 1 {
						for i, name := range s.pathArgs {
							if i+1 < len(matches) {
								args[name] = matches[i+1]
							}
						}
					}
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
		contentType := request.Header().Get("Content-Type")
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

func (ws *WebServer) checkAuth(authLevel int, options *WebServiceOptions, request *Request, response *Response, args map[string]any, logger *log.Logger) (bool, any) {
	ac := ws.webAuthCheckers[authLevel]
	if ac == nil {
		ac = ws.webAuthChecker
	}
	if ac == nil {
		sess := NewSession(request.SessionId(), logger)
		if authLevel > 0 && sess.GetAuthLevel() < authLevel {
			return false, sess
		}
		return true, sess
	}
	pass, obj := ac(authLevel, logger, &request.RequestURI, args, request, response, options)
	if pass && obj == nil {
		obj = NewSession(request.SessionId(), logger)
	}
	return pass, obj
}

func (ws *WebServer) doWebService(service *webServiceType, request *Request, response *Response, args map[string]any,
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
			if object != nil && reflect.TypeOf(object).AssignableTo(t) {
				params[i] = reflect.ValueOf(object)
			} else if obj := ws.GetInject(t); obj != nil {
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

	if response.server != nil && response.server.hasOutFilter {
		response.PhysicalWrite(data)
	} else {
		_, _ = response.Write(data)
	}
}
func (ws *WebServer) handleClientKeys(request *Request, response *Response) {
	// SessionId
	if ws.usedSessionIdKey != "" {
		sessionId := request.Header().Get(ws.usedSessionIdKey)
		if sessionId == "" && !ws.Config.SessionWithoutCookie {
			if ck := request.GetCookie(ws.usedSessionIdKey); ck != nil {
				sessionId = ck.Value
			}
		}
		if sessionId == "" {
			if ws.sessionIdMaker != nil {
				sessionId = ws.sessionIdMaker()
			} else {
				sessionId = IDMaker.Get11Bytes900MPerSecond()
			}
			if !ws.Config.SessionWithoutCookie {
				http.SetCookie(response.Writer, &http.Cookie{
					Name:     ws.usedSessionIdKey,
					Value:    sessionId,
					Path:     "/",
					HttpOnly: true,
				})
			}
		}
		request.Request.Header.Set(discover.HeaderSessionID, sessionId)
		response.Header().Set(ws.usedSessionIdKey, sessionId)
	}

	// DeviceId
	if ws.usedDeviceIdKey != "" {
		deviceId := request.Header().Get(ws.usedDeviceIdKey)
		if deviceId == "" && !ws.Config.DeviceWithoutCookie {
			if ck := request.GetCookie(ws.usedDeviceIdKey); ck != nil {
				deviceId = ck.Value
			}
		}
		if deviceId == "" {
			deviceId = IDMaker.Get11Bytes900MPerSecond()
			if !ws.Config.DeviceWithoutCookie {
				http.SetCookie(response.Writer, &http.Cookie{
					Name:     ws.usedDeviceIdKey,
					Value:    deviceId,
					Path:     "/",
					Expires:  time.Now().AddDate(10, 0, 0),
					HttpOnly: true,
				})
			}
		}
		request.Request.Header.Set(discover.HeaderDeviceID, deviceId)
		response.Header().Set(ws.usedDeviceIdKey, deviceId)
	}
}
