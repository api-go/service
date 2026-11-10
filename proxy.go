package service

import (
	"github.com/api-go/discover"
	gohttp "github.com/api-go/http"
	"github.com/api-go/log"
	"fmt"
	"github.com/gorilla/websocket"
	"io"
	"net/http"
	"reflect"
	"regexp"
	"strings"
	"time"
)

type proxyType struct {
	matcher     *regexp.Regexp
	authLevel   int
	fromPath    string
	toApp       string
	toPath      string
	hasWildcard bool
	prefix      string
	toPrefix    string
}

func parseTo(to string) (app, path string) {
	if to == "" {
		return "", "/"
	}

	// 查找协议
	if strings.Contains(to, "://") {
		protocolPart, rest, _ := strings.Cut(to, "://")
		// 协议后的第一个 /
		if firstSlash := strings.Index(rest, "/"); firstSlash != -1 {
			app = protocolPart + "://" + rest[:firstSlash]
			path = rest[firstSlash:]
		} else {
			app = to
			path = "/"
		}
	} else {
		// 无协议，第一个 / 之前是 app
		if firstSlash := strings.Index(to, "/"); firstSlash != -1 {
			app = to[:firstSlash]
			path = to[firstSlash:]
		} else {
			app = to
			path = "/"
		}
	}
	return
}

func parseProxyRule(authLevel int, path, toApp, toPath string, to string) *proxyType {
	if to != "" {
		toApp, toPath = parseTo(to)
	}

	p := &proxyType{authLevel: authLevel, fromPath: path, toApp: toApp, toPath: toPath}
	if strings.ContainsRune(path, '(') {
		matcher, err := regexp.Compile("^" + path + "$")
		if err == nil {
			p.matcher = matcher
		}
	} else if strings.HasSuffix(path, "/*") {
		p.hasWildcard = true
		p.prefix = path[:len(path)-1]
		if strings.HasSuffix(toPath, "/*") {
			p.toPrefix = toPath[:len(toPath)-1]
		} else {
			p.toPrefix = toPath
		}
	}
	return p
}

func (hc *HostContext) Proxy(authLevel int, path string, to string) *HostContext {
	p := parseProxyRule(authLevel, path, "", "", to)

	hc.ws.hostPoliciesLock.Lock()
	defer hc.ws.hostPoliciesLock.Unlock()
	if hc.ws.codeProxies[hc.host] == nil {
		hc.ws.codeProxies[hc.host] = make([]*proxyType, 0)
	}
	hc.ws.codeProxies[hc.host] = append(hc.ws.codeProxies[hc.host], p)
	hc.ws.rebuildProxiesUnderLock(hc.host)
	return hc
}

func (ws *WebServer) findProxy(request *Request) (int, *string, *string, string) {
	host := request.Host
	hostOnly, port, _ := strings.Cut(host, ":")
	hosts := []string{host}
	if port != "" {
		hosts = append(hosts, hostOnly, ":"+port)
	}
	hosts = append(hosts, "*")

	requestPath := request.RequestURI
	queryString := ""
	if pos := strings.Index(requestPath, "?"); pos != -1 {
		queryString = requestPath[pos:]
		requestPath = requestPath[:pos]
	}

	ws.hostPoliciesLock.RLock()
	defer ws.hostPoliciesLock.RUnlock()

	for _, h := range hosts {
		proxies, exists := ws.hostProxies[h]
		if request.hostPolicy != nil {
			proxies, exists = request.hostPolicy.proxies, true
		}
		if !exists {
			continue
		}

		for _, pi := range proxies {
			if pi.matcher != nil {
				finds := pi.matcher.FindAllStringSubmatch(requestPath, 1)
				if len(finds) > 0 {
					toApp := pi.toApp
					toPath := pi.toPath
					for i, part := range finds[0] {
						toApp = strings.ReplaceAll(toApp, fmt.Sprintf("$%d", i), part)
						toPath = strings.ReplaceAll(toPath, fmt.Sprintf("$%d", i), part)
					}
					toPath += queryString
					return pi.authLevel, &toApp, &toPath, h
				}
			} else if pi.hasWildcard {
				if strings.HasPrefix(requestPath, pi.prefix) {
					suffix := requestPath[len(pi.prefix):]
					toPath := pi.toPrefix + suffix + queryString
					toApp := pi.toApp
					return pi.authLevel, &toApp, &toPath, h
				}
			} else {
				if pi.fromPath == requestPath {
					toPath := pi.toPath + queryString
					return pi.authLevel, &pi.toApp, &toPath, h
				}
			}
		}
		if request.hostPolicy != nil {
			break
		}
	}

	return 0, nil, nil, ""
}

func (ws *WebServer) processProxy(request *Request, response *Response, logger *log.Logger) bool {
	authLevel, proxyToApp, proxyToPath, foundHost := ws.findProxy(request)

	if proxyToApp == nil || proxyToPath == nil || *proxyToApp == "" || *proxyToPath == "" {
		return false
	}

	// 鉴权
	pass, obj := ws.checkAuthForProxy(authLevel, request, response, logger)
	if !pass {
		if !response.changed {
			response.WriteHeader(http.StatusForbidden)
		}
		return true
	}
	_ = obj // Currently unused in proxy

	app := *proxyToApp
	path := *proxyToPath
	logger.Info("proxy", "app", app, "path", path, "host", foundHost)
	if gohttp.IsWebSocketRequest(request.Request) {
		ws.proxyWebSocket(request, response, logger, app, path)
		return true
	}

	if strings.Contains(app, "://") {
		// 直接 URL 代理
		res := ws.getHttpClient().ManualDoByRequest(request.Request, request.Method, app+path, request.Body, proxyRelayHeaders(request)...)
		copyResponse(res, response, logger)
	} else {
		// Discover 代理
		if !ws.discoverStarted {
			logger.Error("proxy failed: Discoverer is not initialized")
			response.WriteHeader(http.StatusBadGateway)
			return true
		}
		caller := discover.NewCaller(request.Request, logger)
		caller.NoBody = true
		res, _ := caller.ManualDoWithNode(request.Method, app, "", path, request.Body, proxyRelayHeaders(request)...)
		copyResponse(res, response, logger)
	}

	return true
}

func (ws *WebServer) getHttpClient() *gohttp.Client {
	ws.proxyClientsMu.RLock()
	client := ws.proxyHTTPClient
	ws.proxyClientsMu.RUnlock()
	if client != nil {
		return client
	}
	// 尝试从注入对象获取
	if obj := ws.GetInject(reflect.TypeOf(&gohttp.Client{})); obj != nil {
		return obj.(*gohttp.Client)
	}
	timeout := time.Duration(ws.Config.RedirectTimeout) * time.Millisecond
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	ws.proxyClientsMu.Lock()
	if ws.proxyDefaultHTTPClient == nil {
		ws.proxyDefaultHTTPClient = gohttp.NewClient(timeout)
		// Proxy users must not share an automatically populated cookie jar.
		ws.proxyDefaultHTTPClient.GetRawClient().Jar = nil
	}
	client = ws.proxyDefaultHTTPClient.Clone()
	ws.proxyClientsMu.Unlock()
	client.GetRawClient().Timeout = timeout
	return client
}

func (ws *WebServer) checkAuthForProxy(authLevel int, request *Request, response *Response, logger *log.Logger) (bool, any) {
	ac := ws.webAuthCheckers[authLevel]
	if ac == nil {
		ac = ws.webAuthChecker
	}
	if ac == nil {
		if authLevel <= 0 {
			return true, nil
		}
		session := NewSession(request.SessionId(), logger)
		return session.GetAuthLevel() >= authLevel, session
	}
	return ac(authLevel, logger, &request.RequestURI, nil, request, response, nil)
}

func copyResponse(res *gohttp.Result, response *Response, logger *log.Logger) {
	if res.Error != nil || res.Response == nil {
		response.WriteHeader(http.StatusBadGateway)
		if res.Error != nil {
			_, _ = response.WriteString(res.Error.Error())
		}
		return
	}

	blocked := proxyHopHeaders(res.Response.Header)
	for k, values := range res.Response.Header {
		if blocked[http.CanonicalHeaderKey(k)] {
			continue
		}
		response.Writer.Header().Del(k)
		for _, value := range values {
			response.Writer.Header().Add(k, value)
		}
	}
	response.WriteHeader(res.Response.StatusCode)
	response.checkWriteHeader()
	if res.Response.Body != nil {
		defer res.Response.Body.Close()
		_, err := io.Copy(response.Writer, res.Response.Body)
		if err != nil {
			logger.Error("proxy copy body failed", "error", err.Error())
		}
	}
}

// ProxyRule 定义了外部传递或 Redis 中获取的代理规则配置
type ProxyRule struct {
	Path      string // 匹配路径或正则，支持变量捕获如 ^/api/(.*)$
	AuthLevel int    // 所需鉴权级别
	To        string // 目标地址，格式为 "app/path" 或 "http://url/path" (支持后缀 /* 映射)
	ToApp     string // [Deprecated] 目标 AppName 或完整 URL (可含 $1 变量替换)
	ToPath    string // [Deprecated] 目标路径 (可含 $1 变量替换)
}

// ReplaceProxies 使用全量指针替换的方式 (Copy-on-Write) 无缝更新指定 host 的动态代理规则。
func ReplaceProxies(host string, rules []ProxyRule) {
	DefaultServer.ReplaceProxies(host, rules)
}

func (ws *WebServer) ReplaceProxies(host string, rules []ProxyRule) {
	newProxies := make([]*proxyType, 0, len(rules))
	for _, r := range rules {
		newProxies = append(newProxies, parseProxyRule(r.AuthLevel, r.Path, r.ToApp, r.ToPath, r.To))
	}

	ws.hostPoliciesLock.Lock()
	defer ws.hostPoliciesLock.Unlock()
	ws.dynamicProxies[host] = newProxies
	ws.rebuildProxiesUnderLock(host)
}

// proxyRelayHeaders preserves ordinary HTTP headers while reconstructing trusted IP metadata.
func proxyRelayHeaders(request *Request) []string {
	sourceHeaders := request.Request.Header.Clone()
	_ = request.ExternalAddress().RelayHeaders(sourceHeaders)
	blocked := proxyHopHeaders(sourceHeaders)
	blocked[":protocol"] = true
	for _, key := range []string{"X-Real-Ip", "X-Client-Ip", "X-Forwarded-For"} {
		blocked[key] = true
	}
	headers := make([]string, 0, len(request.Request.Header)*2+6)
	for key, values := range sourceHeaders {
		if !blocked[http.CanonicalHeaderKey(key)] {
			for _, value := range values {
				headers = append(headers, key, value)
			}
		}
	}
	return append(headers, "Host", request.ExternalAddress().Host, "X-Real-IP", request.ClientIp(), "X-Client-IP", request.ClientIp(), "X-Forwarded-For", request.ClientIp())
}

func proxyHopHeaders(headers http.Header) map[string]bool {
	blocked := map[string]bool{"Connection": true, "Proxy-Connection": true, "Keep-Alive": true, "Proxy-Authenticate": true, "Proxy-Authorization": true, "Te": true, "Trailer": true, "Transfer-Encoding": true, "Upgrade": true}
	for _, value := range headers.Values("Connection") {
		for _, token := range strings.Split(value, ",") {
			blocked[http.CanonicalHeaderKey(strings.TrimSpace(token))] = true
		}
	}
	return blocked
}

// SetProxyHTTPClient sets the direct URL HTTP proxy transport. WebSocket defaults
// inherit its TLS and TCP dialing configuration and automatically negotiate H2/H1.
func (ws *WebServer) SetProxyHTTPClient(client *gohttp.Client) {
	ws.proxyClientsMu.Lock()
	defer ws.proxyClientsMu.Unlock()
	ws.proxyHTTPClient = client
}

// SetProxyWebSocketDialer overrides the WebSocket transport without changing
// ordinary HTTP/2 connections. Overrides use H1; nil restores H2/H1 negotiation.
// The configuration is copied.
func (ws *WebServer) SetProxyWebSocketDialer(dialer *websocket.Dialer) {
	client := gohttp.NewClient(0)
	client.SetWebSocketDialer(dialer)
	var snapshot *websocket.Dialer
	if dialer != nil {
		snapshot = client.WebSocketDialer()
	}
	ws.proxyClientsMu.Lock()
	defer ws.proxyClientsMu.Unlock()
	ws.proxyWebSocketDialer = snapshot
}

func (ws *WebServer) closeDefaultProxyHTTPClient() {
	ws.proxyClientsMu.Lock()
	defer ws.proxyClientsMu.Unlock()
	if ws.proxyDefaultHTTPClient != nil {
		ws.proxyDefaultHTTPClient.Destroy()
		ws.proxyDefaultHTTPClient = nil
	}
}
