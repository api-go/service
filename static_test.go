package service

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestStaticService(t *testing.T) {
	// 创建临时测试目录和文件
	tempDir, _ := os.MkdirTemp("", "static_test")
	defer os.RemoveAll(tempDir)
	
	testFile := filepath.Join(tempDir, "index.html")
	os.WriteFile(testFile, []byte("<h1>Static Page</h1>"), 0644)

	// 注册静态目录
	Static("/ui", tempDir)

	rh := &RouteHandler{}
	
	// 测试成功访问
	req := httptest.NewRequest("GET", "/ui/index.html", nil)
	w := httptest.NewRecorder()
	rh.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected 200, got %d", w.Code)
	}
	if body := w.Body.String(); body != "<h1>Static Page</h1>" {
		t.Errorf("Content mismatch: %s", body)
	}

	// 测试 404
	req404 := httptest.NewRequest("GET", "/ui/notfound.html", nil)
	w404 := httptest.NewRecorder()
	rh.ServeHTTP(w404, req404)
	if w404.Code != http.StatusNotFound {
		t.Errorf("Expected 404 for missing file, got %d", w404.Code)
	}
}

func TestHostStaticService(t *testing.T) {
	tempDir, _ := os.MkdirTemp("", "host_static_test")
	defer os.RemoveAll(tempDir)

	testFile := filepath.Join(tempDir, "index.html")
	_ = os.WriteFile(testFile, []byte("<h1>Host Static Page</h1>"), 0644)

	// 注册域名特定静态文件服务
	Host("example.com").Static("/host-ui", tempDir)

	rh := &RouteHandler{}

	// 1. 匹配域名访问
	req1 := httptest.NewRequest("GET", "/host-ui/index.html", nil)
	req1.Host = "example.com"
	w1 := httptest.NewRecorder()
	rh.ServeHTTP(w1, req1)

	if w1.Code != http.StatusOK {
		t.Errorf("Expected 200, got %d", w1.Code)
	}
	if body := w1.Body.String(); body != "<h1>Host Static Page</h1>" {
		t.Errorf("Content mismatch: %s", body)
	}

	// 2. 不匹配域名访问 (应该 404)
	req2 := httptest.NewRequest("GET", "/host-ui/index.html", nil)
	req2.Host = "other.com"
	w2 := httptest.NewRecorder()
	rh.ServeHTTP(w2, req2)

	if w2.Code != http.StatusNotFound {
		t.Errorf("Expected 404 for mismatched host, got %d", w2.Code)
	}
}

