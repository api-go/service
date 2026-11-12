package service_test

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/api-go/discover"
	"github.com/api-go/log"
	"github.com/api-go/service"
	"github.com/gorilla/websocket"
	"golang.org/x/net/http2"
	"golang.org/x/net/http2/h2c"
)

func TestProxyWebSocket(t *testing.T) {
	for _, compact := range []bool{false, true} {
		for _, discovery := range []bool{false, true} {
			name := "direct"
			if discovery {
				name = "discover"
			}
			if compact {
				name += "-compact"
			}
			t.Run(name, func(t *testing.T) {
				upstreamDone := make(chan struct{})
				upstream := httptest.NewServer(h2c.NewHandler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if !websocket.IsWebSocketUpgrade(r) {
						if r.ProtoMajor != 2 {
							t.Errorf("ordinary request protocol: %s", r.Proto)
						}
						_, _ = io.WriteString(w, "h2")
						return
					}
					defer close(upstreamDone)
					if r.ProtoMajor != 1 || r.URL.RequestURI() != "/socket?x=1" || r.Host != "public.example" {
						t.Errorf("handshake: %s %s %s", r.Proto, r.URL, r.Host)
					}
					for key, want := range map[string]string{"Origin": "https://public.example", "Cookie": "session=test", "Authorization": "Bearer test"} {
						if r.Header.Get(key) != want {
							t.Errorf("%s = %q", key, r.Header.Get(key))
						}
					}
					upgrader := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }, Subprotocols: []string{"chat"}}
					conn, err := upgrader.Upgrade(w, r, http.Header{"Set-Cookie": {"upstream=one; Path=/", "second=two; Path=/"}})
					if err != nil {
						t.Error(err)
						return
					}
					defer conn.Close()
					for {
						kind, data, err := conn.ReadMessage()
						if err != nil {
							return
						}
						if err := conn.WriteMessage(kind, data); err != nil {
							return
						}
					}
				}), &http2.Server{}))
				defer upstream.Close()
				ws := service.NewWebServer()
				ws.Config.CompactProxyLog = compact
				target := upstream.URL
				if discovery {
					app := "service-proxy-websocket-test"
					ws.Config.Listen = "127.0.0.1:0,h2c"
					ws.Config.Register = "redis://127.0.0.1:6379/15"
					ws.Config.App = "service-proxy-websocket-ingress-test"
					if err := ws.Start(context.Background(), nil); err != nil {
						t.Fatal(err)
					}
					defer ws.Stop(context.Background())
					discover.AddExternalApp(app, "2")
					addr := strings.TrimPrefix(upstream.URL, "http://")
					registerDiscoverFixture(t, ws.Config.Register, app, addr)
					target = app
				}
				ws.SetAuthChecker(func(_ int, _ *log.Logger, _ *string, _ map[string]any, request *service.Request, _ *service.Response, _ *service.WebServiceOptions) (bool, any) {
					return request.Request.Header.Get("Authorization") == "Bearer test", nil
				})
				ws.Host("*").Proxy(1, "/entry", target+"/socket")
				gateway := httptest.NewServer(ws.Handler())
				defer gateway.Close()
				dialer := websocket.Dialer{HandshakeTimeout: time.Second, Subprotocols: []string{"chat"}}
				url := "ws" + strings.TrimPrefix(gateway.URL, "http") + "/entry?x=1"
				if conn, res, err := dialer.Dial(url, nil); err == nil {
					conn.Close()
					t.Fatal("unauthenticated connection accepted")
				} else if res == nil || res.StatusCode != 403 {
					t.Fatalf("auth response: %v %v", res, err)
				} else {
					res.Body.Close()
				}
				conn, res, err := dialer.Dial(url, http.Header{"Host": {"public.example"}, "Origin": {"https://public.example"}, "Cookie": {"session=test"}, "Authorization": {"Bearer test"}})
				if err != nil {
					t.Fatal(err)
				}
				defer conn.Close()
				if conn.Subprotocol() != "chat" {
					t.Fatal("subprotocol lost")
				}
				cookies := strings.Join(res.Header.Values("Set-Cookie"), "\n")
				if !strings.Contains(cookies, "upstream=one; Path=/") || !strings.Contains(cookies, "second=two; Path=/") {
					t.Fatal("upstream cookies lost")
				}
				_ = conn.SetReadDeadline(time.Now().Add(3 * time.Second))
				pong := make(chan string, 1)
				conn.SetPongHandler(func(data string) error { pong <- data; return nil })
				if err := conn.WriteControl(websocket.PingMessage, []byte("probe"), time.Now().Add(time.Second)); err != nil {
					t.Fatal(err)
				}
				for _, kind := range []int{websocket.TextMessage, websocket.BinaryMessage} {
					if err := conn.WriteMessage(kind, []byte("hello")); err != nil {
						t.Fatal(err)
					}
					gotKind, data, err := conn.ReadMessage()
					if err != nil || gotKind != kind || string(data) != "hello" {
						t.Fatalf("echo: %d %q %v", gotKind, data, err)
					}
				}
				select {
				case data := <-pong:
					if data != "probe" {
						t.Fatalf("pong: %q", data)
					}
				default:
					t.Fatal("pong lost")
				}
				if discovery {
					ordinary, _ := http.NewRequest("GET", gateway.URL+"/entry", nil)
					ordinary.Header.Set("Authorization", "Bearer test")
					result, err := gateway.Client().Do(ordinary)
					if err != nil {
						t.Fatal(err)
					}
					body, _ := io.ReadAll(result.Body)
					result.Body.Close()
					if string(body) != "h2" {
						t.Fatalf("ordinary proxy: %s", body)
					}
				}
				_ = conn.WriteControl(websocket.CloseMessage, websocket.FormatCloseMessage(1000, "done"), time.Now().Add(time.Second))
				_, _, err = conn.ReadMessage()
				if !websocket.IsCloseError(err, 1000) {
					t.Fatalf("close: %v", err)
				}
				select {
				case <-upstreamDone:
				case <-time.After(3 * time.Second):
					t.Fatal("upstream did not close")
				}
			})
		}
	}
}

func TestProxyWebSocketRejectPreservesResponse(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Add("Set-Cookie", "rejected=yes")
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = io.WriteString(w, "denied")
	}))
	defer upstream.Close()
	ws := service.NewWebServer()
	ws.Host("*").Proxy(0, "/", upstream.URL+"/")
	gateway := httptest.NewServer(ws.Handler())
	defer gateway.Close()
	_, res, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(gateway.URL, "http")+"/", nil)
	if err == nil || res == nil {
		t.Fatalf("response: %v %v", res, err)
	}
	defer res.Body.Close()
	body, _ := io.ReadAll(res.Body)
	if res.StatusCode != 401 || string(body) != "denied" || res.Header.Get("Set-Cookie") != "rejected=yes" {
		t.Fatalf("response: %v %q", res, body)
	}
}

func TestDiscoverProxyWebSocketRejectPreservesResponse(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Add("Set-Cookie", "rejected=yes")
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = io.WriteString(w, "denied")
	}))
	defer upstream.Close()
	ws := service.NewWebServer()
	ws.Config.Listen = "127.0.0.1:0,h2c"
	ws.Config.Register = "redis://127.0.0.1:6379/15"
	ws.Config.App = "service-proxy-rejection-ingress-test"
	if err := ws.Start(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
	defer ws.Stop(context.Background())
	app := "service-proxy-rejection-upstream-test"
	discover.AddExternalApp(app, "1")
	addr := strings.TrimPrefix(upstream.URL, "http://")
	registerDiscoverFixture(t, ws.Config.Register, app, addr)
	ws.Host("*").Proxy(0, "/", app+"/")
	gateway := httptest.NewServer(ws.Handler())
	defer gateway.Close()
	_, response, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(gateway.URL, "http")+"/", nil)
	if err == nil || response == nil {
		t.Fatalf("response: %v %v", response, err)
	}
	defer response.Body.Close()
	body, _ := io.ReadAll(response.Body)
	if response.StatusCode != http.StatusUnauthorized || string(body) != "denied" || response.Header.Get("Set-Cookie") != "rejected=yes" {
		t.Fatalf("response: %v %q", response, body)
	}
}
