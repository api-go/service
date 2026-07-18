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

// Header 包装 http.Header 以提供 JS 友好的方法
type Header struct {
	H http.Header `js:"-"`
}

func (h *Header) Get(key string) string {
	return h.H.Get(key)
}

func (h *Header) Set(key, value string) {
	h.H.Set(key, value)
}

func (h *Header) Add(key, value string) {
	h.H.Add(key, value)
}

func (h *Header) Del(key string) {
	h.H.Del(key)
}

func (h *Header) Values(key string) []string {
	return h.H.Values(key)
}

// Request 封装 http.Request
type Request struct {
	*http.Request `js:"-"`
	contextValues map[string]any `js:"-"`
	Id            string
}

// Cookie 简化的 JS 友好 Cookie 结构
type Cookie struct {
	Name     string
	Value    string
	Path     string
	Domain   string
	MaxAge   int
	Secure   bool
	HttpOnly bool
}

func (r *Request) GetCookie(name string) *Cookie {
	c, err := r.Request.Cookie(name)
	if err != nil {
		return nil
	}
	return &Cookie{
		Name:     c.Name,
		Value:    c.Value,
		Path:     c.Path,
		Domain:   c.Domain,
		MaxAge:   c.MaxAge,
		Secure:   c.Secure,
		HttpOnly: c.HttpOnly,
	}
}

func (r *Request) GetCookies() []*Cookie {
	res := make([]*Cookie, 0)
	for _, c := range r.Request.Cookies() {
		res = append(res, &Cookie{
			Name:     c.Name,
			Value:    c.Value,
			Path:     c.Path,
			Domain:   c.Domain,
			MaxAge:   c.MaxAge,
			Secure:   c.Secure,
			HttpOnly: c.HttpOnly,
		})
	}
	return res
}

// AddCookie 遮蔽原生的 AddCookie
func (r *Request) AddCookie(c *Cookie) {
	if c == nil {
		return
	}
	r.Request.AddCookie(&http.Cookie{
		Name:     c.Name,
		Value:    c.Value,
		Path:     c.Path,
		Domain:   c.Domain,
		MaxAge:   c.MaxAge,
		Secure:   c.Secure,
		HttpOnly: c.HttpOnly,
	})
}

// CookiesNamed 遮蔽原生的 CookiesNamed
func (r *Request) CookiesNamed(name string) []*Cookie {
	res := make([]*Cookie, 0)
	for _, c := range r.Request.Cookies() {
		if c.Name == name {
			res = append(res, &Cookie{
				Name:     c.Name,
				Value:    c.Value,
				Path:     c.Path,
				Domain:   c.Domain,
				MaxAge:   c.MaxAge,
				Secure:   c.Secure,
				HttpOnly: c.HttpOnly,
			})
		}
	}
	return res
}

func (r *Request) Header() *Header {
	return &Header{H: r.Request.Header}
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
	scheme := r.Header().Get(discover.HeaderScheme)
	if scheme == "" {
		scheme = "http"
	}
	host := r.Header().Get(discover.HeaderHost)
	if host == "" {
		host = r.Host
	}
	return scheme + "://" + host + path
}

// DeviceId 获取设备 ID
func (r *Request) DeviceId() string {
	return r.Header().Get(discover.HeaderDeviceId)
}

// SessionId 获取会话 ID
func (r *Request) SessionId() string {
	return r.Header().Get(discover.HeaderSessionId)
}

// SetUserId 设置用户 ID（传递给下游）
func (r *Request) SetUserId(userId string) {
	r.Header().Set(discover.HeaderUserId, userId)
}

// ClientIp 获取真实 IP
func (r *Request) ClientIp() string {
	ip := r.Header().Get(discover.HeaderClientIp)
	if ip == "" {
		ip = r.Header().Get(discover.HeaderForwardedFor)
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
