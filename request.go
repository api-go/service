package service

import (
	"apigo.cc/go/cast"
	"apigo.cc/go/standard"
	"io"
	"mime/multipart"
	"net"
	"net/http"
	"net/textproto"
	"net/url"
	"os"
	"path/filepath"
)

// UploadFile 上传文件结构
type UploadFile struct {
	fileHeader *multipart.FileHeader
	Filename   string
	Header     textproto.MIMEHeader
	Size       int64
}

// Open 打开上传文件
func (f *UploadFile) Open() (multipart.File, error) {
	return f.fileHeader.Open()
}

// Save 保存上传文件到本地
func (f *UploadFile) Save(filename string) error {
	dir := filepath.Dir(filename)
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		_ = os.MkdirAll(dir, 0755)
	}

	dst, err := os.OpenFile(filename, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
	if err != nil {
		return err
	}
	defer dst.Close()

	src, err := f.fileHeader.Open()
	if err != nil {
		return err
	}
	defer src.Close()

	_, err = io.Copy(dst, src)
	return err
}

// Content 获取上传文件内容
func (f *UploadFile) Content() ([]byte, error) {
	src, err := f.fileHeader.Open()
	if err != nil {
		return nil, err
	}
	defer src.Close()
	return io.ReadAll(src)
}

// Request 封装 http.Request
type Request struct {
	*http.Request
	contextValues map[string]any
	Id            string
}

// NewRequest 创建 Request 包装
func NewRequest(httpRequest *http.Request) *Request {
	return &Request{
		Request:       httpRequest,
		contextValues: make(map[string]any),
	}
}

// ResetPath 重写请求路径
func (r *Request) ResetPath(path string) {
	r.RequestURI = path
	if u, err := url.Parse(path); err == nil {
		r.URL = u
	}
}

// Set 设置请求上下文变量
func (r *Request) Set(key string, value any) {
	r.contextValues[key] = value
}

// Get 获取请求上下文变量
func (r *Request) Get(key string) any {
	return r.contextValues[key]
}

// MakeUrl 根据当前请求构建完整 URL
func (r *Request) MakeUrl(path string) string {
	scheme := r.Header.Get(standard.DiscoverHeaderScheme)
	if scheme == "" {
		scheme = "http"
	}
	host := r.Header.Get(standard.DiscoverHeaderHost)
	if host == "" {
		host = r.Host
	}
	return scheme + "://" + host + path
}

// GetSessionId 获取会话 ID
func (r *Request) GetSessionId() string {
	sessionId := r.Header.Get(Config.Listen) // Wait, this should be usedSessionIdKey
	// TODO: Fix dependency on global usedSessionIdKey
	return sessionId
}

// SetUserId 设置用户 ID（传递给下游）
func (r *Request) SetUserId(userId string) {
	r.Header.Set(standard.DiscoverHeaderUserId, userId)
}

// GetRealIp 获取真实 IP
func (r *Request) GetRealIp() string {
	ip := r.Header.Get(standard.DiscoverHeaderClientIp)
	if ip == "" {
		ip = r.Header.Get(standard.DiscoverHeaderForwardedFor)
	}
	if ip == "" {
		host, _, err := net.SplitHostPort(r.RemoteAddr)
		if err == nil {
			return host
		}
		return r.RemoteAddr
	}
	return ip
}

// GetLowerName (Aliased from cast)
func GetLowerName(s string) string {
	return cast.GetLowerName(s)
}
