package service

import (
	"apigo.cc/go/log"
	"fmt"
	"net/url"
	"regexp"
	"strings"
)

type rewriteType struct {
	matcher  *regexp.Regexp
	fromPath string
	toPath   string
}

func (hc *HostContext) Rewrite(path string, toPath string) *HostContext {
	s := &rewriteType{fromPath: path, toPath: toPath}

	if strings.ContainsRune(path, '(') {
		matcher, err := regexp.Compile("^" + path + "$")
		if err == nil {
			s.matcher = matcher
		}
	}

	hostPoliciesLock.Lock()
	defer hostPoliciesLock.Unlock()
	hostRewrites[hc.host] = append(hostRewrites[hc.host], s)
	return hc
}

func processRewrite(request *Request, response *Response, logger *log.Logger) bool {
	host := request.Host
	hostOnly, port, _ := strings.Cut(host, ":")
	hosts := []string{host}
	if port != "" {
		hosts = append(hosts, hostOnly, ":"+port)
	}
	hosts = append(hosts, "*")

	hostPoliciesLock.RLock()
	defer hostPoliciesLock.RUnlock()

	requestPath := request.RequestURI
	queryString := ""
	if pos := strings.Index(requestPath, "?"); pos != -1 {
		queryString = requestPath[pos:]
		requestPath = requestPath[:pos]
	}

	for _, h := range hosts {
		rewrites, exists := hostRewrites[h]
		if !exists {
			continue
		}

		for _, ri := range rewrites {
			found := false
			rewriteToPath := ""

			if ri.matcher == nil {
				if ri.fromPath == requestPath {
					rewriteToPath = ri.toPath
					found = true
				}
			} else {
				finds := ri.matcher.FindAllStringSubmatch(request.RequestURI, 1)
				if len(finds) > 0 {
					toPath := ri.toPath
					for i, part := range finds[0] {
						toPath = strings.ReplaceAll(toPath, fmt.Sprintf("$%d", i), part)
					}
					rewriteToPath = toPath
					found = true
				}
			}

			if found {
				if strings.Contains(rewriteToPath, "://") {
					// 外部重定向
					if !strings.Contains(rewriteToPath, "?") && queryString != "" {
						rewriteToPath += queryString
					}
					response.Header().Set("Location", rewriteToPath)
					response.WriteHeader(302)
					return true
				} else {
					// 内部重写
					logger.Info("rewrite", "from", request.RequestURI, "to", rewriteToPath, "host", h)
					if queryString != "" && !strings.Contains(rewriteToPath, "?") {
						rewriteToPath += queryString
					}
					request.RequestURI = rewriteToPath
					request.URL, _ = url.Parse(rewriteToPath)
					return false // 继续后续处理
				}
			}
		}
	}

	return false
}
