package service

import (
	"apigo.cc/go/cast"
	_ "embed"
	"encoding/json"
	"reflect"
)

// Api 接口文档信息
type Api struct {
	Type      string
	Path      string
	AuthLevel int
	Method    string
	In        any
	Out       any
	Memo      string
}

//go:embed DocTpl.html
var defaultDocTpl string

// MakeDocument 生成文档数据
func MakeDocument() []Api {
	out := make([]Api, 0)

	// 1. Rewrite
	rewritesLock.RLock()
	for _, a := range rewrites {
		out = append(out, Api{
			Type: "Rewrite",
			Path: a.fromPath + " -> " + a.toPath,
		})
	}
	rewritesLock.RUnlock()

	// 2. Proxy
	proxiesLock.RLock()
	for _, a := range proxies {
		out = append(out, Api{
			Type: "Proxy",
			Path: a.fromPath + " -> " + a.toApp + ":" + a.toPath,
		})
	}
	proxiesLock.RUnlock()

	// 3. Web Services
	webServicesLock.RLock()
	for _, a := range webServicesList {
		if a.options.NoDoc {
			continue
		}
		api := Api{
			Type:      "Web",
			Path:      a.path,
			AuthLevel: a.authLevel,
			Method:    a.method,
			Memo:      a.memo,
		}
		if a.inType != nil {
			api.In = getType(a.inType)
		}
		if a.funcType.NumOut() > 0 {
			api.Out = getType(a.funcType.Out(0))
		}
		out = append(out, api)
	}
	webServicesLock.RUnlock()

	// 4. WebSocket Services
	websocketServicesLock.RLock()
	for _, a := range websocketServices {
		api := Api{
			Type:      "WebSocket",
			Path:      a.path,
			AuthLevel: a.authLevel,
			Memo:      a.memo,
		}
		if a.openFuncType != nil && a.openFuncType.NumIn() > 0 {
			// Find struct in
			for i := 0; i < a.openFuncType.NumIn(); i++ {
				t := a.openFuncType.In(i)
				if t.Kind() == reflect.Struct {
					api.In = getType(t)
					break
				}
			}
		}
		out = append(out, api)

		for name, act := range a.actions {
			actionApi := Api{
				Type:      "Action",
				Path:      name,
				AuthLevel: act.authLevel,
				Memo:      act.memo,
			}
			if act.inType != nil {
				actionApi.In = getType(act.inType)
			}
			if act.funcType.NumOut() > 0 {
				actionApi.Out = getType(act.funcType.Out(0))
			}
			out = append(out, actionApi)
		}
	}
	websocketServicesLock.RUnlock()

	return out
}

// MakeJsonDocument 生成 JSON 格式文档
func MakeJsonDocument() string {
	apis := MakeDocument()
	data, _ := json.MarshalIndent(map[string]any{
		"api": apis,
	}, "", "\t")
	return string(data)
}

func getType(t reflect.Type) any {
	if t == nil {
		return ""
	}
	for t.Kind() == reflect.Ptr {
		t = t.Elem()
	}

	switch t.Kind() {
	case reflect.Struct:
		outs := Map{}
		for i := 0; i < t.NumField(); i++ {
			f := t.Field(i)
			if f.Anonymous {
				if subMap, ok := getType(f.Type).(Map); ok {
					for k, v := range subMap {
						outs[k] = v
					}
				}
			} else {
				outs[cast.GetLowerName(f.Name)] = getType(f.Type)
			}
		}
		return outs
	case reflect.Map:
		return map[string]any{t.Key().String(): getType(t.Elem())}
	case reflect.Slice:
		return []any{getType(t.Elem())}
	case reflect.Interface:
		return "Any"
	default:
		return t.String()
	}
}

// 自动注册文档服务
func init() {
	Register(0, "/__DOC__", func() string {
		return MakeJsonDocument()
	}, "API Document")
}
