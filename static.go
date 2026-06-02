package service

import (
	"apigo.cc/go/file"
	"apigo.cc/go/log"
	"mime"
	"net/http"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

var (
	statics           = make(map[string]*string)
	staticsByHost     = make(map[string]map[string]*string)
	
	codeStatics       = make(map[string]map[string]*string)
	fileStatics       = make(map[string]map[string]*string)
	dynamicStatics    = make(map[string]map[string]*string)

	staticsByHostLock = sync.RWMutex{}
)

// Static 注册静态文件目录
func (hc *HostContext) Static(path, rootPath string) *HostContext {
	host := hc.host
	if host == "*" {
		host = ""
	}
	StaticByHost(path, rootPath, host)
	return hc
}

// Static 注册静态文件目录 (使用默认 Host "*")
func Static(path, rootPath string) {
	Host("*").Static(path, rootPath)
}

// StaticByHost 为指定域名注册静态文件目录
func StaticByHost(path, rootPath, host string) {
	if !filepath.IsAbs(rootPath) {
		if absPath, err := filepath.Abs(rootPath); err == nil {
			rootPath = absPath
		}
	}

	staticsByHostLock.Lock()
	defer staticsByHostLock.Unlock()

	if codeStatics[host] == nil {
		codeStatics[host] = make(map[string]*string)
	}
	codeStatics[host][path] = &rootPath
	rebuildStaticsUnderLock(host)
}

// ReplaceStatics 使用 Copy-on-Write 机制原子地替换指定 host 下的动态静态目录规则
func ReplaceStatics(host string, config map[string]string) {
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

	staticsByHostLock.Lock()
	defer staticsByHostLock.Unlock()
	
	dynamicStatics[host] = newStatics
	rebuildStaticsUnderLock(host)
}

func rebuildStaticsUnderLock(host string) {
	combined := make(map[string]*string)
	
	// 合并三种来源的静态路由
	for k, v := range codeStatics[host] {
		combined[k] = v
	}
	for k, v := range fileStatics[host] {
		combined[k] = v
	}
	for k, v := range dynamicStatics[host] {
		combined[k] = v
	}
	
	if host == "" {
		statics = combined
	} else {
		staticsByHost[host] = combined
	}
}

func getStaticFilePath(requestPath, host string) string {
	staticsByHostLock.RLock()
	defer staticsByHostLock.RUnlock()

	// 优先匹配指定域名的配置
	if hostConfig, exists := staticsByHost[host]; exists {
		if filePath := findMatchedPath(hostConfig, requestPath); filePath != "" {
			return filePath
		}
	}

	// 匹配全局配置
	return findMatchedPath(statics, requestPath)
}

func findMatchedPath(config map[string]*string, requestPath string) string {
	for urlPath, rootPath := range config {
		if strings.HasPrefix(requestPath, urlPath) {
			return filepath.Join(*rootPath, requestPath[len(urlPath):])
		}
	}
	return ""
}

func processStatic(requestPath string, request *Request, response *Response, logger *log.Logger) bool {
	filePath := getStaticFilePath(requestPath, request.Host)
	if filePath == "" {
		return false
	}

	info := file.GetFileInfo(filePath)
	if info == nil {
		return false
	}

	if info.IsDir {
		// 自动查找索引文件
		for _, indexFile := range Config.IndexFiles {
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

	// 检查 304
	if ifModifiedSince := request.Header.Get("If-Modified-Since"); ifModifiedSince != "" {
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

	_, _ = response.Write(data)
	return true
}
