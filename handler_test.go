package service

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestServeHTTP(t *testing.T) {
	// 注册服务
	handler := func(in struct{ Name string }) string {
		return "Hello " + in.Name
	}
	Register(0, "/hello", handler, "say hello")

	rh := &routeHandler{}
	
	// 模拟请求
	req := httptest.NewRequest("POST", "/hello", strings.NewReader(`{"name":"Star"}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	rh.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", w.Code)
	}

	body := w.Body.String()
	if body != "Hello Star" {
		t.Errorf("Expected 'Hello Star', got '%s'", body)
	}
}

func TestServeHTTP_404(t *testing.T) {
	rh := &routeHandler{}
	req := httptest.NewRequest("GET", "/notfound", nil)
	w := httptest.NewRecorder()

	rh.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("Expected status 404, got %d", w.Code)
	}
}

func TestServeHTTP_VerifyFailed(t *testing.T) {
	type ValidIn struct {
		Age int `verify:"between:18-100"`
	}
	handler := func(in ValidIn) string {
		return "ok"
	}
	Register(0, "/verify", handler, "test verify")

	rh := &routeHandler{}
	req := httptest.NewRequest("POST", "/verify", strings.NewReader(`{"age":10}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	rh.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("Expected status 400, got %d", w.Code)
	}
}
