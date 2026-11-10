package service_test

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/api-go/service"
	"github.com/gorilla/websocket"
)

func TestProxyHTTP2AndWebSocketShareTLSListener(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !websocket.IsWebSocketUpgrade(r) {
			_, _ = io.WriteString(w, "ordinary")
			return
		}
		upgrader := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Error(err)
			return
		}
		defer conn.Close()
		kind, data, err := conn.ReadMessage()
		if err == nil {
			_ = conn.WriteMessage(kind, data)
		}
	}))
	defer upstream.Close()
	ws := service.NewWebServer()
	ws.Host("*").Proxy(0, "/", upstream.URL+"/")
	gateway := httptest.NewUnstartedServer(ws.Handler())
	gateway.EnableHTTP2 = true
	gateway.StartTLS()
	defer gateway.Close()
	res, err := gateway.Client().Get(gateway.URL)
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.ProtoMajor != 2 {
		t.Fatalf("ordinary protocol: %s", res.Proto)
	}
	tlsConfig := gateway.Client().Transport.(*http.Transport).TLSClientConfig.Clone()
	tlsConfig.NextProtos = []string{"http/1.1"}
	dialer := websocket.Dialer{TLSClientConfig: tlsConfig, HandshakeTimeout: time.Second}
	conn, _, err := dialer.Dial("wss"+strings.TrimPrefix(gateway.URL, "https")+"/", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	_ = conn.SetReadDeadline(time.Now().Add(time.Second))
	if err := conn.WriteMessage(websocket.TextMessage, []byte("hello")); err != nil {
		t.Fatal(err)
	}
	_, data, err := conn.ReadMessage()
	if err != nil || string(data) != "hello" {
		t.Fatalf("echo: %q %v", data, err)
	}
}

func TestProxyWebSocketStopClosesBothSides(t *testing.T) {
	done := make(chan struct{})
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer close(done)
		upgrader := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Error(err)
			return
		}
		defer conn.Close()
		_, _, _ = conn.ReadMessage()
	}))
	defer upstream.Close()
	ws := service.NewWebServer()
	ws.Config.Listen = "127.0.0.1:0,http"
	ws.Host("*").Proxy(0, "/", upstream.URL+"/")
	if err := ws.Start(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
	defer ws.Stop(context.Background())
	conn, _, err := websocket.DefaultDialer.Dial("ws://"+ws.Addrs[0]+"/", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	result := make(chan error, 1)
	go func() { _, _, err := conn.ReadMessage(); result <- err }()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := ws.Stop(ctx); err != nil {
		t.Fatal(err)
	}
	if err := <-result; !websocket.IsCloseError(err, websocket.CloseGoingAway) {
		t.Fatalf("close: %v", err)
	}
	select {
	case <-done:
	case <-ctx.Done():
		t.Fatal("upstream remains open")
	}
}
