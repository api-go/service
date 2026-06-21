package service

import (
	"apigo.cc/go/jsmod"
	"apigo.cc/go/log"
	"github.com/gorilla/websocket"
	"net/http"
	"reflect"
)

var defaultUpgrader = &websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true },
}

// WebSocketConn 包装标准的 websocket.Conn，提供更友好的接口
type WebSocketConn struct {
	Conn *websocket.Conn
}

// Send 发送消息，支持 string, []byte 或 自动转 JSON
func (c *WebSocketConn) Send(data any) error {
	var err error
	switch v := data.(type) {
	case string:
		err = c.Conn.WriteMessage(websocket.TextMessage, []byte(v))
	case []byte:
		err = c.Conn.WriteMessage(websocket.BinaryMessage, v)
	default:
		err = c.Conn.WriteJSON(v)
	}
	if err != nil {
		return jsmod.MakeError(err)
	}
	return nil
}

// ReadString 读取字符串消息
func (c *WebSocketConn) ReadString() (string, error) {
	_, b, err := c.Conn.ReadMessage()
	if err != nil {
		return "", jsmod.MakeError(err)
	}
	return string(b), nil
}

// ReadBytes 读取二进制消息
func (c *WebSocketConn) ReadBytes() ([]byte, error) {
	_, b, err := c.Conn.ReadMessage()
	if err != nil {
		return nil, jsmod.MakeError(err)
	}
	return b, nil
}

// ReadJSON 读取 JSON 消息
func (c *WebSocketConn) ReadJSON(v any) error {
	if err := c.Conn.ReadJSON(v); err != nil {
		return jsmod.MakeError(err)
	}
	return nil
}

// Close 关闭连接
func (c *WebSocketConn) Close() error {
	if err := c.Conn.Close(); err != nil {
		return jsmod.MakeError(err)
	}
	return nil
}

// Upgrade 将 HTTP 请求升级为 WebSocket 连接
func Upgrade(response *Response, request *Request) (*WebSocketConn, error) {
	conn, err := defaultUpgrader.Upgrade(response.Writer, request.Request, nil)
	if err != nil {
		return nil, jsmod.MakeError(err)
	}
	return &WebSocketConn{Conn: conn}, nil
}

func (ws *WebServer) doWebsocketService(wsc *websocketServiceType, request *Request, response *Response, logger *log.Logger, object any) {
	wsConn, err := Upgrade(response, request)
	if err != nil {
		logger.Error("websocket upgrade failed", "error", err.Error())
		return
	}
	defer wsConn.Close()

	// 调用业务处理函数，注入依赖
	params := make([]reflect.Value, wsc.funcType.NumIn())
	for i := 0; i < len(params); i++ {
		t := wsc.funcType.In(i)
		if t == reflect.TypeOf(request) {
			params[i] = reflect.ValueOf(request)
		} else if t == reflect.TypeOf(logger) {
			params[i] = reflect.ValueOf(logger)
		} else if t == reflect.TypeOf(wsConn) {
			params[i] = reflect.ValueOf(wsConn)
		} else if t == reflect.TypeOf(wsConn.Conn) {
			params[i] = reflect.ValueOf(wsConn.Conn)
		} else if object != nil && reflect.TypeOf(object).AssignableTo(t) {
			params[i] = reflect.ValueOf(object)
		} else if obj := ws.GetInject(t); obj != nil {
			params[i] = reflect.ValueOf(obj)
		} else {
			params[i] = reflect.New(t).Elem()
		}
	}
	wsc.funcValue.Call(params)
}
