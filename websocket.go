package service

import (
	"apigo.cc/go/log"
	"github.com/gorilla/websocket"
	"net/http"
	"reflect"
)

// websocketServiceType WebSocket 服务元数据
type websocketServiceType struct {
	authLevel    int
	path         string
	updater      *websocket.Upgrader
	handlerValue reflect.Value
	handlerType  reflect.Type
	memo         string
}

// RegisterWebsocket 注册 WebSocket 服务
func RegisterWebsocket(authLevel int, path string, handler any, memo string) {
	v := reflect.ValueOf(handler)
	t := v.Type()
	if t.Kind() != reflect.Func {
		return
	}

	s := &websocketServiceType{
		authLevel:    authLevel,
		path:         path,
		memo:         memo,
		updater:      &websocket.Upgrader{CheckOrigin: func(r *http.Request) bool { return true }},
		handlerValue: v,
		handlerType:  t,
	}

	websocketServicesLock.Lock()
	websocketServices[path] = s
	websocketServicesLock.Unlock()
}

func doWebsocketService(ws *websocketServiceType, request *Request, response *Response, logger *log.Logger) {
	conn, err := ws.updater.Upgrade(response.Writer, request.Request, nil)
	if err != nil {
		logger.Error("websocket upgrade failed", "error", err.Error())
		return
	}
	defer conn.Close()

	// 调用业务处理函数，注入依赖
	params := make([]reflect.Value, ws.handlerType.NumIn())
	for i := 0; i < len(params); i++ {
		t := ws.handlerType.In(i)
		if t == reflect.TypeOf(request) {
			params[i] = reflect.ValueOf(request)
		} else if t == reflect.TypeOf(logger) {
			params[i] = reflect.ValueOf(logger)
		} else if t == reflect.TypeOf(conn) {
			params[i] = reflect.ValueOf(conn)
		} else if obj := GetInject(t); obj != nil {
			params[i] = reflect.ValueOf(obj)
		} else {
			params[i] = reflect.New(t).Elem()
		}
	}
	ws.handlerValue.Call(params)
}
