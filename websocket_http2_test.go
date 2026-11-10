package service_test

import (
	gohttp "github.com/api-go/http"
	"github.com/api-go/service"
	"context"
	"github.com/gorilla/websocket"
	"golang.org/x/net/http2"
	"golang.org/x/net/http2/h2c"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"
)

func TestWebSocketHTTP2ProxyMatrix(t *testing.T) {
	if !strings.Contains(os.Getenv("GODEBUG"), "http2xconnect=1") {
		t.Skip("requires startup GODEBUG=http2xconnect=1")
	}
	for _, incoming := range []bool{false, true} {
		for _, outgoing := range []bool{false, true} {
			name := "h1"
			if incoming {
				name = "h2"
			}
			if outgoing {
				name += "-h2"
			} else {
				name += "-h1"
			}
			t.Run(name, func(t *testing.T) {
				upstream := httptest.NewServer(h2c.NewHandler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if (r.ProtoMajor == 2) != outgoing || r.Host != "public.test" || r.URL.RequestURI() != "/socket?q=1" || r.Header.Get("Authorization") != "Bearer test" {
						t.Errorf("upstream request: %s %s %s %v", r.Proto, r.Host, r.URL, r.Header)
					}
					upgrader := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }, Subprotocols: []string{"chat"}, EnableCompression: true}
					conn, err := gohttp.UpgradeWebSocket(&upgrader, w, r, http.Header{"Set-Cookie": {"one=1", "two=2"}})
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
						if conn.WriteMessage(kind, data) != nil {
							return
						}
					}
				}), &http2.Server{}))
				defer upstream.Close()
				server := service.NewWebServer()
				server.SetRequestGate(func(request *service.Request, response *service.Response) bool {
					if request.Request.Header.Get("Authorization") == "Bearer test" {
						return false
					}
					response.Writer.Header().Set("WWW-Authenticate", "Bearer")
					response.WriteHeader(http.StatusUnauthorized)
					response.WriteString("denied")
					return true
				})
				server.Config.WebSocketProxy.Compression = true
				if outgoing {
					server.SetProxyHTTPClient(gohttp.NewClientH2C(time.Second))
				}
				server.Host("*").Proxy(0, "/*", upstream.URL+"/")
				ingress := httptest.NewServer(h2c.NewHandler(server.Handler(), &http2.Server{}))
				defer ingress.Close()
				client := gohttp.NewClientH2C(time.Second)
				client.SetWebSocketHTTP2(incoming)
				_, rejected, rejectErr := client.Open("ws"+ingress.URL[4:]+"/socket?q=1", http.Header{"Host": {"public.test"}})
				if rejectErr == nil || rejected == nil || rejected.StatusCode != http.StatusUnauthorized {
					t.Fatalf("authentication rejection: %v %+v", rejectErr, rejected)
				}
				rejected.Body.Close()
				conn, response, err := client.Open("ws"+ingress.URL[4:]+"/socket?q=1", http.Header{"Host": {"public.test"}, "Authorization": {"Bearer test"}, "Sec-WebSocket-Protocol": {"chat"}})
				if err != nil {
					t.Fatalf("handshake: %v %+v", err, response)
				}
				defer conn.Close()
				if incoming && response.StatusCode != 200 || !incoming && response.StatusCode != 101 || len(response.Header.Values("Set-Cookie")) < 2 {
					t.Fatalf("response: %+v", response)
				}
				for _, kind := range []int{websocket.TextMessage, websocket.BinaryMessage} {
					if err := conn.Conn.WriteMessage(kind, []byte("echo")); err != nil {
						t.Fatal(err)
					}
					conn.SetReadDeadline(time.Now().Add(time.Second))
					got, data, err := conn.Conn.ReadMessage()
					if err != nil || got != kind || string(data) != "echo" {
						t.Fatalf("echo: %d %s %v", got, data, err)
					}
				}
				conn.Conn.WriteControl(websocket.CloseMessage, websocket.FormatCloseMessage(1000, "done"), time.Now().Add(time.Second))
				_, _, err = conn.Conn.ReadMessage()
				if !websocket.IsCloseError(err, 1000) {
					t.Fatalf("close: %v", err)
				}
			})
		}
	}
}

func TestWebSocketHTTP2Service(t *testing.T) {
	if !strings.Contains(os.Getenv("GODEBUG"), "http2xconnect=1") {
		t.Skip("requires startup GODEBUG=http2xconnect=1")
	}
	server := service.NewWebServer()
	server.Host("*").WebSocket("/socket", func(conn *service.WebSocketConn) {
		message, err := conn.ReadString()
		if err == nil {
			conn.Send(message)
		}
	}).Auth(0)
	ingress := httptest.NewServer(h2c.NewHandler(server.Handler(), &http2.Server{}))
	defer ingress.Close()
	client := gohttp.NewClientH2C(time.Second)
	client.SetWebSocketHTTP2(true)
	conn, _, err := client.Open("ws" + ingress.URL[4:] + "/socket")
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	conn.Send("native")
	message, err := conn.ReadString()
	if err != nil || message != "native" {
		t.Fatalf("echo: %s %v", message, err)
	}
}

func TestWebSocketHTTP2ServiceShutdown(t *testing.T) {
	if !strings.Contains(os.Getenv("GODEBUG"), "http2xconnect=1") {
		t.Skip("requires startup GODEBUG=http2xconnect=1")
	}
	server := service.NewWebServer()
	server.Config.Listen = "127.0.0.1:0,h2c"
	finished := make(chan struct{})
	server.RegisterWebsocket("/socket", func(conn *service.WebSocketConn) { defer close(finished); conn.ReadString() })
	if err := server.Start(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
	defer server.Stop(context.Background())
	client := gohttp.NewClientH2C(time.Second)
	client.SetWebSocketHTTP2(true)
	defer client.Destroy()
	conn, _, err := client.Open("ws://" + server.Addrs[0] + "/socket")
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	closed := make(chan error, 1)
	go func() { _, _, err := conn.Conn.ReadMessage(); closed <- err }()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := server.Stop(ctx); err != nil {
		t.Fatal(err)
	}
	select {
	case <-finished:
	case <-ctx.Done():
		t.Fatal("handler did not exit")
	}
	select {
	case err := <-closed:
		if !websocket.IsCloseError(err, websocket.CloseGoingAway) {
			t.Fatalf("close: %v", err)
		}
	case <-ctx.Done():
		t.Fatal("client did not close")
	}
}

func TestProxyDefaultClientKeepsUserCookiesIsolated(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Cookie") != "" {
			t.Errorf("unexpected shared cookie: %s", r.Header.Get("Cookie"))
		}
		http.SetCookie(w, &http.Cookie{Name: "upstream-user", Value: "one", Path: "/"})
		w.Write([]byte("ok"))
	}))
	defer upstream.Close()
	server := service.NewWebServer()
	defer server.Stop(context.Background())
	server.Host("*").Proxy(0, "/*", upstream.URL+"/")
	ingress := httptest.NewServer(server.Handler())
	defer ingress.Close()
	client := &http.Client{Timeout: time.Second}
	for i := 0; i < 2; i++ {
		response, err := client.Get(ingress.URL + "/ordinary")
		if err != nil {
			t.Fatal(err)
		}
		io.Copy(io.Discard, response.Body)
		response.Body.Close()
		if response.StatusCode != 200 {
			t.Fatal(response.StatusCode)
		}
	}
}
