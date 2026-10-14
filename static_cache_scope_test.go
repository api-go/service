package service_test

import (
	"apigo.cc/go/service"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
	"time"
)

func TestAutoMinifyIsolatesHostsAndReplacedMounts(t *testing.T) {
	ws := service.NewWebServer()
	ws.Config.AutoMinify = true
	ws.Config.AutoReversionCachePath = t.TempDir()
	mount := func(host, title string) {
		fs := fstest.MapFS{"index.html": {Data: []byte("<html><title>" + title + "</title><body>" + title + "</body></html>"), ModTime: time.Unix(100, 0)}}
		ws.ReplaceStaticFSExclusive(host, map[string]http.FileSystem{"/": http.FS(fs)})
	}
	mount("one.localhost", "First site")
	mount("two.localhost", "Second site")
	server := httptest.NewServer(ws.Handler())
	defer server.Close()
	read := func(host, expected string) {
		t.Helper()
		req, err := http.NewRequest(http.MethodGet, server.URL+"/index.html", nil)
		if err != nil {
			t.Fatal(err)
		}
		req.Host = host
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		data, err := io.ReadAll(resp.Body)
		if err != nil || resp.StatusCode != http.StatusOK || !strings.Contains(string(data), expected) {
			t.Fatalf("host %s got status %d body %q error %v", host, resp.StatusCode, data, err)
		}
	}
	read("one.localhost", "First site")
	read("two.localhost", "Second site")
	read("one.localhost", "First site")
	mount("one.localhost", "Replaced site")
	read("one.localhost", "Replaced site")
	read("two.localhost", "Second site")
}
