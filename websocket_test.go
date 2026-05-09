package service

import (
	"github.com/gorilla/websocket"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestWebSocketService(t *testing.T) {
	// 注册 WebSocket 服务
	Host("*").WebSocket("/ws", func(conn *websocket.Conn) {
		for {
			var msg map[string]any
			if err := conn.ReadJSON(&msg); err != nil {
				break
			}
			_ = conn.WriteJSON(map[string]any{"reply": msg["msg"]})
		}
	}).Auth(0).Memo("test websocket")

	// 启动测试服务器
	server := httptest.NewServer(&RouteHandler{})
	defer server.Close()

	// 建立连接
	wsURL := "ws" + strings.TrimPrefix(server.URL, "http") + "/ws"
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("Dial failed: %v", err)
	}
	defer conn.Close()

	// 发送消息
	msg := map[string]any{"action": "echo", "msg": "hello"}
	if err := conn.WriteJSON(msg); err != nil {
		t.Fatalf("WriteJSON failed: %v", err)
	}

	// 接收响应
	var reply map[string]any
	if err := conn.ReadJSON(&reply); err != nil {
		t.Fatalf("ReadJSON failed: %v", err)
	}

	if reply["reply"] != "hello" {
		t.Errorf("Reply mismatch: %v", reply)
	}
}
