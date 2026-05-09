package service

import (
	"apigo.cc/go/log"
	"github.com/gorilla/websocket"
	"net/http"
	"reflect"
)

var defaultUpgrader = &websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true },
}

func doWebsocketService(ws *websocketServiceType, request *Request, response *Response, logger *log.Logger) {
	conn, err := defaultUpgrader.Upgrade(response.Writer, request.Request, nil)
	if err != nil {
		logger.Error("websocket upgrade failed", "error", err.Error())
		return
	}
	defer conn.Close()

	// 调用业务处理函数，注入依赖
	params := make([]reflect.Value, ws.funcType.NumIn())
	for i := 0; i < len(params); i++ {
		t := ws.funcType.In(i)
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
	ws.funcValue.Call(params)
}
