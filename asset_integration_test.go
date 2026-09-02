package service

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
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

func TestAutoAssetReversionDynamicHTMLWriteString(t *testing.T) {
	ws := NewWebServer()
	ws.Config.AutoReversion = true
	ws.EnableAssetReversion(func(_, _, asset string) (time.Time, bool) {
		if asset == "/ui/app.js" {
			return time.Unix(100, 0), true
		}
		return time.Time{}, false
	})
	ws.Register("GET", "/dynamic-write", func(response *Response) {
		response.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = response.WriteString(`<html><script src="/ui/app.js"></script></html>`)
	})
	server := httptest.NewServer(&RouteHandler{ws: ws})
	defer server.Close()
	resp, err := http.Get(server.URL + "/dynamic-write")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), "app.js?v=2s") {
		t.Fatalf("dynamic WriteString HTML was not rewritten: %s", body)
	}
}

func TestAutoAssetReversionDetectsDynamicHTMLWriteString(t *testing.T) {
	ws := NewWebServer()
	ws.Config.AutoReversion = true
	ws.EnableAssetReversion(func(_, _, asset string) (time.Time, bool) {
		return time.Unix(100, 0), asset == "/ui/app.js"
	})
	ws.Register("GET", "/dynamic-detect", func(response *Response) {
		_, _ = response.WriteString(`<!doctype html><script src="/ui/app.js"></script>`)
	})
	server := httptest.NewServer(&RouteHandler{ws: ws})
	defer server.Close()
	resp, err := http.Get(server.URL + "/dynamic-detect")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if !strings.HasPrefix(resp.Header.Get("Content-Type"), "text/html") || !strings.Contains(string(body), "app.js?v=2s") {
		t.Fatalf("detected dynamic HTML was not rewritten: type=%q body=%s", resp.Header.Get("Content-Type"), body)
	}
}

func TestAutoAssetReversionFlushKeepsDynamicHTMLStreaming(t *testing.T) {
	ws := NewWebServer()
	ws.Config.AutoReversion = true
	ws.EnableAssetReversion(func(_, _, _ string) (time.Time, bool) {
		return time.Unix(100, 0), true
	})
	ws.Register("GET", "/dynamic-stream", func(response *Response) {
		response.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = response.WriteString(`<script src="/ui/app.js"></script>`)
		response.Flush()
	})
	server := httptest.NewServer(&RouteHandler{ws: ws})
	defer server.Close()
	resp, err := http.Get(server.URL + "/dynamic-stream")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if strings.Contains(string(body), "?v=") {
		t.Fatalf("flushed HTML must bypass rewriting: %s", body)
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

func TestAutoAssetReversionExpandsStaticPatterns(t *testing.T) {
	ws := NewWebServer()
	ws.Config.AutoReversion = true
	dir := t.TempDir()
	files := map[string]string{
		"index.html":            `<script srcs="/ui/*.js, /ui/**/*.js"></script>`,
		"ui/State.js":           `var state = 1;`,
		"ui/components/List.js": `var list = 1;`,
	}
	baseTime := time.Unix(1700000000, 0)
	for name, content := range files {
		filePath := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(filePath), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filePath, []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(filePath, baseTime, baseTime); err != nil {
			t.Fatal(err)
		}
	}
	ws.ReplaceStaticFSExclusive("*", map[string]http.FileSystem{"/": http.Dir(dir)})
	server := httptest.NewServer(&RouteHandler{ws: ws})
	defer server.Close()

	resp, err := http.Get(server.URL + "/index.html")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	text := string(body)
	if strings.Contains(text, "srcs=") || !strings.Contains(text, `/ui/State.js?v=`) || !strings.Contains(text, `/ui/components/List.js?v=`) {
		t.Fatalf("patterns were not expanded and versioned: %s", text)
	}
	if strings.Index(text, "/ui/State.js") > strings.Index(text, "/ui/components/List.js") {
		t.Fatalf("pattern order was not preserved: %s", text)
	}
}

func TestAutoAssetReversionInvalidatesPatternWhenMatchedFileIsRemoved(t *testing.T) {
	ws := NewWebServer()
	ws.Config.AutoReversion = true
	dir := t.TempDir()
	baseTime := time.Unix(1700000000, 0)
	for name, content := range map[string]string{
		"index.html": `<script srcs="/ui/*.js"></script>`,
		"ui/A.js":    `var a = 1;`,
		"ui/B.js":    `var b = 1;`,
	} {
		filePath := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(filePath), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filePath, []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(filePath, baseTime, baseTime); err != nil {
			t.Fatal(err)
		}
	}
	ws.ReplaceStaticFSExclusive("*", map[string]http.FileSystem{"/": http.Dir(dir)})
	server := httptest.NewServer(&RouteHandler{ws: ws})
	defer server.Close()

	first, err := http.Get(server.URL + "/index.html")
	if err != nil {
		t.Fatal(err)
	}
	firstBody, _ := io.ReadAll(first.Body)
	first.Body.Close()
	lastModified := first.Header.Get("Last-Modified")
	if !strings.Contains(string(firstBody), "/ui/B.js?v=") {
		t.Fatalf("initial pattern did not include B.js: %s", firstBody)
	}
	if err := os.Remove(filepath.Join(dir, "ui", "B.js")); err != nil {
		t.Fatal(err)
	}
	ws.assetGraph.scan()

	request, _ := http.NewRequest(http.MethodGet, server.URL+"/index.html", nil)
	request.Header.Set("If-Modified-Since", lastModified)
	changed, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer changed.Body.Close()
	changedBody, _ := io.ReadAll(changed.Body)
	if changed.StatusCode != http.StatusOK || strings.Contains(string(changedBody), "/ui/B.js") {
		t.Fatalf("removed match did not invalidate HTML: status=%d body=%s", changed.StatusCode, changedBody)
	}
}

func TestAutoAssetReversionInvalidatesHTMLWhenDependencyChanges(t *testing.T) {
	ws := NewWebServer()
	ws.Config.AutoReversion = true
	dir := t.TempDir()
	indexPath := filepath.Join(dir, "index.html")
	appPath := filepath.Join(dir, "app.js")
	baseTime := time.Unix(1700000000, 0)
	if err := os.WriteFile(indexPath, []byte(`<script src="app.js"></script>`), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(appPath, []byte(`var app = 1;`), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(indexPath, baseTime, baseTime); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(appPath, baseTime, baseTime); err != nil {
		t.Fatal(err)
	}
	ws.ReplaceStaticFSExclusive("*", map[string]http.FileSystem{"/": http.Dir(dir)})
	server := httptest.NewServer(&RouteHandler{ws: ws})
	defer server.Close()

	first, err := http.Get(server.URL + "/index.html")
	if err != nil {
		t.Fatal(err)
	}
	first.Body.Close()
	lastModified := first.Header.Get("Last-Modified")
	if lastModified == "" {
		t.Fatal("missing Last-Modified")
	}

	if err := os.Chtimes(appPath, baseTime.Add(2*time.Second), baseTime.Add(2*time.Second)); err != nil {
		t.Fatal(err)
	}
	ws.assetGraph.scan()
	request, err := http.NewRequest(http.MethodGet, server.URL+"/index.html", nil)
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("If-Modified-Since", lastModified)
	changed, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	changed.Body.Close()
	if changed.StatusCode != http.StatusOK {
		t.Fatalf("dependency change status: got %d, want %d", changed.StatusCode, http.StatusOK)
	}
	if changed.Header.Get("Last-Modified") == lastModified {
		t.Fatalf("Last-Modified did not follow dependency: %s", lastModified)
	}

	unchangedRequest, err := http.NewRequest(http.MethodGet, server.URL+"/index.html", nil)
	if err != nil {
		t.Fatal(err)
	}
	unchangedRequest.Header.Set("If-Modified-Since", changed.Header.Get("Last-Modified"))
	unchanged, err := http.DefaultClient.Do(unchangedRequest)
	if err != nil {
		t.Fatal(err)
	}
	unchanged.Body.Close()
	if unchanged.StatusCode != http.StatusNotModified {
		t.Fatalf("unchanged dependency status: got %d, want %d", unchanged.StatusCode, http.StatusNotModified)
	}
}

func TestAutoAssetReversionInvalidatesPhysicalHTMLWhenDependencyChanges(t *testing.T) {
	ws := NewWebServer()
	ws.Config.AutoReversion = true
	dir := t.TempDir()
	indexPath := filepath.Join(dir, "index.html")
	appPath := filepath.Join(dir, "app.js")
	baseTime := time.Unix(1700000100, 0)
	if err := os.WriteFile(indexPath, []byte(`<script src="app.js"></script>`), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(appPath, []byte(`var app = 1;`), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(indexPath, baseTime, baseTime); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(appPath, baseTime, baseTime); err != nil {
		t.Fatal(err)
	}
	ws.Static("/", dir)
	server := httptest.NewServer(&RouteHandler{ws: ws})
	defer server.Close()

	first, err := http.Get(server.URL + "/index.html")
	if err != nil {
		t.Fatal(err)
	}
	first.Body.Close()
	lastModified := first.Header.Get("Last-Modified")
	if lastModified == "" {
		t.Fatal("missing Last-Modified")
	}

	if err := os.Chtimes(appPath, baseTime.Add(2*time.Second), baseTime.Add(2*time.Second)); err != nil {
		t.Fatal(err)
	}
	ws.assetGraph.scan()
	request, err := http.NewRequest(http.MethodGet, server.URL+"/index.html", nil)
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("If-Modified-Since", lastModified)
	changed, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	changed.Body.Close()
	if changed.StatusCode != http.StatusOK {
		t.Fatalf("dependency change status: got %d, want %d", changed.StatusCode, http.StatusOK)
	}

	unchangedRequest, err := http.NewRequest(http.MethodGet, server.URL+"/index.html", nil)
	if err != nil {
		t.Fatal(err)
	}
	unchangedRequest.Header.Set("If-Modified-Since", changed.Header.Get("Last-Modified"))
	unchanged, err := http.DefaultClient.Do(unchangedRequest)
	if err != nil {
		t.Fatal(err)
	}
	unchanged.Body.Close()
	if unchanged.StatusCode != http.StatusNotModified {
		t.Fatalf("unchanged dependency status: got %d, want %d", unchanged.StatusCode, http.StatusNotModified)
	}
}

func TestAutoAssetReversionInvalidatesDirectoryIndexForNestedDependency(t *testing.T) {
	ws := NewWebServer()
	ws.Config.AutoReversion = true
	dir := t.TempDir()
	baseTime := time.Unix(1700000200, 0)
	files := map[string]string{
		"index.html": `<link href="app.css">`,
		"app.css":    `.app{background:url("theme.css")}`,
		"theme.css":  `.app{color:red}`,
	}
	for name, content := range files {
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(path, baseTime, baseTime); err != nil {
			t.Fatal(err)
		}
	}
	ws.Static("/", dir)
	server := httptest.NewServer(&RouteHandler{ws: ws})
	defer server.Close()

	first, err := http.Get(server.URL + "/")
	if err != nil {
		t.Fatal(err)
	}
	firstBody, err := io.ReadAll(first.Body)
	first.Body.Close()
	if err != nil {
		t.Fatal(err)
	}
	lastModified := first.Header.Get("Last-Modified")
	if lastModified == "" {
		t.Fatal("missing Last-Modified")
	}
	if want := "app.css?v=" + strconv.FormatInt(baseTime.Unix(), 36); !strings.Contains(string(firstBody), want) {
		t.Fatalf("directory index was not rewritten with nested graph: %s", firstBody)
	}

	changedTime := baseTime.Add(2 * time.Second)
	if err := os.Chtimes(filepath.Join(dir, "theme.css"), changedTime, changedTime); err != nil {
		t.Fatal(err)
	}
	ws.assetGraph.scan()
	request, err := http.NewRequest(http.MethodGet, server.URL+"/", nil)
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("If-Modified-Since", lastModified)
	changed, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	changedBody, err := io.ReadAll(changed.Body)
	changed.Body.Close()
	if err != nil {
		t.Fatal(err)
	}
	if changed.StatusCode != http.StatusOK {
		t.Fatalf("nested dependency change status: got %d, want %d", changed.StatusCode, http.StatusOK)
	}
	if want := "app.css?v=" + strconv.FormatInt(changedTime.Unix(), 36); !strings.Contains(string(changedBody), want) {
		t.Fatalf("parent dependency version did not follow leaf: %s", changedBody)
	}
}
