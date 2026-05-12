package service

import (
	gohttp "apigo.cc/go/http"
	"apigo.cc/go/log"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"
)

type proxyType struct {
	matcher   *regexp.Regexp
	authLevel int
	fromPath  string
	toApp     string
	toPath    string
}

func (hc *HostContext) Proxy(authLevel int, path string, toApp, toPath string) *HostContext {
	p := &proxyType{authLevel: authLevel, fromPath: path, toApp: toApp, toPath: toPath}
	if strings.Contains(path, "(") {
		matcher, err := regexp.Compile("^" + path + "$")
		if err == nil {
			p.matcher = matcher
		}
	}

	hostPoliciesLock.Lock()
	defer hostPoliciesLock.Unlock()
	hostProxies[hc.host] = append(hostProxies[hc.host], p)
	return hc
}

var httpClientPool *gohttp.Client

func findProxy(request *Request) (int, *string, *string, string) {
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

	hostPoliciesLock.RLock()
	defer hostPoliciesLock.RUnlock()

	for _, h := range hosts {
		proxies, exists := hostProxies[h]
		if !exists {
			continue
		}

		for _, pi := range proxies {
			if pi.matcher == nil {
				if pi.fromPath == requestPath {
					toPath := pi.toPath + queryString
					return pi.authLevel, &pi.toApp, &toPath, h
				}
			} else {
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
			}
		}
	}

	return 0, nil, nil, ""
}

func processProxy(request *Request, response *Response, logger *log.Logger) bool {
	authLevel, proxyToApp, proxyToPath, foundHost := findProxy(request)

	if proxyToApp == nil || proxyToPath == nil || *proxyToApp == "" || *proxyToPath == "" {
		return false
	}

	// 鉴权
	pass, obj := checkAuthForProxy(authLevel, request, response, logger)
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

	if strings.Contains(app, "://") {
		// 直接 URL 代理
		if httpClientPool == nil {
			httpClientPool = gohttp.NewClient(time.Duration(Config.RedirectTimeout) * time.Millisecond)
		}
		res := httpClientPool.ManualDoByRequest(request.Request, request.Method, app+path, request.Body)
		copyResponse(res, response, logger)
	} else {
		// Discover 代理
		if GlobalDiscoverer == nil {
			logger.Error("proxy failed: GlobalDiscoverer is not initialized")
			response.WriteHeader(http.StatusBadGateway)
			return true
		}
		caller := GlobalDiscoverer.NewCaller(request.Request, logger)
		caller.NoBody = true
		res, _ := caller.ManualDoWithNode(request.Method, app, "", path, request.Body)
		copyResponse(res, response, logger)
	}

	return true
}

func checkAuthForProxy(authLevel int, request *Request, response *Response, logger *log.Logger) (bool, any) {
	ac := webAuthCheckers[authLevel]
	if ac == nil {
		ac = webAuthChecker
	}
	if ac == nil {
		return true, nil
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

	for k, v := range res.Response.Header {
		response.Header().Set(k, v[0])
	}
	response.WriteHeader(res.Response.StatusCode)
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
	ToApp     string // 目标 AppName 或完整 URL (可含 $1 变量替换)
	ToPath    string // 目标路径 (可含 $1 变量替换)
}

// ReplaceProxies 使用全量指针替换的方式 (Copy-on-Write) 无缝更新指定 host 的所有代理规则。
// 该方法非常轻量，仅在赋值瞬间短暂持有写锁，不会阻塞任何并发请求，并且自动淘汰旧规则。
func ReplaceProxies(host string, rules []ProxyRule) {
	newProxies := make([]*proxyType, 0, len(rules))
	for _, r := range rules {
		p := &proxyType{authLevel: r.AuthLevel, fromPath: r.Path, toApp: r.ToApp, toPath: r.ToPath}
		if strings.ContainsRune(r.Path, '(') {
			matcher, err := regexp.Compile("^" + r.Path + "$")
			if err == nil {
				p.matcher = matcher
			}
		}
		newProxies = append(newProxies, p)
	}

	hostPoliciesLock.Lock()
	defer hostPoliciesLock.Unlock()
	hostProxies[host] = newProxies
}
