package service

import (
	"apigo.cc/go/file"
	"apigo.cc/go/log"
	"io"
	"mime"
	"net/http"
	"net/url"
	pathpkg "path"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

type staticFSMatch struct {
	fileSystem http.FileSystem
	name       string
}

// Static 注册静态文件目录
func (hc *HostContext) Static(path, rootPath string) *HostContext {
	host := hc.host
	if host == "*" {
		host = ""
	}
	hc.ws.StaticByHost(path, rootPath, host)
	return hc
}

// Static 注册静态文件目录 (使用默认 Host "*")
func Static(path, rootPath string) {
	DefaultServer.Static(path, rootPath)
}

func (ws *WebServer) Static(path, rootPath string) {
	ws.Host("*").Static(path, rootPath)
}

// StaticFS registers a custom static file source for a URL prefix.
func (hc *HostContext) StaticFS(path string, source http.FileSystem) *HostContext {
	host := hc.host
	if host == "*" {
		host = ""
	}
	hc.ws.staticsByHostLock.Lock()
	if hc.ws.staticFSByHost[host] == nil {
		hc.ws.staticFSByHost[host] = map[string]http.FileSystem{}
	}
	hc.ws.staticFSByHost[host][path] = source
	hc.ws.staticsByHostLock.Unlock()
	return hc
}

// ReplaceStaticFS atomically replaces custom static file sources for a host.
func ReplaceStaticFS(host string, config map[string]http.FileSystem) {
	DefaultServer.ReplaceStaticFS(host, config)
}

func (ws *WebServer) ReplaceStaticFS(host string, config map[string]http.FileSystem) {
	ws.replaceStaticFS(host, config, false)
}

// ReplaceStaticFSExclusive atomically replaces custom static file sources for
// a host and stops custom StaticFS fallback after that host is selected.
func ReplaceStaticFSExclusive(host string, config map[string]http.FileSystem) {
	DefaultServer.ReplaceStaticFSExclusive(host, config)
}

func (ws *WebServer) ReplaceStaticFSExclusive(host string, config map[string]http.FileSystem) {
	ws.replaceStaticFS(host, config, true)
}

func (ws *WebServer) replaceStaticFS(host string, config map[string]http.FileSystem, exclusive bool) {
	if host == "*" {
		host = ""
	}
	next := make(map[string]http.FileSystem, len(config))
	for route, source := range config {
		if source != nil {
			next[route] = source
		}
	}
	ws.staticsByHostLock.Lock()
	if len(next) == 0 {
		delete(ws.staticFSByHost, host)
		delete(ws.staticFSExclusive, host)
	} else {
		ws.staticFSByHost[host] = next
		ws.staticFSExclusive[host] = exclusive
	}
	ws.staticsByHostLock.Unlock()
}

// StaticByHost 为指定域名注册静态文件目录
func StaticByHost(path, rootPath, host string) {
	DefaultServer.StaticByHost(path, rootPath, host)
}

func (ws *WebServer) StaticByHost(path, rootPath, host string) {
	if !filepath.IsAbs(rootPath) {
		if absPath, err := filepath.Abs(rootPath); err == nil {
			rootPath = absPath
		}
	}

	ws.staticsByHostLock.Lock()
	defer ws.staticsByHostLock.Unlock()

	if ws.codeStatics[host] == nil {
		ws.codeStatics[host] = make(map[string]*string)
	}
	ws.codeStatics[host][path] = &rootPath
	ws.rebuildStaticsUnderLock(host)
}

// ReplaceStatics 使用 Copy-on-Write 机制原子地替换指定 host 下的动态静态目录规则
func ReplaceStatics(host string, config map[string]string) {
	DefaultServer.ReplaceStatics(host, config)
}

func (ws *WebServer) ReplaceStatics(host string, config map[string]string) {
	newStatics := make(map[string]*string, len(config))
	for path, rootPath := range config {
		rp := rootPath
		if !filepath.IsAbs(rp) {
			if absPath, err := filepath.Abs(rp); err == nil {
				rp = absPath
			}
		}
		newStatics[path] = &rp
	}

	ws.staticsByHostLock.Lock()
	defer ws.staticsByHostLock.Unlock()

	ws.dynamicStatics[host] = newStatics
	ws.rebuildStaticsUnderLock(host)
}

func (ws *WebServer) getStaticFilePath(requestPath, host string) string {
	requestPath, _ = url.PathUnescape(requestPath)
	ws.staticsByHostLock.RLock()
	defer ws.staticsByHostLock.RUnlock()

	for _, candidate := range hostCandidates(host) {
		if candidate == "*" {
			candidate = ""
		}
		if filePath := ws.findMatchedPathSorted(ws.hostStatics[candidate], requestPath); filePath != "" {
			return filePath
		}
	}
	return ""
}

func (ws *WebServer) getStaticFSMatches(requestPath, host string) []staticFSMatch {
	requestPath, _ = url.PathUnescape(requestPath)
	ws.staticsByHostLock.RLock()
	defer ws.staticsByHostLock.RUnlock()
	matches := make([]staticFSMatch, 0)
	for _, candidate := range hostCandidates(host) {
		if candidate == "*" {
			candidate = ""
		}
		config, exists := ws.staticFSByHost[candidate]
		if !exists {
			continue
		}
		routes := make([]string, 0, len(config))
		for route := range config {
			routes = append(routes, route)
		}
		sort.Slice(routes, func(i, j int) bool { return len(routes[i]) > len(routes[j]) })
		for _, route := range routes {
			if !strings.HasPrefix(requestPath, route) {
				continue
			}
			name := strings.TrimPrefix(requestPath, route)
			name = strings.TrimPrefix(pathpkg.Clean("/"+name), "/")
			matches = append(matches, staticFSMatch{fileSystem: config[route], name: name})
		}
		if ws.staticFSExclusive[candidate] {
			break
		}
	}
	return matches
}

func (ws *WebServer) findMatchedPathSorted(config []*staticType, requestPath string) string {
	for _, rule := range config {
		if strings.HasPrefix(requestPath, rule.path) {
			return filepath.Join(*rule.rootPath, requestPath[len(rule.path):])
		}
	}
	return ""
}

func (ws *WebServer) processStatic(requestPath string, request *Request, response *Response, logger *log.Logger) bool {
	for _, match := range ws.getStaticFSMatches(requestPath, request.Host) {
		if ws.processStaticFS(match, request, response) {
			return true
		}
	}
	filePath := ws.getStaticFilePath(requestPath, request.Host)
	if filePath == "" {
		return false
	}

	info := file.GetFileInfo(filePath)
	if info == nil {
		return false
	}

	if info.IsDir {
		// 自动查找索引文件
		indexFiles := ws.Config.IndexFiles
		if len(indexFiles) == 0 {
			indexFiles = []string{"index.html", "index.htm"}
		}

		for _, indexFile := range indexFiles {
			f := filepath.Join(filePath, indexFile)
			if i := file.GetFileInfo(f); i != nil && !i.IsDir {
				filePath = f
				info = i
				break
			}
		}
	}

	if info.IsDir {
		return false
	}

	// 静态文件通过 Cookie 维护 ID，不应答 Device-Id / Session-Id 头
	response.Header().Del(ws.usedDeviceIdKey)
	response.Header().Del(ws.usedSessionIdKey)

	// 检查 304
	if ifModifiedSince := request.Header().Get("If-Modified-Since"); ifModifiedSince != "" {
		if t, err := time.Parse(http.TimeFormat, ifModifiedSince); err == nil {
			if time.Unix(info.ModTime, 0).Truncate(time.Second).Before(t.Truncate(time.Second)) ||
				time.Unix(info.ModTime, 0).Truncate(time.Second).Equal(t.Truncate(time.Second)) {
				response.WriteHeader(http.StatusNotModified)
				return true
			}
		}
	}

	// 发送文件
	contentType := mime.TypeByExtension(filepath.Ext(filePath))
	if contentType == "" {
		contentType = "application/octet-stream"
	}
	response.Header().Set("Content-Type", contentType)
	response.Header().Set("Last-Modified", time.Unix(info.ModTime, 0).UTC().Format(http.TimeFormat))

	data, err := file.ReadBytes(filePath)
	if err != nil {
		return false
	}

	if ws.webDevEnabled {
		_, _ = response.WriteFiltered(data)
	} else {
		_, _ = response.Write(data)
	}
	return true
}

func (ws *WebServer) processStaticFS(match staticFSMatch, request *Request, response *Response) bool {
	name := match.name
	opened, err := match.fileSystem.Open(name)
	if err != nil {
		return false
	}
	defer func() { _ = opened.Close() }()
	info, err := opened.Stat()
	if err != nil {
		return false
	}
	if info.IsDir() {
		_ = opened.Close()
		indexFiles := ws.Config.IndexFiles
		if len(indexFiles) == 0 {
			indexFiles = []string{"index.html", "index.htm"}
		}
		found := false
		for _, indexFile := range indexFiles {
			candidate := pathpkg.Join(name, indexFile)
			opened, err = match.fileSystem.Open(candidate)
			if err != nil {
				continue
			}
			info, err = opened.Stat()
			if err == nil && !info.IsDir() {
				name = candidate
				found = true
				break
			}
			_ = opened.Close()
		}
		if !found {
			return false
		}
	}
	if info.IsDir() {
		return false
	}
	seeker, ok := opened.(interface {
		Read([]byte) (int, error)
		Seek(int64, int) (int64, error)
	})
	if !ok {
		return false
	}
	response.Header().Del(ws.usedDeviceIdKey)
	response.Header().Del(ws.usedSessionIdKey)
	contentType := mime.TypeByExtension(filepath.Ext(name))
	if contentType != "" {
		response.Header().Set("Content-Type", contentType)
	}
	if ws.webDevEnabled && strings.HasPrefix(contentType, "text/html") {
		data, readErr := io.ReadAll(seeker)
		if readErr != nil {
			return false
		}
		response.Header().Set("Last-Modified", info.ModTime().UTC().Format(http.TimeFormat))
		_, _ = response.WriteFiltered(data)
		return true
	}
	http.ServeContent(&staticResponseWriter{response: response}, request.Request, info.Name(), info.ModTime(), seeker)
	return true
}

type staticResponseWriter struct {
	response *Response
}

func (w *staticResponseWriter) Header() http.Header {
	return w.response.Header().H
}

func (w *staticResponseWriter) WriteHeader(statusCode int) {
	w.response.WriteHeader(statusCode)
}

func (w *staticResponseWriter) Write(data []byte) (int, error) {
	return w.response.Write(data)
}

var _ http.ResponseWriter = (*staticResponseWriter)(nil)
