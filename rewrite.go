package service

import (
	"apigo.cc/go/log"
	"fmt"
	"net/url"
	"regexp"
	"strings"
)

type rewriteType struct {
	matcher     *regexp.Regexp
	fromPath    string
	toPath      string
	hasWildcard bool
	prefix      string
	toPrefix    string
}

func parseRewriteRule(fromPath, toPath string) *rewriteType {
	s := &rewriteType{fromPath: fromPath, toPath: toPath}
	if strings.ContainsRune(fromPath, '(') {
		matcher, err := regexp.Compile("^" + fromPath + "$")
		if err == nil {
			s.matcher = matcher
		}
	} else if strings.HasSuffix(fromPath, "/*") {
		s.hasWildcard = true
		s.prefix = fromPath[:len(fromPath)-1]
		if strings.HasSuffix(toPath, "/*") {
			s.toPrefix = toPath[:len(toPath)-1]
		} else {
			s.toPrefix = toPath
		}
	}
	return s
}

func (hc *HostContext) Rewrite(path string, toPath string) *HostContext {
	s := parseRewriteRule(path, toPath)

	hostPoliciesLock.Lock()
	defer hostPoliciesLock.Unlock()
	codeRewrites[hc.host] = append(codeRewrites[hc.host], s)
	rebuildRewritesUnderLock(hc.host)
	return hc
}

func rebuildRewritesUnderLock(host string) {
	var combined []*rewriteType
	combined = append(combined, codeRewrites[host]...)
	combined = append(combined, fileRewrites[host]...)
	combined = append(combined, dynamicRewrites[host]...)
	hostRewrites[host] = combined
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

			if ri.matcher != nil {
				finds := ri.matcher.FindAllStringSubmatch(request.RequestURI, 1)
				if len(finds) > 0 {
					toPath := ri.toPath
					for i, part := range finds[0] {
						toPath = strings.ReplaceAll(toPath, fmt.Sprintf("$%d", i), part)
					}
					rewriteToPath = toPath
					found = true
				}
			} else if ri.hasWildcard {
				if strings.HasPrefix(requestPath, ri.prefix) {
					suffix := requestPath[len(ri.prefix):]
					rewriteToPath = ri.toPrefix + suffix
					found = true
				}
			} else {
				if ri.fromPath == requestPath {
					rewriteToPath = ri.toPath
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

// RewriteRule 定义了外部传递的 URL 重写规则
type RewriteRule struct {
	Path   string // 原始路径或匹配正则，例如 ^/old/(.*)$
	ToPath string // 重写后的路径，例如 /new/$1
}

// ReplaceRewrites 使用 Copy-on-Write 机制原子地替换指定 host 下的动态重写规则。
func ReplaceRewrites(host string, rules []RewriteRule) {
	newRewrites := make([]*rewriteType, 0, len(rules))
	for _, r := range rules {
		newRewrites = append(newRewrites, parseRewriteRule(r.Path, r.ToPath))
	}

	hostPoliciesLock.Lock()
	defer hostPoliciesLock.Unlock()
	dynamicRewrites[host] = newRewrites
	rebuildRewritesUnderLock(host)
}
