package service_test

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"apigo.cc/go/service"
	"github.com/gorilla/websocket"
)

func BenchmarkProxyWebSocket(b *testing.B) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upgrader := websocket.Upgrader{}
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()
		for {
			kind, data, err := conn.ReadMessage()
			if err != nil {
				return
			}
			if conn.WriteMessage(kind, data) != nil {
				return
			}
		}
	}))
	defer upstream.Close()
	ws := service.NewWebServer()
	ws.Config.NoLogGets = true
	ws.Host("*").Proxy(0, "/", upstream.URL+"/")
	gateway := httptest.NewServer(ws.Handler())
	defer gateway.Close()
	conn, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(gateway.URL, "http")+"/", nil)
	if err != nil {
		b.Fatal(err)
	}
	defer conn.Close()
	payload := bytes.Repeat([]byte("x"), 1024)
	b.ReportAllocs()
	b.SetBytes(int64(len(payload)))
	b.ResetTimer()
	for b.Loop() {
		if err := conn.WriteMessage(websocket.BinaryMessage, payload); err != nil {
			b.Fatal(err)
		}
		_, data, err := conn.ReadMessage()
		if err != nil || len(data) != len(payload) {
			b.Fatalf("reply=%d err=%v", len(data), err)
		}
	}
}
