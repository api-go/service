package service

import (
	"apigo.cc/go/file"
	"apigo.cc/go/log"
	"io"
	"mime"
	"net/http"
	"net/url"
	"os"
	pathpkg "path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/gobwas/glob"
)

func isAssetTextType(contentType, name string) bool {
	if strings.HasPrefix(contentType, "text/html") || strings.HasPrefix(contentType, "text/css") {
		return true
	}
	ext := strings.ToLower(pathpkg.Ext(name))
	return ext == ".js" || ext == ".mjs" || ext == ".json" || ext == ".svg" || ext == ".xml" || ext == ".html" || ext == ".htm" || ext == ".css"
}

func minifiedSiblingName(name string) string {
	ext := pathpkg.Ext(name)
	if ext == "" || strings.HasSuffix(strings.TrimSuffix(name, ext), ".min") {
		return ""
	}
	return strings.TrimSuffix(name, ext) + ".min" + ext
}

func physicalMinifiedSibling(name string) (string, *file.FileInfo) {
	candidate := minifiedSiblingName(name)
	if candidate == "" {
		return "", nil
	}
	info := file.GetFileInfo(candidate)
	if info == nil || info.IsDir {
		return "", nil
	}
	return candidate, info
}

func openMinifiedSibling(source http.FileSystem, name string) (http.File, os.FileInfo, bool) {
	candidate := minifiedSiblingName(name)
	if candidate == "" {
		return nil, nil, false
	}
	opened, err := source.Open(candidate)
	if err != nil {
		return nil, nil, false
	}
	info, err := opened.Stat()
	if err != nil || info.IsDir() {
		_ = opened.Close()
		return nil, nil, false
	}
	return opened, info, true
}

func (ws *WebServer) assetCachePath(name string) string {
	root := ws.Config.AutoReversionCachePath
	if root == "" {
		root = "data/wwwcache"
	}
	name = strings.TrimPrefix(pathpkg.Clean("/"+name), "/")
	return filepath.Join(root, filepath.FromSlash(name+".min"))
}

type staticFSMatch struct {
	fileSystem http.FileSystem
	route      string
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
			matches = append(matches, staticFSMatch{fileSystem: config[route], route: route, name: name})
		}
		if ws.staticFSExclusive[candidate] {
			break
		}
	}
	return matches
}

func physicalAssetNamespace(host string) string {
	return "file:" + host
}

func (ws *WebServer) physicalAssetLoader(host string) assetGraphLoader {
	return func(assetPath string, readContent bool) ([]byte, int64, bool) {
		physicalPath := ws.getStaticFilePath(assetPath, host)
		if physicalPath == "" {
			return nil, 0, false
		}
		info := file.GetFileInfo(physicalPath)
		if info == nil || info.IsDir {
			return nil, 0, false
		}
		if ws.Config.AutoMinify && isAssetTextType(mime.TypeByExtension(filepath.Ext(physicalPath)), physicalPath) {
			if candidate, candidateInfo := physicalMinifiedSibling(physicalPath); candidateInfo != nil {
				physicalPath, info = candidate, candidateInfo
			}
		}
		if !readContent {
			return nil, info.ModTime, true
		}
		data, err := file.ReadBytes(physicalPath)
		return data, info.ModTime, err == nil
	}
}

func staticFSAssetNamespace(host string, match staticFSMatch) string {
	return "fs:" + host + ":" + match.route
}

func (ws *WebServer) staticFSAssetLoader(match staticFSMatch) assetGraphLoader {
	return func(assetPath string, readContent bool) ([]byte, int64, bool) {
		name := strings.TrimPrefix(assetPath, match.route)
		name = strings.TrimPrefix(pathpkg.Clean("/"+name), "/")
		opened, err := match.fileSystem.Open(name)
		if err != nil {
			return nil, 0, false
		}
		defer opened.Close()
		info, err := opened.Stat()
		if err != nil || info.IsDir() {
			return nil, 0, false
		}
		if ws.Config.AutoMinify && isAssetTextType(mime.TypeByExtension(filepath.Ext(name)), name) {
			if candidate, candidateInfo, ok := openMinifiedSibling(match.fileSystem, name); ok {
				_ = opened.Close()
				opened, info = candidate, candidateInfo
			}
		}
		if !readContent {
			return nil, info.ModTime().Unix(), true
		}
		data, err := io.ReadAll(opened)
		return data, info.ModTime().Unix(), err == nil
	}
}

func (ws *WebServer) graphAssetModTime(namespace, assetPath string, loader assetGraphLoader) (time.Time, bool) {
	_, physicalTime, ok := loader(assetPath, false)
	if !ok {
		return time.Time{}, false
	}
	ws.assetGraph.observe(namespace, assetPath, physicalTime, loader)
	return time.Unix(ws.assetGraph.logicalTime(namespace, assetPath, physicalTime), 0), true
}

func (ws *WebServer) expandedHTMLAssetLoader(namespace, host, sourcePath string, loader assetGraphLoader) assetGraphLoader {
	return func(assetPath string, readContent bool) ([]byte, int64, bool) {
		if resolveAssetPath("/", assetPath) != resolveAssetPath("/", sourcePath) {
			return loader(assetPath, readContent)
		}
		content, physicalTime, ok := loader(assetPath, true)
		if !ok {
			return nil, 0, false
		}
		expanded := ExpandAssetReferences(host, sourcePath, content, ws.expandAssetPattern)
		fingerprint := strings.Builder{}
		for _, reference := range collectAssetReferences(string(expanded)) {
			dependencyPath, local := graphDependencyPath(sourcePath, reference)
			if !local {
				continue
			}
			if _, dependencyTime, found := loader(dependencyPath, false); found {
				fingerprint.WriteString(dependencyPath)
				fingerprint.WriteByte(0)
				fingerprint.WriteString(strconv.FormatInt(dependencyTime, 10))
				fingerprint.WriteByte(0)
				if dependencyTime > physicalTime {
					physicalTime = dependencyTime
				}
			}
		}
		physicalTime = ws.assetPatternModTime(namespace+"\x00"+sourcePath, fingerprint.String(), physicalTime)
		if !readContent {
			return nil, physicalTime, true
		}
		return expanded, physicalTime, true
	}
}

func (ws *WebServer) assetPatternModTime(key, fingerprint string, physicalTime int64) int64 {
	ws.assetPatternMu.Lock()
	defer ws.assetPatternMu.Unlock()
	previous, exists := ws.assetPatternStates[key]
	if exists && previous.fingerprint != fingerprint && physicalTime <= previous.modTime {
		physicalTime = previous.modTime + 1
	}
	if !exists || previous.fingerprint != fingerprint || physicalTime > previous.modTime {
		ws.assetPatternStates[key] = assetPatternState{fingerprint: fingerprint, modTime: physicalTime}
		return physicalTime
	}
	return previous.modTime
}

func (ws *WebServer) expandAssetPattern(host, sourcePath, pattern string) []string {
	pattern = resolveAssetPath(sourcePath, pattern)
	matcher, err := glob.Compile(pattern, '/')
	if err != nil {
		return nil
	}
	matches := make([]string, 0)
	seen := map[string]bool{}
	add := func(assetPath string) {
		assetPath = resolveAssetPath("/", assetPath)
		if !seen[assetPath] && matcher.Match(assetPath) {
			seen[assetPath] = true
			matches = append(matches, assetPath)
		}
	}
	basePath := assetPatternBase(pattern)

	for _, match := range ws.getStaticFSMatches(basePath, host) {
		walkHTTPFileSystem(match.fileSystem, match.name, func(name string) {
			add(pathpkg.Join("/", match.route, name))
		})
	}

	if physicalBase := ws.getStaticFilePath(basePath, host); physicalBase != "" {
		_ = filepath.Walk(physicalBase, func(filePath string, info os.FileInfo, walkErr error) error {
			if walkErr != nil || info.IsDir() {
				return nil
			}
			rel, relErr := filepath.Rel(physicalBase, filePath)
			if relErr == nil {
				add(pathpkg.Join(basePath, filepath.ToSlash(rel)))
			}
			return nil
		})
	}
	sort.Strings(matches)
	if ws.Config.AutoMinify && !strings.Contains(pattern, ".min") {
		logical := make(map[string]bool, len(matches))
		for _, assetPath := range matches {
			logical[assetPath] = true
		}
		filtered := matches[:0]
		for _, assetPath := range matches {
			ext := pathpkg.Ext(assetPath)
			base := strings.TrimSuffix(assetPath, ext)
			if strings.HasSuffix(base, ".min") && logical[strings.TrimSuffix(base, ".min")+ext] {
				continue
			}
			filtered = append(filtered, assetPath)
		}
		matches = filtered
	}
	return matches
}

func assetPatternBase(pattern string) string {
	if index := strings.IndexAny(pattern, "*?["); index >= 0 {
		return pathpkg.Dir(pattern[:index])
	}
	return pattern
}

func walkHTTPFileSystem(source http.FileSystem, name string, visit func(string)) {
	opened, err := source.Open(name)
	if err != nil {
		return
	}
	defer opened.Close()
	info, err := opened.Stat()
	if err != nil {
		return
	}
	if !info.IsDir() {
		visit(name)
		return
	}
	entries, err := opened.Readdir(-1)
	if err != nil {
		return
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })
	for _, entry := range entries {
		child := pathpkg.Join(name, entry.Name())
		if entry.IsDir() {
			walkHTTPFileSystem(source, child, visit)
		} else {
			visit(child)
		}
	}
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
	wasDirectory := info.IsDir

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
	assetSourcePath := request.URL.Path
	if wasDirectory {
		assetSourcePath = pathpkg.Join(assetSourcePath, filepath.Base(filePath))
	}
	preMinified := false
	if ws.Config.AutoMinify && isAssetTextType(mime.TypeByExtension(filepath.Ext(filePath)), filePath) {
		if candidate, candidateInfo := physicalMinifiedSibling(filePath); candidateInfo != nil {
			filePath, info, preMinified = candidate, candidateInfo, true
		}
	}
	// 静态文件通过 Cookie 维护 ID，不应答 Device-Id / Session-Id 头
	response.Header().Del(ws.usedDeviceIdKey)
	response.Header().Del(ws.usedSessionIdKey)
	contentType := mime.TypeByExtension(filepath.Ext(filePath))
	if contentType == "" {
		contentType = "application/octet-stream"
	}
	modTime := time.Unix(info.ModTime, 0)
	assetNamespace := physicalAssetNamespace(request.Host)
	assetLoader := ws.physicalAssetLoader(request.Host)
	if strings.HasPrefix(contentType, "text/html") {
		assetLoader = ws.expandedHTMLAssetLoader(assetNamespace, request.Host, assetSourcePath, assetLoader)
	}
	if ws.Config.AutoReversion {
		ws.assetGraph.observe(assetNamespace, assetSourcePath, info.ModTime, assetLoader)
		modTime = time.Unix(ws.assetGraph.logicalTime(assetNamespace, assetSourcePath, info.ModTime), 0)
	}

	// 检查 304
	if ifModifiedSince := request.Header().Get("If-Modified-Since"); ifModifiedSince != "" {
		if t, err := time.Parse(http.TimeFormat, ifModifiedSince); err == nil {
			if modTime.Truncate(time.Second).Before(t.Truncate(time.Second)) || modTime.Truncate(time.Second).Equal(t.Truncate(time.Second)) {
				response.WriteHeader(http.StatusNotModified)
				return true
			}
		}
	}

	// 发送文件
	response.Header().Set("Content-Type", contentType)
	response.Header().Set("Last-Modified", modTime.UTC().Format(http.TimeFormat))

	data, err := file.ReadBytes(filePath)
	if err != nil {
		return false
	}

	if ws.webDevEnabled || ((ws.Config.AutoReversion || ws.Config.AutoMinify) && isAssetTextType(contentType, filePath)) {
		if ws.Config.AutoMinify && !preMinified && isAssetTextType(contentType, filePath) {
			if cached, cacheErr := BuildAssetCache(filePath, ws.assetCachePath(request.URL.Path), time.Unix(info.ModTime, 0), data, func(input []byte) ([]byte, error) { return minifyAsset(filePath, input) }); len(cached) > 0 {
				data = cached
				if cacheErr != nil {
					logger.Error("failed to minify asset", "path", filePath, "error", cacheErr.Error())
				}
			}
		}
		if ws.Config.AutoReversion && (strings.HasPrefix(contentType, "text/html") || strings.HasPrefix(contentType, "text/css")) {
			if strings.HasPrefix(contentType, "text/html") {
				data = ExpandAssetReferences(request.Host, assetSourcePath, data, ws.expandAssetPattern)
			}
			data = RewriteAssetReferences(request.Host, assetSourcePath, data, func(_ string, _ string, assetPath string) (time.Time, bool) {
				return ws.graphAssetModTime(assetNamespace, assetPath, assetLoader)
			})
		}
		_, _ = response.writeFiltered(data)
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
	preMinified := false
	if ws.Config.AutoMinify && isAssetTextType(mime.TypeByExtension(filepath.Ext(name)), name) {
		if candidate, candidateInfo, ok := openMinifiedSibling(match.fileSystem, name); ok {
			_ = opened.Close()
			opened, info, preMinified = candidate, candidateInfo, true
		}
	}
	assetSourcePath := pathpkg.Join("/", match.route, name)
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
	assetNamespace := staticFSAssetNamespace(request.Host, match)
	assetLoader := ws.staticFSAssetLoader(match)
	if strings.HasPrefix(contentType, "text/html") {
		assetLoader = ws.expandedHTMLAssetLoader(assetNamespace, request.Host, assetSourcePath, assetLoader)
	}
	if (ws.webDevEnabled && strings.HasPrefix(contentType, "text/html")) || ((ws.Config.AutoReversion || ws.Config.AutoMinify) && isAssetTextType(contentType, name)) {
		modTime := info.ModTime()
		if ws.Config.AutoReversion {
			ws.assetGraph.observe(assetNamespace, assetSourcePath, info.ModTime().Unix(), assetLoader)
			modTime = time.Unix(ws.assetGraph.logicalTime(assetNamespace, assetSourcePath, info.ModTime().Unix()), 0)
		}
		response.Header().Set("Last-Modified", modTime.UTC().Format(http.TimeFormat))
		if ifModifiedSince := request.Header().Get("If-Modified-Since"); ifModifiedSince != "" {
			if t, err := time.Parse(http.TimeFormat, ifModifiedSince); err == nil && !modTime.Truncate(time.Second).After(t.Truncate(time.Second)) {
				response.WriteHeader(http.StatusNotModified)
				return true
			}
		}
		data, readErr := io.ReadAll(seeker)
		if readErr != nil {
			return false
		}
		if ws.Config.AutoMinify && !preMinified && isAssetTextType(contentType, name) {
			if cached, cacheErr := BuildAssetCache(name, ws.assetCachePath(name), info.ModTime(), data, func(input []byte) ([]byte, error) { return minifyAsset(name, input) }); len(cached) > 0 {
				data = cached
				if cacheErr != nil {
					log.DefaultLogger.Error("failed to minify asset", "path", name, "error", cacheErr.Error())
				}
			}
		}
		if ws.Config.AutoReversion && (strings.HasPrefix(contentType, "text/html") || strings.HasPrefix(contentType, "text/css")) {
			if strings.HasPrefix(contentType, "text/html") {
				data = ExpandAssetReferences(request.Host, assetSourcePath, data, ws.expandAssetPattern)
			}
			data = RewriteAssetReferences(request.Host, assetSourcePath, data, func(_ string, _ string, assetPath string) (time.Time, bool) {
				return ws.graphAssetModTime(assetNamespace, assetPath, assetLoader)
			})
		}
		_, _ = response.writeFiltered(data)
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
