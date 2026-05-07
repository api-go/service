package service

// Map 通用 Map 类型
type Map = map[string]any

// Arr 通用切片类型
type Arr = []any

// Argot 错误码/标识符类型
type Argot string

// Result 通用返回结构
type Result struct {
	Ok      bool   `json:"ok"`
	Argot   Argot  `json:"argot,omitempty"`
	Message string `json:"message,omitempty"`
}

// CodeResult 带状态码的返回结构
type CodeResult struct {
	Code    int    `json:"code"`
	Message string `json:"message,omitempty"`
}

// ArgotInfo 标识符信息（用于文档生成）
type ArgotInfo struct {
	Name Argot
	Memo string
}

// OK 设置成功状态
func (r *Result) OK(argots ...Argot) {
	r.Ok = true
	if len(argots) > 0 {
		r.Argot = argots[0]
	}
}

// Failed 设置失败状态
func (r *Result) Failed(message string, argots ...Argot) {
	r.Ok = false
	r.Message = message
	if len(argots) > 0 {
		r.Argot = argots[0]
	}
}

// Done 根据布尔值设置状态
func (r *Result) Done(ok bool, failedMessage string, argots ...Argot) {
	r.Ok = ok
	if !ok {
		r.Message = failedMessage
		if len(argots) > 0 {
			r.Argot = argots[0]
		}
	}
}

// OK 设置成功状态 (Code=1)
func (r *CodeResult) OK() {
	r.Code = 1
}

// Failed 设置失败状态与错误码
func (r *CodeResult) Failed(code int, message string) {
	r.Code = code
	r.Message = message
}
