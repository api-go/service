package service

import (
	"apigo.cc/go/cast"
	"apigo.cc/go/log"
	"github.com/gorilla/websocket"
	"net/http"
	"reflect"
	"regexp"
)

// websocketServiceType WebSocket 服务元数据
type websocketServiceType struct {
	authLevel             int
	path                  string
	pathMatcher           *regexp.Regexp
	pathArgs              []string
	updater               *websocket.Upgrader
	openFuncValue         reflect.Value
	openFuncType          reflect.Type
	closeFuncValue        reflect.Value
	closeFuncType         reflect.Type
	sessionType           reflect.Type
	actions               map[string]*websocketActionType
	isSimple              bool
	options               WebServiceOptions
	memo                  string
}

// websocketActionType WebSocket Action 元数据
type websocketActionType struct {
	authLevel int
	funcValue reflect.Value
	funcType  reflect.Type
	inType    reflect.Type
	memo      string
}

// ActionRegister WebSocket Action 注册器
type ActionRegister struct {
	ws *websocketServiceType
}

// RegisterWebsocket 注册 WebSocket 服务
func RegisterWebsocket(authLevel int, path string, onOpen, onClose any, memo string) *ActionRegister {
	s := &websocketServiceType{
		authLevel: authLevel,
		path:      path,
		memo:      memo,
		updater:   &websocket.Upgrader{CheckOrigin: func(r *http.Request) bool { return true }},
		actions:   make(map[string]*websocketActionType),
	}

	if onOpen != nil {
		s.openFuncValue = reflect.ValueOf(onOpen)
		s.openFuncType = s.openFuncValue.Type()
		if s.openFuncType.NumOut() > 0 {
			s.sessionType = s.openFuncType.Out(0)
		}
	}

	if onClose != nil {
		s.closeFuncValue = reflect.ValueOf(onClose)
		s.closeFuncType = s.closeFuncValue.Type()
	}

	websocketServicesLock.Lock()
	websocketServices[path] = s
	websocketServicesLock.Unlock()

	return &ActionRegister{ws: s}
}

// RegisterAction 注册 WebSocket Action
func (ar *ActionRegister) RegisterAction(authLevel int, name string, action any, memo string) {
	v := reflect.ValueOf(action)
	t := v.Type()
	a := &websocketActionType{
		authLevel: authLevel,
		funcValue: v,
		funcType:  t,
		memo:      memo,
	}

	// 查找输入参数类型
	for i := 0; i < t.NumIn(); i++ {
		inT := t.In(i)
		if inT.Kind() == reflect.Struct {
			a.inType = inT
			break
		}
	}

	ar.ws.actions[name] = a
}

func doWebsocketService(ws *websocketServiceType, request *Request, response *Response, logger *log.Logger) {
	conn, err := ws.updater.Upgrade(response.Writer, request.Request, nil)
	if err != nil {
		logger.Error("websocket upgrade failed", "error", err.Error())
		return
	}
	defer conn.Close()

	var session any
	if ws.openFuncValue.IsValid() {
		// 简化版：仅支持基础参数注入
		params := make([]reflect.Value, ws.openFuncType.NumIn())
		for i := 0; i < len(params); i++ {
			t := ws.openFuncType.In(i)
			if t == reflect.TypeOf(request) {
				params[i] = reflect.ValueOf(request)
			} else if t == reflect.TypeOf(logger) {
				params[i] = reflect.ValueOf(logger)
			} else {
				params[i] = reflect.New(t).Elem()
			}
		}
		outs := ws.openFuncValue.Call(params)
		if len(outs) > 0 {
			session = outs[0].Interface()
		}
	}

	for {
		var msg Map
		if err := conn.ReadJSON(&msg); err != nil {
			break
		}

		actionName := cast.String(msg["action"])
		action := ws.actions[actionName]
		if action == nil {
			action = ws.actions[""] // 默认 action
		}

		if action != nil {
			params := make([]reflect.Value, action.funcType.NumIn())
			for i := 0; i < len(params); i++ {
				t := action.funcType.In(i)
				if t == ws.sessionType {
					params[i] = reflect.ValueOf(session)
				} else if t == reflect.TypeOf(conn) {
					params[i] = reflect.ValueOf(conn)
				} else if t.Kind() == reflect.Struct {
					in := reflect.New(t).Interface()
					cast.Convert(in, msg)
					params[i] = reflect.ValueOf(in).Elem()
				} else {
					params[i] = reflect.New(t).Elem()
				}
			}
			outs := action.funcValue.Call(params)
			if len(outs) > 0 {
				result := outs[0].Interface()
				if result != nil {
					_ = conn.WriteJSON(result)
				}
			}
		}
	}

	if ws.closeFuncValue.IsValid() {
		params := make([]reflect.Value, ws.closeFuncType.NumIn())
		for i := 0; i < len(params); i++ {
			t := ws.closeFuncType.In(i)
			if t == ws.sessionType {
				params[i] = reflect.ValueOf(session)
			} else {
				params[i] = reflect.New(t).Elem()
			}
		}
		ws.closeFuncValue.Call(params)
	}
}
