package service_test

import (
	"apigo.cc/go/service"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestClientIPPolicyTrustBoundary(t *testing.T) {
	cases := []struct{ name, mode, peer, forward, real, id, want string }{
		{"direct ignores forged ID", "direct", "192.0.2.5:3000", "203.0.113.1", "203.0.113.2", "forged", "192.0.2.5"},
		{"untrusted peer", "trusted", "192.0.2.5:3000", "203.0.113.1", "", "forged", "192.0.2.5"},
		{"rightmost untrusted hop", "trusted", "10.0.0.2:3000", "203.0.113.1, 198.51.100.4, 10.0.0.3", "", "", "198.51.100.4"},
		{"invalid hop stops trust", "trusted", "10.0.0.2:3000", "203.0.113.1, invalid", "", "", "10.0.0.2"},
		{"IPv6", "trusted", "[::1]:3000", "2001:db8::9", "", "", "2001:db8::9"},
		{"legacy compatibility", "legacy", "192.0.2.5:3000", "", "203.0.113.2", "relay", "203.0.113.2"},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			ws := service.NewWebServer()
			ws.Config.NoLogGets = true
			if err := ws.SetClientIPPolicy(service.ClientIPPolicy{Mode: test.mode, TrustedProxies: []string{"10.0.0.0/8", "::1/128"}}); err != nil {
				t.Fatal(err)
			}
			ws.SetRequestGate(func(r *service.Request, w *service.Response) bool { w.WriteString(r.ClientIp()); return true })
			request := httptest.NewRequest("GET", "http://example.test/", nil)
			request.RemoteAddr = test.peer
			request.Header.Set("X-Forwarded-For", test.forward)
			request.Header.Set("X-Real-IP", test.real)
			request.Header.Set("X-Request-Id", test.id)
			response := httptest.NewRecorder()
			ws.Handler().ServeHTTP(response, request)
			if response.Body.String() != test.want {
				t.Fatalf("got %q want %q", response.Body.String(), test.want)
			}
		})
	}
	ws := service.NewWebServer()
	if err := ws.SetClientIPPolicy(service.ClientIPPolicy{Mode: "trusted", TrustedProxies: []string{"invalid"}}); err == nil {
		t.Fatal("accepted malformed CIDR")
	}
}

type unreadBody struct{ read bool }

func (b *unreadBody) Read(p []byte) (int, error) { b.read = true; return 0, io.EOF }
func (b *unreadBody) Close() error               { return nil }
func TestGateRunsBeforeProxyRedirectAndBody(t *testing.T) {
	ws := service.NewWebServer()
	ws.Config.NoLogGets = true
	ws.Host("*").Proxy(0, "/upload", "http://127.0.0.1:1/upload")
	ws.ReplaceForceHTTPS("*", true)
	ws.SetRequestGate(func(r *service.Request, w *service.Response) bool { w.WriteHeader(429); return true })
	body := &unreadBody{}
	request := httptest.NewRequest("POST", "http://example.test/upload", nil)
	request.Body = body
	response := httptest.NewRecorder()
	ws.Handler().ServeHTTP(response, request)
	if response.Code != 429 || body.read {
		t.Fatalf("code %d body read %v", response.Code, body.read)
	}
}
func TestManagedSnapshotPinnedAcrossPublish(t *testing.T) {
	ws := service.NewWebServer()
	ws.Config.NoLogGets = true
	old, _ := service.PrepareHostPolicy(service.HostPolicy{Rewrites: []service.RewriteRule{{Path: "/x", To: "https://old.test/"}}})
	next, _ := service.PrepareHostPolicy(service.HostPolicy{Rewrites: []service.RewriteRule{{Path: "/x", To: "https://new.test/"}}})
	ws.ReplaceHostPolicies(map[string]*service.PreparedHostPolicy{"example.test": old})
	ws.SetRequestGate(func(r *service.Request, w *service.Response) bool {
		ws.ReplaceHostPolicies(map[string]*service.PreparedHostPolicy{"example.test": next})
		return false
	})
	response := httptest.NewRecorder()
	ws.Handler().ServeHTTP(response, httptest.NewRequest("GET", "http://example.test/x", nil))
	if response.Header().Get("Location") != "https://old.test/" {
		t.Fatalf("mixed snapshot: %v", response.Header())
	}
}
func TestManagedPolicyValidationAndStaticBoundary(t *testing.T) {
	if _, err := service.PrepareHostPolicy(service.HostPolicy{Proxies: []service.ProxyRule{{Path: "/(", To: "http://example.test/"}}}); err == nil {
		t.Fatal("invalid regex accepted")
	}
	if _, err := service.PrepareHostPolicy(service.HostPolicy{Proxies: []service.ProxyRule{{Path: "/", To: "file:///secret"}}}); err == nil {
		t.Fatal("unsupported scheme accepted")
	}
	ws := service.NewWebServer()
	ws.Config.NoLogGets = true
	ws.Host("*").Rewrite("/fallback", "https://fallback.test/")
	policy, _ := service.PrepareHostPolicy(service.HostPolicy{})
	ws.ReplaceHostPolicies(map[string]*service.PreparedHostPolicy{"*.example.test": policy})
	response := httptest.NewRecorder()
	ws.Handler().ServeHTTP(response, httptest.NewRequest("GET", "http://a.example.test/fallback", nil))
	if response.Code != 404 {
		t.Fatalf("managed host fell back: %d", response.Code)
	}
}
func TestProxyAuthenticationDoesNotDefaultToAllow(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("upstream")) }))
	defer upstream.Close()
	ws := service.NewWebServer()
	ws.Config.NoLogGets = true
	ws.Host("*").Proxy(1, "/private", upstream.URL+"/")
	response := httptest.NewRecorder()
	ws.Handler().ServeHTTP(response, httptest.NewRequest("GET", "http://example.test/private", strings.NewReader("")))
	if response.Code != 403 {
		t.Fatalf("unauthenticated proxy status %d", response.Code)
	}
}
func BenchmarkIngressDirectGate(b *testing.B) {
	ws := service.NewWebServer()
	ws.Config.NoLogGets = true
	ws.SetClientIPPolicy(service.ClientIPPolicy{Mode: "direct"})
	ws.SetRequestGate(func(r *service.Request, w *service.Response) bool { w.WriteHeader(200); return true })
	b.ReportAllocs()
	for b.Loop() {
		ws.Handler().ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "http://example.test/", nil))
	}
}

func TestProxyPreservesHeadersAndResolvedIP(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Real-IP") != "192.0.2.1" || r.Header.Get("Cookie") != "account=abc" || r.Header.Get("X-Api-Key") != "test-value" || r.Header.Get("X-Hop") != "" {
			t.Errorf("unexpected relay headers: %v", r.Header)
		}
		w.WriteHeader(200)
	}))
	defer upstream.Close()
	ws := service.NewWebServer()
	ws.Config.NoLogGets = true
	ws.SetClientIPPolicy(service.ClientIPPolicy{Mode: "direct"})
	ws.Host("*").Proxy(0, "/", upstream.URL+"/")
	request := httptest.NewRequest("GET", "/", nil)
	request.Host = "example.test"
	request.Header.Set("X-Real-IP", "203.0.113.1")
	request.Header.Set("Cookie", "account=abc")
	request.Header.Set("X-Api-Key", "test-value")
	request.Header.Set("Connection", "X-Hop")
	request.Header.Set("X-Hop", "remove")
	response := httptest.NewRecorder()
	ws.Handler().ServeHTTP(response, request)
	if response.Code != 200 {
		t.Fatalf("code %d", response.Code)
	}
}
