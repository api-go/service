package service_test

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/api-go/cast"
	"github.com/api-go/file"
	"github.com/api-go/log"
	"github.com/api-go/service"
)

func TestCompactProxyLogsKeepStaticAndFailures(t *testing.T) {
	old := log.DefaultConfig()
	defer log.SetDefaultConfig(old)
	path := filepath.Join(t.TempDir(), "gateway.log")
	log.SetDefaultConfig(log.Config{File: path, NoCompress: true})
	defer log.WriterService.Stop(nil)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/failure":
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(503)
			w.Write([]byte(`{"password":"hidden-body-secret","error":"unavailable"}`))
		case "/truncated":
			w.Header().Set("Content-Length", "100")
			w.Write([]byte("short"))
		default:
			w.Write([]byte("okay"))
		}
	}))
	defer upstream.Close()
	ws := service.NewWebServer()
	ws.Config.CompactProxyLog = true
	ws.Host("*").Proxy(0, "/proxy/*", upstream.URL+"/*")
	static := t.TempDir()
	if err := file.Write(filepath.Join(static, "asset.txt"), "asset"); err != nil {
		t.Fatal(err)
	}
	ws.Host("*").Static("/static", static)
	for _, path := range []string{"/proxy/okay", "/proxy/failure", "/proxy/truncated", "/static/asset.txt", "/missing"} {
		req := httptest.NewRequest("GET", "http://gateway.test"+path, nil)
		req.Header.Set("X-Request-ID", "trace-proxy-log")
		req.Header.Set("Authorization", "hidden-header-secret")
		ws.Handler().ServeHTTP(httptest.NewRecorder(), req)
	}
	data, err := file.Read(path)
	if err != nil {
		t.Fatal(err)
	}
	proxyCount, requestCount := 0, 0
	streamFailure := false
	for _, line := range strings.Split(strings.TrimSpace(data), "\n") {
		var fields []any
		if err = cast.UnmarshalJSON([]byte(line), &fields); err != nil {
			t.Fatal(err)
		}
		switch fields[1] {
		case "proxy":
			proxyCount++
			if strings.Contains(line, "truncated") && strings.Contains(line, "stream") {
				streamFailure = true
			}
		case "request":
			requestCount++
		}
	}
	if proxyCount != 3 || requestCount != 2 || !streamFailure {
		t.Fatalf("proxy=%d request=%d stream=%v\n%s", proxyCount, requestCount, streamFailure, data)
	}
	if strings.Contains(data, "hidden-body-secret") || strings.Contains(data, "hidden-header-secret") {
		t.Fatal("sensitive diagnostics leaked")
	}
}
