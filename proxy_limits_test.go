package service_test

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	gohttp "github.com/api-go/http"
	"github.com/api-go/service"
	"github.com/gorilla/websocket"
)

func TestProxyUsesHTTP2UpstreamAndWSSWithPrivateCA(t *testing.T) {
	// The server advertises extended CONNECT only when enabled at process startup.
	// Otherwise a WSS client must retain its HTTP/1.1 compatibility path.
	wantWebSocketProtocol := 1
	if strings.Contains(os.Getenv("GODEBUG"), "http2xconnect=1") {
		wantWebSocketProtocol = 2
	}
	upstream := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !gohttp.IsWebSocketRequest(r) {
			if r.ProtoMajor != 2 {
				t.Errorf("ordinary upstream: %s", r.Proto)
			}
			_, _ = io.WriteString(w, "h2")
			return
		}
		if r.ProtoMajor != wantWebSocketProtocol {
			t.Errorf("WS upstream: %s, want HTTP/%d", r.Proto, wantWebSocketProtocol)
		}
		upgrader := websocket.Upgrader{}
		conn, err := gohttp.UpgradeWebSocket(&upgrader, w, r, http.Header{"X-Handshake": {"preserved"}})
		if err != nil {
			t.Error(err)
			return
		}
		defer conn.Close()
		_ = conn.WriteMessage(websocket.TextMessage, []byte("ready"))
		_, _, _ = conn.ReadMessage()
	}))
	upstream.EnableHTTP2 = true
	upstream.StartTLS()
	defer upstream.Close()
	ws := service.NewWebServer()
	client := gohttp.NewClient(time.Second)
	client.GetRawClient().Transport = upstream.Client().Transport
	ws.SetProxyHTTPClient(client)
	ws.Host("*").Proxy(0, "/", upstream.URL+"/")
	gateway := httptest.NewServer(ws.Handler())
	defer gateway.Close()
	response, err := gateway.Client().Get(gateway.URL)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(response.Body)
	response.Body.Close()
	if string(body) != "h2" {
		t.Fatalf("ordinary body=%s", body)
	}
	conn, handshake, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(gateway.URL, "http")+"/", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if handshake.Header.Get("X-Handshake") != "preserved" {
		t.Fatal(handshake.Header)
	}
	_ = conn.SetReadDeadline(time.Now().Add(time.Second))
	_, data, err := conn.ReadMessage()
	if err != nil || string(data) != "ready" {
		t.Fatalf("message=%s %v", data, err)
	}
}

func TestProxyLongRejectionHasConsistentLengthAndNoHopHeaders(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Connection", "X-Internal")
		w.Header().Set("X-Internal", "private")
		w.WriteHeader(http.StatusForbidden)
		_, _ = io.WriteString(w, strings.Repeat("x", 4096))
	}))
	defer upstream.Close()
	ws := service.NewWebServer()
	ws.Host("*").Proxy(0, "/", upstream.URL+"/")
	gateway := httptest.NewServer(ws.Handler())
	defer gateway.Close()
	_, response, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(gateway.URL, "http")+"/", nil)
	if err == nil || response == nil {
		t.Fatalf("handshake=%v %v", response, err)
	}
	defer response.Body.Close()
	body, readErr := io.ReadAll(response.Body)
	if readErr != nil || len(body) != 1024 || response.ContentLength != 1024 || response.Header.Get("X-Internal") != "" {
		t.Fatalf("length=%d response=%v err=%v", len(body), response, readErr)
	}
}

func TestProxyLargeFragmentedMessageAndCompression(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upgrader := websocket.Upgrader{EnableCompression: true}
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()
		conn.EnableWriteCompression(true)
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
	ws.Config.WebSocketProxy.Compression = true
	ws.Host("*").Proxy(0, "/", upstream.URL+"/")
	gateway := httptest.NewServer(ws.Handler())
	defer gateway.Close()
	dialer := websocket.Dialer{EnableCompression: true, WriteBufferSize: 1024, HandshakeTimeout: time.Second}
	conn, response, err := dialer.Dial("ws"+strings.TrimPrefix(gateway.URL, "http")+"/", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if !strings.Contains(response.Header.Get("Sec-WebSocket-Extensions"), "permessage-deflate") {
		t.Fatal(response.Header)
	}
	_ = conn.SetReadDeadline(time.Now().Add(3 * time.Second))
	want := bytes.Repeat([]byte("fragment"), 128*1024)
	writer, err := conn.NextWriter(websocket.BinaryMessage)
	if err != nil {
		t.Fatal(err)
	}
	for offset := 0; offset < len(want); offset += 4096 {
		end := min(offset+4096, len(want))
		if _, err = writer.Write(want[offset:end]); err != nil {
			t.Fatal(err)
		}
	}
	if err = writer.Close(); err != nil {
		t.Fatal(err)
	}
	kind, data, err := conn.ReadMessage()
	if err != nil || kind != websocket.BinaryMessage || !bytes.Equal(data, want) {
		t.Fatalf("echo length=%d err=%v", len(data), err)
	}
}

func TestProxyIdleAndMessageLimits(t *testing.T) {
	for _, name := range []string{"idle", "message"} {
		t.Run(name, func(t *testing.T) {
			done := make(chan struct{})
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				defer close(done)
				upgrader := websocket.Upgrader{}
				conn, err := upgrader.Upgrade(w, r, nil)
				if err != nil {
					return
				}
				defer conn.Close()
				_, _, _ = conn.ReadMessage()
			}))
			defer upstream.Close()
			ws := service.NewWebServer()
			if name == "idle" {
				ws.Config.WebSocketProxy.IdleTimeout = 50
			} else {
				ws.Config.WebSocketProxy.MaxMessageSize = 1024
			}
			ws.Host("*").Proxy(0, "/", upstream.URL+"/")
			gateway := httptest.NewServer(ws.Handler())
			defer gateway.Close()
			conn, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(gateway.URL, "http")+"/", nil)
			if err != nil {
				t.Fatal(err)
			}
			defer conn.Close()
			_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
			code := websocket.CloseGoingAway
			if name == "message" {
				code = websocket.CloseMessageTooBig
				_ = conn.WriteMessage(websocket.TextMessage, bytes.Repeat([]byte("x"), 2048))
			}
			_, _, err = conn.ReadMessage()
			if !websocket.IsCloseError(err, code) {
				t.Fatalf("close=%v want=%d", err, code)
			}
			select {
			case <-done:
			case <-time.After(time.Second):
				t.Fatal("upstream remains open")
			}
		})
	}
}

func TestProxyWriteTimeoutReleasesSlowReceiver(t *testing.T) {
	done := make(chan struct{})
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer close(done)
		upgrader := websocket.Upgrader{}
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()
		writer, err := conn.NextWriter(websocket.BinaryMessage)
		if err != nil {
			return
		}
		chunk := make([]byte, 64*1024)
		for range 512 {
			if _, err := writer.Write(chunk); err != nil {
				return
			}
		}
		_ = writer.Close()
		_, _, _ = conn.ReadMessage()
	}))
	defer upstream.Close()
	ws := service.NewWebServer()
	ws.Config.WebSocketProxy.WriteTimeout = 50
	ws.Host("*").Proxy(0, "/", upstream.URL+"/")
	gateway := httptest.NewServer(ws.Handler())
	defer gateway.Close()
	conn, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(gateway.URL, "http")+"/", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("write timeout did not release upstream")
	}
}

func TestProxyDoesNotShareUpstreamCookieJar(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if len(r.Cookies()) != 1 || r.Cookies()[0].Name != "ingress" {
			t.Errorf("cookies leaked: %v", r.Cookies())
		}
		upgrader := websocket.Upgrader{}
		conn, err := upgrader.Upgrade(w, r, http.Header{"Set-Cookie": {"private=secret; Path=/"}})
		if err == nil {
			conn.Close()
		}
	}))
	defer upstream.Close()
	ws := service.NewWebServer()
	ws.SetProxyHTTPClient(gohttp.NewClient(time.Second))
	ws.Host("*").Proxy(0, "/", upstream.URL+"/")
	gateway := httptest.NewServer(ws.Handler())
	defer gateway.Close()
	for range 2 {
		conn, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(gateway.URL, "http")+"/", http.Header{"Cookie": {"ingress=user"}})
		if err != nil {
			t.Fatal(err)
		}
		conn.Close()
	}
}

func TestProxyUpstreamInitiatedClosePreservesCode(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upgrader := websocket.Upgrader{}
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()
		_ = conn.WriteControl(websocket.CloseMessage, websocket.FormatCloseMessage(1008, "policy"), time.Now().Add(time.Second))
		_, _, _ = conn.ReadMessage()
	}))
	defer upstream.Close()
	ws := service.NewWebServer()
	ws.Host("*").Proxy(0, "/", upstream.URL+"/")
	gateway := httptest.NewServer(ws.Handler())
	defer gateway.Close()
	conn, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(gateway.URL, "http")+"/", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	_ = conn.SetReadDeadline(time.Now().Add(time.Second))
	_, _, err = conn.ReadMessage()
	if !websocket.IsCloseError(err, 1008) {
		t.Fatalf("close=%v", err)
	}
}

func TestProxyCompressedMessageLimitDoesNotDeliverPartialMessage(t *testing.T) {
	var processed atomic.Int32
	done := make(chan struct{})
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer close(done)
		upgrader := websocket.Upgrader{EnableCompression: true}
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()
		if _, _, err = conn.ReadMessage(); err == nil {
			processed.Add(1)
		}
	}))
	defer upstream.Close()
	ws := service.NewWebServer()
	ws.Config.WebSocketProxy.Compression = true
	ws.Config.WebSocketProxy.MaxMessageSize = 1024
	ws.Host("*").Proxy(0, "/", upstream.URL+"/")
	gateway := httptest.NewServer(ws.Handler())
	defer gateway.Close()
	dialer := websocket.Dialer{EnableCompression: true, HandshakeTimeout: time.Second}
	conn, _, err := dialer.Dial("ws"+strings.TrimPrefix(gateway.URL, "http")+"/", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	conn.EnableWriteCompression(true)
	_ = conn.SetReadDeadline(time.Now().Add(time.Second))
	if err = conn.WriteMessage(websocket.BinaryMessage, bytes.Repeat([]byte("x"), 64*1024)); err != nil {
		t.Fatal(err)
	}
	_, _, err = conn.ReadMessage()
	if !websocket.IsCloseError(err, 1009) {
		t.Fatalf("close=%v", err)
	}
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("upstream remains open")
	}
	if processed.Load() != 0 {
		t.Fatal("partial message delivered as complete")
	}
}

func TestProxyDoesNotInventClientSubprotocol(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Sec-WebSocket-Protocol") != "upstream-only" {
			t.Errorf("dialer snapshot was changed: %v", r.Header)
		}
		upgrader := websocket.Upgrader{Subprotocols: []string{"upstream-only"}}
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()
		_, _, _ = conn.ReadMessage()
	}))
	defer upstream.Close()
	ws := service.NewWebServer()
	ws.Host("*").Proxy(0, "/", upstream.URL+"/")
	dialer := *websocket.DefaultDialer
	dialer.Subprotocols = []string{"upstream-only"}
	ws.SetProxyWebSocketDialer(&dialer)
	dialer.Subprotocols[0] = "mutated"
	gateway := httptest.NewServer(ws.Handler())
	defer gateway.Close()
	conn, response, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(gateway.URL, "http")+"/", nil)
	if err == nil {
		conn.Close()
		t.Fatal("unoffered subprotocol accepted")
	}
	if response == nil || response.StatusCode != 502 {
		t.Fatalf("response=%v error=%v", response, err)
	}
	response.Body.Close()
}
