package service

import (
	"net/http"
	"sort"
	"strings"

	"apigo.cc/go/cast"
)

// sanitizeOpts 日志脱敏配置
type sanitizeOpts struct {
	maxSize   int // 整体尺寸上限（内容字符估算）
	fieldSize int // 单字符串截断长度
	arrayNum  int // 数组最多保留元素数
	objectNum int // 对象最多保留 key 数
}

// sanitizeLogData 递归脱敏，返回新建的对象，不影响原始数据
func sanitizeLogData(v any, opts sanitizeOpts) any {
	budget := opts.maxSize
	return sanitizeRecursive(v, opts, &budget)
}

func sanitizeRecursive(v any, opts sanitizeOpts, budget *int) any {
	if v == nil {
		return nil
	}

	switch val := v.(type) {
	case string:
		return sanitizeString(val, opts, budget)
	case bool:
		return sanitizeScalar(val, 5, budget)
	case float64:
		return sanitizeScalar(val, 8, budget)
	case map[string]any:
		return sanitizeMapContent(val, opts, budget)
	case []any:
		return sanitizeSliceContent(val, opts, budget)
	default:
		// int 等各种数值类型
		return sanitizeScalar(val, 8, budget)
	}
}

func sanitizeString(s string, opts sanitizeOpts, budget *int) string {
	if len([]rune(s)) > opts.fieldSize {
		s = string([]rune(s)[:opts.fieldSize])
	}
	if *budget < len(s) {
		*budget = 0
		return s // 即使超出预算也返回截断后的内容
	}
	*budget -= len(s)
	return s
}

func sanitizeScalar(v any, cost int, budget *int) any {
	if *budget < cost {
		*budget = 0
		return nil
	}
	*budget -= cost
	return v
}

func sanitizeMapContent(m map[string]any, opts sanitizeOpts, budget *int) map[string]any {
	// 空 map 占 2 预算
	if *budget < 2 {
		*budget = 0
		return map[string]any{}
	}
	*budget -= 2

	result := make(map[string]any)
	// 排序 key 保证遍历顺序确定性
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	count := 0
	for _, k := range keys {
		if count >= opts.objectNum || *budget <= 0 {
			break
		}

		// 扣 key 长度
		keyCost := len(k)
		if *budget < keyCost {
			break
		}
		budgetBefore := *budget
		*budget -= keyCost

		// 递归处理 value
		processed := sanitizeRecursive(m[k], opts, budget)

		// 检查是否因预算不足返回了 nil（仅当原值非 nil 时）
		if processed == nil && m[k] != nil {
			*budget = budgetBefore
			break
		}

		result[k] = processed
		count++
	}
	return result
}

func sanitizeSliceContent(s []any, opts sanitizeOpts, budget *int) []any {
	// 空 slice 占 2 预算
	if *budget < 2 {
		*budget = 0
		return []any{}
	}
	*budget -= 2

	result := make([]any, 0, min(len(s), opts.arrayNum))
	count := 0
	for _, v := range s {
		if count >= opts.arrayNum || *budget <= 0 {
			break
		}

		// 值预算由递归处理
		processed := sanitizeRecursive(v, opts, budget)
		if processed == nil && v != nil {
			break
		}

		result = append(result, processed)
		count++
	}
	return result
}

// sanitizeLogHeaders 过滤请求/响应头，排除 NoLogHeaders 中指定的字段
func sanitizeLogHeaders(h http.Header, noLogHeaders string) map[string]string {
	result := make(map[string]string)
	excludes := strings.Split(noLogHeaders, ",")
	for k, v := range h {
		skip := false
		for _, ex := range excludes {
			if ex != "" && strings.EqualFold(k, strings.TrimSpace(ex)) {
				skip = true
				break
			}
		}
		if !skip {
			result[k] = strings.Join(v, ", ")
		}
	}
	return result
}

// sanitizeRespBody 对响应体进行脱敏：尝试 JSON 解析后走对象脱敏，失败则按字符串截断
func sanitizeRespBody(body []byte, cfg *ServiceConfig) any {
	// 尝试解析为 JSON 对象
	var parsed any
	if err := cast.UnmarshalJSON(body, &parsed); err == nil && parsed != nil {
		// 排除敏感字段
		if cfg.NoLogOutputFields != "" {
			parsed = stripFields(parsed, cfg.NoLogOutputFields)
		}
		return sanitizeLogData(parsed, sanitizeOpts{
			maxSize:   200,
			fieldSize: cfg.LogOutputFieldSize,
			arrayNum:  cfg.LogOutputArrayNum,
			objectNum: cfg.LogOutputObjectNum,
		})
	}

	// 非 JSON 内容，按字符串截断
	if len(body) > cfg.LogOutputMaxSize {
		return string(body[:cfg.LogOutputMaxSize]) + "..."
	}
	return string(body)
}

// stripFields 从对象中删除指定字段（仅处理顶层 map）
func stripFields(v any, fields string) any {
	m, ok := v.(map[string]any)
	if !ok {
		return v
	}
	excludes := strings.Split(fields, ",")
	for _, f := range excludes {
		f = strings.TrimSpace(f)
		if f != "" {
			delete(m, f)
		}
	}
	return m
}
