package service

import (
	"apigo.cc/go/cast"
	"apigo.cc/go/file"
	"io"
	"net/http"
)

// Response 封装 http.ResponseWriter
type Response struct {
	Id            string
	Writer        http.ResponseWriter
	Code          int
	body          []byte
	outLen        int
	changed       bool
	headerWritten bool
	dontLog200    bool
	dontLogArgs   []string
	ProxyHeader   *http.Header
}

// NewResponse 创建 Response 包装
func NewResponse(writer http.ResponseWriter) *Response {
	return &Response{
		Writer: writer,
		Code:   http.StatusOK,
	}
}

// Header 获取响应头部
func (r *Response) Header() http.Header {
	if r.ProxyHeader != nil {
		return *r.ProxyHeader
	}
	return r.Writer.Header()
}

// Write 写入响应内容
func (r *Response) Write(bytes []byte) (int, error) {
	r.checkWriteHeader()
	r.changed = true
	r.outLen += len(bytes)
	if r.Code != http.StatusOK && len(r.body) < 4096 {
		r.body = append(r.body, bytes...)
	}
	if r.ProxyHeader != nil {
		r.copyProxyHeader()
	}
	return r.Writer.Write(bytes)
}

// WriteString 写入字符串响应
func (r *Response) WriteString(s string) (int, error) {
	return r.Write([]byte(s))
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
