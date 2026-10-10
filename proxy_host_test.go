package service_test

import (
	"apigo.cc/go/service"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestProxyPreservesOriginalHost(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Host != "llm-dev.deeler.cn" {
			t.Errorf("upstream Host = %q", r.Host)
		}
		w.Write([]byte(r.Host))
	}))
	defer upstream.Close()
	ws := service.NewWebServer()
	ws.Host("*").Proxy(0, "/", upstream.URL+"/")
	req := httptest.NewRequest("GET", "http://llm-dev.deeler.cn/", nil)
	req.Header.Set("Host", "spoofed.invalid")
	response := httptest.NewRecorder()
	ws.Handler().ServeHTTP(response, req)
	if response.Code != 200 || response.Body.String() != "llm-dev.deeler.cn" {
		t.Fatalf("status %d body %q", response.Code, response.Body.String())
	}
}

func TestProxyPreservesStatusAndCookies(t *testing.T) {
	for _, status := range []int{200, 204, 302, 303, 401, 403, 503} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Add("Set-Cookie", "first=one; Path=/; Secure; HttpOnly")
				w.Header().Add("Set-Cookie", "second=two; Path=/; Secure; HttpOnly")
				w.Header().Set("Location", "https://account-dev.deeler.cn/oidc/authorize")
				w.WriteHeader(status)
				if status != http.StatusNoContent {
					_, _ = w.Write([]byte("upstream body"))
				}
			}))
			defer upstream.Close()
			ws := service.NewWebServer()
			ws.Host("*").Proxy(0, "/", upstream.URL+"/")
			response := httptest.NewRecorder()
			ws.Handler().ServeHTTP(response, httptest.NewRequest("GET", "http://llm-dev.deeler.cn/", nil))
			wantBody := "upstream body"
			if status == http.StatusNoContent {
				wantBody = ""
			}
			if response.Code != status || response.Body.String() != wantBody {
				t.Fatalf("status %d body %q", response.Code, response.Body.String())
			}
			if got := response.Header().Values("Set-Cookie"); len(got) != 2 {
				t.Fatalf("cookies: %v", got)
			}
			if response.Header().Get("Location") != "https://account-dev.deeler.cn/oidc/authorize" {
				t.Fatal("Location was lost or upstream redirect was followed")
			}
		})
	}
}
