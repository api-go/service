package service

import (
	"apigo.cc/go/jsmod"
)

func init() {
	jsmod.Register("service", map[string]any{
		// 类型占位工厂 (用于 AI 发现类型结构，生成文档时隐藏)
		"__exportRequest":   func() *Request { return &Request{} },
		"__exportResponse":  func() *Response { return &Response{} },
		"__exportWebSocket": func() *WebSocketConn { return &WebSocketConn{} },
		"__exportSession":   func() *Session { return &Session{} },
		"__exportFile":      func() *jsUploadFile { return &jsUploadFile{} },

		// 功能函数
		"upgrade": Upgrade,
	})
}

// jsUploadFile 包装 UploadFile 以隐藏敏感方法
type jsUploadFile struct {
	f *UploadFile
}

func (j *jsUploadFile) Filename() string {
	if j.f == nil {
		return ""
	}
	return j.f.Filename
}

func (j *jsUploadFile) Size() int64 {
	if j.f == nil {
		return 0
	}
	return j.f.Size
}

func (j *jsUploadFile) Content() ([]byte, error) {
	if j.f == nil {
		return nil, nil
	}
	return j.f.Content()
}
