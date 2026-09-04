package service

import (
	"io"
	"net"
	"net/http"
	"net/url"
	"reflect"
	"runtime/debug"
	"strings"
	"sync/atomic"
	"time"

	"apigo.cc/go/cast"
	"apigo.cc/go/discover"
	"apigo.cc/go/log"
	"apigo.cc/go/timer"
)

type RouteHandler struct {
	ws               *WebServer
	webRequestingNum int64
}

func (rh *RouteHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	ws := rh.ws
	if ws == nil {
		ws = DefaultServer
	}
	if r.TLS == nil && ws.shouldForceHTTPS(r.Host) {
		target := "https://" + ws.httpsRedirectHost(r.Host) + r.URL.RequestURI()
		http.Redirect(w, r, target, http.StatusPermanentRedirect)
		return
	}
	atomic.AddInt64(&rh.webRequestingNum, 1)
	defer atomic.AddInt64(&rh.webRequestingNum, -1)

	tracker := timer.Start()
	requestId := r.Header.Get(discover.HeaderRequestId)
	if requestId == "" {
		requestId = IDMaker.Get10Bytes14MPerSecond()
		r.Header.Set(discover.HeaderRequestId, requestId)
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

			// 请求头
			var reqHeaders map[string]string
			if !ws.Config.NoLogAllHeaders {
				reqHeaders = sanitizeLogHeaders(r.Header, ws.effectiveNoLogRequestHeaders())
			}

			// 响应头
			var respHeaders map[string]string
			if !ws.Config.NoLogAllHeaders {
				respHeaders = sanitizeLogHeaders(response.Header().H, ws.effectiveNoLogResponseHeaders())
			}

			// 请求输入脱敏
			var reqData any
			if !ws.Config.NoLogInput && args != nil {
				reqData = sanitizeLogData(args, sanitizeOpts{
					maxSize:   200,
					fieldSize: ws.Config.LogInputFieldSize,
					arrayNum:  ws.Config.LogInputArrayNum,
					objectNum: ws.Config.LogInputObjectNum,
				})
			}

			// 响应输出脱敏
			var respData any
			if !ws.Config.NoLogOutput && len(response.body) > 0 {
				respData = sanitizeRespBody(response.body, &ws.Config)
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
				entry.RequestData = reqData
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
	if s == nil && wsc == nil {
		ws.webServicesLock.RLock()
		s = ws.fallbackService
		ws.webServicesLock.RUnlock()
	}

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
	if response.pendingWrite {
		if isEmptyServiceOutput(result) {
			result = response.pending
			response.pending = nil
			response.pendingWrite = false
		} else {
			// Preserve the historical write-before-return ordering for handlers that do both.
			_, _ = response.physicalWrite(response.pending)
			response.pending = nil
			response.pendingWrite = false
		}
	}
	// Direct response writes are already on the wire and intentionally bypass
	// output filters. Static responses opt in through the internal filtered writer.
	if response.changed && !response.filteredWrite && result == nil {
		return
	}
	// 7. 后置过滤器
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
		} else if response.filteredWrite {
			response.physicalWrite(response.body)
		}
	} else {
		// 普通模式：result (业务返回值) 需要写出，而 response.changed (比如静态文件) 已经由 Response.Write 写过了
		if result != nil {
			outputResult(response, result)
		}
	}
}

func isEmptyServiceOutput(result any) bool {
	if result == nil {
		return true
	}
	switch value := result.(type) {
	case string:
		return value == ""
	case []byte:
		return len(value) == 0
	default:
		return false
	}
}

// ReplaceForceHTTPS atomically sets the HTTPS redirect policy for a Host.
func ReplaceForceHTTPS(host string, enabled bool) {
	DefaultServer.ReplaceForceHTTPS(host, enabled)
}

func (ws *WebServer) ReplaceForceHTTPS(host string, enabled bool) {
	host = strings.ToLower(strings.TrimSpace(host))
	if host == "" {
		host = "*"
	}
	ws.hostPoliciesLock.Lock()
	if ws.forceHTTPSHosts == nil {
		ws.forceHTTPSHosts = map[string]bool{}
	}
	if enabled {
		ws.forceHTTPSHosts[host] = true
	} else {
		delete(ws.forceHTTPSHosts, host)
	}
	ws.hostPoliciesLock.Unlock()
}

func (ws *WebServer) shouldForceHTTPS(host string) bool {
	ws.hostPoliciesLock.RLock()
	defer ws.hostPoliciesLock.RUnlock()
	for _, candidate := range hostCandidates(host) {
		if ws.forceHTTPSHosts[candidate] {
			return true
		}
	}
	return false
}

func (ws *WebServer) httpsRedirectHost(host string) string {
	httpsPort := "443"
	for index, config := range ws.listenConfigs {
		if config.protocol != "https" && config.protocol != "h2" {
			continue
		}
		if index < len(ws.Addrs) {
			if _, port, err := net.SplitHostPort(ws.Addrs[index]); err == nil {
				httpsPort = port
			}
		}
		break
	}
	hostname := host
	if parsedHost, _, err := net.SplitHostPort(host); err == nil {
		hostname = parsedHost
	}
	if httpsPort == "443" {
		return hostname
	}
	return net.JoinHostPort(hostname, httpsPort)
}

func hostOnly(host string) string {
	h, _, err := net.SplitHostPort(host)
	if err == nil {
		return strings.Trim(h, "[]")
	}
	if strings.Count(host, ":") == 1 {
		h, _, _ = strings.Cut(host, ":")
		return h
	}
	return strings.Trim(host, "[]")
}

func hostCandidates(host string) []string {
	host = strings.ToLower(strings.TrimSpace(host))
	hostName, port := hostOnly(host), ""
	if _, parsedPort, err := net.SplitHostPort(host); err == nil {
		port = parsedPort
	} else if strings.Count(host, ":") == 1 {
		_, port, _ = strings.Cut(host, ":")
	}
	hostName = strings.TrimSuffix(hostName, ".")
	normalizedHost := hostName
	if port != "" {
		normalizedHost = net.JoinHostPort(hostName, port)
	}
	candidates := []string{normalizedHost}
	if port != "" {
		candidates = append(candidates, hostName, ":"+port)
	}
	candidates = append(candidates, "*")
	out := candidates[:0]
	seen := map[string]bool{}
	for _, candidate := range candidates {
		if candidate != "" && !seen[candidate] {
			seen[candidate] = true
			out = append(out, candidate)
		}
	}
	return out
}

// HostCandidates returns route keys from most specific to the global fallback.
func HostCandidates(host string) []string {
	return append([]string(nil), hostCandidates(host)...)
}

func (ws *WebServer) findService(method, host, path string, args map[string]any) (*webServiceType, *websocketServiceType) {
	ws.webServicesLock.RLock()
	defer ws.webServicesLock.RUnlock()

	// 1. 准备 Host 候选列表: "host:port", "host", ":port", "*"
	hosts := hostCandidates(host)

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

	response.keepBody(data)
	if response.server != nil && response.server.hasOutFilter {
		response.physicalWrite(data)
	} else {
		_, _ = response.Write(data)
	}
}
func (ws *WebServer) handleClientKeys(request *Request, response *Response) {
	// SessionId
	if ws.usedSessionIdKey != "" {
		hasRequestSessionId := false
		sessionId := request.Header().Get(ws.usedSessionIdKey)
		if sessionId != "" {
			hasRequestSessionId = true
			if !ws.Config.SessionWithoutCookie {
				cookie := request.GetCookie(ws.usedSessionIdKey)
				if cookie == nil || cookie.Value != sessionId {
					http.SetCookie(response.Writer, &http.Cookie{Name: ws.usedSessionIdKey, Value: sessionId, Path: "/", HttpOnly: true})
				}
			}
		} else if !ws.Config.SessionWithoutCookie {
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
		request.Request.Header.Set(discover.HeaderSessionId, sessionId)
		if !hasRequestSessionId {
			response.Header().Set(ws.usedSessionIdKey, sessionId)
		}
	}

	// DeviceId
	if ws.usedDeviceIdKey != "" {
		hasRequestDeviceId := false
		deviceId := request.Header().Get(ws.usedDeviceIdKey)
		if deviceId != "" {
			hasRequestDeviceId = true
			if !ws.Config.DeviceWithoutCookie {
				cookie := request.GetCookie(ws.usedDeviceIdKey)
				if cookie == nil || cookie.Value != deviceId {
					http.SetCookie(response.Writer, &http.Cookie{Name: ws.usedDeviceIdKey, Value: deviceId, Path: "/", Expires: time.Now().AddDate(10, 0, 0), HttpOnly: true})
				}
			}
		} else if !ws.Config.DeviceWithoutCookie {
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
		request.Request.Header.Set(discover.HeaderDeviceId, deviceId)
		if !hasRequestDeviceId {
			response.Header().Set(ws.usedDeviceIdKey, deviceId)
		}
	}

	// App-Name / App-Version（客户端上报，注入内部标准头供下游微服务使用）
	if ws.usedClientAppKey != "" {
		if appName := request.Header().Get(ws.usedClientAppKey + "-Name"); appName != "" {
			request.Request.Header.Set(discover.HeaderClientAppName, appName)
		}
		if appVersion := request.Header().Get(ws.usedClientAppKey + "-Version"); appVersion != "" {
			request.Request.Header.Set(discover.HeaderClientAppVersion, appVersion)
		}
	}
}

// effectiveNoLogRequestHeaders 返回请求头排除列表（动态默认值 + 用户配置追加）
func (ws *WebServer) effectiveNoLogRequestHeaders() string {
	parts := []string{"X-Request-Id", "X-Device-Id", "X-Session-Id"}
	if ws.usedDeviceIdKey != "" {
		parts = append(parts, ws.usedDeviceIdKey)
	}
	if ws.usedSessionIdKey != "" {
		parts = append(parts, ws.usedSessionIdKey)
	}
	if ws.usedClientAppKey != "" {
		parts = append(parts, ws.usedClientAppKey+"-Name", ws.usedClientAppKey+"-Version")
	}
	if ws.Config.NoLogRequestHeaders != "" {
		parts = append(parts, ws.Config.NoLogRequestHeaders)
	}
	return strings.Join(parts, ",")
}

// effectiveNoLogResponseHeaders 返回响应头排除列表（用户配置追加）
func (ws *WebServer) effectiveNoLogResponseHeaders() string {
	return ws.Config.NoLogResponseHeaders
}
