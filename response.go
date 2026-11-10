package service

import (
	"github.com/api-go/cast"
	"github.com/api-go/file"
	"github.com/api-go/jsmod"
	"fmt"
	"io"
	"net/http"
	"reflect"
)

// Response 封装 http.ResponseWriter
type Response struct {
	Id            string
	Writer        http.ResponseWriter `js:"-"`
	Code          int
	body          []byte       `js:"-"`
	pending       []byte       `js:"-"`
	outLen        int          `js:"-"`
	changed       bool         `js:"-"`
	pendingWrite  bool         `js:"-"`
	filteredWrite bool         `js:"-"`
	streaming     bool         `js:"-"`
	headerWritten bool         `js:"-"`
	dontLog200    bool         `js:"-"`
	dontLogArgs   []string     `js:"-"`
	ProxyHeader   *http.Header `js:"-"`
	server        *WebServer   `js:"-"`
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
	r.changed = true
	r.outLen += len(bytes)
	if r.shouldBufferWrite(bytes) {
		r.pendingWrite = true
		r.pending = append(r.pending, bytes...)
		return len(bytes), nil
	}
	r.checkWriteHeader()

	// 缓冲 body 用于日志记录
	r.keepBody(bytes)

	if r.ProxyHeader != nil {
		r.copyProxyHeader()
	}
	n, err := r.Writer.Write(bytes)
	if err != nil {
		return n, jsmod.MakeError(err)
	}
	return n, nil
}

func (r *Response) shouldBufferWrite(data []byte) bool {
	if r.streaming || r.server == nil {
		return false
	}
	contentType := r.Header().Get("Content-Type")
	if contentType == "" && len(data) > 0 {
		contentType = http.DetectContentType(data)
	}
	if !r.server.shouldFilterDynamicContent(contentType, data) {
		return false
	}
	if r.Header().Get("Content-Type") == "" {
		r.Header().Set("Content-Type", contentType)
	}
	return true
}

func (r *Response) writeFiltered(bytes []byte) (int, error) {
	if r.server == nil || !r.server.hasOutFilter {
		return r.Write(bytes)
	}
	r.checkWriteHeader()
	r.changed = true
	r.filteredWrite = true
	r.outLen += len(bytes)
	r.body = append(r.body, bytes...)
	return len(bytes), nil
}

// WriteBytes writes a byte array received through a dynamic runtime without
// converting its bytes to text first. This keeps UTF-8 stream chunks intact.
func (r *Response) WriteBytes(value any) (int, error) {
	bytes, err := responseBytes(value)
	if err != nil {
		return 0, jsmod.MakeError(err)
	}
	return r.Write(bytes)
}

func responseBytes(value any) ([]byte, error) {
	if value == nil {
		return nil, nil
	}
	if bytes, ok := value.([]byte); ok {
		return bytes, nil
	}
	if text, ok := value.(string); ok {
		return []byte(text), nil
	}

	rv := reflect.ValueOf(value)
	if rv.Kind() != reflect.Array && rv.Kind() != reflect.Slice {
		return nil, fmt.Errorf("response bytes must be a string or byte array, got %T", value)
	}
	bytes := make([]byte, rv.Len())
	for i := 0; i < rv.Len(); i++ {
		item := rv.Index(i)
		for item.IsValid() && (item.Kind() == reflect.Interface || item.Kind() == reflect.Pointer) {
			if item.IsNil() {
				return nil, fmt.Errorf("response byte at index %d is nil", i)
			}
			item = item.Elem()
		}
		if !item.IsValid() {
			return nil, fmt.Errorf("response byte at index %d is nil", i)
		}
		switch item.Kind() {
		case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
			if item.Uint() > 255 {
				return nil, fmt.Errorf("response byte at index %d is out of range", i)
			}
			bytes[i] = byte(item.Uint())
		case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
			if item.Int() < 0 || item.Int() > 255 {
				return nil, fmt.Errorf("response byte at index %d is out of range", i)
			}
			bytes[i] = byte(item.Int())
		case reflect.Float32, reflect.Float64:
			n := item.Float()
			if n < 0 || n > 255 || n != float64(byte(n)) {
				return nil, fmt.Errorf("response byte at index %d is out of range", i)
			}
			bytes[i] = byte(n)
		default:
			return nil, fmt.Errorf("response byte at index %d has type %s", i, item.Kind())
		}
	}
	return bytes, nil
}

// keepBody 缓冲数据用于日志记录，限制大小防止内存问题
func (r *Response) keepBody(bytes []byte) {
	maxBuf := 200
	if r.server != nil && r.server.Config.LogOutputMaxSize > 0 {
		maxBuf = r.server.Config.LogOutputMaxSize
	}
	if len(r.body) < maxBuf {
		space := maxBuf - len(r.body)
		if len(bytes) <= space {
			r.body = append(r.body, bytes...)
		} else {
			r.body = append(r.body, bytes[:space]...)
		}
	}
}

func (r *Response) physicalWrite(bytes []byte) (int, error) {
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
	if len(r.pending) > 0 {
		_, _ = r.physicalWrite(r.pending)
		r.pending = nil
		r.pendingWrite = false
	}
	r.streaming = true
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
