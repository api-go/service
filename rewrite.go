package service

import (
	"apigo.cc/go/log"
	"fmt"
	"net/url"
	"regexp"
	"strings"
	"sync"
)

type rewriteInfo struct {
	matcher  *regexp.Regexp
	fromPath string
	toPath   string
}

var (
	rewrites      = make(map[string]*rewriteInfo)
	regexRewrites = make([]*rewriteInfo, 0)
	rewriteBy     func(*Request) (string, bool)
	rewritesLock  = sync.RWMutex{}
)

// Rewrite 注册重写规则
func Rewrite(path string, toPath string) {
	s := &rewriteInfo{fromPath: path, toPath: toPath}

	if strings.ContainsRune(path, '(') {
		matcher, err := regexp.Compile("^" + path + "$")
		if err == nil {
			s.matcher = matcher
			rewritesLock.Lock()
			regexRewrites = append(regexRewrites, s)
			rewritesLock.Unlock()
		}
	} else {
		rewritesLock.Lock()
		rewrites[path] = s
		rewritesLock.Unlock()
	}
}

// SetRewriteBy 设置动态重写函数
func SetRewriteBy(by func(request *Request) (toPath string, rewrite bool)) {
	rewriteBy = by
}

func processRewrite(request *Request, response *Response, logger *log.Logger) bool {
	requestPath := request.RequestURI
	queryString := ""
	if pos := strings.Index(requestPath, "?"); pos != -1 {
		queryString = requestPath[pos:]
		requestPath = requestPath[:pos]
	}

	var rewriteToPath string
	var found bool

	rewritesLock.RLock()
	// 1. 精确匹配
	if ri, ok := rewrites[requestPath]; ok {
		rewriteToPath = ri.toPath
		found = true
	}

	// 2. 动态重写
	if !found && rewriteBy != nil {
		rewriteToPath, found = rewriteBy(request)
	}

	// 3. 正则匹配
	if !found {
		for _, ri := range regexRewrites {
			if ri.matcher != nil {
				finds := ri.matcher.FindAllStringSubmatch(request.RequestURI, 1)
				if len(finds) > 0 {
					toPath := ri.toPath
					for i, part := range finds[0] {
						toPath = strings.ReplaceAll(toPath, fmt.Sprintf("$%d", i), part)
					}
					rewriteToPath = toPath
					found = true
					break
				}
			}
		}
	}
	rewritesLock.RUnlock()

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
			logger.Info("rewrite", "from", request.RequestURI, "to", rewriteToPath)
			if queryString != "" && !strings.Contains(rewriteToPath, "?") {
				rewriteToPath += queryString
			}
			request.RequestURI = rewriteToPath
			request.URL, _ = url.Parse(rewriteToPath)
			return false // 继续后续处理
		}
	}

	return false
}
