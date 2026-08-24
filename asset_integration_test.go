package service

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestAutoAssetReversionDynamicHTML(t *testing.T) {
	ws := NewWebServer()
	ws.Config.AutoReversion = true
	ws.Config.AutoMinify = true
	ws.EnableAssetReversion(func(_, _, asset string) (time.Time, bool) {
		if asset == "/ui/app.js" {
			return time.Unix(100, 0), true
		}
		return time.Time{}, false
	})
	ws.Register("GET", "/dynamic", func(response *Response) string {
		response.Header().Set("Content-Type", "text/html; charset=utf-8")
		return `<html><script src="/ui/app.js"></script></html>`
	})
	server := httptest.NewServer(&RouteHandler{ws: ws})
	defer server.Close()
	resp, err := http.Get(server.URL + "/dynamic")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body := make([]byte, 4096)
	n, _ := resp.Body.Read(body)
	if !strings.Contains(string(body[:n]), "app.js?v=2s") {
		t.Fatalf("dynamic HTML was not rewritten: %s", body[:n])
	}
}

func TestAutoAssetReversionStaticHTTPScenario(t *testing.T) {
	ws := NewWebServer()
	ws.Config.AutoReversion = true
	ws.Config.AutoMinify = true
	dir := t.TempDir()
	for name, content := range map[string]string{"index.html": `<script src="app.js"></script><link href="main.css">`, "app.js": "var app = 1;", "main.css": ".app { color: red; }", "video.bin": "0123456789"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
	}
	fs := http.Dir(dir)
	ws.ReplaceStaticFSExclusive("*", map[string]http.FileSystem{"/": fs})
	server := httptest.NewServer(&RouteHandler{ws: ws})
	defer server.Close()
	resp, err := http.Get(server.URL + "/index.html")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("index status: %d", resp.StatusCode)
	}
	partial, err := http.NewRequest(http.MethodGet, server.URL+"/video.bin", nil)
	if err != nil {
		t.Fatal(err)
	}
	partial.Header.Set("Range", "bytes=0-3")
	rangeResp, err := http.DefaultClient.Do(partial)
	if err != nil {
		t.Fatal(err)
	}
	defer rangeResp.Body.Close()
	if rangeResp.StatusCode != http.StatusPartialContent {
		t.Fatalf("range status: %d", rangeResp.StatusCode)
	}
}
