package service

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"net/url"
)

func TestStaticRobustness(t *testing.T) {
	tempDir, _ := os.MkdirTemp("", "robustness_test")
	defer os.RemoveAll(tempDir)

	// 创建复杂的目录结构
	subDir := filepath.Join(tempDir, "The NPC Awakens", "The Loop", "scene", "M")
	_ = os.MkdirAll(subDir, 0755)

	fileName := "画面逐渐亮起，铁匠铺的铁锤在无人操作的情况下，机械地敲击着烧红的铁块。_large.webp"
	testFile := filepath.Join(subDir, fileName)
	content := []byte("fake webp content")
	_ = os.WriteFile(testFile, content, 0644)

	// 注册静态目录
	ws := NewWebServer()
	ws.Config.App = "test"
	ws.Static("/img/", tempDir)

	rh := &RouteHandler{ws: ws}

	// 构造编码后的请求路径
	encodedPath := "/img/" + url.PathEscape("The NPC Awakens/The Loop/scene/M/画面逐渐亮起，铁匠铺的铁锤在无人操作的情况下，机械地敲击着烧红的铁块。_large.webp")
	
	// 测试静态文件访问
	req := httptest.NewRequest("GET", encodedPath+"?v=1780317467305", nil)
	w := httptest.NewRecorder()
	rh.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected 200 for complex static file, got %d. Path: %s", w.Code, encodedPath)
	} else if string(w.Body.Bytes()) != string(content) {
		t.Errorf("Content mismatch for complex static file")
	}
}

func TestDynamicRobustness(t *testing.T) {
	ws := NewWebServer()
	ws.Config.App = "test"
	
	pathPattern := "/api/scene/{name}"
	ws.Host("*").GET(pathPattern, func(in struct{ Name string }) string {
		return "Hello " + in.Name
	})

	rh := &RouteHandler{ws: ws}

	complexName := "画面逐渐亮起，铁匠铺的铁锤"
	encodedPath := "/api/scene/" + url.PathEscape(complexName)

	req := httptest.NewRequest("GET", encodedPath, nil)
	w := httptest.NewRecorder()
	rh.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected 200 for complex dynamic path, got %d", w.Code)
	}
	expectedBody := "Hello " + complexName
	if w.Body.String() != expectedBody {
		t.Errorf("Got body: %s, expected: %s", w.Body.String(), expectedBody)
	}
}

func TestHostMatching(t *testing.T) {
	ws := NewWebServer()
	ws.Config.App = "test"

	// 1. 注册只带端口的 Host
	ws.Host(":8080").GET("/port", func() string { return "port" })
	// 2. 注册只带域名的 Host
	ws.Host("localhost").GET("/host", func() string { return "host" })
	// 3. 注册完整 Host
	ws.Host("example.com:9000").GET("/full", func() string { return "full" })

	rh := &RouteHandler{ws: ws}

	tests := []struct {
		requestHost string
		path        string
		expected    string
		code        int
	}{
		{"localhost:8080", "/port", "port", http.StatusOK},
		{"otherhost:8080", "/port", "port", http.StatusOK},
		{"localhost:9999", "/host", "host", http.StatusOK},
		{"example.com:9000", "/full", "full", http.StatusOK},
		{"example.com:8080", "/port", "port", http.StatusOK},
		{"localhost:8080", "/host", "host", http.StatusOK},
	}

	for _, tt := range tests {
		req := httptest.NewRequest("GET", tt.path, nil)
		req.Host = tt.requestHost
		w := httptest.NewRecorder()
		rh.ServeHTTP(w, req)

		if w.Code != tt.code {
			t.Errorf("Host [%s] Path [%s] expected code %d, got %d", tt.requestHost, tt.path, tt.code, w.Code)
		}
		if tt.code == http.StatusOK && w.Body.String() != tt.expected {
			t.Errorf("Host [%s] Path [%s] expected body %s, got %s", tt.requestHost, tt.path, tt.expected, w.Body.String())
		}
	}
}
