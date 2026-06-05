package service

import (
	"apigo.cc/go/log"
	"apigo.cc/go/watch"
	"github.com/gorilla/websocket"
	"net/http/httptest"
	"os"
	"path/filepath"
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
	server := httptest.NewServer(&RouteHandler{ws: DefaultServer})
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

func TestEnableWebDev(t *testing.T) {
	// 1. 初始化 EnableWebDev
	EnableWebDev(watch.Config{
		Paths: []string{"."},
	})

	// 必须手动调用 initWebDev 或触发 Start，因为现在的逻辑是延迟初始化的
	DefaultServer.initWebDev(log.DefaultLogger)

	// 2. 准备一个真实的静态 HTML 文件
	staticDir := "test_static"
	_ = os.MkdirAll(staticDir, 0755)
	htmlFile := filepath.Join(staticDir, "index.html")
	_ = os.WriteFile(htmlFile, []byte("<html><head></head><body>Static Content</body></html>"), 0644)
	defer os.RemoveAll(staticDir)

	// 注册静态服务
	Static("/static/", staticDir)

	handler := &RouteHandler{ws: DefaultServer}

	// 3. 测试静态文件注入
	req := httptest.NewRequest("GET", "/static/index.html", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	body := w.Body.String()
	if !strings.Contains(body, "let _watchWS = null") {
		t.Errorf("Static HTML injection failed, code not found in body: %s", body)
	}

	// 4. 测试普通服务注入
	Register("GET", "/test-dev", func() string {
		return "<html><head></head><body>Hello</body></html>"
	})

	req2 := httptest.NewRequest("GET", "/test-dev", nil)
	w2 := httptest.NewRecorder()
	handler.ServeHTTP(w2, req2)

	body2 := w2.Body.String()
	if !strings.Contains(body2, "let _watchWS = null") {
		t.Errorf("Dynamic HTML injection failed")
	}

	// 5. 验证非 HTML 不注入
	Register("GET", "/test-json", func() map[string]string {
		return map[string]string{"foo": "bar"}
	})
	req3 := httptest.NewRequest("GET", "/test-json", nil)
	w3 := httptest.NewRecorder()
	handler.ServeHTTP(w3, req3)

	body3 := w3.Body.String()
	if strings.Contains(body3, "let _watchWS = null") {
		t.Errorf("JSON should not be injected")
	}
}

