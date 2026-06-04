package service

import (
	"apigo.cc/go/file"
	"apigo.cc/go/log"
	"mime"
	"net/http"
	"path/filepath"
	"strings"
	"time"
)

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

func (ws *webServer) Static(path, rootPath string) {
	ws.Host("*").Static(path, rootPath)
}

// StaticByHost 为指定域名注册静态文件目录
func StaticByHost(path, rootPath, host string) {
	DefaultServer.StaticByHost(path, rootPath, host)
}

func (ws *webServer) StaticByHost(path, rootPath, host string) {
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

func (ws *webServer) ReplaceStatics(host string, config map[string]string) {
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

func (ws *webServer) getStaticFilePath(requestPath, host string) string {
	ws.staticsByHostLock.RLock()
	defer ws.staticsByHostLock.RUnlock()

	// 优先匹配指定域名的配置
	if hostConfig, exists := ws.staticsByHost[host]; exists {
		if filePath := ws.findMatchedPath(hostConfig, requestPath); filePath != "" {
			return filePath
		}
	}

	// 匹配全局配置
	return ws.findMatchedPath(ws.statics, requestPath)
}

func (ws *webServer) findMatchedPath(config map[string]*string, requestPath string) string {
	for urlPath, rootPath := range config {
		if strings.HasPrefix(requestPath, urlPath) {
			return filepath.Join(*rootPath, requestPath[len(urlPath):])
		}
	}
	return ""
}

func (ws *webServer) processStatic(requestPath string, request *Request, response *Response, logger *log.Logger) bool {
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
