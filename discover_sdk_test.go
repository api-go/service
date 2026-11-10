package service_test

import (
	"github.com/api-go/discover"
	"github.com/api-go/service"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestDiscoverHealthMountedAutomatically(t *testing.T) {
	ws := service.NewWebServer()
	defer discover.SetHealthHandler(nil)
	response := httptest.NewRecorder()
	ws.Handler().ServeHTTP(response, httptest.NewRequest("GET", discover.HealthPath, nil))
	if response.Code != 200 || response.Body.String() != "OK" {
		t.Fatal(response.Code, response.Body.String())
	}
	discover.SetHealthHandler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(503); w.Write([]byte("draining")) }))
	response = httptest.NewRecorder()
	ws.Handler().ServeHTTP(response, httptest.NewRequest("GET", discover.HealthPath, nil))
	if response.Code != 503 || response.Body.String() != "draining" {
		t.Fatal(response.Code, response.Body.String())
	}
}

func TestDiscoverIngressMetadataUsesResolvedIP(t *testing.T) {
	ws := service.NewWebServer()
	ws.Config.NoLogGets = true
	if err := ws.SetClientIPPolicy(service.ClientIPPolicy{Mode: "direct"}); err != nil {
		t.Fatal(err)
	}
	ws.SetRequestGate(func(request *service.Request, response *service.Response) bool {
		headers := request.Request.Header
		for name, want := range map[string]string{"X-Real-IP": "192.0.2.8", "X-Host": "entry.example", "X-Scheme": "http", "X-User-Agent": "browser"} {
			if headers.Get(name) != want {
				t.Errorf("%s=%q want %q", name, headers.Get(name), want)
			}
		}
		if headers.Get("X-Request-ID") == "" || len(headers.Values("X-App-Labels")) != 2 {
			t.Error(headers)
		}
		response.WriteString("OK")
		return true
	})
	incoming := httptest.NewRequest("GET", "http://entry.example/", nil)
	incoming.RemoteAddr = "192.0.2.8:3000"
	incoming.Header.Set("X-Real-IP", "203.0.113.1")
	incoming.Header.Set("User-Agent", "browser")
	incoming.Header["X-App-Labels"] = []string{"a", "b"}
	response := httptest.NewRecorder()
	ws.Handler().ServeHTTP(response, incoming)
	if response.Body.String() != "OK" {
		t.Fatal(response.Body.String())
	}
}
