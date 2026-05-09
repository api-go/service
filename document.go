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
	Host      string
}

//go:embed DocTpl.html
var defaultDocTpl string

// MakeDocument 生成文档数据
func MakeDocument() []Api {
	out := make([]Api, 0)

	// 1. Rewrite & Proxy
	hostPoliciesLock.RLock()
	for host, rewrites := range hostRewrites {
		for _, a := range rewrites {
			out = append(out, Api{
				Type: "Rewrite",
				Host: host,
				Path: a.fromPath + " -> " + a.toPath,
			})
		}
	}
	for host, proxies := range hostProxies {
		for _, a := range proxies {
			out = append(out, Api{
				Type: "Proxy",
				Host: host,
				Path: a.fromPath + " -> " + a.toApp + ":" + a.toPath,
			})
		}
	}
	hostPoliciesLock.RUnlock()

	// 2. Web Services
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
			Host:      a.host,
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
	for _, ws := range websocketServicesList {
		api := Api{
			Type:      "WebSocket",
			Path:      ws.path,
			AuthLevel: ws.authLevel,
			Memo:      ws.memo,
			Host:      ws.host,
		}
		if ws.funcType != nil && ws.funcType.NumIn() > 0 {
			// Find struct in
			for i := 0; i < ws.funcType.NumIn(); i++ {
				t := ws.funcType.In(i)
				if t.Kind() == reflect.Struct {
					api.In = getType(t)
					break
				}
			}
		}
		out = append(out, api)
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
		outs := make(map[string]any)
		for i := 0; i < t.NumField(); i++ {
			f := t.Field(i)
			if f.Anonymous {
				if subMap, ok := getType(f.Type).(map[string]any); ok {
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
