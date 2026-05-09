package service

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestRewrite(t *testing.T) {
	// 注册重写规则
	Host("*").Rewrite("/old", "/new")
	Host("*").Rewrite("/regex/(.*)", "/target/$1")
	
	// 注册目标服务
	Host("*").ANY("/new", func() string { return "new content" }).Memo("new")
	Host("*").ANY("/target/123", func() string { return "target content" }).Memo("target")

	rh := &RouteHandler{}

	// 测试精确匹配重写
	req1 := httptest.NewRequest("GET", "/old", nil)
	w1 := httptest.NewRecorder()
	rh.ServeHTTP(w1, req1)
	if w1.Body.String() != "new content" {
		t.Errorf("Expected 'new content', got '%s'", w1.Body.String())
	}

	// 测试正则匹配重写
	req2 := httptest.NewRequest("GET", "/regex/123", nil)
	w2 := httptest.NewRecorder()
	rh.ServeHTTP(w2, req2)
	if w2.Body.String() != "target content" {
		t.Errorf("Expected 'target content', got '%s'", w2.Body.String())
	}
}

func TestProxyDirect(t *testing.T) {
	// 启动后端服务器
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Backend", "ok")
		w.Write([]byte("backend content"))
	}))
	defer backend.Close()

	// 注册代理规则
	Host("*").Proxy(0, "/proxy", backend.URL, "/hello")

	rh := &RouteHandler{}
	req := httptest.NewRequest("GET", "/proxy", nil)
	w := httptest.NewRecorder()
	rh.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected 200, got %d", w.Code)
	}
	if w.Header().Get("X-Backend") != "ok" {
		t.Error("Header X-Backend mismatch")
	}
	if w.Body.String() != "backend content" {
		t.Errorf("Expected 'backend content', got '%s'", w.Body.String())
	}
}
