package service

import (
	"apigo.cc/go/jsmod"
)

func init() {
	jsmod.Register("service", map[string]any{
		// 类型占位工厂 (用于 AI 发现类型结构，生成文档时隐藏)
		"__exportRequest":   jsExportRequest,
		"__exportResponse":  jsExportResponse,
		"__exportWebSocket": jsExportWebSocket,
		"__exportSession":   jsExportSession,
		"__exportFile":      jsExportFile,

		// 功能函数
		"Upgrade": jsUpgrade,
	})
}

func jsExportRequest() *Request         { return &Request{} }
func jsExportResponse() *Response       { return &Response{} }
func jsExportWebSocket() *WebSocketConn { return &WebSocketConn{} }
func jsExportSession() *Session         { return &Session{} }
func jsExportFile() *jsUploadFile       { return &jsUploadFile{} }

func jsUpgrade(response *Response, request *Request) (*WebSocketConn, error) {
	conn, err := Upgrade(response, request)
	if err != nil {
		return nil, jsmod.MakeError(err)
	}
	return conn, nil
}

// jsUploadFile 包装 UploadFile 以隐藏敏感方法（如 Open）
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
	res, err := j.f.Content()
	if err != nil {
		return nil, jsmod.MakeError(err)
	}
	return res, nil
}
