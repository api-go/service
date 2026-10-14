package service_test

import (
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"apigo.cc/go/service"
)

func TestProxyInvalidWebSocketHandshakeDoesNotContactUpstream(t *testing.T) {
	var called atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { called.Add(1); w.WriteHeader(403) }))
	defer upstream.Close()
	ws := service.NewWebServer()
	ws.Host("*").Proxy(0, "/", upstream.URL+"/")
	for _, test := range []struct {
		method, version, key string
		status               int
	}{{"POST", "13", "dGhlIHNhbXBsZSBub25jZQ==", 405}, {"GET", "12", "dGhlIHNhbXBsZSBub25jZQ==", 426}, {"GET", "13", "invalid", 400}, {"GET", "13", "dGhlIHNhbXBsZSBub25jZQ", 400}} {
		request := httptest.NewRequest(test.method, "http://entry.test/", nil)
		request.Header.Set("Connection", "Upgrade")
		request.Header.Set("Upgrade", "websocket")
		request.Header.Set("Sec-WebSocket-Version", test.version)
		request.Header.Set("Sec-WebSocket-Key", test.key)
		response := httptest.NewRecorder()
		ws.Handler().ServeHTTP(response, request)
		if response.Code != test.status {
			t.Fatalf("status=%d want=%d", response.Code, test.status)
		}
	}
	if called.Load() != 0 {
		t.Fatalf("upstream calls=%d", called.Load())
	}
}
