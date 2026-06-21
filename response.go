package service

import (
	"apigo.cc/go/cast"
	"apigo.cc/go/file"
	"apigo.cc/go/jsmod"
	"io"
	"net/http"
)

// Response 封装 http.ResponseWriter
type Response struct {
	Id            string
	Writer        http.ResponseWriter `js:"-"`
	Code          int
	body          []byte              `js:"-"`
	outLen        int                 `js:"-"`
	changed       bool                `js:"-"`
	headerWritten bool                `js:"-"`
	dontLog200    bool                `js:"-"`
	dontLogArgs   []string            `js:"-"`
	ProxyHeader   *http.Header        `js:"-"`
	server        *WebServer          `js:"-"`
}

func (r *Response) SetCookie(cookie *Cookie) {
	if cookie == nil {
		return
	}
	http.SetCookie(r.Writer, &http.Cookie{
		Name:     cookie.Name,
		Value:    cookie.Value,
		Path:     cookie.Path,
		Domain:   cookie.Domain,
		MaxAge:   cookie.MaxAge,
		Secure:   cookie.Secure,
		HttpOnly: cookie.HttpOnly,
	})
}

// NewResponse 创建 Response 包装
func NewResponse(writer http.ResponseWriter, server *WebServer) *Response {
	return &Response{
		Writer: writer,
		Code:   http.StatusOK,
		server: server,
	}
}

// Header 获取响应头部
func (r *Response) Header() *Header {
	if r.ProxyHeader != nil {
		return &Header{H: *r.ProxyHeader}
	}
	return &Header{H: r.Writer.Header()}
}

// Write 写入响应内容
func (r *Response) Write(bytes []byte) (int, error) {
	r.checkWriteHeader()
	r.changed = true
	r.outLen += len(bytes)

	// 如果有输出过滤器，我们必须先缓冲，不能直接写入网线，否则会导致重复输出
	if r.server != nil && r.server.hasOutFilter {
		r.body = append(r.body, bytes...)
		return len(bytes), nil
	}

	// 即使没有过滤器，非 200 状态码也进行缓冲以便日志记录
	if r.Code != http.StatusOK {
		r.body = append(r.body, bytes...)
	}

	if r.ProxyHeader != nil {
		r.copyProxyHeader()
	}
	n, err := r.Writer.Write(bytes)
	if err != nil {
		return n, jsmod.MakeError(err)
	}
	return n, nil
}

// PhysicalWrite 物理写入网线，绕过过滤器缓冲逻辑
func (r *Response) PhysicalWrite(bytes []byte) (int, error) {
	r.checkWriteHeader()
	if r.ProxyHeader != nil {
		r.copyProxyHeader()
	}
	n, err := r.Writer.Write(bytes)
	if err != nil {
		return n, jsmod.MakeError(err)
	}
	return n, nil
}

// WriteString 写入字符串响应
func (r *Response) WriteString(s string) (int, error) {
	n, err := r.Write([]byte(s))
	if err != nil {
		return n, jsmod.MakeError(err)
	}
	return n, nil
}

// WriteHeader 设置响应状态码
func (r *Response) WriteHeader(code int) {
	r.changed = true
	r.Code = code
	if r.ProxyHeader != nil && (r.Code == http.StatusBadGateway || r.Code == http.StatusServiceUnavailable || r.Code == http.StatusGatewayTimeout) {
		return
	}
	if r.ProxyHeader != nil {
		r.copyProxyHeader()
	}
}

func (r *Response) checkWriteHeader() {
	if !r.headerWritten {
		r.headerWritten = true
		if r.Code != http.StatusOK {
			r.Writer.WriteHeader(r.Code)
		}
	}
}

func (r *Response) copyProxyHeader() {
	src := *r.ProxyHeader
	dst := r.Writer.Header()
	for k, vv := range src {
		for _, v := range vv {
			dst.Add(k, v)
		}
	}
	r.ProxyHeader = nil
}

// Flush 刷新响应缓冲区
func (r *Response) Flush() {
	if flusher, ok := r.Writer.(http.Flusher); ok {
		flusher.Flush()
	}
}

// GetStatusCode 获取当前状态码
func (r *Response) GetStatusCode() int {
	return r.Code
}

// GetBody 获取响应内容
func (r *Response) GetBody() []byte {
	return r.body
}

// ClearBody 清空响应内容缓冲区 (用于过滤器替换内容)
func (r *Response) ClearBody() {
	r.body = nil
	r.outLen = 0
	// 注意：这里我们不重置 headerWritten 和 Code，因为 Header 已经发出去了。
	// 但是在某些测试环境下（如 httptest.Recorder），我们可以尝试“假装”没写过。
	// 实际上，生产环境下 Header 发出去就收不回来了，所以注入只能发生在 Body 层面。
}

// DontLog200 标记不记录 200 状态码的日志
func (r *Response) DontLog200() {
	r.dontLog200 = true
}

// Location 设置重定向地址
func (r *Response) Location(location string) {
	r.WriteHeader(http.StatusFound)
	r.Header().Set("Location", location)
}

// SendFile 发送文件
func (r *Response) SendFile(contentType, filename string) {
	r.Header().Set("Content-Type", contentType)
	if data, err := file.ReadBytes(filename); err == nil {
		_, _ = r.Write(data)
	}
}

// DownloadFile 下载文件
func (r *Response) DownloadFile(contentType, filename string, data any) {
	if contentType == "" {
		contentType = "application/octet-stream"
	}
	r.Header().Set("Content-Type", contentType)

	if filename != "" {
		r.Header().Set("Content-Disposition", "attachment; filename="+filename)
	}

	var outBytes []byte
	var reader io.Reader

	switch v := data.(type) {
	case []byte:
		outBytes = v
	case string:
		outBytes = []byte(v)
	case io.Reader:
		reader = v
	default:
		outBytes, _ = cast.ToJSONBytes(data)
	}

	if outBytes != nil {
		r.Header().Set("Content-Length", cast.String(len(outBytes)))
		_, _ = r.Write(outBytes)
	} else if reader != nil {
		_, _ = io.Copy(r, reader)
	}
}
