package service_test

import (
	"github.com/api-go/discover"
	gohttp "github.com/api-go/http"
	"github.com/api-go/service"
	"context"
	"golang.org/x/net/http2"
	"golang.org/x/net/http2/h2c"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

func TestExternalExplicitPolicyAndMakeURL(t *testing.T) {
	for _, trusted := range []bool{false, true} {
		ws := service.NewWebServer()
		ws.Config.NoLogGets = true
		if !trusted {
			if err := ws.SetExternalAddressPolicy(gohttp.ExternalAddressPolicy{Mode: "direct"}); err != nil {
				t.Fatal(err)
			}
		}
		if trusted {
			if err := ws.SetExternalAddressPolicy(gohttp.ExternalAddressPolicy{TrustedProxies: []string{"127.0.0.1"}}); err != nil {
				t.Fatal(err)
			}
		}
		ws.SetRequestGate(func(r *service.Request, w *service.Response) bool { w.WriteString(r.MakeUrl("/result")); return true })
		for _, scheme := range []string{"http", "https"} {
			r := httptest.NewRequest("GET", scheme+"://direct.example/", nil)
			r.RemoteAddr = "127.0.0.1:10"
			r.Header.Set("X-Host", "public.example")
			r.Header.Set("X-Scheme", "https")
			r.Header.Set("X-Request-ID", "legacy")
			w := httptest.NewRecorder()
			ws.Handler().ServeHTTP(w, r)
			want := scheme + "://direct.example/result"
			if trusted {
				want = "https://public.example/result"
			}
			if w.Body.String() != want {
				t.Fatal(w.Body.String(), want)
			}
		}
		r := httptest.NewRequest("GET", "http://direct.example/", nil)
		r.RemoteAddr = "127.0.0.1:10"
		r.Header.Set("X-Scheme", "https")
		w := httptest.NewRecorder()
		ws.Handler().ServeHTTP(w, r)
		if trusted && w.Code != 400 {
			t.Fatal(w.Code)
		}
	}
}
func TestExternalProxyMultiHopAndConcurrentIsolation(t *testing.T) {
	final := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Origin") != "https://"+r.Header.Get("X-Host") || r.Header.Get("X-Scheme") != "https" || r.Header.Get("X-Forwarded-Proto") != "https" || r.Header.Get("Forwarded") != "" {
			t.Error(r.Header)
		}
		w.Write([]byte(r.Header.Get("X-Host")))
	}))
	defer final.Close()
	makeProxy := func(target string) *service.WebServer {
		ws := service.NewWebServer()
		ws.Config.NoLogGets = true
		ws.Host("*").Proxy(0, "/*", target+"/*")
		ws.ReplaceForceHTTPS("*", true)
		return ws
	}
	second := httptest.NewServer(makeProxy(final.URL).Handler())
	defer second.Close()
	first := makeProxy(second.URL)
	var wg sync.WaitGroup
	for _, host := range []string{"one.example", "two.example", "three.example"} {
		wg.Go(func() {
			r := httptest.NewRequest("GET", "https://"+host+"/path", nil)
			r.RemoteAddr = "127.0.0.1:10"
			r.Header.Set("X-Host", "client-supplied.example")
			r.Header.Set("X-Scheme", "http")
			r.Header.Set("Origin", "https://"+host)
			r.Header.Set("Forwarded", "unparsed")
			w := httptest.NewRecorder()
			first.Handler().ServeHTTP(w, r)
			if w.Code != 200 || w.Body.String() != host {
				t.Errorf("%d %s", w.Code, w.Body.String())
			}
		})
	}
	wg.Wait()
}

func TestExternalDiscoverH2CProxy(t *testing.T) {
	upstream := httptest.NewServer(h2c.NewHandler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.ProtoMajor != 2 || r.Header.Get("X-Host") != "browser.example" || r.Header.Get("X-Scheme") != "https" || r.Header.Get("Origin") != "https://browser.example" || r.Host != "browser.example" {
			t.Error(r.Proto, r.Host, r.Header)
		}
		w.WriteHeader(204)
	}), &http2.Server{}))
	defer upstream.Close()
	ws := service.NewWebServer()
	ws.Config.Listen = "127.0.0.1:0,h2c"
	ws.Config.Register = "redis://127.0.0.1:6379/15"
	ws.Config.App = "external-proxy-ingress"
	ws.Config.NoLogGets = true
	if err := ws.Start(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
	defer ws.Stop(context.Background())
	app := "external-discovery-proxy"
	discover.AddExternalApp(app, "2")
	addr := strings.TrimPrefix(upstream.URL, "http://")
	registerDiscoverFixture(t, ws.Config.Register, app, addr)
	ws.Host("*").Proxy(0, "/entry", app+"/target")
	r := httptest.NewRequest("GET", "http://internal/entry", nil)
	r.Header.Set("X-Request-ID", "outer-gateway")
	r.RemoteAddr = "127.0.0.1:1234"
	r.Header.Set("X-Forwarded-Host", "browser.example")
	r.Header.Set("X-Forwarded-Proto", "https")
	r.Header.Set("Origin", "https://browser.example")
	w := httptest.NewRecorder()
	ws.Handler().ServeHTTP(w, r)
	if w.Code != 204 {
		t.Fatal(w.Code, w.Body.String())
	}
}

func TestDefaultExternalAddressMakeURL(t *testing.T) {
	for _, id := range []string{"", "from-edge"} {
		ws := service.NewWebServer()
		ws.Config.NoLogGets = true
		ws.SetRequestGate(func(r *service.Request, w *service.Response) bool {
			if r.TLS != nil {
				t.Fatal("internal connection TLS changed")
			}
			w.WriteString(r.MakeUrl("/result"))
			return true
		})
		r := httptest.NewRequest("GET", "http://internal/", nil)
		r.Header.Set("X-Host", "browser.example")
		r.Header.Set("X-Scheme", "https")
		if id != "" {
			r.Header.Set("X-Request-ID", id)
		}
		w := httptest.NewRecorder()
		ws.Handler().ServeHTTP(w, r)
		want := "http://internal/result"
		if id != "" {
			want = "https://browser.example/result"
		}
		if w.Body.String() != want {
			t.Fatal(w.Body.String(), want)
		}
	}
}
