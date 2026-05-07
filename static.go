package service

import (
	"apigo.cc/go/file"
	"apigo.cc/go/log"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

var (
	statics           = make(map[string]*string)
	staticsByHost     = make(map[string]map[string]*string)
	staticsByHostLock = sync.RWMutex{}
)

// Static 注册静态文件目录
func Static(path, rootPath string) {
	StaticByHost(path, rootPath, "")
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

	if host == "" {
		statics[path] = &rootPath
	} else {
		if staticsByHost[host] == nil {
			staticsByHost[host] = make(map[string]*string)
		}
		staticsByHost[host][path] = &rootPath
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

	info, err := os.Stat(filePath)
	if err != nil {
		return false
	}

	if info.IsDir() {
		// 自动查找索引文件
		for _, indexFile := range Config.IndexFiles {
			f := filepath.Join(filePath, indexFile)
			if i, err := os.Stat(f); err == nil && !i.IsDir() {
				filePath = f
				info = i
				break
			}
		}
	}

	if info.IsDir() {
		return false
	}

	// 检查 304
	if ifModifiedSince := request.Header.Get("If-Modified-Since"); ifModifiedSince != "" {
		if t, err := time.Parse(http.TimeFormat, ifModifiedSince); err == nil {
			if !info.ModTime().Truncate(time.Second).After(t.Truncate(time.Second)) {
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
	response.Header().Set("Last-Modified", info.ModTime().UTC().Format(http.TimeFormat))

	data, err := file.ReadBytes(filePath)
	if err != nil {
		return false
	}

	_, _ = response.Write(data)
	return true
}
