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

	rh := &routeHandler{}
	
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
