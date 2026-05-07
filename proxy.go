package service

import (
	"apigo.cc/go/discover"
	gohttp "apigo.cc/go/http"
	"apigo.cc/go/log"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"time"
)

type proxyInfo struct {
	matcher   *regexp.Regexp
	authLevel int
	fromPath  string
	toApp     string
	toPath    string
}

var (
	proxies      = make(map[string]*proxyInfo)
	regexProxies = make([]*proxyInfo, 0)
	proxyBy      func(*Request) (int, *string, *string, map[string]string)
	proxiesLock  = sync.RWMutex{}

	httpClientPool *gohttp.Client
)

// Proxy 注册代理规则
func Proxy(authLevel int, path string, toApp, toPath string) {
	p := &proxyInfo{authLevel: authLevel, fromPath: path, toApp: toApp, toPath: toPath}
	if strings.Contains(path, "(") {
		matcher, err := regexp.Compile("^" + path + "$")
		if err == nil {
			p.matcher = matcher
			proxiesLock.Lock()
			regexProxies = append(regexProxies, p)
			proxiesLock.Unlock()
		}
	} else {
		proxiesLock.Lock()
		proxies[path] = p
		proxiesLock.Unlock()
	}
}

// SetProxyBy 设置动态代理函数
func SetProxyBy(by func(request *Request) (authLevel int, toApp, toPath *string, headers map[string]string)) {
	proxyBy = by
}

func findProxy(request *Request) (int, *string, *string) {
	requestPath := request.RequestURI
	queryString := ""
	if pos := strings.Index(requestPath, "?"); pos != -1 {
		queryString = requestPath[pos:]
		requestPath = requestPath[:pos]
	}

	proxiesLock.RLock()
	defer proxiesLock.RUnlock()

	if pi, ok := proxies[requestPath]; ok {
		toPath := pi.toPath + queryString
		return pi.authLevel, &pi.toApp, &toPath
	}

	for _, pi := range regexProxies {
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
				return pi.authLevel, &toApp, &toPath
			}
		}
	}

	return 0, nil, nil
}

func processProxy(request *Request, response *Response, logger *log.Logger) bool {
	authLevel, proxyToApp, proxyToPath := findProxy(request)
	var proxyHeaders map[string]string

	if proxyBy != nil && (proxyToApp == nil || proxyToPath == nil || *proxyToApp == "" || *proxyToPath == "") {
		authLevel, proxyToApp, proxyToPath, proxyHeaders = proxyBy(request)
	}

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

	// 构建自定义头部
	headerArgs := make([]string, 0)
	for k, v := range proxyHeaders {
		headerArgs = append(headerArgs, k, v)
	}

	if strings.Contains(app, "://") {
		// 直接 URL 代理
		if httpClientPool == nil {
			httpClientPool = gohttp.NewClient(time.Duration(Config.RedirectTimeout) * time.Millisecond)
		}
		res := httpClientPool.ManualDoByRequest(request.Request, request.Method, app+path, request.Body, headerArgs...)
		copyResponse(res, response, logger)
	} else {
		// Discover 代理
		caller := discover.NewCaller(request.Request, logger)
		caller.NoBody = true
		res, _ := caller.ManualDoWithNode(request.Method, app, "", path, request.Body, headerArgs...)
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
