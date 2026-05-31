package service

import (
	"apigo.cc/go/discover"
	"io"
	"mime/multipart"
	"net"
	"net/http"
	"net/textproto"
	"net/url"
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
	scheme := r.Header.Get(discover.HeaderScheme)
	if scheme == "" {
		scheme = "http"
	}
	host := r.Header.Get(discover.HeaderHost)
	if host == "" {
		host = r.Host
	}
	return scheme + "://" + host + path
}

// DeviceId 获取设备 ID
func (r *Request) DeviceId() string {
	return r.Header.Get(discover.HeaderDeviceID)
}

// SessionId 获取会话 ID
func (r *Request) SessionId() string {
	return r.Header.Get(discover.HeaderSessionID)
}

// SetUserId 设置用户 ID（传递给下游）
func (r *Request) SetUserId(userId string) {
	r.Header.Set(discover.HeaderUserID, userId)
}

// ClientIp 获取真实 IP
func (r *Request) ClientIp() string {
	ip := r.Header.Get(discover.HeaderClientIP)
	if ip == "" {
		ip = r.Header.Get(discover.HeaderForwardedFor)
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
