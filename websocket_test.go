package service

import (
	"github.com/gorilla/websocket"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestWebSocketService(t *testing.T) {
	// 注册 WebSocket 服务
	ar := RegisterWebsocket(0, "/ws", nil, nil, "test websocket")
	ar.RegisterAction(0, "echo", func(in struct{ Msg string }) Map {
		return Map{"action": "echo", "reply": in.Msg}
	}, "echo action")

	// 启动测试服务器
	server := httptest.NewServer(&routeHandler{})
	defer server.Close()

	// 建立连接
	wsURL := "ws" + strings.TrimPrefix(server.URL, "http") + "/ws"
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("Dial failed: %v", err)
	}
	defer conn.Close()

	// 发送消息
	msg := Map{"action": "echo", "msg": "hello"}
	if err := conn.WriteJSON(msg); err != nil {
		t.Fatalf("WriteJSON failed: %v", err)
	}

	// 接收响应
	var reply Map
	if err := conn.ReadJSON(&reply); err != nil {
		t.Fatalf("ReadJSON failed: %v", err)
	}

	if reply["reply"] != "hello" {
		t.Errorf("Reply mismatch: %v", reply)
	}
}
